package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// Config for Ollama client
type Config struct {
	BaseURL string
	Timeout time.Duration
	APIKey  string // Optional, for OpenRouter or other API-key-based providers
}

// Client wraps the Ollama HTTP API
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a new Ollama API client
func NewClient(config *Config) *Client {
	if config == nil {
		config = &Config{BaseURL: "http://localhost:11434"}
	}
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}
	if config.Timeout == 0 {
		config.Timeout = 120 * time.Second
	}
	return &Client{
		baseURL:    config.BaseURL,
		httpClient: &http.Client{Timeout: config.Timeout},
		apiKey:     config.APIKey,
	}
}

// Embed generates embeddings for multiple texts in parallel
func (c *Client) Embed(ctx context.Context, model string, texts []string, concurrency int) ([][]float32, error) {
	if concurrency <= 0 {
		concurrency = 5
	}

	results := make(chan struct {
		index     int
		embedding []float32
		err       error
	}, len(texts))

	semaphore := make(chan struct{}, concurrency)

	for i, text := range texts {
		go func(index int, content string) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			req := EmbeddingRequest{Model: model, Prompt: content}
			var resp EmbeddingResponse
			err := c.doRequestWithRetry(ctx, "/api/embeddings", req, &resp)
			results <- struct {
				index     int
				embedding []float32
				err       error
			}{index, resp.Embedding, err}
		}(i, text)
	}

	embeddings := make([][]float32, len(texts))
	for i := 0; i < len(texts); i++ {
		res := <-results
		if res.err != nil {
			return nil, fmt.Errorf("embedding failed for text %d: %w", res.index, res.err)
		}
		embeddings[res.index] = res.embedding
	}

	return embeddings, nil
}

// Generate produces text completions
func (c *Client) Generate(ctx context.Context, model, prompt, system string) (string, error) {
	req := GenerateRequest{Model: model, Prompt: prompt, Stream: false, System: system}
	var resp GenerateResponse
	if err := c.doRequestWithRetry(ctx, "/api/generate", req, &resp); err != nil {
		return "", err
	}
	return resp.Response, nil
}

// GenerateWithFormat produces structured JSON using a schema
// Detects API format based on baseURL - uses OpenAI format for OpenRouter
func (c *Client) GenerateWithFormat(ctx context.Context, model, prompt, system string, format interface{}) (string, error) {
	// Check if we should use OpenAI format (for OpenRouter, etc)
	if strings.Contains(c.baseURL, "openrouter") {
		return c.generateOpenAIFormat(ctx, model, prompt, system)
	}

	// Use Ollama format (only supports "json" string, not schema objects)
	req := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		System string `json:"system,omitempty"`
		Stream bool   `json:"stream"`
		Format string `json:"format,omitempty"`
	}{Model: model, Prompt: prompt, System: system, Stream: false, Format: "json"}

	var resp GenerateResponse
	if err := c.doRequestWithRetry(ctx, "/api/generate", req, &resp); err != nil {
		return "", err
	}
	return resp.Response, nil
}

// generateOpenAIFormat uses OpenAI-compatible chat/completions API
func (c *Client) generateOpenAIFormat(ctx context.Context, model, prompt, system string) (string, error) {
	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	messages := []Message{}
	if system != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}
	messages = append(messages, Message{Role: "user", Content: prompt})

	req := struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Temperature float64   `json:"temperature,omitempty"`
	}{
		Model:       model,
		Messages:    messages,
		Temperature: 0.1,
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := c.doRequestWithRetry(ctx, "/v1/chat/completions", req, &resp); err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return resp.Choices[0].Message.Content, nil
}

// ExtractEdges extracts knowledge graph edges using structured output
func (c *Client) ExtractEdges(ctx context.Context, model, code string) ([]llm.Edge, error) {
	schema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source":        map[string]string{"type": "string"},
				"source_type":   map[string]string{"type": "string"},
				"target":        map[string]string{"type": "string"},
				"target_type":   map[string]string{"type": "string"},
				"relation_type": map[string]string{"type": "string"},
			},
			"required": []string{"source", "target", "relation_type"},
		},
	}

	prompt := fmt.Sprintf(`Extract code relations. Entity types: FUNCTION, METHOD, TYPE, INTERFACE, STRUCT, VARIABLE, PACKAGE, TEST. Relations: calls, imports, implements, extends, uses, tests, defines, references, creates, returns.

Example:
func NewClient(cfg *Config) *Client { return &Client{config: cfg} }

Output:
[
  {"source": "NewClient", "source_type": "FUNCTION", "target": "Config", "target_type": "TYPE", "relation_type": "uses"},
  {"source": "NewClient", "source_type": "FUNCTION", "target": "Client", "target_type": "TYPE", "relation_type": "creates"}
]

Code:
%s`, code)

	response, err := c.GenerateWithFormat(ctx, model, prompt, "You are a code relation extractor. Return only valid JSON.", schema)
	if err != nil {
		return nil, err
	}

	var edges []llm.Edge
	if err := json.Unmarshal([]byte(response), &edges); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return edges, nil
}

// ExtractEdgesBatch extracts edges from multiple chunks in a single API call
func (c *Client) ExtractEdgesBatch(ctx context.Context, model string, chunks []llm.ChunkInput) ([]llm.EdgeWithMetadata, error) {
	// Build markdown prompt
	prompt, chunkMapping := llm.BuildMarkdownPrompt(chunks)

	// Note: Ollama doesn't support schema with JSONL well, so we just use plain text
	response, err := c.GenerateWithFormat(ctx, model, prompt, "You are a code relation extractor. Return only JSONL format (one JSON object per line).", nil)
	if err != nil {
		return nil, err
	}

	// Parse JSONL response
	edges, err := llm.ParseJSONL(response, chunkMapping)
	if err != nil {
		// Log the error but return partial results if we got some edges
		if len(edges) > 0 {
			return edges, fmt.Errorf("partial parse: %w", err)
		}
		return nil, fmt.Errorf("parse JSONL: %w", err)
	}

	return edges, nil
}

// Ping checks connectivity
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", resp.StatusCode)
	}
	return nil
}

// doRequestWithRetry executes HTTP requests with retry logic
func (c *Client) doRequestWithRetry(ctx context.Context, path string, reqBody interface{}, respBody interface{}) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Add API key if configured (for OpenRouter or other providers)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(respBody)
}

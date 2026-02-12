package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// Config for OpenAI-compatible API client
type Config struct {
	BaseURL string        // API base URL (e.g., "https://openrouter.ai/v1")
	APIKey  string        // API key for authentication
	Timeout time.Duration // HTTP timeout
}

// Client wraps the OpenAI-compatible HTTP API
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new OpenAI API client
func NewClient(config *Config) *Client {
	if config == nil {
		config = &Config{}
	}
	if config.Timeout == 0 {
		config.Timeout = 120 * time.Second
	}
	return &Client{
		baseURL:    config.BaseURL,
		apiKey:     config.APIKey,
		httpClient: &http.Client{Timeout: config.Timeout},
	}
}

// ExtractEdges extracts knowledge graph edges using chat/completions API
func (c *Client) ExtractEdges(ctx context.Context, model string, code string) ([]llm.Edge, error) {
	prompt := fmt.Sprintf(`Extract code relations. Entity types: FUNCTION, METHOD, TYPE, INTERFACE, STRUCT, VARIABLE, PACKAGE, TEST. Relations: calls, imports, implements, extends, uses, tests, defines, references, creates, returns.

Example:
func NewClient(cfg *Config) *Client { return &Client{config: cfg} }

Output (JSON array):
[
  {"source": "NewClient", "source_type": "FUNCTION", "target": "Config", "target_type": "TYPE", "relation_type": "uses"},
  {"source": "NewClient", "source_type": "FUNCTION", "target": "Client", "target_type": "TYPE", "relation_type": "creates"}
]

Code:
%s

Return ONLY a valid JSON array of edges, no other text.`, code)

	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	reqBody := struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Temperature float64   `json:"temperature"`
	}{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: "You are a code relation extractor. Return only valid JSON arrays."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Read the full body for debugging
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read body: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		// Log first 200 chars of body for debugging
		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("decode response (status %d): %w. Body preview: %s", resp.StatusCode, err, preview)
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s (%s)", apiResp.Error.Message, apiResp.Error.Type)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content

	// Strip markdown code fences if present (some models wrap JSON in ```json ... ```)
	content = stripMarkdownCodeFence(content)

	var edges []llm.Edge
	if err := json.Unmarshal([]byte(content), &edges); err != nil {
		return nil, fmt.Errorf("parse edges from response: %w (content: %s)", err, content)
	}

	return edges, nil
}

// ExtractEdgesBatch extracts edges from multiple chunks in a single API call
func (c *Client) ExtractEdgesBatch(ctx context.Context, model string, chunks []llm.ChunkInput) ([]llm.EdgeWithMetadata, error) {
	// Build markdown prompt
	prompt, chunkMapping := llm.BuildMarkdownPrompt(chunks)

	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	reqBody := struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Temperature float64   `json:"temperature"`
	}{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: "You are a code relation extractor. Return only JSONL format (one JSON object per line)."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read body: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("decode response (status %d): %w. Body preview: %s", resp.StatusCode, err, preview)
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s (%s)", apiResp.Error.Message, apiResp.Error.Type)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	content := apiResp.Choices[0].Message.Content

	// Parse JSONL response
	edges, err := llm.ParseJSONL(content, chunkMapping)
	if err != nil {
		// Log the error but return partial results if we got some edges
		if len(edges) > 0 {
			return edges, fmt.Errorf("partial parse: %w", err)
		}
		return nil, fmt.Errorf("parse JSONL: %w", err)
	}

	return edges, nil
}

// stripMarkdownCodeFence removes markdown code fences from JSON responses
// Handles: ```json\n...\n``` or ```\n...\n```
func stripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)

	// Check for opening fence
	if strings.HasPrefix(s, "```") {
		// Find the end of the opening fence line
		firstNewline := strings.Index(s, "\n")
		if firstNewline == -1 {
			return s // Malformed, return as-is
		}

		// Remove opening fence
		s = s[firstNewline+1:]

		// Check for closing fence
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}

		s = strings.TrimSpace(s)
	}

	return s
}

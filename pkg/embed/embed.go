// Package embed turns text into vectors by asking a model, over the Ollama
// API or the OpenAI one that most other servers speak.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The protocols a client can speak
const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

// Config says which model to ask, and where
type Config struct {
	Provider string // ProviderOllama or ProviderOpenAI
	BaseURL  string // e.g. "http://localhost:11434", or "https://api.openai.com/v1"
	APIKey   string // Sent as a bearer token when set
	Model    string
	Timeout  time.Duration // For one request; two minutes when zero
}

// Client asks a model for embeddings
type Client struct {
	cfg  Config
	http *http.Client
}

// New creates a client. It does not contact the server.
func New(cfg Config) (*Client, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("no embedding model configured")
	}
	if cfg.Provider != ProviderOllama && cfg.Provider != ProviderOpenAI {
		return nil, fmt.Errorf("unknown embedding provider %q (known: %s, %s)", cfg.Provider, ProviderOllama, ProviderOpenAI)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("no base_url configured for embedding provider %s", cfg.Provider)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Minute
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Model returns the name of the model the client asks
func (c *Client) Model() string {
	return c.cfg.Model
}

// Embed returns a vector for each text, in order
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	request := map[string]any{"model": c.cfg.Model, "input": texts}

	var vectors [][]float32
	if c.cfg.Provider == ProviderOllama {
		var response struct {
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := c.post(ctx, "/api/embed", request, &response); err != nil {
			return nil, err
		}
		vectors = response.Embeddings
	} else {
		var response struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := c.post(ctx, "/embeddings", request, &response); err != nil {
			return nil, err
		}
		// The order of the answers is given, not promised
		vectors = make([][]float32, len(texts))
		for _, d := range response.Data {
			if d.Index >= 0 && d.Index < len(vectors) {
				vectors[d.Index] = d.Embedding
			}
		}
	}

	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("%s returned %d embeddings for %d texts", c.cfg.Model, len(vectors), len(texts))
	}
	for i, v := range vectors {
		if len(v) == 0 {
			return nil, fmt.Errorf("%s returned no embedding for text %d of %d", c.cfg.Model, i+1, len(texts))
		}
	}
	return vectors, nil
}

func (c *Client) post(ctx context.Context, path string, request, response any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("asking %s for embeddings: %w", c.cfg.BaseURL, err)
	}
	defer resp.Body.Close()

	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading the answer of %s: %w", c.cfg.BaseURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		// What the server says is the useful part: an unknown model, a bad key
		message := strings.TrimSpace(string(answer))
		if len(message) > 300 {
			message = message[:300] + "..."
		}
		return fmt.Errorf("%s%s answered %s: %s", c.cfg.BaseURL, path, resp.Status, message)
	}
	if err := json.Unmarshal(answer, response); err != nil {
		return fmt.Errorf("%s%s gave an answer that is not what was asked for: %w", c.cfg.BaseURL, path, err)
	}
	return nil
}

// Some embedding models are trained with a task prefix and retrieve poorly
// without it: what is stored and what is asked must be marked as such.

// DocumentText returns the text to embed for content that is stored in the index
func DocumentText(model, text string) string {
	if usesTaskPrefix(model) {
		return "search_document: " + text
	}
	return text
}

// QueryText returns the text to embed for a search query
func QueryText(model, text string) string {
	if usesTaskPrefix(model) {
		return "search_query: " + text
	}
	return text
}

func usesTaskPrefix(model string) bool {
	return strings.Contains(strings.ToLower(model), "nomic-embed")
}

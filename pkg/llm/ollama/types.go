package ollama

import (
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// EmbeddingRequest represents a request to generate embeddings
type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingResponse represents the response from Ollama embedding API
type EmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// GenerateRequest represents a request to generate text/extractions
type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	System string `json:"system,omitempty"`
}

// GenerateResponse represents the response from Ollama generate API
type GenerateResponse struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Response  string    `json:"response"`
	Done      bool      `json:"done"`
}

// ExtractionResult contains edges extracted from code
type ExtractionResult struct {
	Edges []llm.Edge `json:"edges"`
}

// JSON schema for structured output
type JSONSchema map[string]interface{}

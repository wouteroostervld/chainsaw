package ollama

import "time"

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

// Edge represents a knowledge graph edge extracted from code
type Edge struct {
	Source       string  `json:"source"`
	SourceType   string  `json:"source_type,omitempty"`
	Target       string  `json:"target"`
	TargetType   string  `json:"target_type,omitempty"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
}

// ExtractionResult contains edges extracted from code
type ExtractionResult struct {
	Edges []Edge `json:"edges"`
}

// JSON schema for structured output
type JSONSchema map[string]interface{}

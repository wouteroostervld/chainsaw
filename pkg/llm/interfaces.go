package llm

import "context"

// EmbeddingProvider generates vector embeddings from text
type EmbeddingProvider interface {
	// Embed generates embeddings for multiple texts in parallel
	Embed(ctx context.Context, model string, texts []string, concurrency int) ([][]float32, error)
}

package llm

import "context"

// EmbeddingProvider generates vector embeddings from text
type EmbeddingProvider interface {
	// Embed generates embeddings for multiple texts in parallel
	Embed(ctx context.Context, model string, texts []string, concurrency int) ([][]float32, error)
}

// GraphExtractor extracts knowledge graph edges from code
type GraphExtractor interface {
	// ExtractEdges analyzes a single code chunk and extracts entity relationships
	ExtractEdges(ctx context.Context, model string, code string) ([]Edge, error)

	// ExtractEdgesBatch analyzes multiple code chunks in a single LLM call
	// Returns edges with metadata tracking which chunk each edge came from
	ExtractEdgesBatch(ctx context.Context, model string, chunks []ChunkInput) ([]EdgeWithMetadata, error)
}

package indexer

import (
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// Config holds indexer configuration
type Config struct {
	ChunkSize              int
	ChunkOverlap           int
	EmbedModel             string
	GraphModel             string
	BatchSize              int // Embedding batch size (chunks per embedding call)
	GraphBatchSize         int // Graph extraction batch size (chunks per LLM call)
	MaxConcurrency         int
	EnableGraphMode        bool
	MinChunkSize           int
	MaxChunkSize           int
	GraphDistanceThreshold float64 // Max cosine distance for creating edges (0.0-2.0, default 0.5)
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:              512,
		ChunkOverlap:           64,
		EmbedModel:             "nomic-embed-text",
		GraphModel:             "qwen2.5:3b",
		BatchSize:              20,  // Embedding batch size
		GraphBatchSize:         100, // Graph extraction batch size (increased for large context models)
		MaxConcurrency:         5,
		EnableGraphMode:        true,
		MinChunkSize:           10,
		MaxChunkSize:           4096,
		GraphDistanceThreshold: 0.5, // Cosine distance threshold (0=identical, 1=orthogonal, 2=opposite)
	}
}

// Indexer coordinates the indexing pipeline
type Indexer struct {
	config      *Config
	db          db.Database
	ollama      llm.EmbeddingProvider // For embeddings
	graphClient llm.GraphExtractor    // For graph extraction
}

// IndexResult contains statistics about an indexing operation
type IndexResult struct {
	FilesProcessed int
	ChunksCreated  int
	EdgesCreated   int
	Duration       time.Duration
	Errors         []error
}

// Chunk represents a text chunk ready for embedding
type Chunk struct {
	FileID      int64
	FilePath    string
	Content     string
	StartOffset int
	EndOffset   int
	StartLine   int
	EndLine     int
}

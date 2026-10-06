package indexer

import (
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/gotypes"
	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// Config holds indexer configuration
type Config struct {
	ChunkSize        int
	ChunkOverlap     int
	EmbedModel       string
	BatchSize        int // Embedding batch size (chunks per embedding call)
	MaxConcurrency   int
	MinChunkSize     int
	MaxChunkSize     int
	SemanticChunking bool // Chunk known languages along their syntax tree and derive the graph from it
	GoTypes          bool // Resolve the calls in Go files with the Go type checker
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:        512,
		ChunkOverlap:     64,
		EmbedModel:       "nomic-embed-text",
		BatchSize:        20, // Embedding batch size
		MaxConcurrency:   5,
		MinChunkSize:     10,
		MaxChunkSize:     4096,
		SemanticChunking: true,
		GoTypes:          true,
	}
}

// Indexer coordinates the indexing pipeline
type Indexer struct {
	config  *Config
	db      db.Database
	ollama  llm.EmbeddingProvider // For embeddings
	goTypes *gotypes.Resolver     // For exact call resolution in Go files
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
	Context     string // Where the chunk is from: file, and definition or section
}

// embedText returns what is embedded for the chunk. The stored content is a
// piece of a file; saying which file and which definition it is from lets a
// fragment be found by what the whole is about.
func (c Chunk) embedText() string {
	if c.Context == "" {
		return c.Content
	}
	return c.Context + "\n\n" + c.Content
}

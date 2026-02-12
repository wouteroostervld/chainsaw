package ollama

import (
	"context"

	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// OllamaClient defines the interface for Ollama API operations
type OllamaClient interface {
	// Embed generates embeddings for the given text
	Embed(ctx context.Context, model string, texts []string, concurrency int) ([][]float32, error)

	// EmbedBatch generates embeddings for multiple texts in parallel
	EmbedBatch(ctx context.Context, model string, texts []string) ([][]float32, error)

	// Generate produces text completions using an LLM
	Generate(ctx context.Context, model, prompt, system string) (string, error)

	// ExtractEdges uses an LLM to extract knowledge graph edges from code
	ExtractEdges(ctx context.Context, model, code string) ([]llm.Edge, error)

	// Ping checks if Ollama server is reachable
	Ping(ctx context.Context) error
}

// Ensure Client implements OllamaClient

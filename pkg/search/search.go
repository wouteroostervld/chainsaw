package search

import (
	"context"
	"fmt"

	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/ollama"
)

// Engine provides search capabilities
type Engine struct {
	db     db.Database
	ollama ollama.OllamaClient
	model  string
}

// Config holds search engine configuration
type Config struct {
	EmbedModel string
}

// Result represents a search result
type Result struct {
	ChunkID   int64
	FileID    int64
	FilePath  string
	Snippet   string
	Score     float64
	Neighbors []Neighbor
}

// Neighbor represents a graph neighbor
type Neighbor struct {
	ChunkID      int64
	Snippet      string
	RelationType string
	Weight       float64
}

// New creates a new search engine
func New(cfg *Config, database db.Database, ollamaClient ollama.OllamaClient) *Engine {
	if cfg == nil {
		cfg = &Config{EmbedModel: "nomic-embed-text"}
	}
	return &Engine{
		db:     database,
		ollama: ollamaClient,
		model:  cfg.EmbedModel,
	}
}

// VectorSearch performs vector similarity search
func (e *Engine) VectorSearch(ctx context.Context, query string, limit int) ([]Result, error) {
	embedding, err := e.ollama.Embed(ctx, e.model, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	results, err := e.db.SearchSimilar(embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var searchResults []Result
	for _, r := range results {
		file, err := e.db.GetFileByID(r.Chunk.FileID)
		if err != nil {
			continue
		}

		score := 1.0 - r.Distance
		searchResults = append(searchResults, Result{
			ChunkID:  r.Chunk.ChunkID,
			FileID:   r.Chunk.FileID,
			FilePath: file.Path,
			Snippet:  r.Chunk.ContentSnippet,
			Score:    score,
		})
	}

	return searchResults, nil
}

// GraphSearch performs graph-expanded search
func (e *Engine) GraphSearch(ctx context.Context, query string, limit int, maxDepth int) ([]Result, error) {
	vectorResults, err := e.VectorSearch(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	for i := range vectorResults {
		neighbors, err := e.db.GetNeighbors(db.GetNeighborsOptions{
			ChunkID:   vectorResults[i].ChunkID,
			MaxDepth:  maxDepth,
			MinWeight: 0.5,
			Limit:     10,
		})
		if err != nil {
			continue
		}

		for _, n := range neighbors {
			chunk, err := e.db.GetChunk(n.ChunkID)
			if err != nil {
				continue
			}

			vectorResults[i].Neighbors = append(vectorResults[i].Neighbors, Neighbor{
				ChunkID:      n.ChunkID,
				Snippet:      chunk.ContentSnippet,
				RelationType: n.RelationType,
				Weight:       n.TotalWeight,
			})
		}
	}

	return vectorResults, nil
}

// HybridSearch combines vector and graph search
func (e *Engine) HybridSearch(ctx context.Context, query string, limit int) ([]Result, error) {
	return e.GraphSearch(ctx, query, limit, 2)
}

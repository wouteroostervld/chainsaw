package indexer

import (
	"context"
	"fmt"

	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// ProcessGraphBatch extracts relations from a specific set of chunks using batched LLM calls
// This is called by the graph worker, not per-file indexing
func (idx *Indexer) ProcessGraphBatch(ctx context.Context, chunkIDs []int64) (int, error) {
	if len(chunkIDs) == 0 {
		return 0, nil
	}

	// Get chunk data for the specified IDs
	dbChunks, err := idx.db.GetChunksByIDs(chunkIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to get chunks: %w", err)
	}

	if len(dbChunks) == 0 {
		return 0, nil
	}

	// Use configured batch size for LLM calls
	batchSize := idx.config.GraphBatchSize
	if batchSize <= 0 {
		batchSize = 100 // Fallback default
	}
	if batchSize > len(dbChunks) {
		batchSize = len(dbChunks)
	}

	totalEdges := 0
	batchNum := 0

	// Process chunks in batches
	for i := 0; i < len(dbChunks); i += batchSize {
		batchNum++
		end := i + batchSize
		if end > len(dbChunks) {
			end = len(dbChunks)
		}

		batchChunks := dbChunks[i:end]

		// Convert to LLM chunk input format
		llmChunks := make([]llm.ChunkInput, len(batchChunks))
		for j, chunk := range batchChunks {
			llmChunks[j] = llm.ChunkInput{
				ChunkID:  chunk.ChunkID,
				FileID:   chunk.FileID,
				FilePath: chunk.FilePath,
				Content:  chunk.ContentSnippet,
			}
		}

		// Extract edges from batch
		edges, err := idx.graphClient.ExtractEdgesBatch(ctx, idx.config.GraphModel, llmChunks)
		if err != nil {
			return totalEdges, fmt.Errorf("batch %d: %w", batchNum, err)
		}

		// Store each edge with proper entities
		for _, edgeWithMeta := range edges {
			// Create or get source entity
			sourceID, err := idx.db.UpsertEntity(edgeWithMeta.Source, edgeWithMeta.SourceType, edgeWithMeta.ChunkID)
			if err != nil {
				continue
			}

			// Create or get target entity
			targetID, err := idx.db.UpsertEntity(edgeWithMeta.Target, edgeWithMeta.TargetType, edgeWithMeta.ChunkID)
			if err != nil {
				continue
			}

			// Create edge between entities
			if err := idx.db.UpsertEntityEdge(sourceID, targetID, edgeWithMeta.RelationType, edgeWithMeta.ChunkID); err != nil {
				continue
			}
			totalEdges++
		}
	}

	return totalEdges, nil
}

// CodeRelationship represents an extracted relation
type CodeRelationship struct {
	Source     string
	SourceType string // FUNCTION, TYPE, INTERFACE, METHOD, etc.
	Relation   string
	Target     string
	TargetType string
}

// extractRelationshipsFromChunk uses LLM with JSON schema to extract all relations
func (idx *Indexer) extractRelationshipsFromChunk(ctx context.Context, chunkID int64, content string) []CodeRelationship {
	// Use Ollama's structured JSON output with schema enforcement
	edges, err := idx.graphClient.ExtractEdges(ctx, idx.config.GraphModel, content)
	if err != nil {
		fmt.Printf("⚠️  ExtractEdges error for chunk %d: %v\n", chunkID, err)
		return nil
	}

	if len(edges) == 0 {
		fmt.Printf("  Chunk %d: no relations found\n", chunkID)
	} else {
		fmt.Printf("  Chunk %d: found %d relations\n", chunkID, len(edges))
	}

	// Convert to CodeRelationship format
	var relations []CodeRelationship
	for _, edge := range edges {
		relations = append(relations, CodeRelationship{
			Source:     edge.Source,
			SourceType: edge.SourceType,
			Relation:   edge.RelationType,
			Target:     edge.Target,
			TargetType: edge.TargetType,
		})
	}

	return relations
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// isCommonName filters out generic names to reduce noise
func isCommonName(name string) bool {
	// Go standard library types and common names
	common := map[string]bool{
		// Common short names
		"main": true, "init": true, "New": true,
		"Get": true, "Set": true, "String": true,
		"Error": true, "Close": true, "Read": true,
		"Write": true, "Open": true, "Start": true,
		"Stop": true, "Run": true, "Wait": true,

		// Go keywords and built-ins
		"int": true, "string": true, "bool": true,
		"byte": true, "rune": true, "error": true,
		"true": true, "false": true, "nil": true,
		"if": true, "else": true, "for": true,
		"range": true, "return": true, "func": true,
		"var": true, "const": true, "type": true,
		"struct": true, "interface": true, "map": true,
		"chan": true, "go": true, "defer": true,
		"select": true, "case": true, "default": true,
		"break": true, "continue": true, "goto": true,
		"fallthrough": true, "import": true, "package": true,

		// Common stdlib types
		"Context": true, "Reader": true, "Writer": true,
		"Handler": true, "Server": true, "Client": true,
		"Request": true, "Response": true, "Header": true,
		"Body": true, "Status": true, "Code": true,
		"Time": true, "Duration": true, "Timeout": true,
		"Buffer": true, "Scanner": true, "File": true,

		// Generic terms
		"API": true, "HTTP": true, "JSON": true,
		"XML": true, "SQL": true, "DB": true,
		"ID": true, "Key": true, "Value": true,
		"Data": true, "Info": true, "Config": true,
		"Options": true, "Settings": true, "Params": true,
		"Args": true, "Flag": true, "Env": true,
	}

	// Filter by length
	if len(name) < 4 {
		return true
	}

	// Filter common patterns
	if common[name] {
		return true
	}

	// Filter single letter variables
	if len(name) == 1 {
		return true
	}

	// Filter all lowercase (likely variables, not types/functions)
	allLower := true
	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			allLower = false
			break
		}
	}
	if allLower {
		return true
	}

	return false
}

// isCapitalized checks if a string starts with a capital letter
func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// getChunkDistance queries the distance between two chunks using sqlite-vec
func (idx *Indexer) getChunkDistance(chunkA, chunkB int64) (float64, error) {
	return idx.db.GetChunkDistance(chunkA, chunkB)
}

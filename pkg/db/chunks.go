package db

import (
	"database/sql"
	"fmt"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// Chunk represents a vector chunk in the database
type Chunk struct {
	ChunkID        int64
	FileID         int64
	ContentSnippet string
	Embedding      []float32
	StartLine      int // Starting line number (1-indexed)
	EndLine        int // Ending line number (1-indexed)
}

// InsertChunk inserts a new chunk with its embedding vector
// Returns the chunk ID
func (db *DB) InsertChunk(fileID int64, contentSnippet string, embedding []float32, startLine, endLine int) (int64, error) {
	// Validate embedding dimension
	if len(embedding) != db.embeddingDim {
		return 0, fmt.Errorf("embedding dimension mismatch: expected %d, got %d", db.embeddingDim, len(embedding))
	}

	// Serialize embedding to compact binary format for sqlite-vec
	embBytes, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize embedding: %w", err)
	}

	// Insert into vec_chunks virtual table
	result, err := db.conn.Exec(`
		INSERT INTO vec_chunks (file_id, content_snippet, start_line, end_line, embedding)
		VALUES (?, ?, ?, ?, ?)
	`, fileID, contentSnippet, startLine, endLine, embBytes)

	if err != nil {
		return 0, fmt.Errorf("failed to insert chunk: %w", err)
	}

	chunkID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get chunk ID: %w", err)
	}

	return chunkID, nil
}

// GetChunk retrieves a chunk by ID
func (db *DB) GetChunk(chunkID int64) (*Chunk, error) {
	var c Chunk
	var embBytes []byte

	err := db.conn.QueryRow(`
		SELECT chunk_id, file_id, content_snippet, start_line, end_line, embedding
		FROM vec_chunks
		WHERE chunk_id = ?
	`, chunkID).Scan(&c.ChunkID, &c.FileID, &c.ContentSnippet, &c.StartLine, &c.EndLine, &embBytes)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk: %w", err)
	}

	// Note: We store the raw embedding bytes.
	// For display/export, we'd need to parse but that's rarely needed.
	// The binary format is only used internally by sqlite-vec.
	c.Embedding = nil // Not typically needed by callers

	return &c, nil
}

// GetChunksForFile retrieves all chunks for a given file
func (db *DB) GetChunksForFile(fileID int64) ([]*Chunk, error) {
	rows, err := db.conn.Query(`
		SELECT chunk_id, file_id, content_snippet, start_line, end_line
		FROM vec_chunks
		WHERE file_id = ?
		ORDER BY chunk_id
	`, fileID)

	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk
	for rows.Next() {
		var c Chunk

		err := rows.Scan(&c.ChunkID, &c.FileID, &c.ContentSnippet, &c.StartLine, &c.EndLine)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}

		// Embeddings not needed for listing
		c.Embedding = nil
		chunks = append(chunks, &c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// DeleteChunksForFile removes all chunks associated with a file
// This is used when re-indexing a file
func (db *DB) DeleteChunksForFile(fileID int64) error {
	// Delete from vec_chunks (will cascade to graph_edges due to FK)
	_, err := db.conn.Exec("DELETE FROM vec_chunks WHERE file_id = ?", fileID)
	if err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}
	return nil
}

// SearchResult represents a chunk with its similarity score
type SearchResult struct {
	Chunk         *Chunk
	Distance      float64         // Cosine distance (lower is more similar)
	RelatedChunks []*RelatedChunk // Graph-connected chunks
}

// RelatedChunk represents a chunk connected via graph edges
type RelatedChunk struct {
	Chunk        *Chunk
	RelationType string
	Weight       float64
	Depth        int // Graph distance from source
}

// SearchSimilar finds chunks similar to the query embedding
// Uses cosine distance for vector similarity
func (db *DB) SearchSimilar(queryEmbedding []float32, limit int, pathFilter string) ([]*SearchResult, error) {
	if len(queryEmbedding) != db.embeddingDim {
		return nil, fmt.Errorf("query embedding dimension mismatch: expected %d, got %d", db.embeddingDim, len(queryEmbedding))
	}

	if limit <= 0 {
		limit = 10
	}

	// Serialize query embedding to binary format
	queryBytes, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query embedding: %w", err)
	}

	// Build query with optional path filtering
	query := `
		SELECT 
			c.chunk_id, c.file_id, c.content_snippet, c.start_line, c.end_line, c.embedding,
			distance,
			f.path
		FROM vec_chunks c
		JOIN files f ON c.file_id = f.id
		WHERE embedding MATCH ?
		  AND k = ?`

	args := []interface{}{queryBytes, limit}

	// Add path filtering if specified
	if pathFilter != "" {
		query += "\n  AND f.path LIKE ?"
		args = append(args, pathFilter)
	}

	query += "\nORDER BY distance"

	rows, err := db.conn.Query(query, args...)

	if err != nil {
		return nil, fmt.Errorf("failed to search similar chunks: %w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		var c Chunk
		var embBytes []byte
		var distance float64
		var path string

		err := rows.Scan(&c.ChunkID, &c.FileID, &c.ContentSnippet, &c.StartLine, &c.EndLine, &embBytes, &distance, &path)
		if err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}

		// Embedding stored but not typically displayed
		c.Embedding = nil

		results = append(results, &SearchResult{
			Chunk:    &c,
			Distance: distance,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search results: %w", err)
	}

	return results, nil
}

// SearchWithRelations is like SearchSimilar but also includes graph-connected chunks
func (db *DB) SearchWithRelations(queryEmbedding []float32, limit int, maxDepth int, pathFilter string) ([]*SearchResult, error) {
	// First get vector search results WITH path filtering
	results, err := db.SearchSimilar(queryEmbedding, limit, pathFilter)
	if err != nil {
		return nil, err
	}

	// For each result, get related chunks via graph
	for _, result := range results {
		neighbors, err := db.GetNeighbors(GetNeighborsOptions{
			ChunkID:   result.Chunk.ChunkID,
			MaxDepth:  maxDepth,
			MinWeight: 0.0,
			Limit:     5, // Top 5 related chunks per result
		})
		if err != nil {
			// Log but don't fail the search
			continue
		}

		// Convert neighbors to RelatedChunk
		for _, n := range neighbors {
			chunk, err := db.GetChunk(n.ChunkID)
			if err != nil {
				continue
			}

			// Apply pathFilter to related chunks too!
			if pathFilter != "" {
				file, err := db.GetFileByID(chunk.FileID)
				if err != nil || file == nil {
					continue
				}
				// SQL LIKE pattern uses %, so convert to prefix match
				// pathFilter is like "/home/user/project/%"
				prefix := strings.TrimSuffix(pathFilter, "%")
				if !strings.HasPrefix(file.Path, prefix) {
					continue
				}
			}

			result.RelatedChunks = append(result.RelatedChunks, &RelatedChunk{
				Chunk:        chunk,
				RelationType: n.RelationType,
				Weight:       n.TotalWeight,
				Depth:        n.Depth,
			})
		}
	}

	return results, nil
}

// GetAllChunks retrieves all chunks from the database
// Used for post-processing operations like graph building
func (db *DB) GetAllChunks() ([]*Chunk, error) {
	rows, err := db.conn.Query(`
		SELECT chunk_id, file_id, content_snippet, start_line, end_line
		FROM vec_chunks
		ORDER BY chunk_id
	`)

	if err != nil {
		return nil, fmt.Errorf("failed to query all chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk
	for rows.Next() {
		var c Chunk
		err := rows.Scan(&c.ChunkID, &c.FileID, &c.ContentSnippet, &c.StartLine, &c.EndLine)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}
		c.Embedding = nil
		chunks = append(chunks, &c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// ChunkWithPath extends Chunk with file path information
type ChunkWithPath struct {
	ChunkID        int64
	FileID         int64
	FilePath       string
	ContentSnippet string
	StartLine      int
	EndLine        int
}

// GetAllChunksWithPaths retrieves all chunks with their file paths
// Used for batched graph extraction
func (db *DB) GetAllChunksWithPaths() ([]*ChunkWithPath, error) {
	rows, err := db.conn.Query(`
		SELECT v.chunk_id, v.file_id, f.path, v.content_snippet, v.start_line, v.end_line
		FROM vec_chunks v
		JOIN files f ON v.file_id = f.id
		ORDER BY v.chunk_id
	`)

	if err != nil {
		return nil, fmt.Errorf("failed to query chunks with paths: %w", err)
	}
	defer rows.Close()

	var chunks []*ChunkWithPath
	for rows.Next() {
		var c ChunkWithPath
		err := rows.Scan(&c.ChunkID, &c.FileID, &c.FilePath, &c.ContentSnippet, &c.StartLine, &c.EndLine)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}
		chunks = append(chunks, &c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// GetChunkDistance calculates distance between two chunk embeddings
// Uses cosine distance which works better for text embeddings like nomic-embed-text
// Cosine distance = 1 - cosine_similarity, range [0, 2]
func (db *DB) GetChunkDistance(chunkA, chunkB int64) (float64, error) {
	var distance float64

	// Use cosine distance - better for normalized text embeddings
	err := db.conn.QueryRow(`
		SELECT vec_distance_cosine(a.embedding, b.embedding)
		FROM vec_chunks a, vec_chunks b
		WHERE a.chunk_id = ? AND b.chunk_id = ?
	`, chunkA, chunkB).Scan(&distance)

	if err != nil {
		return 2.0, fmt.Errorf("failed to calculate distance: %w", err)
	}

	return distance, nil
}

// CountChunks returns the total number of chunks
func (db *DB) CountChunks() (int64, error) {
	var count int64
	err := db.conn.QueryRow("SELECT COUNT(*) FROM vec_chunks").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count chunks: %w", err)
	}
	return count, nil
}

// GetChunksByIDs retrieves chunks with file paths for specific chunk IDs
func (db *DB) GetChunksByIDs(chunkIDs []int64) ([]*ChunkWithPath, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(chunkIDs))
	args := make([]interface{}, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
SELECT 
v.chunk_id,
v.file_id,
f.path as file_path,
v.content_snippet,
v.start_line,
v.end_line
FROM vec_chunks v
JOIN files f ON v.file_id = f.id
WHERE v.chunk_id IN (%s)
ORDER BY v.chunk_id
`, strings.Join(placeholders, ","))

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query chunks by IDs: %w", err)
	}
	defer rows.Close()

	var results []*ChunkWithPath
	for rows.Next() {
		chunk := &ChunkWithPath{}
		err := rows.Scan(
			&chunk.ChunkID,
			&chunk.FileID,
			&chunk.FilePath,
			&chunk.ContentSnippet,
			&chunk.StartLine,
			&chunk.EndLine,
		)
		if err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		results = append(results, chunk)
	}

	return results, rows.Err()
}

package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/llm"
)

// isBinaryFile checks if a file is binary by reading its first 512 bytes
func isBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Read first 512 bytes for content type detection
	buffer := make([]byte, 512)
	n, err := f.Read(buffer)
	if err != nil && err != io.EOF {
		return false, err
	}

	// Detect content type using Go's standard library
	contentType := http.DetectContentType(buffer[:n])

	// Skip binary types but allow text-based application formats
	if strings.HasPrefix(contentType, "application/") {
		// Allow these text-based application types
		allowed := []string{"json", "xml", "javascript", "x-sh", "x-perl", "x-python"}
		for _, a := range allowed {
			if strings.Contains(contentType, a) {
				return false, nil
			}
		}
		return true, nil // Other application/* types are binary
	}

	// Allow all text/* types
	return !strings.HasPrefix(contentType, "text/"), nil
}

// New creates a new indexer
func New(cfg *Config, database db.Database, embeddingProvider llm.EmbeddingProvider) *Indexer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Indexer{
		config:      cfg,
		db:          database,
		ollama:      embeddingProvider,
		graphClient: embeddingProvider.(llm.GraphExtractor), // Default to same client
	}
}

// NewWithSeparateClients creates an indexer with separate clients for embeddings and graph extraction
func NewWithSeparateClients(cfg *Config, database db.Database, embeddingProvider llm.EmbeddingProvider, graphExtractor llm.GraphExtractor) *Indexer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Indexer{
		config:      cfg,
		db:          database,
		ollama:      embeddingProvider,
		graphClient: graphExtractor,
	}
}

// IndexFile indexes a single file
func (idx *Indexer) IndexFile(ctx context.Context, filePath string) error {
	// Check if file is binary
	isBinary, err := isBinaryFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to check file type: %w", err)
	}
	if isBinary {
		slog.Debug("Skipping binary file", "file", filePath)
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	modTime := info.ModTime().Unix()

	fileID, err := idx.db.UpsertFile(filePath, modTime, hash)
	if err != nil {
		return fmt.Errorf("failed to upsert file: %w", err)
	}
	slog.Debug("Upserted file", "file", filePath, "file_id", fileID)

	if err := idx.db.DeleteChunksForFile(fileID); err != nil {
		return fmt.Errorf("failed to delete old chunks: %w", err)
	}

	slog.Info("About to chunk file", "file", filePath, "size", len(content))
	chunks := idx.chunkContent(string(content), fileID, filePath)
	slog.Info("Chunking complete", "file", filePath, "chunk_count", len(chunks))

	// Skip processing if file is empty or produces no chunks
	if len(chunks) == 0 {
		slog.Debug("No chunks generated for file", "file", filePath, "size", len(content))
		return nil
	}

	slog.Debug("Generated chunks for file", "file", filePath, "count", len(chunks))

	for i := 0; i < len(chunks); i += idx.config.BatchSize {
		end := min(i+idx.config.BatchSize, len(chunks))
		batch := chunks[i:end]

		if err := idx.processBatch(ctx, batch, fileID); err != nil {
			return fmt.Errorf("failed to process batch: %w", err)
		}

		// Rate limit embedding requests to avoid overwhelming Ollama
		if end < len(chunks) {
			time.Sleep(2 * time.Second)
		}
	}

	// Graph extraction is now handled by a separate worker
	// Don't call BuildGraphFromCooccurrence here - it would re-process all chunks

	return nil
}

// IndexDirectory recursively indexes all files in a directory
func (idx *Indexer) IndexDirectory(ctx context.Context, dirPath string, filter func(string) bool) (*IndexResult, error) {
	result := &IndexResult{}
	startTime := time.Now()

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("walk error for %s: %w", path, err))
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if filter != nil && !filter(path) {
			return nil
		}

		if err := idx.IndexFile(ctx, path); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("index error for %s: %w", path, err))
		} else {
			result.FilesProcessed++
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("directory walk failed: %w", err)
	}

	// Post-process: Graph extraction is now handled by a separate worker
	// Don't call BuildGraphFromCooccurrence here

	result.Duration = time.Since(startTime)
	return result, nil
}

// chunkContent splits content into overlapping chunks with line number tracking
// Chunks are aligned to line boundaries for better readability
func (idx *Indexer) chunkContent(content string, fileID int64, filePath string) []Chunk {
	var chunks []Chunk

	contentBytes := []byte(content)
	contentLen := len(contentBytes)

	if contentLen < idx.config.MinChunkSize {
		return chunks
	}

	// For small files, just use the whole content as one chunk
	if contentLen <= idx.config.ChunkSize {
		chunks = append(chunks, Chunk{
			FileID:      fileID,
			FilePath:    filePath,
			Content:     content,
			StartOffset: 0,
			EndOffset:   contentLen,
			StartLine:   1,
			EndLine:     countLines(contentBytes),
		})
		return chunks
	}

	stride := idx.config.ChunkSize - idx.config.ChunkOverlap
	if stride <= 0 {
		stride = idx.config.ChunkSize
	}

	for offset := 0; offset < contentLen; {
		end := min(offset+idx.config.ChunkSize, contentLen)

		if end-offset > idx.config.MaxChunkSize {
			end = offset + idx.config.MaxChunkSize
		}

		// Align to line boundaries
		// Find start of line (move forward to first newline, then +1)
		if offset > 0 {
			for offset < contentLen && contentBytes[offset] != '\n' {
				offset++
			}
			if offset < contentLen {
				offset++ // Move past the newline
			}
		}

		// Find end of line (move backward to last newline, inclusive)
		if end < contentLen {
			for end > offset && contentBytes[end-1] != '\n' {
				end--
			}
		}

		// Skip if too small after alignment
		if end <= offset {
			// Move forward by smaller increment for small files
			offset += min(stride, 100)
			continue
		}

		chunkBytes := contentBytes[offset:end]
		if len(chunkBytes) >= idx.config.MinChunkSize {
			// Calculate line numbers from byte offsets
			startLine := countLines(contentBytes[:offset]) + 1
			endLine := countLines(contentBytes[:end])

			chunks = append(chunks, Chunk{
				FileID:      fileID,
				FilePath:    filePath,
				Content:     string(chunkBytes),
				StartOffset: offset,
				EndOffset:   end,
				StartLine:   startLine,
				EndLine:     endLine,
			})
		}

		offset += stride

		if end >= contentLen {
			break
		}
	}

	return chunks
}

// countLines counts the number of newlines in a byte slice
func countLines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

// processBatch generates embeddings and optionally extracts edges for a batch of chunks
func (idx *Indexer) processBatch(ctx context.Context, chunks []Chunk, fileID int64) error {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}

	slog.Debug("Generating embeddings", "chunks", len(chunks))
	embeddings, err := idx.ollama.Embed(ctx, idx.config.EmbedModel, texts, 5)
	if err != nil {
		slog.Error("Failed to generate embeddings", "error", err)
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}
	slog.Debug("Embeddings generated", "count", len(embeddings))

	var chunkIDs []int64
	for i, chunk := range chunks {
		chunkID, err := idx.db.InsertChunk(chunk.FileID, chunk.Content, embeddings[i], chunk.StartLine, chunk.EndLine)
		if err != nil {
			slog.Error("Failed to insert chunk", "error", err, "chunk_index", i)
			return fmt.Errorf("failed to insert chunk: %w", err)
		}
		chunkIDs = append(chunkIDs, chunkID)
		slog.Debug("Inserted chunk", "chunk_id", chunkID, "file_id", chunk.FileID, "start_line", chunk.StartLine)
	}
	slog.Debug("Inserted chunks", "count", len(chunkIDs))

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractGraphEdges builds a symbol→chunkID map and creates cross-chunk edges
// extractGraphEdges is deprecated - use BuildGraphFromCooccurrence instead
func (idx *Indexer) extractGraphEdges(ctx context.Context, chunks []Chunk, chunkIDs []int64) error {
	// Legacy function - no longer used with entity-based graph
	return nil
}

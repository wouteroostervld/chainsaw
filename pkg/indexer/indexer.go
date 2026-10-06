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

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/gotypes"
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
		config:  cfg,
		db:      database,
		ollama:  embeddingProvider,
		goTypes: gotypes.NewResolver(),
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
	chunks, parsed, chunkIndex := idx.chunkFile(content, fileID, filePath)
	slog.Info("Chunking complete", "file", filePath, "chunk_count", len(chunks))

	// Skip processing if file is empty or produces no chunks
	if len(chunks) == 0 {
		slog.Debug("No chunks generated for file", "file", filePath, "size", len(content))
		return nil
	}

	slog.Debug("Generated chunks for file", "file", filePath, "count", len(chunks))

	chunkIDs := make([]int64, 0, len(chunks))
	for i := 0; i < len(chunks); i += idx.config.BatchSize {
		end := min(i+idx.config.BatchSize, len(chunks))
		batch := chunks[i:end]

		ids, err := idx.processBatch(ctx, batch, fileID)
		if err != nil {
			return fmt.Errorf("failed to process batch: %w", err)
		}
		chunkIDs = append(chunkIDs, ids...)
	}

	// Files that could be parsed get their graph from the syntax tree; other
	// files, and documents, have none
	if parsed != nil {
		if err := idx.storeOutline(fileID, parsed, chunkIndex, chunkIDs); err != nil {
			return fmt.Errorf("failed to store outline: %w", err)
		}
		if !parsed.Markup {
			if err := idx.storeSymbols(fileID, filePath, parsed, chunkIndex, chunkIDs); err != nil {
				return fmt.Errorf("failed to store symbols: %w", err)
			}
		}
	}

	return nil
}

// chunkFile splits a file into chunks. Source files in a known language are
// split along their syntax tree; the parse result is returned along with, for
// each of its chunks, the index of the first chunk stored for it (-1 if none).
// Other files are split into overlapping blocks.
func (idx *Indexer) chunkFile(content []byte, fileID int64, filePath string) ([]Chunk, *chunker.File, []int) {
	if idx.config.SemanticChunking {
		if parsed, ok := chunker.Parse(filePath, content, idx.config.MaxChunkSize); ok {
			chunks, chunkIndex := idx.semanticChunks(parsed, fileID, filePath)
			if len(chunks) > 0 {
				if parsed.Language == "go" && idx.config.GoTypes && idx.goTypes != nil {
					// Without it the calls stay as the syntax tree gives them
					if err := idx.goTypes.Apply(parsed, filePath); err != nil {
						slog.Debug("Go type check failed", "file", filePath, "error", err)
					}
				}
				return chunks, parsed, chunkIndex
			}
		}
	}
	chunks := idx.chunkContent(string(content), fileID, filePath)
	for i := range chunks {
		chunks[i].Context = displayPath(filePath)
	}
	return chunks, nil, nil
}

// displayPath returns a file's path within its project
func displayPath(filePath string) string {
	if root := projectRoot(filePath); root != "" {
		if rel, err := filepath.Rel(filepath.Dir(root), filePath); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Join(filepath.Base(filepath.Dir(filePath)), filepath.Base(filePath))
}

// semanticChunks converts the chunks of a parsed file. A chunk that is still
// too large after folding its nested definitions is split into blocks.
func (idx *Indexer) semanticChunks(parsed *chunker.File, fileID int64, filePath string) ([]Chunk, []int) {
	var chunks []Chunk
	chunkIndex := make([]int, len(parsed.Chunks))
	path := displayPath(filePath)

	for i, c := range parsed.Chunks {
		context := path
		if c.Title != "" {
			context = path + ": " + c.Title
		}
		chunkIndex[i] = -1
		if len(c.Content) < idx.config.MinChunkSize {
			continue
		}
		chunkIndex[i] = len(chunks)

		if len(c.Content) <= idx.config.MaxChunkSize {
			chunks = append(chunks, Chunk{
				FileID:      fileID,
				FilePath:    filePath,
				Content:     c.Content,
				StartOffset: c.StartOffset,
				EndOffset:   c.EndOffset,
				StartLine:   c.StartLine,
				EndLine:     c.EndLine,
				Context:     context,
			})
			continue
		}

		for _, block := range idx.chunkContent(c.Content, fileID, filePath) {
			block.Context = context
			if c.Verbatim {
				block.StartOffset += c.StartOffset
				block.EndOffset += c.StartOffset
				block.StartLine += c.StartLine - 1
				block.EndLine += c.StartLine - 1
			} else {
				// Folded content no longer lines up with the file
				block.StartOffset, block.EndOffset = c.StartOffset, c.EndOffset
				block.StartLine, block.EndLine = c.StartLine, c.EndLine
			}
			chunks = append(chunks, block)
		}
		if chunkIndex[i] == len(chunks) {
			chunkIndex[i] = -1
		}
	}

	return chunks, chunkIndex
}

// storeOutline stores the tree of semantic chunks, so a search hit can be
// served in the context of the folded file
func (idx *Indexer) storeOutline(fileID int64, parsed *chunker.File, chunkIndex []int, chunkIDs []int64) error {
	nodes := make([]db.OutlineNode, len(parsed.Chunks))
	for i, c := range parsed.Chunks {
		nodes[i] = db.OutlineNode{
			Parent:    c.Parent,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Content:   c.Content,
			Stub:      c.Stub,
		}
		if chunkIndex[i] < 0 {
			continue
		}
		// A node's chunks run up to the first chunk of the next stored node
		end := len(chunkIDs)
		for _, next := range chunkIndex[i+1:] {
			if next >= 0 {
				end = next
				break
			}
		}
		nodes[i].ChunkIDs = chunkIDs[chunkIndex[i]:end]
	}
	return idx.db.StoreFileOutline(fileID, nodes)
}

// storeSymbols stores the definitions, calls and imports of a parsed file as
// graph entities and edges
func (idx *Indexer) storeSymbols(fileID int64, filePath string, parsed *chunker.File, chunkIndex []int, chunkIDs []int64) error {
	// A symbol lives in the chunk of its definition, or the nearest enclosing
	// chunk that was stored
	chunkID := func(i int) int64 {
		for ; i >= 0; i = parsed.Chunks[i].Parent {
			if chunkIndex[i] >= 0 {
				return chunkIDs[chunkIndex[i]]
			}
		}
		return chunkIDs[0]
	}

	// The module comes first; it defines everything at file level and is the
	// source of imports and of calls outside any definition
	defs := make([]db.SymbolDef, 0, len(parsed.Symbols)+1)
	defs = append(defs, db.SymbolDef{
		Name:       parsed.Module,
		EntityType: chunker.TypePackage,
		ChunkID:    chunkID(0),
		Parent:     -1,
	})
	for _, sym := range parsed.Symbols {
		defs = append(defs, db.SymbolDef{
			Name:       sym.Name,
			Container:  sym.Container,
			EntityType: sym.Type,
			ChunkID:    chunkID(sym.Chunk),
			Parent:     sym.Parent + 1,
		})
	}

	refs := make([]db.SymbolRef, 0, len(parsed.Refs))
	for _, ref := range parsed.Refs {
		refs = append(refs, db.SymbolRef{
			Source:       ref.Source + 1,
			RelationType: ref.Relation,
			Name:         ref.Name,
			Container:    ref.Container,
			ImportBase:   ref.ImportBase,
			TargetPath:   ref.TargetPath,
			ExternalName: ref.External,
			Member:       ref.Member,
		})
	}

	if err := idx.db.StoreFileSymbols(fileID, projectRoot(filePath), defs, refs); err != nil {
		return err
	}
	slog.Debug("Stored symbols", "file_id", fileID, "definitions", len(defs), "references", len(refs))
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

// projectRoot returns the directory of the repository a file belongs to, or
// an empty string when it is not in one
func projectRoot(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
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

// processBatch generates embeddings for a batch of chunks and stores them
// It returns the IDs of the inserted chunks, in order
func (idx *Indexer) processBatch(ctx context.Context, chunks []Chunk, fileID int64) ([]int64, error) {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = llm.DocumentText(idx.config.EmbedModel, chunk.embedText())
	}

	slog.Debug("Generating embeddings", "chunks", len(chunks))
	embeddings, err := idx.ollama.Embed(ctx, idx.config.EmbedModel, texts, 5)
	if err != nil {
		slog.Error("Failed to generate embeddings", "error", err)
		return nil, fmt.Errorf("failed to generate embeddings: %w", err)
	}
	slog.Debug("Embeddings generated", "count", len(embeddings))

	var chunkIDs []int64
	for i, chunk := range chunks {
		chunkID, err := idx.db.InsertChunk(chunk.FileID, chunk.Content, embeddings[i], chunk.StartLine, chunk.EndLine)
		if err != nil {
			slog.Error("Failed to insert chunk", "error", err, "chunk_index", i)
			return nil, fmt.Errorf("failed to insert chunk: %w", err)
		}
		chunkIDs = append(chunkIDs, chunkID)
		slog.Debug("Inserted chunk", "chunk_id", chunkID, "file_id", chunk.FileID, "start_line", chunk.StartLine)
	}
	slog.Debug("Inserted chunks", "count", len(chunkIDs))

	return chunkIDs, nil
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

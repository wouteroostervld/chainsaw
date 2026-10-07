// Package indexer turns source files into the code graph: it parses a file,
// stores the chunks it folds into, and records its definitions, calls and
// imports.
package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/gotypes"
)

// Config holds indexer configuration
type Config struct {
	GoTypes bool // Resolve the calls in Go files with the Go type checker
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{GoTypes: true}
}

// Indexer coordinates the indexing pipeline
type Indexer struct {
	config  *Config
	db      db.Database
	goTypes *gotypes.Resolver
}

// IndexResult contains statistics about an indexing operation
type IndexResult struct {
	FilesProcessed int
	Duration       time.Duration
	Errors         []error
}

// New creates a new indexer
func New(cfg *Config, database db.Database) *Indexer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Indexer{
		config:  cfg,
		db:      database,
		goTypes: gotypes.NewResolver(),
	}
}

// IndexFile indexes a single file. A file in a language that cannot be parsed
// is registered but contributes nothing to the graph.
func (idx *Indexer) IndexFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	// Nanoseconds, so an edit within the second it was indexed is still seen
	fileID, err := idx.db.UpsertFile(filePath, info.ModTime().UnixNano(), hash)
	if err != nil {
		return fmt.Errorf("failed to upsert file: %w", err)
	}

	if err := idx.db.DeleteChunksForFile(fileID); err != nil {
		return fmt.Errorf("failed to delete old chunks: %w", err)
	}

	parsed, ok := chunker.Parse(filePath, content, 0)
	if !ok {
		slog.Debug("Not a parseable file", "file", filePath)
		return nil
	}

	chunkIDs := make([]int64, len(parsed.Chunks))
	for i, c := range parsed.Chunks {
		if chunkIDs[i], err = idx.db.InsertChunk(fileID, c.StartLine, c.EndLine); err != nil {
			return fmt.Errorf("failed to insert chunk: %w", err)
		}
	}
	slog.Debug("Stored chunks", "file", filePath, "count", len(chunkIDs))

	// Documents fold into chunks but define no symbols
	if parsed.Markup {
		return nil
	}

	if parsed.Language == "go" && idx.config.GoTypes {
		// Without it the calls stay as the syntax tree gives them
		if err := idx.goTypes.Apply(parsed, filePath); err != nil {
			slog.Debug("Go type check failed", "file", filePath, "error", err)
		}
	}

	if err := idx.storeSymbols(fileID, filePath, parsed, chunkIDs); err != nil {
		return fmt.Errorf("failed to store symbols: %w", err)
	}
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

	result.Duration = time.Since(startTime)
	return result, nil
}

// storeSymbols stores the definitions, calls and imports of a parsed file as
// graph entities and edges
func (idx *Indexer) storeSymbols(fileID int64, filePath string, parsed *chunker.File, chunkIDs []int64) error {
	// The module comes first; it defines everything at file level and is the
	// source of imports and of calls outside any definition
	defs := make([]db.SymbolDef, 0, len(parsed.Symbols)+1)
	defs = append(defs, db.SymbolDef{
		Name:       parsed.Module,
		EntityType: chunker.TypePackage,
		ChunkID:    chunkIDs[0],
		Parent:     -1,
		StartLine:  parsed.Chunks[0].StartLine,
		EndLine:    parsed.Chunks[0].EndLine,
	})
	for _, sym := range parsed.Symbols {
		defs = append(defs, db.SymbolDef{
			Name:       sym.Name,
			Container:  sym.Container,
			Signature:  sym.Signature,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			EntityType: sym.Type,
			ChunkID:    chunkIDs[sym.Chunk],
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

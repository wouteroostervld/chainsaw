package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/embed"
)

// EmbeddingStore is where the embeddings module keeps what it derives from a
// file
type EmbeddingStore interface {
	InsertEmbeddings(fileID int64, model string, chunks []db.EmbeddingChunk) error
	DeleteEmbeddings(fileID int64) error
}

// Embedder turns texts into vectors
type Embedder interface {
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ErrNoEmbedder says that embeddings are switched on without a model to make
// them with
var ErrNoEmbedder = errors.New("embeddings are switched on, but no model is configured to make them")

// Embeddings is the module that makes search by meaning possible: a file is
// cut along its syntax tree, as for the graph, and a model gives each chunk
// a vector. That is a request to a model for every file, which makes this
// the one kind of index that is slow and can cost money.
type Embeddings struct {
	MaxChunkSize int // Bytes of a chunk before it is folded further, or cut
	MinChunkSize int // Chunks with less text than this say too little to be found
	BatchSize    int // Chunks sent to the model in one request

	store    EmbeddingStore
	embedder Embedder
}

// NewEmbeddings creates the embeddings module. Without an embedder it can
// forget but not index.
func NewEmbeddings(store EmbeddingStore, embedder Embedder) *Embeddings {
	return &Embeddings{MaxChunkSize: 4096, MinChunkSize: 10, BatchSize: 32, store: store, embedder: embedder}
}

func (e *Embeddings) Name() string { return config.IndexEmbeddings }

func (e *Embeddings) Ready() error {
	if e.embedder == nil {
		return ErrNoEmbedder
	}
	return nil
}

func (e *Embeddings) Forget(fileID int64) error {
	return e.store.DeleteEmbeddings(fileID)
}

// Index embeds the chunks of a file. What was stored for it stays until the
// model has answered for all of them.
func (e *Embeddings) Index(ctx context.Context, src *Source) error {
	if err := e.Ready(); err != nil {
		return err
	}

	var chunks []db.EmbeddingChunk
	var texts []string
	if parsed, ok := src.Parse(e.MaxChunkSize); ok {
		model := e.embedder.Model()
		where := displayPath(src.Path)
		for _, c := range parsed.Chunks {
			// A fragment is found by what the whole is about when it says
			// which file and which definition it is from
			context := where
			if c.Title != "" {
				context += ": " + c.Title
			}
			for _, p := range e.pieces(c) {
				chunks = append(chunks, db.EmbeddingChunk{StartLine: p.startLine, EndLine: p.endLine, Title: c.Title})
				texts = append(texts, embed.DocumentText(model, context+"\n\n"+p.text))
			}
		}
	}

	for i := 0; i < len(texts); i += e.BatchSize {
		end := min(i+e.BatchSize, len(texts))
		vectors, err := e.embedder.Embed(ctx, texts[i:end])
		if err != nil {
			return err
		}
		if len(vectors) != end-i {
			return fmt.Errorf("got %d embeddings for %d chunks", len(vectors), end-i)
		}
		for j, v := range vectors {
			chunks[i+j].Vector = v
		}
	}

	if err := e.store.DeleteEmbeddings(src.FileID); err != nil {
		return err
	}
	return e.store.InsertEmbeddings(src.FileID, e.embedder.Model(), chunks)
}

type piece struct {
	text               string
	startLine, endLine int
}

// pieces returns what to embed of a chunk: the chunk, or when it is still
// too large after folding, runs of its lines that fit
func (e *Embeddings) pieces(c chunker.Chunk) []piece {
	if len(strings.TrimSpace(c.Content)) < e.MinChunkSize {
		return nil
	}
	if len(c.Content) <= e.MaxChunkSize {
		return []piece{{c.Content, c.StartLine, c.EndLine}}
	}

	var pieces []piece
	lines := strings.SplitAfter(strings.TrimSuffix(c.Content, "\n"), "\n")
	for first := 0; first < len(lines); {
		size, next := 0, first
		for next < len(lines) && (next == first || size+len(lines[next]) <= e.MaxChunkSize) {
			size += len(lines[next])
			next++
		}
		text := strings.Join(lines[first:next], "")
		if len(text) > e.MaxChunkSize {
			// One line that is too long by itself; it is not prose or code
			// anyone searches for
			text = text[:e.MaxChunkSize]
		}
		p := piece{text, c.StartLine, c.EndLine}
		if c.Verbatim {
			// Nothing was folded, so the lines are those of the file
			p.startLine, p.endLine = c.StartLine+first, c.StartLine+next-1
		}
		if len(strings.TrimSpace(text)) >= e.MinChunkSize {
			pieces = append(pieces, p)
		}
		first = next
	}
	return pieces
}

// displayPath returns a file's path as it is known in its project
func displayPath(filePath string) string {
	if root := projectRoot(filePath); root != "" {
		if rel, err := filepath.Rel(filepath.Dir(root), filePath); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Join(filepath.Base(filepath.Dir(filePath)), filepath.Base(filePath))
}

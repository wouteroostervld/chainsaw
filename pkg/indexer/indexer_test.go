package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLLM returns fixed-size embeddings
type fakeLLM struct{}

func (fakeLLM) Embed(ctx context.Context, model string, texts []string, concurrency int) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = make([]float32, 4)
	}
	return embeddings, nil
}

func TestChunkContent(t *testing.T) {
	cfg := &Config{
		ChunkSize:    20,
		ChunkOverlap: 5,
		MinChunkSize: 5,
		MaxChunkSize: 100,
	}
	idx := &Indexer{config: cfg}

	// Chunks are aligned to line boundaries
	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name:      "short content",
			content:   "test",
			wantCount: 0, // Below MinChunkSize
		},
		{
			name:      "single chunk",
			content:   "one small line\n",
			wantCount: 1,
		},
		{
			name:      "multiple chunks",
			content:   "first line\nsecond line\nthird line\nfourth line\nfifth line\n",
			wantCount: 3, // With overlap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := idx.chunkContent(tt.content, 1, "test.txt")
			if len(chunks) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(chunks), tt.wantCount)
			}

			for i, chunk := range chunks {
				if chunk.FileID != 1 {
					t.Errorf("chunk %d: fileID = %d, want 1", i, chunk.FileID)
				}
				if len(chunk.Content) < cfg.MinChunkSize {
					t.Errorf("chunk %d: content length %d < min %d", i, len(chunk.Content), cfg.MinChunkSize)
				}
				if !strings.HasSuffix(chunk.Content, "\n") || chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine {
					t.Errorf("chunk %d: not whole lines: %q (lines %d-%d)", i, chunk.Content, chunk.StartLine, chunk.EndLine)
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exact length", 12, "exact length"},
		{"this is a very long string", 10, "this is a ..."},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestIndexFile_WithMock(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")
	content := `package main

func main() {
println("test")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	mockDB := NewMockDatabase()
	mockOllama := fakeLLM{}

	cfg := DefaultConfig()
	cfg.ChunkSize = 50

	idx := New(cfg, mockDB, mockOllama)
	ctx := context.Background()

	if err := idx.IndexFile(ctx, testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	if mockDB.UpsertFileCount == 0 {
		t.Error("expected UpsertFile to be called")
	}

	if mockDB.InsertChunkCount == 0 {
		t.Error("expected InsertChunk to be called")
	}
}

func TestIndexDirectory(t *testing.T) {
	tempDir := t.TempDir()

	files := map[string]string{
		"file1.txt": "content 1",
		"file2.go":  "package main\nfunc main() {}",
	}

	for name, content := range files {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	mockDB := NewMockDatabase()
	mockOllama := fakeLLM{}

	cfg := DefaultConfig()
	idx := New(cfg, mockDB, mockOllama)
	ctx := context.Background()

	result, err := idx.IndexDirectory(ctx, tempDir, func(path string) bool {
		return filepath.Ext(path) == ".go"
	})
	if err != nil {
		t.Fatalf("IndexDirectory failed: %v", err)
	}

	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.FilesProcessed)
	}
}

func TestIndexFile_Semantic(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "calc.py")
	content := `import math


# Adds two numbers.
def add(a, b):
    total = a + b
    total = total + 0
    total = int(total)
    return total


class Calc:
    def area(self, r):
        sq = self.square(r)
        scale = math.pi
        result = scale * sq
        return result

    def square(self, x):
        y = add(x, 0)
        y = y * y
        y = y + 0
        return y
`
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	mockDB := NewMockDatabase()
	idx := New(DefaultConfig(), mockDB, fakeLLM{})
	if err := idx.IndexFile(context.Background(), testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	// Skeleton, add, Calc, Calc.area, Calc.square
	if len(mockDB.Chunks) != 5 {
		t.Fatalf("got %d chunks, want 5", len(mockDB.Chunks))
	}
	skeleton := mockDB.Chunks[0].ContentSnippet
	for _, stub := range []string{"def add(a, b): # ... chunk: lines 4-9", "class Calc: # ... chunk: lines 12-23"} {
		if !strings.Contains(skeleton, stub) {
			t.Errorf("skeleton does not contain %q:\n%s", stub, skeleton)
		}
	}
	if strings.Contains(skeleton, "total = a + b") {
		t.Errorf("skeleton contains a function body:\n%s", skeleton)
	}
	if add := mockDB.Chunks[1]; add.StartLine != 4 || add.EndLine != 9 || !strings.Contains(add.ContentSnippet, "total = a + b") {
		t.Errorf("add chunk = lines %d-%d:\n%s", add.StartLine, add.EndLine, add.ContentSnippet)
	}

	// What is embedded says where the chunk is from
	if got, want := idx.semanticContext(t, testFile, 1), "calc.py: add"; !strings.HasSuffix(got, want) {
		t.Errorf("context of add = %q, want it to end in %q", got, want)
	}

	// Each definition is an entity in the chunk that holds it; the module comes first
	wantDefs := []struct {
		name, container, etype string
		chunk                  int64
		parent                 int
	}{
		{"calc", "", "PACKAGE", 1, -1},
		{"add", "", "FUNCTION", 2, 0},
		{"Calc", "", "CLASS", 3, 0},
		{"area", "Calc", "METHOD", 4, 2},
		{"square", "Calc", "METHOD", 5, 2},
	}
	if len(mockDB.SymbolDefs) != len(wantDefs) {
		t.Fatalf("got %d definitions, want %d: %+v", len(mockDB.SymbolDefs), len(wantDefs), mockDB.SymbolDefs)
	}
	for i, want := range wantDefs {
		got := mockDB.SymbolDefs[i]
		if got.Name != want.name || got.Container != want.container || got.EntityType != want.etype ||
			got.ChunkID != want.chunk || got.Parent != want.parent {
			t.Errorf("definition %d = %+v, want %+v", i, got, want)
		}
	}

	calls := map[string]bool{}
	for _, ref := range mockDB.SymbolRefs {
		calls[mockDB.SymbolDefs[ref.Source].Name+" "+ref.RelationType+" "+ref.ExternalName] = true
	}
	for _, want := range []string{"calc imports math", "area calls square", "square calls add"} {
		if !calls[want] {
			t.Errorf("missing reference %q in %v", want, calls)
		}
	}
}

func TestIndexFile_OversizedDefinition(t *testing.T) {
	var body strings.Builder
	body.WriteString("package big\n\nfunc Big() int {\n\tn := 0\n")
	for i := 0; i < 40; i++ {
		body.WriteString("\tn += 12345\n")
	}
	body.WriteString("\treturn n\n}\n")

	testFile := filepath.Join(t.TempDir(), "big.go")
	if err := os.WriteFile(testFile, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.ChunkSize = 200
	cfg.ChunkOverlap = 20
	cfg.MaxChunkSize = 300

	mockDB := NewMockDatabase()
	idx := New(cfg, mockDB, fakeLLM{})
	if err := idx.IndexFile(context.Background(), testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	// The skeleton, plus the function split into blocks that keep real line numbers
	if len(mockDB.Chunks) < 3 {
		t.Fatalf("got %d chunks, want the function split into several", len(mockDB.Chunks))
	}
	for _, c := range mockDB.Chunks[1:] {
		if len(c.ContentSnippet) > cfg.MaxChunkSize {
			t.Errorf("chunk of %d bytes exceeds the maximum of %d", len(c.ContentSnippet), cfg.MaxChunkSize)
		}
		if c.StartLine < 3 || c.EndLine > 46 || c.StartLine > c.EndLine {
			t.Errorf("block has lines %d-%d, want within 3-46", c.StartLine, c.EndLine)
		}
	}
	if first := mockDB.Chunks[1]; first.StartLine != 3 {
		t.Errorf("first block starts at line %d, want 3", first.StartLine)
	}

	// The outline keeps the function whole and knows all of its blocks
	if len(mockDB.Outline) != 2 {
		t.Fatalf("got %d outline nodes, want 2", len(mockDB.Outline))
	}
	if fn := mockDB.Outline[1]; fn.Parent != 0 || len(fn.ChunkIDs) != len(mockDB.Chunks)-1 ||
		!strings.HasPrefix(fn.Content, "func Big() int {") || !strings.HasSuffix(fn.Content, "return n\n}\n") {
		t.Errorf("function node has %d chunks, parent %d:\n%s", len(fn.ChunkIDs), fn.Parent, fn.Content)
	}

	// The function's entity points at its first block
	if def := mockDB.SymbolDefs[1]; def.Name != "Big" || def.ChunkID != mockDB.Chunks[1].ChunkID {
		t.Errorf("Big = %+v, want chunk %d", def, mockDB.Chunks[1].ChunkID)
	}
}

// semanticContext returns the context of the nth chunk a file is split into
func (idx *Indexer) semanticContext(t *testing.T, path string, n int) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	chunks, _, _ := idx.chunkFile(content, 1, path)
	return chunks[n].Context
}

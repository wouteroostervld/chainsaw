package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wouteroostervld/chainsaw/pkg/ollama"
)

func TestChunkContent(t *testing.T) {
	cfg := &Config{
		ChunkSize:    20,
		ChunkOverlap: 5,
		MinChunkSize: 5,
		MaxChunkSize: 100,
	}
	idx := &Indexer{config: cfg}

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
			content:   "This is a test content.",
			wantCount: 2, // With overlap
		},
		{
			name:      "multiple chunks",
			content:   "This is a longer test content that should be split into multiple chunks for processing.",
			wantCount: 6, // With overlap
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
	mockOllama := ollama.NewMockClient()

	cfg := DefaultConfig()
	cfg.ChunkSize = 50
	cfg.EnableGraphMode = false

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
	mockOllama := ollama.NewMockClient()

	cfg := DefaultConfig()
	cfg.EnableGraphMode = false
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

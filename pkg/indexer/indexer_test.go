package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexFile(t *testing.T) {
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
	idx := New(nil, mockDB)
	if err := idx.IndexFile(context.Background(), testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if mockDB.UpsertFileCount != 1 || mockDB.DeleteChunksCount != 1 {
		t.Errorf("file upserted %d times, old chunks deleted %d times, want 1 and 1", mockDB.UpsertFileCount, mockDB.DeleteChunksCount)
	}

	// Where the chunks are: the file, add, Calc, Calc.area, Calc.square. Their
	// text is not stored.
	wantChunks := [][2]int{{1, 24}, {4, 9}, {12, 23}, {13, 17}, {19, 23}}
	if len(mockDB.Chunks) != len(wantChunks) {
		t.Fatalf("got %d chunks, want %d", len(mockDB.Chunks), len(wantChunks))
	}
	for i, want := range wantChunks {
		if c := mockDB.Chunks[i]; c.StartLine != want[0] || c.EndLine != want[1] {
			t.Errorf("chunk %d = lines %d-%d, want %d-%d", i, c.StartLine, c.EndLine, want[0], want[1])
		}
	}

	// Each definition is an entity in the chunk that holds it; the module comes first
	wantDefs := []struct {
		name, container, etype, signature string
		chunk                             int64
		parent, start, end                int
	}{
		{"calc", "", "PACKAGE", "", 1, -1, 1, 24},
		{"add", "", "FUNCTION", "def add(a, b):", 2, 0, 4, 9},
		{"Calc", "", "CLASS", "class Calc:", 3, 0, 12, 23},
		{"area", "Calc", "METHOD", "def area(self, r):", 4, 2, 13, 17},
		{"square", "Calc", "METHOD", "def square(self, x):", 5, 2, 19, 23},
	}
	if len(mockDB.SymbolDefs) != len(wantDefs) {
		t.Fatalf("got %d definitions, want %d: %+v", len(mockDB.SymbolDefs), len(wantDefs), mockDB.SymbolDefs)
	}
	for i, want := range wantDefs {
		got := mockDB.SymbolDefs[i]
		if got.Name != want.name || got.Container != want.container || got.EntityType != want.etype ||
			got.Signature != want.signature || got.ChunkID != want.chunk || got.Parent != want.parent ||
			got.StartLine != want.start || got.EndLine != want.end {
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

func TestIndexFile_Unparseable(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(testFile, []byte("just some notes\n"), 0600); err != nil {
		t.Fatal(err)
	}

	mockDB := NewMockDatabase()
	if err := New(nil, mockDB).IndexFile(context.Background(), testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if len(mockDB.Chunks) != 0 || len(mockDB.SymbolDefs) != 0 {
		t.Errorf("a text file produced %d chunks and %d definitions, want none", len(mockDB.Chunks), len(mockDB.SymbolDefs))
	}
}

func TestIndexFile_Markup(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "guide.md")
	content := "# Guide\n\nIntro.\n\n## Install\n\nRun it.\n\nThen wait.\n\nDone.\n"
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	mockDB := NewMockDatabase()
	if err := New(nil, mockDB).IndexFile(context.Background(), testFile); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if len(mockDB.Chunks) < 2 || len(mockDB.SymbolDefs) != 0 {
		t.Errorf("a document produced %d chunks and %d definitions, want sections and no definitions", len(mockDB.Chunks), len(mockDB.SymbolDefs))
	}
}

func TestIndexDirectory(t *testing.T) {
	tempDir := t.TempDir()
	for name, content := range map[string]string{
		"file1.txt": "content 1",
		"file2.go":  "package main\nfunc main() {}",
	} {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := New(nil, NewMockDatabase()).IndexDirectory(context.Background(), tempDir, func(path string) bool {
		return filepath.Ext(path) == ".go"
	})
	if err != nil {
		t.Fatalf("IndexDirectory failed: %v", err)
	}
	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.FilesProcessed)
	}
}

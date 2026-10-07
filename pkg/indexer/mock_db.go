package indexer

import (
	"github.com/wouteroostervld/chainsaw/pkg/db"
)

// MockDatabase is a simple mock for testing. It records what the indexer
// stores; operations the indexer does not use are left to the embedded
// interface and panic when called.
type MockDatabase struct {
	db.Database

	UpsertFileCount   int
	DeleteChunksCount int

	Chunks     []MockChunk
	SymbolDefs []db.SymbolDef
	SymbolRefs []db.SymbolRef
}

// MockChunk is a chunk the indexer stored
type MockChunk struct {
	ID        int64
	StartLine int
	EndLine   int
}

func NewMockDatabase() *MockDatabase {
	return &MockDatabase{}
}

func (m *MockDatabase) UpsertFile(path string, modTime int64, contentHash string) (int64, error) {
	m.UpsertFileCount++
	return 1, nil
}

func (m *MockDatabase) InsertChunk(fileID int64, startLine, endLine int) (int64, error) {
	id := int64(len(m.Chunks) + 1)
	m.Chunks = append(m.Chunks, MockChunk{ID: id, StartLine: startLine, EndLine: endLine})
	return id, nil
}

func (m *MockDatabase) DeleteChunksForFile(fileID int64) error {
	m.DeleteChunksCount++
	return nil
}

func (m *MockDatabase) StoreFileSymbols(fileID int64, root string, defs []db.SymbolDef, refs []db.SymbolRef) error {
	m.SymbolDefs = append(m.SymbolDefs, defs...)
	m.SymbolRefs = append(m.SymbolRefs, refs...)
	return nil
}

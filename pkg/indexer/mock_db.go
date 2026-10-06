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
	InsertChunkCount  int
	DeleteChunksCount int
	HasChangedResult  bool

	Chunks          []*db.Chunk
	Outline         []db.OutlineNode
	SymbolDefs      []db.SymbolDef
	SymbolRefs      []db.SymbolRef
	StoreSymbolsErr error
}

func NewMockDatabase() *MockDatabase {
	return &MockDatabase{
		HasChangedResult: true, // Default to file changed
	}
}

func (m *MockDatabase) UpsertFile(path string, modTime int64, contentHash string) (int64, error) {
	m.UpsertFileCount++
	return 1, nil
}

func (m *MockDatabase) HasFileChanged(path string, modTime int64, contentHash string) (bool, error) {
	return m.HasChangedResult, nil
}

func (m *MockDatabase) InsertChunk(fileID int64, contentSnippet string, embedding []float32, startLine, endLine int) (int64, error) {
	m.InsertChunkCount++
	id := int64(m.InsertChunkCount)
	m.Chunks = append(m.Chunks, &db.Chunk{
		ChunkID:        id,
		FileID:         fileID,
		ContentSnippet: contentSnippet,
		StartLine:      startLine,
		EndLine:        endLine,
	})
	return id, nil
}

func (m *MockDatabase) DeleteChunksForFile(fileID int64) error {
	m.DeleteChunksCount++
	return nil
}

func (m *MockDatabase) StoreFileSymbols(fileID int64, root string, defs []db.SymbolDef, refs []db.SymbolRef) error {
	m.SymbolDefs = append(m.SymbolDefs, defs...)
	m.SymbolRefs = append(m.SymbolRefs, refs...)
	return m.StoreSymbolsErr
}

func (m *MockDatabase) StoreFileOutline(fileID int64, nodes []db.OutlineNode) error {
	m.Outline = append(m.Outline, nodes...)
	return nil
}

package indexer

import (
	"github.com/wouteroostervld/chainsaw/pkg/db"
)

// MockDatabase is a simple mock for testing
type MockDatabase struct {
	UpsertFileCount   int
	InsertChunkCount  int
	DeleteChunksCount int
	HasChangedResult  bool
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

func (m *MockDatabase) InsertChunk(fileID int64, contentSnippet string, embedding []float32) (int64, error) {
	m.InsertChunkCount++
	return int64(m.InsertChunkCount), nil
}

func (m *MockDatabase) DeleteChunksForFile(fileID int64) error {
	m.DeleteChunksCount++
	return nil
}

func (m *MockDatabase) UpsertEdge(sourceChunkID, targetChunkID int64, weight float64, relationType, model string) error {
	return nil
}

// Stub implementations for required interface methods
func (m *MockDatabase) Close() error                                           { return nil }
func (m *MockDatabase) HealthCheck() error                                     { return nil }
func (m *MockDatabase) Path() string                                           { return "" }
func (m *MockDatabase) EmbeddingDim() int                                      { return 384 }
func (m *MockDatabase) GetMeta(key string) (string, error)                     { return "", nil }
func (m *MockDatabase) SetMeta(key, value string) error                        { return nil }
func (m *MockDatabase) GetFile(path string) (*db.File, error)                  { return nil, nil }
func (m *MockDatabase) GetFileByID(id int64) (*db.File, error)                 { return nil, nil }
func (m *MockDatabase) DeleteFile(path string) error                           { return nil }
func (m *MockDatabase) ListFiles(opts db.ListFilesOptions) ([]*db.File, error) { return nil, nil }
func (m *MockDatabase) CountFiles() (int64, error)                             { return 0, nil }
func (m *MockDatabase) GetChunk(chunkID int64) (*db.Chunk, error)              { return nil, nil }
func (m *MockDatabase) GetChunksForFile(fileID int64) ([]*db.Chunk, error)     { return nil, nil }
func (m *MockDatabase) SearchSimilar(queryEmbedding []float32, limit int) ([]*db.SearchResult, error) {
	return nil, nil
}
func (m *MockDatabase) CountChunks() (int64, error) { return 0, nil }
func (m *MockDatabase) GetNeighbors(opts db.GetNeighborsOptions) ([]*db.Neighbor, error) {
	return nil, nil
}
func (m *MockDatabase) DeleteEdgesForChunk(chunkID int64) error { return nil }
func (m *MockDatabase) CountEdges() (int64, error)              { return 0, nil }

// Entity operations
func (m *MockDatabase) UpsertEntity(name, entityType string, chunkID int64) (int64, error) {
	return 1, nil
}
func (m *MockDatabase) UpsertEntityEdge(sourceID, targetID int64, relationType string, chunkID int64) error {
	return nil
}
func (m *MockDatabase) GetEntityByName(name string) ([]*db.Entity, error) { return nil, nil }
func (m *MockDatabase) GetEntityEdges(entityID int64) ([]*db.EntityEdge, error) {
	return nil, nil
}
func (m *MockDatabase) GetEntitiesByType(entityType string) ([]*db.Entity, error) {
	return nil, nil
}
func (m *MockDatabase) FindRelatedEntities(entityID int64, relationType string) ([]*db.Entity, error) {
	return nil, nil
}

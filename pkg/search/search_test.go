package search

import (
	"context"
	"testing"

	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/ollama"
)

// MockDB for testing
type MockDB struct{}

func (m *MockDB) SearchSimilar(embedding []float32, limit int) ([]*db.SearchResult, error) {
	return []*db.SearchResult{
		{
			Chunk:    &db.Chunk{ChunkID: 1, FileID: 1, ContentSnippet: "test snippet"},
			Distance: 0.05,
		},
		{
			Chunk:    &db.Chunk{ChunkID: 2, FileID: 2, ContentSnippet: "another snippet"},
			Distance: 0.15,
		},
	}, nil
}

func (m *MockDB) GetFileByID(id int64) (*db.File, error) {
	return &db.File{ID: id, Path: "/test/file.go"}, nil
}

func (m *MockDB) GetChunk(chunkID int64) (*db.Chunk, error) {
	return &db.Chunk{ChunkID: chunkID, ContentSnippet: "neighbor snippet"}, nil
}

func (m *MockDB) GetNeighbors(opts db.GetNeighborsOptions) ([]*db.Neighbor, error) {
	return []*db.Neighbor{
		{ChunkID: 3, RelationType: "calls", TotalWeight: 0.8},
	}, nil
}

// Stub implementations
func (m *MockDB) Close() error                       { return nil }
func (m *MockDB) HealthCheck() error                 { return nil }
func (m *MockDB) Path() string                       { return "" }
func (m *MockDB) EmbeddingDim() int                  { return 384 }
func (m *MockDB) GetMeta(key string) (string, error) { return "", nil }
func (m *MockDB) SetMeta(key, value string) error    { return nil }
func (m *MockDB) UpsertFile(path string, modTime int64, contentHash string) (int64, error) {
	return 0, nil
}
func (m *MockDB) GetFile(path string) (*db.File, error)                  { return nil, nil }
func (m *MockDB) DeleteFile(path string) error                           { return nil }
func (m *MockDB) ListFiles(opts db.ListFilesOptions) ([]*db.File, error) { return nil, nil }
func (m *MockDB) CountFiles() (int64, error)                             { return 0, nil }
func (m *MockDB) HasFileChanged(path string, modTime int64, contentHash string) (bool, error) {
	return false, nil
}
func (m *MockDB) InsertChunk(fileID int64, contentSnippet string, embedding []float32) (int64, error) {
	return 0, nil
}
func (m *MockDB) GetChunksForFile(fileID int64) ([]*db.Chunk, error) { return nil, nil }
func (m *MockDB) DeleteChunksForFile(fileID int64) error             { return nil }
func (m *MockDB) CountChunks() (int64, error)                        { return 0, nil }
func (m *MockDB) UpsertEdge(sourceChunkID, targetChunkID int64, weight float64, relationType, model string) error {
	return nil
}
func (m *MockDB) GetEdge(sourceChunkID, targetChunkID int64) (*db.Edge, error) { return nil, nil }
func (m *MockDB) GetOutgoingEdges(sourceChunkID int64) ([]*db.Edge, error)     { return nil, nil }
func (m *MockDB) GetIncomingEdges(targetChunkID int64) ([]*db.Edge, error)     { return nil, nil }
func (m *MockDB) DeleteEdge(sourceChunkID, targetChunkID int64) error          { return nil }
func (m *MockDB) DeleteEdgesForChunk(chunkID int64) error                      { return nil }
func (m *MockDB) DeleteEdgesByModel(model string) (int64, error)               { return 0, nil }
func (m *MockDB) CountEdges() (int64, error)                                   { return 0, nil }
func (m *MockDB) CountEdgesByModel(model string) (int64, error)                { return 0, nil }

func TestVectorSearch(t *testing.T) {
	mockDB := &MockDB{}
	mockOllama := ollama.NewMockClient()

	engine := New(nil, mockDB, mockOllama)
	ctx := context.Background()

	results, err := engine.VectorSearch(ctx, "test query", 10)
	if err != nil {
		t.Fatalf("VectorSearch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	if results[0].Score < 0.9 {
		t.Errorf("result[0] score = %f, want > 0.9", results[0].Score)
	}
}

func TestGraphSearch(t *testing.T) {
	mockDB := &MockDB{}
	mockOllama := ollama.NewMockClient()

	engine := New(nil, mockDB, mockOllama)
	ctx := context.Background()

	results, err := engine.GraphSearch(ctx, "test query", 10, 2)
	if err != nil {
		t.Fatalf("GraphSearch failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected results from GraphSearch")
	}

	if len(results[0].Neighbors) != 1 {
		t.Errorf("expected 1 neighbor, got %d", len(results[0].Neighbors))
	}

	if results[0].Neighbors[0].RelationType != "calls" {
		t.Errorf("neighbor relation = %s, want calls", results[0].Neighbors[0].RelationType)
	}
}

func TestHybridSearch(t *testing.T) {
	mockDB := &MockDB{}
	mockOllama := ollama.NewMockClient()

	engine := New(nil, mockDB, mockOllama)
	ctx := context.Background()

	results, err := engine.HybridSearch(ctx, "test query", 10)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected results from HybridSearch")
	}
}

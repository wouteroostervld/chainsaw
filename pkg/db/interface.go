package db

// Database is the interface for all database operations
// This enables dependency injection and mocking for testing
type Database interface {
	// Lifecycle
	Close() error
	HealthCheck() error
	Path() string
	EmbeddingDim() int

	// Metadata operations
	GetMeta(key string) (string, error)
	SetMeta(key, value string) error

	// File registry operations
	UpsertFile(path string, modTime int64, contentHash string) (int64, error)
	GetFile(path string) (*File, error)
	GetFileByID(id int64) (*File, error)
	DeleteFile(path string) error
	ListFiles(opts ListFilesOptions) ([]*File, error)
	CountFiles() (int64, error)
	HasFileChanged(path string, modTime int64, contentHash string) (bool, error)

	// Work queue operations
	MarkFilePending(path string, modTime int64, contentHash string) error
	GetPendingFiles(limit int) ([]*File, error)
	MarkFileProcessing(fileID int64) error
	MarkFileIndexed(fileID int64) error
	MarkFileFailed(fileID int64, errorMsg string, retryCount int) error
	ResetStuckProcessing() error

	// Chunk operations
	InsertChunk(fileID int64, contentSnippet string, embedding []float32, startLine, endLine int) (int64, error)
	GetChunk(chunkID int64) (*Chunk, error)
	GetChunksForFile(fileID int64) ([]*Chunk, error)
	GetAllChunks() ([]*Chunk, error)
	GetAllChunksWithPaths() ([]*ChunkWithPath, error)
	GetChunksByIDs(chunkIDs []int64) ([]*ChunkWithPath, error)
	DeleteChunksForFile(fileID int64) error
	SearchSimilar(queryEmbedding []float32, limit int, pathFilter string) ([]*SearchResult, error)
	SearchWithRelations(queryEmbedding []float32, limit int, maxDepth int, pathFilter string) ([]*SearchResult, error)
	GetChunkDistance(chunkA, chunkB int64) (float64, error)
	CountChunks() (int64, error)

	// Graph operations (legacy chunk-to-chunk, deprecated)
	GetNeighbors(opts GetNeighborsOptions) ([]*Neighbor, error)
	DeleteEdgesForChunk(chunkID int64) error
	CountEdges() (int64, error)

	// Entity operations
	UpsertEntity(name, entityType string, chunkID int64) (int64, error)
	UpsertEntityEdge(sourceID, targetID int64, relationType string, chunkID int64) error
	GetEntityByName(name string) ([]*Entity, error)
	GetEntityEdges(entityID int64) ([]*EntityEdge, error)
	GetEntitiesByType(entityType string) ([]*Entity, error)
	FindRelatedEntities(entityID int64, relationType string) ([]*Entity, error)

	// Graph extraction state tracking
	GetChunksNeedingGraphExtraction(limit int) ([]int64, error)
	MarkChunksGraphExtracted(chunkIDs []int64) error
	GetGraphExtractionStats() (total, extracted, pending int, err error)
}

// Ensure DB implements Database interface
var _ Database = (*DB)(nil)

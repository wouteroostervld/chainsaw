package db

// Database is the interface for the database operations of the indexer and
// its worker. This enables dependency injection and mocking for testing
type Database interface {
	// File registry and work queue
	UpsertFile(path string, modTime int64, contentHash string) (int64, error)
	DeleteFile(path string) error
	MarkFilePending(path string, modTime int64, contentHash string) error
	GetPendingFiles(limit int) ([]*File, error)
	MarkFileProcessing(fileID int64) error
	MarkFileIndexed(fileID int64) error
	MarkFileFailed(fileID int64, errorMsg string, retryCount int) error

	// What is derived from a file
	InsertChunk(fileID int64, startLine, endLine int) (int64, error)
	DeleteChunksForFile(fileID int64) error
	StoreFileSymbols(fileID int64, root string, defs []SymbolDef, refs []SymbolRef) error
}

// Ensure DB implements Database interface
var _ Database = (*DB)(nil)

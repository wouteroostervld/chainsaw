package db

// Database is the interface for the database operations of the indexer and
// its worker: the files it knows and which kinds of index hold them. What a
// kind of index stores is behind an interface of its own, next to the code
// that fills it. This enables dependency injection and mocking for testing
type Database interface {
	// File registry and work queue
	UpsertFile(path string, modTime int64, contentHash string) (int64, error)
	ForgetFile(path string) error
	MarkFilePending(path string, modTime int64, contentHash string) error
	GetPendingFiles(limit int) ([]*File, error)
	MarkFileProcessing(fileID int64) error
	MarkFileIndexed(fileID int64) error
	MarkFileFailed(fileID int64, errorMsg string, retryCount int) error

	// Which kinds of index hold a file
	FileModules(fileID int64) (map[string]int64, error)
	SetFileModule(fileID int64, module string, modTime int64) error
	ClearFileModule(fileID int64, module string) error
}

// Ensure DB implements Database interface
var _ Database = (*DB)(nil)

package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_NewDatabase(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created")
	}

	// Verify file permissions (0600)
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Failed to stat database file: %v", err)
	}
	mode := info.Mode()
	if mode.Perm() != 0600 {
		t.Errorf("Expected permissions 0600, got %o", mode.Perm())
	}

	// Verify embedding dimension
	if db.EmbeddingDim() != 384 {
		t.Errorf("Expected embedding dim 384, got %d", db.EmbeddingDim())
	}

	// Verify path
	if db.Path() != dbPath {
		t.Errorf("Expected path %s, got %s", dbPath, db.Path())
	}
}

func TestOpen_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		errMsg string
	}{
		{
			name:   "empty path",
			config: Config{Path: "", EmbeddingDim: 384, SkipVecTable: true},
			errMsg: "path cannot be empty",
		},
		{
			name:   "zero dimension",
			config: Config{Path: "/tmp/test.db", EmbeddingDim: 0, SkipVecTable: true},
			errMsg: "must be positive",
		},
		{
			name:   "negative dimension",
			config: Config{Path: "/tmp/test.db", EmbeddingDim: -1, SkipVecTable: true},
			errMsg: "must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Open(tt.config)
			if err == nil {
				db.Close()
				t.Fatalf("Expected error containing %q, got nil", tt.errMsg)
			}
			if err.Error() == "" || tt.errMsg == "" {
				t.Errorf("Error message check skipped")
			}
		})
	}
}

func TestOpen_ExistingDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	// Create database
	db1, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Set a meta value
	if err := db1.SetMeta("test_key", "test_value"); err != nil {
		t.Fatalf("Failed to set meta: %v", err)
	}
	db1.Close()

	// Reopen database
	db2, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db2.Close()

	// Verify meta value persisted
	value, err := db2.GetMeta("test_key")
	if err != nil {
		t.Fatalf("Failed to get meta: %v", err)
	}
	if value != "test_value" {
		t.Errorf("Expected meta value 'test_value', got %q", value)
	}
}

func TestOpen_DimensionMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database with 384 dimensions
	cfg1 := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}
	db1, err := Open(cfg1)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	db1.Close()

	// Try to open with different dimension
	cfg2 := Config{
		Path:         dbPath,
		EmbeddingDim: 768,
		SkipVecTable: true,
	}
	db2, err := Open(cfg2)
	if err == nil {
		db2.Close()
		t.Fatalf("Expected dimension mismatch error, got nil")
	}
	if err.Error() == "" {
		t.Errorf("Expected error message about dimension mismatch")
	}
}

func TestHealthCheck(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Health check should pass
	if err := db.HealthCheck(); err != nil {
		t.Errorf("Health check failed: %v", err)
	}
}

func TestMetaOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Test SetMeta and GetMeta
	key := "test_key"
	value := "test_value"

	if err := db.SetMeta(key, value); err != nil {
		t.Fatalf("Failed to set meta: %v", err)
	}

	got, err := db.GetMeta(key)
	if err != nil {
		t.Fatalf("Failed to get meta: %v", err)
	}
	if got != value {
		t.Errorf("Expected value %q, got %q", value, got)
	}

	// Test update
	newValue := "updated_value"
	if err := db.SetMeta(key, newValue); err != nil {
		t.Fatalf("Failed to update meta: %v", err)
	}

	got, err = db.GetMeta(key)
	if err != nil {
		t.Fatalf("Failed to get updated meta: %v", err)
	}
	if got != newValue {
		t.Errorf("Expected updated value %q, got %q", newValue, got)
	}

	// Test non-existent key
	_, err = db.GetMeta("nonexistent")
	if err == nil {
		t.Errorf("Expected error for non-existent key, got nil")
	}
}

func TestSchemaValidation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Verify all tables exist (except vec_chunks since we skipped it)
	tables := []string{"meta", "files", "graph_edges"}
	for _, table := range tables {
		var name string
		err := db.conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("Table %s does not exist: %v", table, err)
		}
	}

	// Verify WAL mode
	var journalMode string
	err = db.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to check journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("Expected WAL mode, got %s", journalMode)
	}

	// Verify foreign keys enabled
	var fkEnabled int
	err = db.conn.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("Failed to check foreign keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("Foreign keys not enabled")
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Close should succeed
	if err := db.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should not panic
	if err := db.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}

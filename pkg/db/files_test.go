package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Insert new file
	path := "/test/file.go"
	modTime := time.Now().Unix()
	hash := "abc123"

	id1, err := db.UpsertFile(path, modTime, hash)
	if err != nil {
		t.Fatalf("Failed to insert file: %v", err)
	}
	if id1 <= 0 {
		t.Errorf("Expected positive ID, got %d", id1)
	}

	// Update same file
	newModTime := time.Now().Unix() + 1
	newHash := "def456"

	id2, err := db.UpsertFile(path, newModTime, newHash)
	if err != nil {
		t.Fatalf("Failed to update file: %v", err)
	}

	// ID should be same (update, not insert)
	if id2 != id1 {
		t.Errorf("Expected same ID after update, got %d vs %d", id1, id2)
	}

	// Verify updated values
	file, err := db.GetFile(path)
	if err != nil {
		t.Fatalf("Failed to get file: %v", err)
	}
	if file == nil {
		t.Fatal("File not found after upsert")
	}
	if file.LastModTime != newModTime {
		t.Errorf("Expected mod time %d, got %d", newModTime, file.LastModTime)
	}
	if file.ContentHash != newHash {
		t.Errorf("Expected hash %s, got %s", newHash, file.ContentHash)
	}
}

func TestGetFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Test non-existent file
	file, err := db.GetFile("/nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if file != nil {
		t.Errorf("Expected nil for non-existent file")
	}

	// Insert and retrieve file
	path := "/test/main.go"
	modTime := time.Now().Unix()
	hash := "test-hash"

	id, err := db.UpsertFile(path, modTime, hash)
	if err != nil {
		t.Fatalf("Failed to insert file: %v", err)
	}

	file, err = db.GetFile(path)
	if err != nil {
		t.Fatalf("Failed to get file: %v", err)
	}
	if file == nil {
		t.Fatal("File not found")
	}

	if file.ID != id {
		t.Errorf("Expected ID %d, got %d", id, file.ID)
	}
	if file.Path != path {
		t.Errorf("Expected path %s, got %s", path, file.Path)
	}
	if file.LastModTime != modTime {
		t.Errorf("Expected mod time %d, got %d", modTime, file.LastModTime)
	}
	if file.ContentHash != hash {
		t.Errorf("Expected hash %s, got %s", hash, file.ContentHash)
	}
	if file.IndexedAt.IsZero() {
		t.Errorf("IndexedAt should not be zero")
	}
}

func TestGetFileByID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Test non-existent ID
	file, err := db.GetFileByID(9999)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if file != nil {
		t.Errorf("Expected nil for non-existent ID")
	}

	// Insert and retrieve by ID
	path := "/test/util.go"
	id, err := db.UpsertFile(path, time.Now().Unix(), "hash")
	if err != nil {
		t.Fatalf("Failed to insert file: %v", err)
	}

	file, err = db.GetFileByID(id)
	if err != nil {
		t.Fatalf("Failed to get file by ID: %v", err)
	}
	if file == nil {
		t.Fatal("File not found")
	}
	if file.Path != path {
		t.Errorf("Expected path %s, got %s", path, file.Path)
	}
}

func TestDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	path := "/test/delete.go"
	_, err = db.UpsertFile(path, time.Now().Unix(), "hash")
	if err != nil {
		t.Fatalf("Failed to insert file: %v", err)
	}

	// Delete file
	err = db.DeleteFile(path)
	if err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// Verify deleted
	file, err := db.GetFile(path)
	if err != nil {
		t.Fatalf("Error checking deleted file: %v", err)
	}
	if file != nil {
		t.Errorf("File should be deleted")
	}

	// Delete non-existent file should error
	err = db.DeleteFile("/nonexistent")
	if err == nil {
		t.Errorf("Expected error deleting non-existent file")
	}
}

func TestListFiles(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Insert multiple files
	paths := []string{
		"/test/a.go",
		"/test/b.go",
		"/test/c.go",
		"/test/d.go",
	}

	for _, path := range paths {
		_, err := db.UpsertFile(path, time.Now().Unix(), "hash")
		if err != nil {
			t.Fatalf("Failed to insert file %s: %v", path, err)
		}
	}

	// List all
	files, err := db.ListFiles(ListFilesOptions{Limit: 100})
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}
	if len(files) != 4 {
		t.Errorf("Expected 4 files, got %d", len(files))
	}

	// Test pagination
	files, err = db.ListFiles(ListFilesOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("Failed to list files (page 1): %v", err)
	}
	if len(files) != 2 {
		t.Errorf("Expected 2 files on page 1, got %d", len(files))
	}

	files, err = db.ListFiles(ListFilesOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Failed to list files (page 2): %v", err)
	}
	if len(files) != 2 {
		t.Errorf("Expected 2 files on page 2, got %d", len(files))
	}

	// Files should be sorted by path
	if files[0].Path >= files[1].Path {
		t.Errorf("Files not sorted by path: %s >= %s", files[0].Path, files[1].Path)
	}
}

func TestCountFiles(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initial count should be 0
	count, err := db.CountFiles()
	if err != nil {
		t.Fatalf("Failed to count files: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 files, got %d", count)
	}

	// Insert files
	for i := 0; i < 5; i++ {
		_, err := db.UpsertFile(filepath.Join("/test", string(rune('a'+i))+".go"), time.Now().Unix(), "hash")
		if err != nil {
			t.Fatalf("Failed to insert file: %v", err)
		}
	}

	count, err = db.CountFiles()
	if err != nil {
		t.Fatalf("Failed to count files: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 files, got %d", count)
	}
}

func TestHasFileChanged(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	path := "/test/check.go"
	modTime := time.Now().Unix()
	hash := "original-hash"

	// New file should be marked as changed
	changed, err := db.HasFileChanged(path, modTime, hash)
	if err != nil {
		t.Fatalf("Failed to check if file changed: %v", err)
	}
	if !changed {
		t.Errorf("New file should be marked as changed")
	}

	// Insert file
	_, err = db.UpsertFile(path, modTime, hash)
	if err != nil {
		t.Fatalf("Failed to insert file: %v", err)
	}

	// Same modTime and hash - not changed
	changed, err = db.HasFileChanged(path, modTime, hash)
	if err != nil {
		t.Fatalf("Failed to check if file changed: %v", err)
	}
	if changed {
		t.Errorf("File with same modTime and hash should not be changed")
	}

	// Different modTime - changed
	changed, err = db.HasFileChanged(path, modTime+1, hash)
	if err != nil {
		t.Fatalf("Failed to check if file changed: %v", err)
	}
	if !changed {
		t.Errorf("File with different modTime should be changed")
	}

	// Different hash - changed
	changed, err = db.HasFileChanged(path, modTime, "new-hash")
	if err != nil {
		t.Fatalf("Failed to check if file changed: %v", err)
	}
	if !changed {
		t.Errorf("File with different hash should be changed")
	}
}

package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_NewDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "test.db")

	db, err := Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if db.Path() != dbPath {
		t.Errorf("Path() = %s, want %s", db.Path(), dbPath)
	}
	if version, _ := db.GetMeta(MetaKeySchemaVersion); version != SchemaVersion {
		t.Errorf("schema version = %q, want %q", version, SchemaVersion)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Database file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Database permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestOpen_InvalidConfig(t *testing.T) {
	if _, err := Open(Config{}); err == nil {
		t.Error("Open accepted an empty path")
	}
}

func TestOpen_ExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	if err := db.SetMeta("test_key", "test_value"); err != nil {
		t.Fatalf("Failed to set meta: %v", err)
	}
	db.Close()

	db, err = Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db.Close()

	if value, _ := db.GetMeta("test_key"); value != "test_value" {
		t.Errorf("meta after reopen = %q, want test_value", value)
	}
}

func TestOpen_OtherSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	if err := db.SetMeta(MetaKeySchemaVersion, "2.7.0"); err != nil {
		t.Fatalf("Failed to set meta: %v", err)
	}
	db.Close()

	if _, err := Open(Config{Path: dbPath}); err == nil {
		t.Error("Open accepted a database with another schema version")
	}
}

func TestRawQuery_Regexp(t *testing.T) {
	db, err := Open(Config{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	var matched bool
	rows, err := db.RawQuery("SELECT 'IndexFile' REGEXP '^Index[A-Z]'")
	if err != nil {
		t.Fatalf("REGEXP failed: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		rows.Scan(&matched)
	}
	if !matched {
		t.Error("REGEXP did not match")
	}
}

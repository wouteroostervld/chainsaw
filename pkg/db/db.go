package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // Enable FTS5
)

// DB wraps the SQLite database connection with our schema
type DB struct {
	conn          *sql.DB
	path          string
	embeddingDim  int
	sqliteVecPath string // Path to sqlite-vec extension
}

// Config holds database configuration
type Config struct {
	Path          string // Database file path
	EmbeddingDim  int    // Dimension of embedding vectors (e.g., 384, 768, 1024)
	SqliteVecPath string // Path to sqlite-vec shared library (empty = auto-detect)
	SkipVecTable  bool   // Skip creating vec_chunks table (for testing without sqlite-vec)
}

// Open opens or creates a database with the given configuration
func Open(cfg Config) (*DB, error) {
	// Validate config
	if cfg.Path == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}
	if cfg.EmbeddingDim <= 0 {
		return nil, fmt.Errorf("embedding dimension must be positive, got %d", cfg.EmbeddingDim)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Check if database file already exists
	dbExists := false
	if _, err := os.Stat(cfg.Path); err == nil {
		dbExists = true
	}

	// Enable sqlite-vec extension for all future connections
	sqlite_vec.Auto()

	// Open database connection with query parameters
	// _journal_mode=WAL will be set via PRAGMA after opening
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", cfg.Path)
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool (single writer, multiple readers)
	conn.SetMaxOpenConns(5)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(time.Hour)

	db := &DB{
		conn:          conn,
		path:          cfg.Path,
		embeddingDim:  cfg.EmbeddingDim,
		sqliteVecPath: cfg.SqliteVecPath,
	}

	// Initialize schema
	if err := db.initSchema(dbExists, cfg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Set file permissions to 0600 (user read/write only)
	if err := os.Chmod(cfg.Path, 0600); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set database permissions: %w", err)
	}

	return db, nil
}

// initSchema creates tables and indexes if they don't exist
func (db *DB) initSchema(dbExists bool, cfg Config) error {
	// Enable WAL mode first
	if _, err := db.conn.Exec(EnableWALMode); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Configure WAL checkpointing
	if _, err := db.conn.Exec(SetWALCheckpoint); err != nil {
		return fmt.Errorf("failed to set WAL checkpoint: %w", err)
	}

	// Enable foreign keys
	if _, err := db.conn.Exec(EnableForeignKeys); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create tables in dependency order
	schemas := []string{
		CreateMetaTable,
		CreateFilesTable,
		CreateFilesPathIndex,
		CreateFilesHashIndex,
		CreateFilesStatusIndex,
	}

	// Only create vec_chunks if not skipped (requires sqlite-vec extension)
	if !cfg.SkipVecTable {
		schemas = append(schemas, fmt.Sprintf(CreateVecChunksTableTemplate, db.embeddingDim))
		schemas = append(schemas, CreateEntitiesTableWithFK) // With FK to vec_chunks
		schemas = append(schemas, CreateEntitiesNameIndex)
		schemas = append(schemas, CreateEntitiesTypeIndex)
		schemas = append(schemas, CreateEntitiesChunkIndex)
		schemas = append(schemas, CreateGraphEdgesTableWithFK) // With FK to vec_chunks
	} else {
		// Testing mode: no vec_chunks, no FK constraints to vec_chunks
		schemas = append(schemas, CreateEntitiesTable) // Without FK to vec_chunks
		schemas = append(schemas, CreateEntitiesNameIndex)
		schemas = append(schemas, CreateEntitiesTypeIndex)
		schemas = append(schemas, CreateEntitiesChunkIndex)
		schemas = append(schemas, CreateGraphEdgesTable) // Without FK to vec_chunks
	}

	schemas = append(schemas,
		CreateGraphSourceIndex,
		CreateGraphTargetIndex,
		CreateGraphRelationIndex,
		CreateChunkGraphStateTable,
		CreateChunkGraphStateIndex,
	)

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, schema := range schemas {
		if _, err := tx.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// Initialize or validate metadata
	if !dbExists {
		// New database - set initial metadata
		now := time.Now().UTC().Format(time.RFC3339)
		metaInserts := map[string]string{
			MetaKeySchemaVersion: SchemaVersion,
			MetaKeyCreatedAt:     now,
			MetaKeyEmbeddingDim:  fmt.Sprintf("%d", db.embeddingDim),
		}

		for key, value := range metaInserts {
			_, err := tx.Exec("INSERT INTO meta (key, value) VALUES (?, ?)", key, value)
			if err != nil {
				return fmt.Errorf("failed to insert meta %s: %w", key, err)
			}
		}
	} else {
		// Existing database - check for migration
		var currentVersion string
		err := tx.QueryRow("SELECT value FROM meta WHERE key = ?", MetaKeySchemaVersion).Scan(&currentVersion)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to read schema version: %w", err)
		}

		// Migrate from 2.1.0 to 2.2.0 (add work queue columns)
		if currentVersion == "2.1.0" {
			if err := db.migrateToV2_2(tx); err != nil {
				return fmt.Errorf("migration to 2.2.0 failed: %w", err)
			}
		}

		// Validate embedding dimension
		var storedDim string
		err = tx.QueryRow("SELECT value FROM meta WHERE key = ?", MetaKeyEmbeddingDim).Scan(&storedDim)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to read embedding dimension: %w", err)
		}
		if err == nil && storedDim != fmt.Sprintf("%d", db.embeddingDim) {
			return fmt.Errorf("embedding dimension mismatch: database has %s, config has %d", storedDim, db.embeddingDim)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema transaction: %w", err)
	}

	return nil
}

// migrateToV2_2 adds work queue columns to files table
func (db *DB) migrateToV2_2(tx *sql.Tx) error {
	// Check if migration is needed
	var hasStatus int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('files') WHERE name='status'`).Scan(&hasStatus)
	if err != nil {
		return fmt.Errorf("failed to check for status column: %w", err)
	}

	if hasStatus > 0 {
		// Already migrated
		return nil
	}

	// Add new columns
	alterStmts := []string{
		"ALTER TABLE files ADD COLUMN status TEXT DEFAULT 'indexed'",
		"ALTER TABLE files ADD COLUMN error_message TEXT",
		"ALTER TABLE files ADD COLUMN retry_count INTEGER DEFAULT 0",
		"ALTER TABLE files ADD COLUMN queued_at DATETIME",
	}

	for _, stmt := range alterStmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to alter table: %w", err)
		}
	}

	// Create status index
	_, err = tx.Exec(CreateFilesStatusIndex)
	if err != nil {
		return fmt.Errorf("failed to create status index: %w", err)
	}

	// Update schema version
	_, err = tx.Exec("UPDATE meta SET value = ? WHERE key = ?", SchemaVersion, MetaKeySchemaVersion)
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// Close closes the database connection and flushes WAL
func (db *DB) Close() error {
	if db.conn == nil {
		return nil
	}

	// Checkpoint WAL before closing
	_, err := db.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	closeErr := db.conn.Close()

	if err != nil {
		// Log but don't fail on checkpoint error
		fmt.Fprintf(os.Stderr, "warning: failed to checkpoint WAL: %v\n", err)
	}

	// Mark conn as nil to prevent double-close
	db.conn = nil

	return closeErr
}

// Path returns the database file path
func (db *DB) Path() string {
	return db.path
}

// EmbeddingDim returns the configured embedding dimension
func (db *DB) EmbeddingDim() int {
	return db.embeddingDim
}

// GetMeta retrieves a metadata value by key
func (db *DB) GetMeta(key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("meta key not found: %s", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get meta: %w", err)
	}
	return value, nil
}

// SetMeta stores a metadata key-value pair
func (db *DB) SetMeta(key, value string) error {
	_, err := db.conn.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set meta: %w", err)
	}
	return nil
}

// HealthCheck verifies database connectivity and schema
func (db *DB) HealthCheck() error {
	// Ping connection
	if err := db.conn.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Verify schema version
	version, err := db.GetMeta(MetaKeySchemaVersion)
	if err != nil {
		return fmt.Errorf("failed to read schema version: %w", err)
	}
	if version != SchemaVersion {
		return fmt.Errorf("schema version mismatch: expected %s, got %s", SchemaVersion, version)
	}

	// Verify WAL mode is enabled
	var journalMode string
	err = db.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		return fmt.Errorf("failed to check journal mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("WAL mode not enabled, got: %s", journalMode)
	}

	return nil
}

// RawQuery executes a raw SQL query with args and returns the result rows
func (d *DB) RawQuery(sql string, args ...interface{}) (*sql.Rows, error) {
	return d.conn.Query(sql, args...)
}

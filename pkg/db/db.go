package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
)

// driverName is the SQLite driver with the functions queries rely on
const driverName = "sqlite3_chainsaw"

var registerDriver sync.Once

// DB wraps the SQLite database connection with our schema
type DB struct {
	conn *sql.DB
	path string
}

// Config holds database configuration
type Config struct {
	Path string // Database file path
}

// DefaultPath returns where the index lives for the current user
func DefaultPath() string {
	return filepath.Join(os.Getenv("HOME"), ".chainsaw", "graph.db")
}

// Open opens or creates a database with the given configuration
func Open(cfg Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// REGEXP is an operator SQLite leaves to the application
	registerDriver.Do(func() {
		sql.Register(driverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				return conn.RegisterFunc("regexp", func(pattern, s string) (bool, error) {
					return regexp.MatchString(pattern, s)
				}, true)
			},
		})
	})

	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=10000", cfg.Path)
	conn, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool (single writer, multiple readers)
	conn.SetMaxOpenConns(5)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(time.Hour)

	db := &DB{conn: conn, path: cfg.Path}

	if err := db.initSchema(); err != nil {
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
func (db *DB) initSchema() error {
	for _, pragma := range []string{"PRAGMA journal_mode=WAL;", "PRAGMA wal_autocheckpoint=1000;", "PRAGMA foreign_keys=ON;"} {
		if _, err := db.conn.Exec(pragma); err != nil {
			return fmt.Errorf("failed to configure database: %w", err)
		}
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range schema {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	var version string
	err = tx.QueryRow("SELECT value FROM meta WHERE key = ?", MetaKeySchemaVersion).Scan(&version)
	switch {
	case err == sql.ErrNoRows:
		now := time.Now().UTC().Format(time.RFC3339)
		for key, value := range map[string]string{MetaKeySchemaVersion: SchemaVersion, MetaKeyCreatedAt: now} {
			if _, err := tx.Exec("INSERT INTO meta (key, value) VALUES (?, ?)", key, value); err != nil {
				return fmt.Errorf("failed to insert meta %s: %w", key, err)
			}
		}
	case err != nil:
		return fmt.Errorf("failed to read schema version: %w", err)
	case version != SchemaVersion:
		return fmt.Errorf("database %s has schema %s, this version needs %s; remove it and index again", db.path, version, SchemaVersion)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema transaction: %w", err)
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

// GetMeta retrieves a metadata value by key
func (db *DB) GetMeta(key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get meta: %w", err)
	}
	return value, nil
}

// SetMeta stores a metadata value
func (db *DB) SetMeta(key, value string) error {
	_, err := db.conn.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, value)
	if err != nil {
		return fmt.Errorf("failed to set meta: %w", err)
	}
	return nil
}

// RawQuery executes a raw SQL query with args and returns the result rows
func (db *DB) RawQuery(query string, args ...interface{}) (*sql.Rows, error) {
	return db.conn.Query(query, args...)
}

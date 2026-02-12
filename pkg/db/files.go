package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// File represents a tracked file in the database
type File struct {
	ID           int64
	Path         string
	LastModTime  int64  // Unix timestamp
	ContentHash  string // SHA256 hex
	IndexedAt    time.Time
	Status       string // pending, processing, indexed, failed
	ErrorMessage string
	RetryCount   int
	QueuedAt     *time.Time
}

// UpsertFile inserts or updates a file record
func (db *DB) UpsertFile(path string, modTime int64, contentHash string) (int64, error) {
	slog.Debug("UpsertFile called", "path", path, "mod_time", modTime, "hash", contentHash[:min(8, len(contentHash))])

	_, err := db.conn.Exec(`
		INSERT INTO files (path, last_mod_time, content_hash, indexed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			last_mod_time = excluded.last_mod_time,
			content_hash = excluded.content_hash,
			indexed_at = excluded.indexed_at
	`, path, modTime, contentHash, time.Now().UTC())

	if err != nil {
		return 0, fmt.Errorf("failed to upsert file: %w", err)
	}

	// Always fetch the ID via SELECT since LastInsertId() is unreliable with ON CONFLICT
	var fileID int64
	err = db.conn.QueryRow("SELECT id FROM files WHERE path = ?", path).Scan(&fileID)
	if err != nil {
		return 0, fmt.Errorf("failed to get file ID: %w", err)
	}
	slog.Debug("UpsertFile result", "path", path, "file_id", fileID)
	return fileID, nil
}

// GetFile retrieves a file record by path
func (db *DB) GetFile(path string) (*File, error) {
	var f File
	var indexedAt string

	err := db.conn.QueryRow(`
		SELECT id, path, last_mod_time, content_hash, indexed_at
		FROM files
		WHERE path = ?
	`, path).Scan(&f.ID, &f.Path, &f.LastModTime, &f.ContentHash, &indexedAt)

	if err == sql.ErrNoRows {
		return nil, nil // File not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	// Parse indexed_at timestamp
	f.IndexedAt, err = time.Parse(time.RFC3339, indexedAt)
	if err != nil {
		// Fallback to zero time if parse fails
		f.IndexedAt = time.Time{}
	}

	return &f, nil
}

// GetFileByID retrieves a file record by ID
func (db *DB) GetFileByID(id int64) (*File, error) {
	var f File
	var indexedAt string

	err := db.conn.QueryRow(`
		SELECT id, path, last_mod_time, content_hash, indexed_at
		FROM files
		WHERE id = ?
	`, id).Scan(&f.ID, &f.Path, &f.LastModTime, &f.ContentHash, &indexedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file by ID: %w", err)
	}

	f.IndexedAt, err = time.Parse(time.RFC3339, indexedAt)
	if err != nil {
		f.IndexedAt = time.Time{}
	}

	return &f, nil
}

// DeleteFile removes a file and cascades to related chunks/edges
func (db *DB) DeleteFile(path string) error {
	result, err := db.conn.Exec("DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("file not found: %s", path)
	}

	return nil
}

// ListFilesOptions holds pagination and filtering options
type ListFilesOptions struct {
	Limit  int
	Offset int
}

// ListFiles returns a paginated list of files
func (db *DB) ListFiles(opts ListFilesOptions) ([]*File, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	rows, err := db.conn.Query(`
		SELECT id, path, last_mod_time, content_hash, indexed_at
		FROM files
		ORDER BY path
		LIMIT ? OFFSET ?
	`, opts.Limit, opts.Offset)

	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var f File
		var indexedAt string

		err := rows.Scan(&f.ID, &f.Path, &f.LastModTime, &f.ContentHash, &indexedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		f.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)
		files = append(files, &f)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating files: %w", err)
	}

	return files, nil
}

// CountFiles returns the total number of indexed files
func (db *DB) CountFiles() (int64, error) {
	var count int64
	err := db.conn.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count files: %w", err)
	}
	return count, nil
}

// HasFileChanged checks if a file has changed based on mod time and hash
func (db *DB) HasFileChanged(path string, modTime int64, contentHash string) (bool, error) {
	file, err := db.GetFile(path)
	if err != nil {
		return false, err
	}

	if file == nil {
		return true, nil // File not indexed yet
	}

	// Changed if either mod time or hash differs
	return file.LastModTime != modTime || file.ContentHash != contentHash, nil
}

// MarkFilePending marks a file as pending for indexing
func (db *DB) MarkFilePending(path string, modTime int64, contentHash string) error {
	slog.Debug("MarkFilePending called", "path", path)
	_, err := db.conn.Exec(`
		INSERT INTO files (path, last_mod_time, content_hash, status, queued_at, retry_count)
		VALUES (?, ?, ?, 'pending', ?, 0)
		ON CONFLICT(path) DO UPDATE SET
			last_mod_time = excluded.last_mod_time,
			content_hash = excluded.content_hash,
			status = 'pending',
			queued_at = excluded.queued_at,
			retry_count = 0,
			error_message = NULL
	`, path, modTime, contentHash, time.Now().UTC())

	if err != nil {
		return fmt.Errorf("failed to mark file pending: %w", err)
	}
	return nil
}

// GetPendingFiles retrieves files pending indexing, ordered by queue time
func (db *DB) GetPendingFiles(limit int) ([]*File, error) {
	rows, err := db.conn.Query(`
		SELECT id, path, last_mod_time, content_hash, indexed_at, status, 
		       COALESCE(error_message, ''), retry_count, queued_at
		FROM files
		WHERE status = 'pending'
		ORDER BY queued_at ASC
		LIMIT ?
	`, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query pending files: %w", err)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var f File
		var indexedAt, queuedAt string

		err := rows.Scan(&f.ID, &f.Path, &f.LastModTime, &f.ContentHash, &indexedAt,
			&f.Status, &f.ErrorMessage, &f.RetryCount, &queuedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		f.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)
		if queuedAt != "" {
			t, _ := time.Parse(time.RFC3339, queuedAt)
			f.QueuedAt = &t
		}
		files = append(files, &f)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pending files: %w", err)
	}

	return files, nil
}

// MarkFileProcessing marks a file as currently being processed
func (db *DB) MarkFileProcessing(fileID int64) error {
	_, err := db.conn.Exec(`
		UPDATE files
		SET status = 'processing'
		WHERE id = ?
	`, fileID)

	if err != nil {
		return fmt.Errorf("failed to mark file processing: %w", err)
	}
	return nil
}

// MarkFileIndexed marks a file as successfully indexed
func (db *DB) MarkFileIndexed(fileID int64) error {
	_, err := db.conn.Exec(`
		UPDATE files
		SET status = 'indexed',
		    indexed_at = ?,
		    error_message = NULL,
		    retry_count = 0
		WHERE id = ?
	`, time.Now().UTC(), fileID)

	if err != nil {
		return fmt.Errorf("failed to mark file indexed: %w", err)
	}
	return nil
}

// MarkFileFailed marks a file as failed with error details
func (db *DB) MarkFileFailed(fileID int64, errorMsg string, retryCount int) error {
	_, err := db.conn.Exec(`
		UPDATE files
		SET status = 'failed',
		    error_message = ?,
		    retry_count = ?
		WHERE id = ?
	`, errorMsg, retryCount, fileID)

	if err != nil {
		return fmt.Errorf("failed to mark file failed: %w", err)
	}
	return nil
}

// ResetStuckProcessing resets files stuck in "processing" state
// This should be called on daemon startup to recover from crashes
func (db *DB) ResetStuckProcessing() error {
	result, err := db.conn.Exec(`
		UPDATE files
		SET status = 'pending'
		WHERE status = 'processing'
	`)

	if err != nil {
		return fmt.Errorf("failed to reset stuck processing: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		fmt.Printf("Reset %d files from processing to pending\n", rows)
	}

	return nil
}

// CountFilesByStatus returns counts of files grouped by status
func (db *DB) CountFilesByStatus() (map[string]int64, error) {
	rows, err := db.conn.Query(`
SELECT status, COUNT(*) as count 
FROM files 
GROUP BY status
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}

	return counts, rows.Err()
}

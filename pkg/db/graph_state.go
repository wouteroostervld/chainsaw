package db

import (
	"database/sql"
	"fmt"
	"time"
)

// GetChunksNeedingGraphExtraction returns chunk IDs that haven't had graph extraction performed
func (db *DB) GetChunksNeedingGraphExtraction(limit int) ([]int64, error) {
	query := `
		SELECT v.chunk_id
		FROM vec_chunks v
		LEFT JOIN chunk_graph_state cgs ON v.chunk_id = cgs.chunk_id
		WHERE cgs.graph_extracted IS NULL OR cgs.graph_extracted = 0
		ORDER BY v.chunk_id
		LIMIT ?
	`

	rows, err := db.conn.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("query unextracted chunks: %w", err)
	}
	defer rows.Close()

	var chunkIDs []int64
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			return nil, fmt.Errorf("scan chunk ID: %w", err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return chunkIDs, nil
}

// MarkChunksGraphExtracted marks chunks as having had graph extraction performed
func (db *DB) MarkChunksGraphExtracted(chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO chunk_graph_state (chunk_id, graph_extracted, extracted_at)
		VALUES (?, 1, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET 
			graph_extracted = 1,
			extracted_at = excluded.extracted_at
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, chunkID := range chunkIDs {
		if _, err := stmt.Exec(chunkID, now); err != nil {
			return fmt.Errorf("mark chunk %d: %w", chunkID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// GetGraphExtractionStats returns statistics about graph extraction progress
func (db *DB) GetGraphExtractionStats() (total, extracted, pending int, err error) {
	// Get total chunks
	err = db.conn.QueryRow("SELECT COUNT(*) FROM vec_chunks").Scan(&total)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, 0, fmt.Errorf("count total chunks: %w", err)
	}

	// Get extracted chunks
	err = db.conn.QueryRow(`
		SELECT COUNT(*) 
		FROM chunk_graph_state 
		WHERE graph_extracted = 1
	`).Scan(&extracted)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, 0, fmt.Errorf("count extracted chunks: %w", err)
	}

	pending = total - extracted
	if pending < 0 {
		pending = 0
	}

	return total, extracted, pending, nil
}

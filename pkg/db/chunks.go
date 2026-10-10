package db

import "fmt"

// InsertChunk records where a chunk of a file is and returns its ID
func (db *DB) InsertChunk(fileID int64, startLine, endLine int) (int64, error) {
	result, err := db.conn.Exec(`
		INSERT INTO chunks (file_id, start_line, end_line)
		VALUES (?, ?, ?)
	`, fileID, startLine, endLine)
	if err != nil {
		return 0, fmt.Errorf("failed to insert chunk: %w", err)
	}

	chunkID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get chunk ID: %w", err)
	}
	return chunkID, nil
}

// DeleteChunksForFile removes all chunks associated with a file, along with
// the entities, edges and symbols that were derived from them.
// This is used when re-indexing a file
func (db *DB) DeleteChunksForFile(fileID int64) error {
	// A transaction writes; see pool
	db.conn.writing.Lock()
	defer db.conn.writing.Unlock()

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := deleteFileGraph(tx, fileID); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM chunks WHERE file_id = ?", fileID); err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit chunk deletion: %w", err)
	}
	return nil
}

// Counts holds the size of the index
type Counts struct {
	Files       int64
	Chunks      int64
	Definitions int64
	Externals   int64
	Edges       map[string]int64 // By relation type
}

// Count returns the size of the index
func (db *DB) Count() (*Counts, error) {
	c := &Counts{Edges: make(map[string]int64)}
	single := map[string]*int64{
		"SELECT COUNT(*) FROM files":                       &c.Files,
		"SELECT COUNT(*) FROM chunks":                      &c.Chunks,
		"SELECT COUNT(*) FROM symbol_defs":                 &c.Definitions,
		"SELECT COUNT(*) FROM entities WHERE chunk_id = 0": &c.Externals,
	}
	for query, dest := range single {
		if err := db.conn.QueryRow(query).Scan(dest); err != nil {
			return nil, fmt.Errorf("failed to count: %w", err)
		}
	}

	rows, err := db.conn.Query("SELECT relation_type, COUNT(*) FROM graph_edges GROUP BY relation_type")
	if err != nil {
		return nil, fmt.Errorf("failed to count edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var relation string
		var n int64
		if err := rows.Scan(&relation, &n); err != nil {
			return nil, fmt.Errorf("failed to scan edge count: %w", err)
		}
		c.Edges[relation] = n
	}
	return c, rows.Err()
}

// Location is where a definition is
type Location struct {
	Name      string
	Type      string
	Path      string
	StartLine int
	EndLine   int
}

// Callees returns the definitions that are called from the definitions in a
// line range of a file. Only calls whose target is certain are followed.
func (db *DB) Callees(path string, startLine, endLine int) ([]Location, error) {
	rows, err := db.conn.Query(`
		SELECT DISTINCT t.name, t.entity_type, tf.path, td.start_line, td.end_line
		FROM files sf
		JOIN symbol_defs sd ON sd.file_id = sf.id AND sd.start_line >= ? AND sd.end_line <= ?
		JOIN graph_edges g ON g.source_entity_id = sd.entity_id AND g.relation_type = ?
		JOIN symbol_defs td ON td.entity_id = g.target_entity_id AND td.entity_id != sd.entity_id
		JOIN entities t ON t.id = td.entity_id
		JOIN files tf ON tf.id = td.file_id
		WHERE sf.path = ?
		ORDER BY tf.path, td.start_line
	`, startLine, endLine, relationCalls, path)
	if err != nil {
		return nil, fmt.Errorf("failed to query callees: %w", err)
	}
	defer rows.Close()

	var callees []Location
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.Name, &l.Type, &l.Path, &l.StartLine, &l.EndLine); err != nil {
			return nil, fmt.Errorf("failed to scan callee: %w", err)
		}
		callees = append(callees, l)
	}
	return callees, rows.Err()
}

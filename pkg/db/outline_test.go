package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSearchWithRelations_ServesOutline(t *testing.T) {
	db := openSymbolsDB(t)

	insert := func(fileID int64, content string, embedding []float32, start, end int) int64 {
		t.Helper()
		id, err := db.InsertChunk(fileID, content, embedding, start, end)
		if err != nil {
			t.Fatalf("Failed to insert chunk: %v", err)
		}
		return id
	}

	fileID, err := db.UpsertFile("/proj/app/main.py", 1, "hash")
	if err != nil {
		t.Fatalf("Failed to upsert file: %v", err)
	}
	skeleton := "import lib\n\ndef run(): # ... chunk: lines 3-6\n\ndef helper(): # ... chunk: lines 8-10\n\ndef unused(): # ... chunk: lines 12-14\n"
	run := "def run():\n    helper()\n    lib.load()\n    return 1\n"
	helper := "def helper():\n    x = 1\n    return x\n"
	unused := "def unused():\n    y = 2\n    return y\n"

	skeletonChunk := insert(fileID, skeleton, []float32{0, 0, 0, 1}, 1, 15)
	// run is too large for one embedding and was split into two blocks
	runChunk := insert(fileID, "def run():\n    helper()\n", []float32{1, 0, 0, 0}, 3, 4)
	runChunk2 := insert(fileID, "    lib.load()\n    return 1\n", []float32{0.9, 0.1, 0, 0}, 5, 6)
	helperChunk := insert(fileID, helper, []float32{0, 1, 0, 0}, 8, 10)
	unusedChunk := insert(fileID, unused, []float32{0, 0, 1, 0}, 12, 14)

	if err := db.StoreFileOutline(fileID, []OutlineNode{
		{Parent: -1, StartLine: 1, EndLine: 15, Content: skeleton, ChunkIDs: []int64{skeletonChunk}},
		{Parent: 0, StartLine: 3, EndLine: 6, Content: run, Stub: "def run(): # ... chunk: lines 3-6", ChunkIDs: []int64{runChunk, runChunk2}},
		{Parent: 0, StartLine: 8, EndLine: 10, Content: helper, Stub: "def helper(): # ... chunk: lines 8-10", ChunkIDs: []int64{helperChunk}},
		{Parent: 0, StartLine: 12, EndLine: 14, Content: unused, Stub: "def unused(): # ... chunk: lines 12-14", ChunkIDs: []int64{unusedChunk}},
	}); err != nil {
		t.Fatalf("StoreFileOutline failed: %v", err)
	}

	libFile, err := db.UpsertFile("/proj/lib/lib.py", 1, "hash")
	if err != nil {
		t.Fatalf("Failed to upsert file: %v", err)
	}
	load := "def load():\n    data = 1\n    return data\n"
	libChunk := insert(libFile, load, []float32{0, 0, 0.5, 0.5}, 1, 3)
	if err := db.StoreFileOutline(libFile, []OutlineNode{
		{Parent: -1, StartLine: 1, EndLine: 3, Content: load, ChunkIDs: []int64{libChunk}},
	}); err != nil {
		t.Fatalf("StoreFileOutline failed: %v", err)
	}

	if err := db.StoreFileSymbols(libFile, "/proj", []SymbolDef{
		{Name: "lib", EntityType: "PACKAGE", ChunkID: libChunk, Parent: -1},
		{Name: "load", EntityType: "FUNCTION", ChunkID: libChunk, Parent: 0},
	}, nil); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
	if err := db.StoreFileSymbols(fileID, "/proj", []SymbolDef{
		{Name: "main", EntityType: "PACKAGE", ChunkID: skeletonChunk, Parent: -1},
		{Name: "run", EntityType: "FUNCTION", ChunkID: runChunk, Parent: 0},
		{Name: "helper", EntityType: "FUNCTION", ChunkID: helperChunk, Parent: 0},
		{Name: "unused", EntityType: "FUNCTION", ChunkID: unusedChunk, Parent: 0},
	}, []SymbolRef{
		{Source: 1, RelationType: "calls", Name: "helper", ExternalName: "helper"},
		{Source: 1, RelationType: "calls", Name: "load", ImportBase: "lib", ExternalName: "lib.load", Member: true},
	}); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}

	// Both blocks of run are the nearest chunks
	results, err := db.SearchWithRelations([]float32{1, 0, 0, 0}, 2, 1, "", ViewCalls)
	if err != nil {
		t.Fatalf("SearchWithRelations failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want the two hits in one file merged into 1", len(results))
	}
	result := results[0]

	// The folded file, with the hit and what it calls in this file in full
	wantOutline := "import lib\n\n" + run + "\n" + helper + "\ndef unused(): # ... chunk: lines 12-14\n"
	if result.Outline != wantOutline {
		t.Errorf("outline =\n%s\nwant\n%s", result.Outline, wantOutline)
	}
	if want := []LineRange{{3, 6}}; !reflect.DeepEqual(result.Hits, want) {
		t.Errorf("hits = %v, want %v", result.Hits, want)
	}

	// What it calls in another file comes along as a related chunk
	if len(result.RelatedChunks) != 1 {
		t.Fatalf("got %d related chunks, want 1", len(result.RelatedChunks))
	}
	related := result.RelatedChunks[0]
	if related.Chunk.FileID != libFile || related.Chunk.ContentSnippet != load || related.RelationType != "calls" {
		t.Errorf("related chunk = %+v (%s)", related.Chunk, related.RelationType)
	}

	// Unless it is outside the path the search is limited to
	results, err = db.SearchWithRelations([]float32{1, 0, 0, 0}, 2, 1, "/proj/app/%", ViewCalls)
	if err != nil {
		t.Fatalf("SearchWithRelations failed: %v", err)
	}
	if len(results) != 1 || len(results[0].RelatedChunks) != 0 {
		t.Errorf("path filter did not exclude the related chunk: %+v", results)
	}

	// The folded file with only the hit in full
	results, err = db.SearchWithRelations([]float32{1, 0, 0, 0}, 2, 1, "", ViewFile)
	if err != nil {
		t.Fatalf("SearchWithRelations failed: %v", err)
	}
	wantFile := "import lib\n\n" + run + "\ndef helper(): # ... chunk: lines 8-10\n\ndef unused(): # ... chunk: lines 12-14\n"
	if len(results) != 1 || results[0].Outline != wantFile || len(results[0].RelatedChunks) != 0 {
		t.Errorf("file view = %d results, %d related:\n%s\nwant\n%s", len(results), len(results[0].RelatedChunks), results[0].Outline, wantFile)
	}

	// Just the definition, whole, once, however many of its blocks matched
	results, err = db.SearchWithRelations([]float32{1, 0, 0, 0}, 3, 1, "", ViewBody)
	if err != nil {
		t.Fatalf("SearchWithRelations failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("body view gave %d results, want 2 (run and one other)", len(results))
	}
	if body := results[0]; body.Outline != run || !reflect.DeepEqual(body.Hits, []LineRange{{3, 6}}) || len(body.RelatedChunks) != 0 {
		t.Errorf("body view = %v, %d related:\n%s\nwant\n%s", body.Hits, len(body.RelatedChunks), body.Outline, run)
	}

	// A file without an outline is served as before
	plainFile, plainChunk := addFile(t, db, "/proj/notes.txt")
	results, err = db.SearchWithRelations([]float32{1, 0, 0, 0}, 10, 1, "", ViewCalls)
	if err != nil {
		t.Fatalf("SearchWithRelations failed: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Chunk.ChunkID == plainChunk {
			found = r.Chunk.FileID == plainFile && r.Outline == "" && strings.HasPrefix(r.Chunk.ContentSnippet, "content of")
		}
	}
	if !found {
		t.Errorf("plain chunk not served as is: %+v", results)
	}
}

func TestSearchSimilar_FiltersBeforeRanking(t *testing.T) {
	db := openSymbolsDB(t)

	// Another project has many chunks that are closer to the query
	for i := 0; i < 20; i++ {
		fileID, err := db.UpsertFile(fmt.Sprintf("/other/f%d.go", i), 1, "hash")
		if err != nil {
			t.Fatalf("Failed to upsert file: %v", err)
		}
		if _, err := db.InsertChunk(fileID, "near", []float32{1, 0.01 * float32(i), 0, 0}, 1, 2); err != nil {
			t.Fatalf("Failed to insert chunk: %v", err)
		}
	}
	fileID, err := db.UpsertFile("/proj/a.go", 1, "hash")
	if err != nil {
		t.Fatalf("Failed to upsert file: %v", err)
	}
	want, err := db.InsertChunk(fileID, "far", []float32{0, 1, 0, 0}, 1, 2)
	if err != nil {
		t.Fatalf("Failed to insert chunk: %v", err)
	}

	results, err := db.SearchSimilar([]float32{1, 0, 0, 0}, 3, "/proj/%")
	if err != nil {
		t.Fatalf("SearchSimilar failed: %v", err)
	}
	if len(results) != 1 || results[0].Chunk.ChunkID != want {
		t.Errorf("got %d results, want the one chunk under the path", len(results))
	}
}

// entityID returns the ID of the entity with a name, or 0
func entityID(t *testing.T, db *DB, name string) int64 {
	t.Helper()
	var id int64
	err := db.conn.QueryRow("SELECT id FROM entities WHERE name = ?", name).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("Failed to query entity: %v", err)
	}
	return id
}

func TestOpen_MigratesFrom2_4(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(Config{Path: dbPath, EmbeddingDim: 4})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	if _, err := db.UpsertFile("/proj/a.go", 1, "hash"); err != nil {
		t.Fatalf("Failed to upsert file: %v", err)
	}
	// Two definitions found by parsing, and two entities an LLM came up with
	fileID, chunkID := addFile(t, db, "/proj/b.go")
	if err := db.StoreFileSymbols(fileID, "/proj", []SymbolDef{
		{Name: "A", EntityType: "FUNCTION", ChunkID: chunkID, Parent: -1},
		{Name: "B", EntityType: "FUNCTION", ChunkID: chunkID, Parent: -1},
	}, nil); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
	source, target := entityID(t, db, "A"), entityID(t, db, "B")
	guessed, _ := db.UpsertEntity("Guess", "FUNCTION", 99)
	guessedTarget, _ := db.UpsertEntity("Other", "TYPE", 99)

	// Put the database back in the shape version 2.4.0 left it in
	downgrade := []string{
		"DROP TABLE graph_edges",
		`CREATE TABLE graph_edges (
			source_entity_id INTEGER NOT NULL,
			target_entity_id INTEGER NOT NULL,
			relation_type TEXT NOT NULL,
			chunk_id INTEGER NOT NULL,
			weight REAL DEFAULT 1.0,
			PRIMARY KEY (source_entity_id, target_entity_id, chunk_id)
		)`,
		"UPDATE meta SET value = '2.4.0' WHERE key = 'schema_version'",
		"UPDATE files SET status = 'indexed', queued_at = NULL",
	}
	for _, stmt := range downgrade {
		if _, err := db.conn.Exec(stmt); err != nil {
			t.Fatalf("Failed to downgrade: %v", err)
		}
	}
	for _, e := range [][2]int64{{source, target}, {guessed, guessedTarget}, {guessed, target}} {
		if _, err := db.conn.Exec("INSERT INTO graph_edges VALUES (?, ?, 'calls', 1, 1.0)", e[0], e[1]); err != nil {
			t.Fatalf("Failed to insert edge: %v", err)
		}
	}
	if _, err := db.conn.Exec("CREATE TABLE chunk_graph_state (chunk_id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("Failed to create legacy table: %v", err)
	}
	db.Close()

	db, err = Open(Config{Path: dbPath, EmbeddingDim: 4})
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db.Close()

	if err := db.HealthCheck(); err != nil {
		t.Errorf("HealthCheck failed after migration: %v", err)
	}

	// What the LLM extracted is gone, along with its bookkeeping
	if got := entityID(t, db, "Guess") + entityID(t, db, "Other"); got != 0 {
		t.Error("entities without a definition survived the migration")
	}
	var legacy int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'chunk_graph_state'").Scan(&legacy); err != nil || legacy != 0 {
		t.Errorf("chunk_graph_state still exists (%v)", err)
	}

	// Edges between definitions survive, and a second relation between the same entities fits
	if err := db.UpsertEntityEdge(source, target, "uses", 1); err != nil {
		t.Fatalf("Failed to add a second relation: %v", err)
	}
	if count, _ := db.CountEdges(); count != 2 {
		t.Errorf("got %d edges, want 2", count)
	}

	// Every file is queued so it gets chunked along its syntax tree
	var status string
	var queuedAt sql.NullString
	if err := db.conn.QueryRow("SELECT status, queued_at FROM files WHERE path = '/proj/a.go'").Scan(&status, &queuedAt); err != nil {
		t.Fatalf("Failed to query file: %v", err)
	}
	if status != "pending" || !queuedAt.Valid {
		t.Errorf("file status = %q (queued: %v), want pending", status, queuedAt.Valid)
	}
}

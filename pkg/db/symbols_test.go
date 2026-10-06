package db

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func openSymbolsDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(Config{
		Path:         filepath.Join(t.TempDir(), "test.db"),
		EmbeddingDim: 4,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// addFile registers a file with a single chunk and returns their IDs
func addFile(t *testing.T, db *DB, path string) (fileID, chunkID int64) {
	t.Helper()
	fileID, err := db.UpsertFile(path, 1, "hash")
	if err != nil {
		t.Fatalf("Failed to upsert file: %v", err)
	}
	chunkID, err = db.InsertChunk(fileID, "content of "+path, []float32{1, 0, 0, 0}, 1, 10)
	if err != nil {
		t.Fatalf("Failed to insert chunk: %v", err)
	}
	return fileID, chunkID
}

type edge struct {
	Target string
	Type   string
	Weight float64
}

// edgesFrom returns the edges of one relation leaving the named entity
func edgesFrom(t *testing.T, db *DB, source, relation string) []edge {
	t.Helper()
	rows, err := db.conn.Query(`
		SELECT e2.name, e2.entity_type, g.weight
		FROM graph_edges g
		JOIN entities e1 ON e1.id = g.source_entity_id
		JOIN entities e2 ON e2.id = g.target_entity_id
		WHERE e1.name = ? AND g.relation_type = ?
	`, source, relation)
	if err != nil {
		t.Fatalf("Failed to query edges: %v", err)
	}
	defer rows.Close()

	var edges []edge
	for rows.Next() {
		var e edge
		if err := rows.Scan(&e.Target, &e.Type, &e.Weight); err != nil {
			t.Fatalf("Failed to scan edge: %v", err)
		}
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].Target < edges[j].Target })
	return edges
}

func TestStoreFileSymbols_ResolvesAcrossFiles(t *testing.T) {
	db := openSymbolsDB(t)

	// The caller is indexed before the file that defines what it calls
	callerFile, callerChunk := addFile(t, db, "/proj/app/main.go")
	callerDefs := []SymbolDef{
		{Name: "main", EntityType: "PACKAGE", ChunkID: callerChunk, Parent: -1},
		{Name: "Run", EntityType: "FUNCTION", ChunkID: callerChunk, Parent: 0},
	}
	callerRefs := []SymbolRef{
		{Source: 0, RelationType: "imports", Name: "store", ImportBase: "store", ExternalName: "example.com/proj/store"},
		{Source: 1, RelationType: "calls", Name: "Open", ImportBase: "store", ExternalName: "store.Open", Member: true},
		{Source: 1, RelationType: "calls", Name: "Println", ImportBase: "fmt", ExternalName: "fmt.Println", Member: true},
		{Source: 1, RelationType: "calls", Name: "Close", Container: "s", ExternalName: "Close", Member: true},
	}
	if err := db.StoreFileSymbols(callerFile, "/proj", callerDefs, callerRefs); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}

	unresolved := []edge{
		{"Close", EntityTypeExternal, weightExact},
		{"fmt.Println", EntityTypeExternal, weightExact},
		{"store.Open", EntityTypeExternal, weightExact},
	}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, unresolved) {
		t.Errorf("calls before definitions are indexed = %v, want %v", got, unresolved)
	}
	if got, want := edgesFrom(t, db, "main", "defines"), []edge{{"Run", "FUNCTION", weightExact}}; !reflect.DeepEqual(got, want) {
		t.Errorf("defines = %v, want %v", got, want)
	}

	storeFile, storeChunk := addFile(t, db, "/proj/store/store.go")
	storeDefs := []SymbolDef{
		{Name: "store", EntityType: "PACKAGE", ChunkID: storeChunk, Parent: -1},
		{Name: "Open", EntityType: "FUNCTION", ChunkID: storeChunk, Parent: 0},
		{Name: "Close", Container: "Store", EntityType: "METHOD", ChunkID: storeChunk, Parent: 0},
	}
	if err := db.StoreFileSymbols(storeFile, "/proj", storeDefs, nil); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}

	resolved := []edge{
		{"Open", "FUNCTION", weightExact}, // Through the import
		{"fmt.Println", EntityTypeExternal, weightExact},
	}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, resolved) {
		t.Errorf("calls after definitions are indexed = %v, want %v", got, resolved)
	}
	// The receiver's type is unknown, so this is a match by name only
	if got, want := edgesFrom(t, db, "Run", RelationMayCall), []edge{{"Store.Close", "METHOD", weightByName}}; !reflect.DeepEqual(got, want) {
		t.Errorf("may_call = %v, want %v", got, want)
	}
	if got, want := edgesFrom(t, db, "main", "imports"), []edge{{"store", "PACKAGE", weightExact}}; !reflect.DeepEqual(got, want) {
		t.Errorf("imports = %v, want %v", got, want)
	}

	// Removing the definitions turns the calls back into external ones
	if err := db.DeleteChunksForFile(storeFile); err != nil {
		t.Fatalf("DeleteChunksForFile failed: %v", err)
	}
	if err := db.StoreFileSymbols(storeFile, "/proj", nil, nil); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, unresolved) {
		t.Errorf("calls after definitions are removed = %v, want %v", got, unresolved)
	}
	if got := edgesFrom(t, db, "Run", RelationMayCall); len(got) != 0 {
		t.Errorf("may_call after definitions are removed = %v, want none", got)
	}
}

func TestDeleteChunksForFile_RemovesGraph(t *testing.T) {
	db := openSymbolsDB(t)

	fileID, chunkID := addFile(t, db, "/proj/a.py")
	defs := []SymbolDef{
		{Name: "a", EntityType: "PACKAGE", ChunkID: chunkID, Parent: -1},
		{Name: "f", EntityType: "FUNCTION", ChunkID: chunkID, Parent: 0},
		{Name: "g", EntityType: "FUNCTION", ChunkID: chunkID, Parent: 0},
	}
	refs := []SymbolRef{{Source: 1, RelationType: "calls", Name: "g", ExternalName: "g"}}
	if err := db.StoreFileSymbols(fileID, "/proj", defs, refs); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
	if got, want := edgesFrom(t, db, "f", "calls"), []edge{{"g", "FUNCTION", weightExact}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	if err := db.DeleteChunksForFile(fileID); err != nil {
		t.Fatalf("DeleteChunksForFile failed: %v", err)
	}

	for _, table := range []string{"vec_chunks", "entities", "graph_edges", "symbol_defs", "symbol_refs"} {
		var count int
		if err := db.conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("Failed to count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s has %d rows after deleting the file's chunks, want 0", table, count)
		}
	}
}

func TestSelectTargets(t *testing.T) {
	cands := []candidate{
		{entityID: 1, entityType: "FUNCTION", path: "/proj/app/util.go", root: "/proj"},
		{entityID: 2, entityType: "FUNCTION", path: "/proj/lib/util.go", root: "/proj"},
		{entityID: 3, entityType: "METHOD", container: "Server", path: "/proj/app/server.go", root: "/proj"},
		{entityID: 4, entityType: "METHOD", container: "Client", path: "/proj/lib/client.go", root: "/proj"},
		{entityID: 5, entityType: "PACKAGE", path: "/proj/lib/util.go", root: "/proj"},
		{entityID: 6, entityType: "FUNCTION", path: "/other/lib/util.go", root: "/other"},
	}

	tests := []struct {
		name       string
		ref        pendingRef
		wantIDs    []int64
		wantWeight float64
	}{
		{
			name:       "plain call prefers the same directory",
			ref:        pendingRef{relationType: "calls", path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    []int64{1},
			wantWeight: weightExact,
		},
		{
			name:       "plain call elsewhere is a match by name",
			ref:        pendingRef{relationType: "calls", path: "/proj/cmd/main.go", root: "/proj"},
			wantIDs:    []int64{1, 2},
			wantWeight: weightByName,
		},
		{
			name:       "import selects the directory",
			ref:        pendingRef{relationType: "calls", importBase: "lib", member: true, path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    []int64{2, 4},
			wantWeight: weightExact,
		},
		{
			name:       "import reaches into other projects",
			ref:        pendingRef{relationType: "calls", importBase: "lib", path: "/other/main.go", root: "/other"},
			wantIDs:    []int64{6},
			wantWeight: weightExact,
		},
		{
			name:       "plain call stays within the project",
			ref:        pendingRef{relationType: "calls", path: "/other/main.go", root: "/other"},
			wantIDs:    []int64{6},
			wantWeight: weightByName,
		},
		{
			name:       "import of something that is not indexed",
			ref:        pendingRef{relationType: "calls", importBase: "fmt", member: true, path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    nil,
			wantWeight: weightExact,
		},
		{
			name:       "known container",
			ref:        pendingRef{relationType: "calls", container: "Client", member: true, path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    []int64{4},
			wantWeight: weightExact,
		},
		{
			name:       "unknown receiver matches everything by that name",
			ref:        pendingRef{relationType: "calls", container: "c", member: true, path: "/proj/lib/x.go", root: "/proj"},
			wantIDs:    []int64{1, 2, 3, 4},
			wantWeight: weightByName,
		},
		{
			name:       "type-checked target is that definition only",
			ref:        pendingRef{relationType: "calls", targetPath: "/proj/lib/util.go", path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    []int64{2},
			wantWeight: weightExact,
		},
		{
			name:       "type-checked target outside the index",
			ref:        pendingRef{relationType: "calls", targetPath: "/usr/lib/go/src/fmt/print.go", path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    nil,
			wantWeight: weightExact,
		},
		{
			name:       "imports only match packages",
			ref:        pendingRef{relationType: "imports", importBase: "lib", path: "/proj/app/main.go", root: "/proj"},
			wantIDs:    []int64{5},
			wantWeight: weightExact,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, weight := selectTargets(tt.ref, cands)
			if !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Errorf("targets = %v, want %v", ids, tt.wantIDs)
			}
			if weight != tt.wantWeight {
				t.Errorf("weight = %v, want %v", weight, tt.wantWeight)
			}
		})
	}
}

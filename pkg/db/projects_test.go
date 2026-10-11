package db

import (
	"reflect"
	"testing"
)

// storeCaller stores a function Run that calls Get through an import of lib
func storeCaller(t *testing.T, db *DB, path, root string) {
	t.Helper()
	fileID, chunkID := addFile(t, db, path)
	defs := []SymbolDef{{Name: "Run", EntityType: "FUNCTION", ChunkID: chunkID, Parent: -1}}
	refs := []SymbolRef{{Source: 0, RelationType: "calls", Name: "Get", ImportBase: "lib", ExternalName: "lib.Get", Member: true}}
	if err := db.StoreFileSymbols(fileID, root, defs, refs); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
}

// storeLibrary stores a function Get in a file
func storeLibrary(t *testing.T, db *DB, path, root string) {
	t.Helper()
	fileID, chunkID := addFile(t, db, path)
	defs := []SymbolDef{{Name: "Get", EntityType: "FUNCTION", ChunkID: chunkID, Parent: -1}}
	if err := db.StoreFileSymbols(fileID, root, defs, nil); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
}

func TestSetProjectFollows(t *testing.T) {
	db := openSymbolsDB(t)
	storeLibrary(t, db, "/venv/lib/site-packages/lib/api.py", "/venv")
	storeCaller(t, db, "/proj/main.py", "/proj")

	// The library is in the index, but in another project
	outside := []edge{{"lib.Get", EntityTypeExternal, weightExact}}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, outside) {
		t.Errorf("calls before the project follows the library = %v, want %v", got, outside)
	}

	if err := db.SetProjectFollows("/proj", []string{"/venv"}); err != nil {
		t.Fatalf("SetProjectFollows failed: %v", err)
	}
	if err := db.ResolveSymbols(); err != nil {
		t.Fatalf("ResolveSymbols failed: %v", err)
	}
	if got, want := edgesFrom(t, db, "Run", "calls"), []edge{{"Get", "FUNCTION", weightExact}}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls when the project follows the library = %v, want %v", got, want)
	}

	// Following it no longer takes the edge away again
	if err := db.SetProjectFollows("/proj", nil); err != nil {
		t.Fatalf("SetProjectFollows failed: %v", err)
	}
	if err := db.ResolveSymbols(); err != nil {
		t.Fatalf("ResolveSymbols failed: %v", err)
	}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, outside) {
		t.Errorf("calls when the project follows nothing again = %v, want %v", got, outside)
	}
}

func TestDeferResolving(t *testing.T) {
	db := openSymbolsDB(t)
	done := db.DeferResolving(2)

	// The first file waits; nothing of it is in the graph yet
	storeCaller(t, db, "/proj/main.py", "/proj")
	if got := edgesFrom(t, db, "Run", "calls"); len(got) != 0 {
		t.Errorf("calls before references are resolved = %v, want none", got)
	}

	// The second is the one that references are resolved at
	storeLibrary(t, db, "/proj/lib/api.py", "/proj")
	resolved := []edge{{"Get", "FUNCTION", weightExact}}
	if got := edgesFrom(t, db, "Run", "calls"); !reflect.DeepEqual(got, resolved) {
		t.Errorf("calls after as many files as were asked for = %v, want %v", got, resolved)
	}

	// What is stored after that is resolved at the end
	fileID, chunkID := addFile(t, db, "/proj/other.py")
	defs := []SymbolDef{{Name: "Other", EntityType: "FUNCTION", ChunkID: chunkID, Parent: -1}}
	refs := []SymbolRef{{Source: 0, RelationType: "calls", Name: "Run", ExternalName: "Run"}}
	if err := db.StoreFileSymbols(fileID, "/proj", defs, refs); err != nil {
		t.Fatalf("StoreFileSymbols failed: %v", err)
	}
	if got := edgesFrom(t, db, "Other", "calls"); len(got) != 0 {
		t.Errorf("calls before the end = %v, want none", got)
	}
	if err := done(); err != nil {
		t.Fatalf("Resolving at the end failed: %v", err)
	}
	if got, want := edgesFrom(t, db, "Other", "calls"), []edge{{"Run", "FUNCTION", weightExact}}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls at the end = %v, want %v", got, want)
	}

	// And from then on a file is resolved as it is stored
	storeCaller(t, db, "/proj/sub/again.py", "/proj")
	if got := edgesFrom(t, db, "Run", "calls"); len(got) == 0 {
		t.Error("calls of a file stored after the end were not resolved")
	}
}

package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// Entity types used for graph nodes that are not definitions in the index
const (
	EntityTypePackage  = "PACKAGE"
	EntityTypeExternal = "EXTERNAL" // Call target that is not defined in any indexed file
)

const (
	relationDefines = "defines"
	relationImports = "imports"
	relationCalls   = "calls"

	// RelationMayCall is used instead of "calls" when the target was matched
	// on name alone, such as a method call on a value whose type is not
	// known. Every definition in the project with that name is a target.
	RelationMayCall = "may_call"

	// weightExact is used for edges whose target follows from the syntax:
	// same package, an import, or a known receiver type.
	weightExact = 1.0
	// weightByName is used for edges matched on name alone
	weightByName = 0.5
)

// SymbolDef is a definition found by parsing a source file
type SymbolDef struct {
	Name       string // Bare name, e.g. "IndexFile"
	Container  string // Class or receiver type, e.g. "Indexer"
	Signature  string // The definition up to its body
	StartLine  int    // 1-indexed
	EndLine    int    // 1-indexed, inclusive
	EntityType string
	ChunkID    int64 // Chunk holding the definition
	Parent     int   // Index of the defining symbol in the same batch, -1 for none
}

// SymbolRef is a reference from a definition to a name
type SymbolRef struct {
	Source       int    // Index of the referencing symbol in the same batch
	RelationType string // "calls" or "imports"
	Name         string // Bare name of the target
	Container    string // Container the target must be in, when known
	ImportBase   string // Last element of the import path the target comes from, when known
	TargetPath   string // File the target is declared in, when a type checker resolved it
	ExternalName string // Entity name to use when the target is not in the index; empty to leave it out of the graph
	Member       bool   // Reached through a receiver expression
}

// qualifiedName is the entity name of a definition
func qualifiedName(name, container string) string {
	if container == "" {
		return name
	}
	return container + "." + name
}

// StoreFileSymbols stores the definitions and references parsed from a file
// and links the references to their targets. root is the directory of the
// project the file belongs to; names are only matched across projects when an
// import says so. The file's previous symbols must have been removed with
// DeleteChunksForFile.
func (db *DB) StoreFileSymbols(fileID int64, root string, defs []SymbolDef, refs []SymbolRef) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	entityIDs := make([]int64, len(defs))
	for i, def := range defs {
		name := qualifiedName(def.Name, def.Container)
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO entities (name, entity_type, chunk_id)
			VALUES (?, ?, ?)
		`, name, def.EntityType, def.ChunkID); err != nil {
			return fmt.Errorf("failed to insert entity: %w", err)
		}
		if err := tx.QueryRow(`
			SELECT id FROM entities
			WHERE name = ? AND entity_type = ? AND chunk_id = ?
		`, name, def.EntityType, def.ChunkID).Scan(&entityIDs[i]); err != nil {
			return fmt.Errorf("failed to query entity: %w", err)
		}

		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO symbol_defs (entity_id, file_id, root, name, container, signature, start_line, end_line)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, entityIDs[i], fileID, root, def.Name, def.Container, def.Signature, def.StartLine, def.EndLine); err != nil {
			return fmt.Errorf("failed to insert symbol definition: %w", err)
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO symbol_dirty (name) VALUES (?)", def.Name); err != nil {
			return fmt.Errorf("failed to mark symbol dirty: %w", err)
		}
	}

	for i, def := range defs {
		if def.Parent < 0 || def.Parent >= len(defs) || entityIDs[def.Parent] == entityIDs[i] {
			continue
		}
		if err := upsertEdge(tx, entityIDs[def.Parent], entityIDs[i], relationDefines, defs[def.Parent].ChunkID, weightExact); err != nil {
			return err
		}
	}

	for _, ref := range refs {
		if ref.Source < 0 || ref.Source >= len(defs) {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO symbol_refs
				(file_id, root, source_entity_id, chunk_id, relation_type, name, container, import_base, target_path, external_name, member)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, fileID, root, entityIDs[ref.Source], defs[ref.Source].ChunkID, ref.RelationType,
			ref.Name, ref.Container, ref.ImportBase, ref.TargetPath, ref.ExternalName, ref.Member); err != nil {
			return fmt.Errorf("failed to insert symbol reference: %w", err)
		}
	}

	if err := resolveSymbols(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit symbols: %w", err)
	}
	return nil
}

// deleteFileGraph removes the entities, edges and symbols derived from a
// file's chunks. References from other files to the removed definitions are
// resolved again the next time symbols are stored.
func deleteFileGraph(tx *sql.Tx, fileID int64) error {
	const fileChunks = "(SELECT chunk_id FROM chunks WHERE file_id = ?)"

	statements := []string{
		"INSERT OR IGNORE INTO symbol_dirty (name) SELECT name FROM symbol_defs WHERE file_id = ?",
		"DELETE FROM symbol_defs WHERE file_id = ?",
		"DELETE FROM symbol_refs WHERE file_id = ?",
		"DELETE FROM graph_edges WHERE chunk_id IN " + fileChunks,
		"DELETE FROM entities WHERE chunk_id IN " + fileChunks,
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(stmt, fileID); err != nil {
			return fmt.Errorf("failed to delete graph for file: %w", err)
		}
	}
	return nil
}

func upsertEdge(tx *sql.Tx, sourceID, targetID int64, relationType string, chunkID int64, weight float64) error {
	_, err := tx.Exec(`
		INSERT INTO graph_edges (source_entity_id, target_entity_id, relation_type, chunk_id, weight)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source_entity_id, target_entity_id, relation_type, chunk_id) DO UPDATE SET
			weight = excluded.weight
	`, sourceID, targetID, relationType, chunkID, weight)
	if err != nil {
		return fmt.Errorf("failed to upsert edge: %w", err)
	}
	return nil
}

// pendingRef is a reference waiting to be linked to its targets
type pendingRef struct {
	id           int64
	sourceID     int64
	chunkID      int64
	relationType string
	name         string
	container    string
	importBase   string
	targetPath   string
	externalName string
	member       bool
	resolved     bool
	path         string // File the reference is in
	root         string // Project the reference is in
}

// candidate is a definition a reference may point at
type candidate struct {
	entityID   int64
	entityType string
	container  string
	path       string // File the definition is in
	root       string // Project the definition is in
}

// resolveSymbols links references to definitions. It handles references that
// were never resolved, and all references to names whose definitions changed.
func resolveSymbols(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT r.id, r.source_entity_id, r.chunk_id, r.relation_type, r.name, r.container,
		       r.import_base, r.target_path, r.external_name, r.member, r.resolved, f.path, r.root
		FROM symbol_refs r
		JOIN files f ON f.id = r.file_id
		WHERE r.resolved = 0 OR r.name IN (SELECT name FROM symbol_dirty)
	`)
	if err != nil {
		return fmt.Errorf("failed to query symbol references: %w", err)
	}
	var refs []pendingRef
	for rows.Next() {
		var r pendingRef
		if err := rows.Scan(&r.id, &r.sourceID, &r.chunkID, &r.relationType, &r.name, &r.container,
			&r.importBase, &r.targetPath, &r.externalName, &r.member, &r.resolved, &r.path, &r.root); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan symbol reference: %w", err)
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating symbol references: %w", err)
	}

	// Drop the edges of references that are resolved again, before adding any
	// new ones: references from one symbol to the same name share their edges.
	for _, r := range refs {
		if !r.resolved {
			continue
		}
		if _, err := tx.Exec(`
			DELETE FROM graph_edges
			WHERE source_entity_id = ? AND chunk_id = ? AND relation_type IN (?, ?)
			  AND target_entity_id IN (
				SELECT entity_id FROM symbol_defs WHERE name = ?
				UNION
				SELECT id FROM entities WHERE chunk_id = 0 AND name = ?
			  )
		`, r.sourceID, r.chunkID, r.relationType, RelationMayCall, r.name, r.externalName); err != nil {
			return fmt.Errorf("failed to delete stale edges: %w", err)
		}
	}

	candidates := make(map[string][]candidate)
	for _, r := range refs {
		cands, ok := candidates[r.name]
		if !ok {
			if cands, err = queryCandidates(tx, r.name); err != nil {
				return err
			}
			candidates[r.name] = cands
		}

		targets, weight := selectTargets(r, cands)
		relation := r.relationType
		if weight < weightExact && relation == relationCalls {
			relation = RelationMayCall
		}
		if relation == RelationMayCall {
			weight = weightByName
		}
		if len(targets) == 0 && r.externalName != "" {
			id, err := externalEntity(tx, r)
			if err != nil {
				return err
			}
			targets, weight, relation = []int64{id}, weightExact, r.relationType
		}
		for _, target := range targets {
			if err := upsertEdge(tx, r.sourceID, target, relation, r.chunkID, weight); err != nil {
				return err
			}
		}
		if !r.resolved {
			if _, err := tx.Exec("UPDATE symbol_refs SET resolved = 1 WHERE id = ?", r.id); err != nil {
				return fmt.Errorf("failed to mark reference resolved: %w", err)
			}
		}
	}

	if _, err := tx.Exec("DELETE FROM symbol_dirty"); err != nil {
		return fmt.Errorf("failed to clear dirty symbols: %w", err)
	}
	return nil
}

func queryCandidates(tx *sql.Tx, name string) ([]candidate, error) {
	rows, err := tx.Query(`
		SELECT d.entity_id, e.entity_type, d.container, f.path, d.root
		FROM symbol_defs d
		JOIN entities e ON e.id = d.entity_id
		JOIN files f ON f.id = d.file_id
		WHERE d.name = ?
	`, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query symbol definitions: %w", err)
	}
	defer rows.Close()

	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.entityID, &c.entityType, &c.container, &c.path, &c.root); err != nil {
			return nil, fmt.Errorf("failed to scan symbol definition: %w", err)
		}
		cands = append(cands, c)
	}
	return cands, rows.Err()
}

// externalEntity returns the placeholder entity for a reference whose target
// is not defined in the index. Placeholders have no chunk.
func externalEntity(tx *sql.Tx, r pendingRef) (int64, error) {
	entityType := EntityTypeExternal
	if r.relationType == relationImports {
		entityType = EntityTypePackage
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO entities (name, entity_type, chunk_id)
		VALUES (?, ?, 0)
	`, r.externalName, entityType); err != nil {
		return 0, fmt.Errorf("failed to insert external entity: %w", err)
	}
	var id int64
	if err := tx.QueryRow(`
		SELECT id FROM entities
		WHERE name = ? AND entity_type = ? AND chunk_id = 0
	`, r.externalName, entityType).Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to query external entity: %w", err)
	}
	return id, nil
}

// selectTargets picks the definitions a reference points at, and how certain
// the match is. The syntax tree gives the name and, often, where it comes
// from; it does not give types, so what remains is matched by name against
// the definitions closest to the reference.
func selectTargets(r pendingRef, cands []candidate) ([]int64, float64) {
	var pool []candidate
	for _, c := range cands {
		if (c.entityType == EntityTypePackage) == (r.relationType == relationImports) {
			pool = append(pool, c)
		}
	}

	filter := func(keep func(c candidate) bool) []candidate {
		var out []candidate
		for _, c := range pool {
			if keep(c) {
				out = append(out, c)
			}
		}
		return out
	}

	// Resolved by a type checker: the definition is that one, or not indexed
	if r.targetPath != "" {
		var ids []int64
		for _, c := range pool {
			if c.path == r.targetPath && c.container == r.container {
				ids = append(ids, c.entityID)
			}
		}
		return ids, weightExact
	}

	// Named through an import: the definition must be in that file or directory
	if r.importBase != "" {
		matched := filter(func(c candidate) bool { return matchesImport(c.path, r.importBase) })
		ids, _ := nearest(r.path, matched)
		return ids, weightExact
	}

	// Without an import, a name refers to something in the same project
	pool = filter(func(c candidate) bool { return c.root == r.root })

	// Called on self/this or on a named type: the definition must be in that container
	if r.container != "" {
		if matched := filter(func(c candidate) bool { return c.container == r.container }); len(matched) > 0 {
			ids, _ := nearest(r.path, matched)
			return ids, weightExact
		}
	}

	// A method call on a value of unknown type can only be matched by name,
	// and nothing says which of the definitions with that name it is
	if r.member {
		var ids []int64
		for _, c := range pool {
			ids = append(ids, c.entityID)
		}
		return ids, weightByName
	}

	// A plain call refers to a function or type, not a method
	ids, sameDir := nearest(r.path, filter(func(c candidate) bool { return c.container == "" }))
	if sameDir {
		return ids, weightExact
	}
	return ids, weightByName
}

// matchesImport reports whether a file can be what an import path ending in
// base refers to: the file itself (utils.py) or a file in that directory (db/x.go).
func matchesImport(path, base string) bool {
	file := filepath.Base(path)
	if strings.TrimSuffix(file, filepath.Ext(file)) == base {
		return true
	}
	return filepath.Base(filepath.Dir(path)) == base
}

// nearest returns the candidates closest to the referencing file: in the same
// file, else the same directory, else sharing the longest directory prefix.
// It also reports whether they are in the same directory.
func nearest(path string, cands []candidate) ([]int64, bool) {
	dir := filepath.Dir(path)
	best, sameDir := -1, false
	var ids []int64
	for _, c := range cands {
		candDir := filepath.Dir(c.path)
		score := 4 * commonDirs(dir, candDir)
		if candDir == dir {
			score += 2
		}
		if c.path == path {
			score++
		}
		if score > best {
			best, sameDir, ids = score, candDir == dir, ids[:0]
		}
		if score == best {
			ids = append(ids, c.entityID)
		}
	}
	return ids, sameDir
}

func splitDirs(dir string) []string {
	return strings.Split(filepath.ToSlash(dir), "/")
}

// commonDirs counts the leading directories two paths share
func commonDirs(a, b string) int {
	as, bs := splitDirs(a), splitDirs(b)
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

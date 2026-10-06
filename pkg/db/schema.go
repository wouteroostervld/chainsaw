package db

// Schema version for migration tracking
const SchemaVersion = "2.7.0"

// DDL statements for database initialization
const (
	// Meta table stores configuration and version info
	CreateMetaTable = `
CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);`

	// Files table tracks indexed files to avoid redundant processing
	CreateFilesTable = `
CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT UNIQUE NOT NULL,
    last_mod_time INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    status TEXT DEFAULT 'indexed',
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    queued_at DATETIME
);`

	// Index for fast path lookups
	CreateFilesPathIndex = `
CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);`

	// Index for hash-based change detection
	CreateFilesHashIndex = `
CREATE INDEX IF NOT EXISTS idx_files_hash ON files(content_hash);`

	// Index for queue queries
	CreateFilesStatusIndex = `
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status, queued_at);`

	// Vec_chunks virtual table for vector similarity search
	// Note: Dimension must be specified at creation time
	// Using cosine distance which is better for text embeddings like nomic-embed-text
	CreateVecChunksTableTemplate = `
CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    file_id INTEGER,
    content_snippet TEXT,
    start_line INTEGER,
    end_line INTEGER,
    embedding FLOAT[%d] distance_metric=cosine
);`

	// Entities table stores code entities (functions, types, etc.)
	// Note: Cannot use FK to vec_chunks (virtual table) - causes "malformed" errors
	CreateEntitiesTable = `
CREATE TABLE IF NOT EXISTS entities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    chunk_id INTEGER NOT NULL,
    UNIQUE(name, entity_type, chunk_id)
);`

	// For backward compat - same as above (FK to virtual tables not supported)
	CreateEntitiesTableWithFK = `
CREATE TABLE IF NOT EXISTS entities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    chunk_id INTEGER NOT NULL,
    UNIQUE(name, entity_type, chunk_id)
);`

	// Index for fast entity lookups by name
	CreateEntitiesNameIndex = `
CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);`

	// Index for filtering by entity type
	CreateEntitiesTypeIndex = `
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(entity_type);`

	// Index for finding entities in a chunk
	CreateEntitiesChunkIndex = `
CREATE INDEX IF NOT EXISTS idx_entities_chunk ON entities(chunk_id);`

	// Graph edges table stores knowledge graph relations between entities
	// Note: Cannot use FK to vec_chunks (virtual table) - causes "malformed" errors
	CreateGraphEdgesTable = `
CREATE TABLE IF NOT EXISTS graph_edges (
    source_entity_id INTEGER NOT NULL,
    target_entity_id INTEGER NOT NULL,
    relation_type TEXT NOT NULL,
    chunk_id INTEGER NOT NULL,
    weight REAL DEFAULT 1.0,
    PRIMARY KEY (source_entity_id, target_entity_id, relation_type, chunk_id),
    FOREIGN KEY(source_entity_id) REFERENCES entities(id) ON DELETE CASCADE,
    FOREIGN KEY(target_entity_id) REFERENCES entities(id) ON DELETE CASCADE
);`

	// For backward compat - same as above (FK to virtual tables not supported)
	CreateGraphEdgesTableWithFK = `
CREATE TABLE IF NOT EXISTS graph_edges (
    source_entity_id INTEGER NOT NULL,
    target_entity_id INTEGER NOT NULL,
    relation_type TEXT NOT NULL,
    chunk_id INTEGER NOT NULL,
    weight REAL DEFAULT 1.0,
    PRIMARY KEY (source_entity_id, target_entity_id, relation_type, chunk_id),
    FOREIGN KEY(source_entity_id) REFERENCES entities(id) ON DELETE CASCADE,
    FOREIGN KEY(target_entity_id) REFERENCES entities(id) ON DELETE CASCADE
);`

	// Index for finding all edges from a source entity
	CreateGraphSourceIndex = `
CREATE INDEX IF NOT EXISTS idx_graph_source ON graph_edges(source_entity_id);`

	// Index for finding all edges to a target entity
	CreateGraphTargetIndex = `
CREATE INDEX IF NOT EXISTS idx_graph_target ON graph_edges(target_entity_id);`

	// Index for filtering by relation type
	CreateGraphRelationIndex = `
CREATE INDEX IF NOT EXISTS idx_graph_relation ON graph_edges(relation_type);`

	// Symbol definitions found by parsing source files. Each row marks an
	// entity as a real definition, as opposed to a placeholder for something
	// outside the index.
	CreateSymbolDefsTable = `
CREATE TABLE IF NOT EXISTS symbol_defs (
    entity_id INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL,
    root TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    container TEXT NOT NULL DEFAULT ''
);`

	CreateSymbolDefsNameIndex = `
CREATE INDEX IF NOT EXISTS idx_symbol_defs_name ON symbol_defs(name);`

	CreateSymbolDefsFileIndex = `
CREATE INDEX IF NOT EXISTS idx_symbol_defs_file ON symbol_defs(file_id);`

	// Symbol references (calls, imports) found by parsing source files.
	// Graph edges are derived from these by matching names to definitions,
	// which can be redone whenever definitions appear or disappear.
	CreateSymbolRefsTable = `
CREATE TABLE IF NOT EXISTS symbol_refs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL,
    root TEXT NOT NULL DEFAULT '',
    source_entity_id INTEGER NOT NULL,
    chunk_id INTEGER NOT NULL,
    relation_type TEXT NOT NULL,
    name TEXT NOT NULL,
    container TEXT NOT NULL DEFAULT '',
    import_base TEXT NOT NULL DEFAULT '',
    target_path TEXT NOT NULL DEFAULT '',
    external_name TEXT NOT NULL,
    member BOOLEAN NOT NULL DEFAULT 0,
    resolved BOOLEAN NOT NULL DEFAULT 0
);`

	CreateSymbolRefsNameIndex = `
CREATE INDEX IF NOT EXISTS idx_symbol_refs_name ON symbol_refs(name);`

	CreateSymbolRefsFileIndex = `
CREATE INDEX IF NOT EXISTS idx_symbol_refs_file ON symbol_refs(file_id);`

	CreateSymbolRefsResolvedIndex = `
CREATE INDEX IF NOT EXISTS idx_symbol_refs_resolved ON symbol_refs(resolved);`

	// Names whose definitions changed; references to them need resolving again
	CreateSymbolDirtyTable = `
CREATE TABLE IF NOT EXISTS symbol_dirty (
    name TEXT PRIMARY KEY
);`

	// Outline nodes are the semantic chunks of a parsed file, as a tree: the
	// file skeleton at the root, each folded definition below the node whose
	// content holds its stub. A node keeps its full content even when it had
	// to be split into several chunks for embedding.
	CreateOutlineNodesTable = `
CREATE TABLE IF NOT EXISTS outline_nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL,
    parent_id INTEGER NOT NULL DEFAULT 0,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    content TEXT NOT NULL,
    stub TEXT NOT NULL DEFAULT ''
);`

	CreateOutlineNodesFileIndex = `
CREATE INDEX IF NOT EXISTS idx_outline_nodes_file ON outline_nodes(file_id);`

	// Maps each embedded chunk to the outline node it is (part of)
	CreateChunkNodesTable = `
CREATE TABLE IF NOT EXISTS chunk_nodes (
    chunk_id INTEGER PRIMARY KEY,
    node_id INTEGER NOT NULL
);`

	CreateChunkNodesNodeIndex = `
CREATE INDEX IF NOT EXISTS idx_chunk_nodes_node ON chunk_nodes(node_id);`

	// Enable WAL mode for concurrent reads/writes
	EnableWALMode = `PRAGMA journal_mode=WAL;`

	// Set reasonable WAL checkpoint parameters
	SetWALCheckpoint = `PRAGMA wal_autocheckpoint=1000;`

	// Enable foreign key constraints
	EnableForeignKeys = `PRAGMA foreign_keys=ON;`
)

// MetaKeys are standard keys stored in the meta table
const (
	MetaKeySchemaVersion = "schema_version"
	MetaKeyCreatedAt     = "created_at"
	MetaKeyLastIndexed   = "last_indexed"
	MetaKeyEmbeddingDim  = "embedding_dimension"
)

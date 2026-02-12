package db

// Schema version for migration tracking
const SchemaVersion = "2.4.0"

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
    PRIMARY KEY (source_entity_id, target_entity_id, chunk_id),
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
    PRIMARY KEY (source_entity_id, target_entity_id, chunk_id),
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

	// Chunk graph state table tracks which chunks have had graph extraction performed
	// Separate table since vec_chunks is a virtual table that can't be altered
	CreateChunkGraphStateTable = `
CREATE TABLE IF NOT EXISTS chunk_graph_state (
    chunk_id INTEGER PRIMARY KEY,
    graph_extracted BOOLEAN DEFAULT 0,
    extracted_at DATETIME
);`

	// Index for finding unextracted chunks
	CreateChunkGraphStateIndex = `
CREATE INDEX IF NOT EXISTS idx_chunk_graph_extracted ON chunk_graph_state(graph_extracted);`

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

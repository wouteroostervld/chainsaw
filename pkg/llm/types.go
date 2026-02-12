package llm

// Edge represents a relationship in the knowledge graph
type Edge struct {
	Source       string `json:"source"`
	SourceType   string `json:"source_type"`
	Target       string `json:"target"`
	TargetType   string `json:"target_type"`
	RelationType string `json:"relation_type"`
}

// ChunkInput contains a code chunk to analyze
type ChunkInput struct {
	ChunkID  int64  // Database chunk ID
	FileID   int64  // Database file ID
	FilePath string // File path for display in prompt
	Content  string // Code content
}

// EdgeWithMetadata represents an edge with source tracking
type EdgeWithMetadata struct {
	Edge          // Embedded edge data
	ChunkID int64 // Which chunk this edge came from
	FileID  int64 // Which file this edge came from
}

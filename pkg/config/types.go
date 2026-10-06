package config

// GlobalConfig represents the main configuration file at ~/.chainsaw/config.yaml
type GlobalConfig struct {
	Version       string              `yaml:"version"`
	ActiveProfile string              `yaml:"active_profile"`
	Profiles      map[string]*Profile `yaml:"profiles"`
}

// Profile represents a single configuration profile with all settings
type Profile struct {
	// Directory-level filtering
	Include []string `yaml:"include"` // Paths to recursively index
	Exclude []string `yaml:"exclude"` // Directory patterns to skip

	// File-level filtering (regex on absolute paths)
	Blacklist []string `yaml:"blacklist"` // Reject patterns (applied first)
	Whitelist []string `yaml:"whitelist"` // Exception patterns (override blacklist)

	// Embedding settings
	EmbeddingModel string `yaml:"embedding_model"`
	ChunkSize      int    `yaml:"chunk_size"`
	Overlap        int    `yaml:"overlap"`
}

// LocalConfig represents a project-local .config.yaml file
// Only include, exclude, and blacklist are allowed
type LocalConfig struct {
	Include   []string `yaml:"include,omitempty"`   // CLI only - additional search paths
	Exclude   []string `yaml:"exclude,omitempty"`   // Additional directories to skip
	Blacklist []string `yaml:"blacklist,omitempty"` // Additional file patterns to reject
}

// MergedConfig represents the final runtime configuration after merging global + local
type MergedConfig struct {
	// Merged filters
	Include   []string
	Exclude   []string
	Blacklist []string
	Whitelist []string // Global only, never modified by local

	// Model settings (from global profile only)
	EmbeddingModel string
	ChunkSize      int
	Overlap        int

	// Metadata for tracking
	LocalConfigPath string // Path to the .config.yaml that was used (empty if none)
	ProfileName     string // Name of the active profile
}

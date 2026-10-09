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

	// Which kinds of index are kept, by name (see DefaultIndex). A kind that
	// is not named keeps its default.
	Index map[string]bool `yaml:"index,omitempty"`

	// The model that makes embeddings. Needed for the embeddings index.
	Embeddings *EmbeddingsConfig `yaml:"embeddings,omitempty"`

	// Settings of older versions. EmbeddingModel names the model when
	// Embeddings does not; the others are not used.
	EmbeddingModel string `yaml:"embedding_model"`
	ChunkSize      int    `yaml:"chunk_size"`
	Overlap        int    `yaml:"overlap"`
}

// EmbeddingsConfig says where embeddings come from
type EmbeddingsConfig struct {
	Provider string `yaml:"provider,omitempty"` // "ollama" or "openai"; told from the other settings when empty
	BaseURL  string `yaml:"base_url,omitempty"` // e.g. "http://localhost:11434" or "https://api.openai.com/v1"
	APIKey   string `yaml:"api_key,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

// LocalConfig represents a project-local .config.yaml file
// Only include, exclude, blacklist and index are allowed. Where embeddings
// come from is never read from a project: a checkout must not be able to
// say where its source is sent.
type LocalConfig struct {
	Include   []string `yaml:"include,omitempty"`   // CLI only - additional search paths
	Exclude   []string `yaml:"exclude,omitempty"`   // Additional directories to skip
	Blacklist []string `yaml:"blacklist,omitempty"` // Additional file patterns to reject

	// Kinds of index switched on or off for the files below this directory
	Index map[string]bool `yaml:"index,omitempty"`
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

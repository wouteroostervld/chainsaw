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

	// LLM Provider settings
	LLMProvider string `yaml:"llm_provider,omitempty"` // "ollama" or "openai" (optional: auto-detect from URL)
	LLMBaseURL  string `yaml:"llm_base_url,omitempty"` // e.g., "https://openrouter.ai/v1"
	LLMAPIKey   string `yaml:"llm_api_key,omitempty"`  // API key for OpenRouter, etc.

	// Graph extraction settings
	GraphDriver *GraphDriverConfig `yaml:"graph_driver,omitempty"`
}

// GraphDriverConfig configures the graph extraction model and parsing strategy
type GraphDriverConfig struct {
	Model              string  `yaml:"model"`
	Temperature        float64 `yaml:"temperature"`
	Concurrency        int     `yaml:"concurrency"`
	BatchSize          int     `yaml:"batch_size,omitempty"` // Chunks per LLM call (0=auto-detect from model)
	OutputFormat       string  `yaml:"output_format"`        // "json" or "regex"
	ParsingRegex       string  `yaml:"parsing_regex"`        // For regex mode
	CustomSystemPrompt string  `yaml:"custom_system_prompt"` // Optional override
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
	LLMProvider    string // "ollama" or "openai"
	LLMBaseURL     string
	LLMAPIKey      string
	GraphDriver    *GraphDriverConfig

	// Metadata for tracking
	LocalConfigPath string // Path to the .config.yaml that was used (empty if none)
	ProfileName     string // Name of the active profile
}

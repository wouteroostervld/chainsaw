package config

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadGlobalConfigFromPath loads global config from a specific path using provided FileSystem
func LoadGlobalConfigFromPath(path string, fs FileSystem) (*GlobalConfig, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config GlobalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate active profile exists
	if config.ActiveProfile == "" {
		return nil, fmt.Errorf("active_profile not specified in config")
	}

	if _, ok := config.Profiles[config.ActiveProfile]; !ok {
		return nil, fmt.Errorf("active profile %s not found in config", config.ActiveProfile)
	}

	return &config, nil
}

// FindLocalConfigWithFS walks up from startDir to find the nearest .config.yaml file
// Returns the path to the config file, or empty string if not found
func FindLocalConfigWithFS(startDir string, fs FileSystem) (string, error) {
	// Normalize the start directory
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	currentDir := absDir
	for {
		configPath := filepath.Join(currentDir, ".config.yaml")

		// Check if .config.yaml exists
		if _, err := fs.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)

		// Stop if we've reached the root
		if parentDir == currentDir {
			break
		}

		currentDir = parentDir
	}

	// No config found
	return "", nil
}

// LoadLocalConfigWithFS loads and validates a local .config.yaml file
func LoadLocalConfigWithFS(path string, fs FileSystem) (*LocalConfig, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read local config: %w", err)
	}

	var config LocalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse local config: %w", err)
	}

	return &config, nil
}

// MergeConfigForDaemon merges global and local configs with daemon-specific rules
// Daemon can only ADD to exclude and blacklist
func MergeConfigForDaemon(global *GlobalConfig, local *LocalConfig, localConfigDir string) (*MergedConfig, error) {
	profile, ok := global.Profiles[global.ActiveProfile]
	if !ok {
		return nil, fmt.Errorf("active profile %s not found", global.ActiveProfile)
	}

	merged := &MergedConfig{
		// Copy global settings (daemon doesn't use local include)
		Include:   profile.Include,
		Whitelist: profile.Whitelist,

		// Model settings from profile
		EmbeddingModel: profile.EmbeddingModel,
		ChunkSize:      profile.ChunkSize,
		Overlap:        profile.Overlap,
		LLMBaseURL:     profile.LLMBaseURL,
		LLMAPIKey:      profile.LLMAPIKey,
		GraphDriver:    profile.GraphDriver,
		ProfileName:    global.ActiveProfile,
	}

	// Merge exclude (global + local)
	merged.Exclude = append([]string{}, profile.Exclude...)
	if local != nil {
		// Resolve local exclude paths
		resolvedExclude, err := ResolvePaths(localConfigDir, local.Exclude)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve local exclude paths: %w", err)
		}
		merged.Exclude = append(merged.Exclude, resolvedExclude...)
	}

	// Merge blacklist (global + local, already regex patterns - no path resolution needed)
	merged.Blacklist = append([]string{}, profile.Blacklist...)
	if local != nil {
		merged.Blacklist = append(merged.Blacklist, local.Blacklist...)
	}

	return merged, nil
}

// MergeConfigForCLI merges global and local configs with CLI-specific rules
// CLI can ADD to include (validated), exclude, and blacklist
func MergeConfigForCLI(global *GlobalConfig, local *LocalConfig, localConfigDir string) (*MergedConfig, error) {
	profile, ok := global.Profiles[global.ActiveProfile]
	if !ok {
		return nil, fmt.Errorf("active profile %s not found", global.ActiveProfile)
	}

	merged := &MergedConfig{
		// Copy global settings
		Whitelist: profile.Whitelist,

		// Model settings from profile
		EmbeddingModel: profile.EmbeddingModel,
		ChunkSize:      profile.ChunkSize,
		Overlap:        profile.Overlap,
		LLMBaseURL:     profile.LLMBaseURL,
		LLMAPIKey:      profile.LLMAPIKey,
		GraphDriver:    profile.GraphDriver,
		ProfileName:    global.ActiveProfile,
	}

	// Merge include (global + validated local)
	merged.Include = append([]string{}, profile.Include...)
	if local != nil && len(local.Include) > 0 {
		// Validate that local include paths are within global include
		if err := ValidateIncludeWithinGlobal(local.Include, profile.Include, localConfigDir); err != nil {
			return nil, fmt.Errorf("local include validation failed: %w", err)
		}

		// Resolve and add local include paths
		resolvedInclude, err := ResolvePaths(localConfigDir, local.Include)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve local include paths: %w", err)
		}
		merged.Include = append(merged.Include, resolvedInclude...)
	}

	// Merge exclude (global + local)
	merged.Exclude = append([]string{}, profile.Exclude...)
	if local != nil {
		resolvedExclude, err := ResolvePaths(localConfigDir, local.Exclude)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve local exclude paths: %w", err)
		}
		merged.Exclude = append(merged.Exclude, resolvedExclude...)
	}

	// Merge blacklist (global + local)
	merged.Blacklist = append([]string{}, profile.Blacklist...)
	if local != nil {
		merged.Blacklist = append(merged.Blacklist, local.Blacklist...)
	}

	return merged, nil
}

// GetConfigForFileWithFS finds and merges the appropriate config for a specific file
// Used by daemon to get per-file configuration
func GetConfigForFileWithFS(filePath string, global *GlobalConfig, isDaemon bool, fs FileSystem) (*MergedConfig, error) {
	// Get the directory containing the file
	fileDir := filepath.Dir(filePath)

	// Find local config (if any)
	localConfigPath, err := FindLocalConfigWithFS(fileDir, fs)
	if err != nil {
		return nil, fmt.Errorf("failed to find local config: %w", err)
	}

	// If no local config, use global only
	if localConfigPath == "" {
		if isDaemon {
			return MergeConfigForDaemon(global, nil, "")
		}
		return MergeConfigForCLI(global, nil, "")
	}

	// Load local config
	localConfig, err := LoadLocalConfigWithFS(localConfigPath, fs)
	if err != nil {
		return nil, fmt.Errorf("failed to load local config: %w", err)
	}

	// Get config directory for path resolution
	localConfigDir := filepath.Dir(localConfigPath)

	// Merge based on context
	var merged *MergedConfig
	if isDaemon {
		merged, err = MergeConfigForDaemon(global, localConfig, localConfigDir)
	} else {
		merged, err = MergeConfigForCLI(global, localConfig, localConfigDir)
	}

	if err != nil {
		return nil, err
	}

	merged.LocalConfigPath = localConfigPath
	return merged, nil
}

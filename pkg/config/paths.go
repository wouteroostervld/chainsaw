package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveRelativePath resolves a path relative to the config file's directory
// Handles relative paths, absolute paths, and tilde expansion
func ResolveRelativePath(configDir, path string) (string, error) {
	// Expand tilde
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	// If already absolute, return as-is
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	// Resolve relative to config directory
	absPath := filepath.Join(configDir, path)
	return filepath.Clean(absPath), nil
}

// NormalizePath cleans and normalizes a path
func NormalizePath(path string) (string, error) {
	// Expand tilde
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	// Get absolute path and clean it
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return filepath.Clean(absPath), nil
}

// ValidatePathSecurity checks if a path is within allowed roots
// Prevents path traversal attacks
func ValidatePathSecurity(path string, allowedRoots []string) error {
	// Normalize the path
	normalizedPath, err := NormalizePath(path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}

	// Check against each allowed root
	for _, root := range allowedRoots {
		normalizedRoot, err := NormalizePath(root)
		if err != nil {
			continue // Skip invalid roots
		}

		// Check if path is within this root
		if strings.HasPrefix(normalizedPath, normalizedRoot+string(filepath.Separator)) ||
			normalizedPath == normalizedRoot {
			return nil
		}
	}

	return fmt.Errorf("path %s is not within any allowed root", path)
}

// ValidateIncludeWithinGlobal validates that local include paths are within global include scope
// This is a security check to prevent CLI from searching unauthorized directories
func ValidateIncludeWithinGlobal(localInclude []string, globalInclude []string, configDir string) error {
	for _, localPath := range localInclude {
		// Resolve local path relative to config directory
		resolvedLocal, err := ResolveRelativePath(configDir, localPath)
		if err != nil {
			return fmt.Errorf("failed to resolve local include path %s: %w", localPath, err)
		}

		// Check if it's within any global include path
		withinGlobal := false
		for _, globalPath := range globalInclude {
			normalizedGlobal, err := NormalizePath(globalPath)
			if err != nil {
				continue
			}

			// Check if local path is within global path
			if strings.HasPrefix(resolvedLocal, normalizedGlobal+string(filepath.Separator)) ||
				resolvedLocal == normalizedGlobal {
				withinGlobal = true
				break
			}
		}

		if !withinGlobal {
			return fmt.Errorf("local include path %s is not within global include scope", localPath)
		}
	}

	return nil
}

// ResolvePaths resolves a list of paths relative to a config directory
func ResolvePaths(configDir string, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		resolvedPath, err := ResolveRelativePath(configDir, path)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %s: %w", path, err)
		}
		resolved = append(resolved, resolvedPath)
	}
	return resolved, nil
}

package filter

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
)

// ShouldIndexDirectory checks if a directory should be indexed
// Returns: is_in_include AND NOT is_in_exclude
func ShouldIndexDirectory(path string, include []string, exclude []string) (bool, error) {
	// Normalize the path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	absPath = filepath.Clean(absPath)

	// Check if path is in include
	inInclude := false
	for _, includePath := range include {
		normalizedInclude := filepath.Clean(includePath)

		// Check if path is within or equals the include path
		if strings.HasPrefix(absPath, normalizedInclude+string(filepath.Separator)) ||
			absPath == normalizedInclude {
			inInclude = true
			break
		}
	}

	if !inInclude {
		return false, nil
	}

	// Check if path matches any exclude pattern
	for _, excludePattern := range exclude {
		// Support both glob patterns and literal paths
		matched, err := filepath.Match(excludePattern, filepath.Base(absPath))
		if err != nil {
			// If pattern is invalid, try literal comparison
			if strings.Contains(absPath, excludePattern) {
				return false, nil
			}
		} else if matched {
			return false, nil
		}

		// Also check full path match
		if strings.HasPrefix(absPath, excludePattern) ||
			strings.Contains(absPath, string(filepath.Separator)+excludePattern+string(filepath.Separator)) ||
			strings.HasSuffix(absPath, string(filepath.Separator)+excludePattern) {
			return false, nil
		}
	}

	return true, nil
}

// ShouldIndexFile checks if a file should be indexed
// Returns: NOT matches_blacklist OR matches_whitelist
// Equivalent: File rejected if matches_blacklist AND NOT matches_whitelist
func ShouldIndexFile(filePath string, blacklist []string, whitelist []string) (bool, error) {
	// Normalize path to absolute
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false, err
	}

	slog.Debug("Checking file filter", "path", absPath, "blacklist_count", len(blacklist))

	// Check blacklist first
	matchesBlacklist := false
	matchedPattern := ""
	for _, pattern := range blacklist {
		matched, err := regexp.MatchString(pattern, absPath)
		if err != nil {
			slog.Debug("Invalid regex pattern", "pattern", pattern, "error", err)
			continue
		}
		if matched {
			matchesBlacklist = true
			matchedPattern = pattern
			slog.Debug("Matched blacklist pattern", "pattern", pattern, "path", absPath)
			break
		}
	}

	// If doesn't match blacklist, allow
	if !matchesBlacklist {
		slog.Debug("No blacklist match - allowing file", "path", absPath)
		return true, nil
	}

	// Matches blacklist - check if whitelist provides exception
	for _, pattern := range whitelist {
		matched, err := regexp.MatchString(pattern, absPath)
		if err != nil {
			continue
		}
		if matched {
			slog.Debug("Whitelist exception matched - allowing file", "pattern", pattern, "path", absPath)
			return true, nil
		}
	}

	// Matched blacklist and no whitelist exception - reject
	slog.Debug("Rejecting file", "path", absPath, "blacklist_pattern", matchedPattern)
	return false, nil
}

// MatchesPattern checks if a path matches a glob or regex pattern
func MatchesPattern(path string, pattern string) bool {
	// Try as glob pattern first
	matched, err := filepath.Match(pattern, filepath.Base(path))
	if err == nil && matched {
		return true
	}

	// Try as regex pattern
	matched, err = regexp.MatchString(pattern, path)
	if err == nil && matched {
		return true
	}

	// Try literal substring match
	return strings.Contains(path, pattern)
}

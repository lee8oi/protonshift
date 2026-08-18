// exclude.go
package main

import (
	"os"
	"path/filepath"
	"strings"
)

// excludePattern represents a single glob pattern for excluding
// files or directories during push operations.
type excludePattern struct {
	pattern string
	dirOnly bool // true if pattern had a trailing /
}

// loadIgnoreFile reads .psignore from the given directory.
// Returns nil patterns (not an error) if the file doesn't exist.
func loadIgnoreFile(dir string) ([]excludePattern, error) {
	ignorePath := filepath.Join(dir, ".psignore")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patterns []excludePattern
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, parsePattern(line))
	}

	return patterns, nil
}

// parseExcludeFlag parses a comma-separated --exclude flag value
// into a slice of excludePattern structs.
func parseExcludeFlag(s string) []excludePattern {
	if s == "" {
		return nil
	}

	var patterns []excludePattern
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		patterns = append(patterns, parsePattern(p))
	}

	return patterns
}

// parsePattern converts a raw pattern string into an excludePattern,
// detecting trailing slashes for directory-only matching.
func parsePattern(s string) excludePattern {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "/") {
		return excludePattern{
			pattern: strings.TrimSuffix(s, "/"),
			dirOnly: true,
		}
	}
	return excludePattern{pattern: s}
}

// mergePatterns combines two pattern slices into one.
func mergePatterns(a, b []excludePattern) []excludePattern {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	return append(a, b...)
}

// isExcluded checks whether a given name matches any of the
// exclude patterns. The isDir parameter determines whether
// dirOnly patterns are eligible to match.
func isExcluded(name string, isDir bool, patterns []excludePattern) bool {
	for _, p := range patterns {
		// dirOnly patterns only match directories
		if p.dirOnly && !isDir {
			continue
		}
		matched, err := filepath.Match(p.pattern, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}

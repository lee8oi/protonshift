package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Profile represents a saved upload/download configuration.
// Profiles are loaded from a JSON config file and allow the user
// to run recurring transfers without specifying paths each time.
type Profile struct {
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	DirConflict  string `json:"dir_conflict"`
	FileConflict string `json:"file_conflict"`
	BrowserPath  string `json:"browser"`
	ProtonExe    string `json:"binary"`
	Verbose      bool   `json:"verbose"`
}

// ConfigFile represents the on-disk JSON structure containing
// one or more named profiles and an optional default.
type ConfigFile struct {
	Profiles       map[string]Profile `json:"profiles"`
	DefaultProfile string             `json:"default_profile"`
}

// RuntimeConfig is the merged configuration used during execution.
// It combines built-in defaults, config file values, and command-line
// flags according to the resolution priority:
//  1. Command-line flags (highest)
//  2. Named profile from config file
//  3. Default profile from config file
//  4. Built-in defaults (lowest)
type RuntimeConfig struct {
	Subcommand   string // "push", "pull", or "list"
	Source       string
	Destination  string
	DirConflict  string
	FileConflict string
	BrowserPath  string
	ProtonExe    string
	Verbose      bool
	ConfigPath   string
	ProfileName  string
	// Explicit tracks which fields were set via command-line flags.
	// These take priority over config file values.
	Explicit explicitFlags
}

// explicitFlags tracks which command-line flags were explicitly set
// so we know which values should override the config file.
type explicitFlags struct {
	Source       bool
	Destination  bool
	DirConflict  bool
	FileConflict bool
	BrowserPath  bool
	ProtonExe    bool
	Verbose      bool
}

// Built-in defaults applied when no config or flag overrides exist.
const (
	defaultDirConflict  = "merge"
	defaultFileConflict = "skip"
	defaultProtonExe    = "proton-drive"
)

// configFilePath resolves the config file location by checking
// the following locations in order (first match wins):
//  1. --config flag value (explicit path)
//  2. ./protonshift.json (current directory)
//  3. ~/.protonshift/config.json (user home directory)
func configFilePath(explicitPath string) string {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath
		}
	}

	// Check current directory
	cwd, err := os.Getwd()
	if err == nil {
		p := filepath.Join(cwd, "protonshift.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Check user home directory
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".protonshift", "config.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// loadConfigFile reads and parses the JSON config file at the given path.
// Returns an empty ConfigFile if the path is empty or the file doesn't exist.
func loadConfigFile(path string) (*ConfigFile, error) {
	if path == "" {
		return &ConfigFile{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cf ConfigFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cf.Profiles == nil {
		cf.Profiles = make(map[string]Profile)
	}

	return &cf, nil
}

// resolveConfig merges defaults, config file values, and explicit flag
// values into a single RuntimeConfig ready for execution.
func resolveConfig(rt *RuntimeConfig) (*RuntimeConfig, error) {
	// Load config file
	cf, err := loadConfigFile(configFilePath(rt.ConfigPath))
	if err != nil {
		return nil, err
	}

	// Determine which profile to use
	var profile Profile
	profileFound := false

	if rt.ProfileName != "" {
		// Named profile specified via --profile flag
		p, ok := cf.Profiles[rt.ProfileName]
		if !ok {
			return nil, fmt.Errorf("profile '%s' not found in config", rt.ProfileName)
		}
		profile = p
		profileFound = true
	} else if cf.DefaultProfile != "" {
		// Fall back to default profile
		p, ok := cf.Profiles[cf.DefaultProfile]
		if ok {
			profile = p
			profileFound = true
		}
	}

	// Apply built-in defaults first
	result := &RuntimeConfig{
		Subcommand:   rt.Subcommand,
		DirConflict:  defaultDirConflict,
		FileConflict: defaultFileConflict,
		ProtonExe:    defaultProtonExe,
		Explicit:     rt.Explicit,
		ConfigPath:   rt.ConfigPath,
		ProfileName:  rt.ProfileName,
	}

	// Layer on config file profile values (if found)
	if profileFound {
		if !rt.Explicit.Source && profile.Source != "" {
			result.Source = profile.Source
		}
		if !rt.Explicit.Destination && profile.Destination != "" {
			result.Destination = profile.Destination
		}
		if !rt.Explicit.DirConflict && profile.DirConflict != "" {
			result.DirConflict = profile.DirConflict
		}
		if !rt.Explicit.FileConflict && profile.FileConflict != "" {
			result.FileConflict = profile.FileConflict
		}
		if !rt.Explicit.BrowserPath && profile.BrowserPath != "" {
			result.BrowserPath = profile.BrowserPath
		}
		if !rt.Explicit.ProtonExe && profile.ProtonExe != "" {
			result.ProtonExe = profile.ProtonExe
		}
		result.Verbose = profile.Verbose
	}

	// Layer on explicit command-line flags (override everything)
	if rt.Explicit.Source {
		result.Source = rt.Source
	}
	if rt.Explicit.Destination {
		result.Destination = rt.Destination
	}
	if rt.Explicit.DirConflict {
		result.DirConflict = rt.DirConflict
	}
	if rt.Explicit.FileConflict {
		result.FileConflict = rt.FileConflict
	}
	if rt.Explicit.BrowserPath {
		result.BrowserPath = rt.BrowserPath
	}
	if rt.Explicit.ProtonExe {
		result.ProtonExe = rt.ProtonExe
	}
	if rt.Explicit.Verbose {
		result.Verbose = rt.Verbose
	}

	return result, nil
}

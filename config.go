// config.go
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
	Local        string `json:"local"`
	Remote       string `json:"remote"`
	DirConflict  string `json:"dir_conflict"`
	FileConflict string `json:"file_conflict"`
	BrowserPath  string `json:"browser"`
	ProtonExe    string `json:"binary"`
	Verbose      bool   `json:"verbose"`
	Exclude      string `json:"exclude"`
}

// ConfigFile represents the on-disk JSON structure containing
// one or more named profiles, an optional default profile, and
// an optional top-level defaults section.
type ConfigFile struct {
	Defaults       *Profile           `json:"defaults"`
	Profiles       map[string]Profile `json:"profiles"`
	DefaultProfile string             `json:"default_profile"`
}

// RuntimeConfig is the merged configuration used during execution.
// It combines built-in defaults, config file defaults, profile
// values, and command-line flags according to the resolution priority:
//  1. Command-line flags (highest)
//  2. Named profile from config file
//  3. Config file "defaults" section
//  4. Built-in defaults (lowest)
type RuntimeConfig struct {
	Subcommand   string // "push", "pull", or "list"
	Local        string
	Remote       string
	DirConflict  string
	FileConflict string
	BrowserPath  string
	ProtonExe    string
	Verbose      bool
	ConfigPath   string
	ProfileName  string
	Exclude      string
	// Explicit tracks which fields were set via command-line flags.
	// These take priority over config file values.
	Explicit explicitFlags
}

// explicitFlags tracks which command-line flags were explicitly set
// so we know which values should override the config file.
type explicitFlags struct {
	Local        bool
	Remote       bool
	DirConflict  bool
	FileConflict bool
	BrowserPath  bool
	ProtonExe    bool
	Verbose      bool
	Exclude      bool
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

// resolveConfig merges built-in defaults, config file defaults,
// profile values, and explicit flag values into a single
// RuntimeConfig ready for execution.
//
// Resolution priority (highest to lowest):
//  1. Command-line flags
//  2. Named profile from config file
//  3. Config file "defaults" section
//  4. Built-in constants (merge/skip/proton-drive)
func resolveConfig(rt *RuntimeConfig) (*RuntimeConfig, error) {
	// Load config file
	cf, err := loadConfigFile(configFilePath(rt.ConfigPath))
	if err != nil {
		return nil, err
	}

	// Start with built-in defaults
	result := &RuntimeConfig{
		Subcommand:   rt.Subcommand,
		DirConflict:  defaultDirConflict,
		FileConflict: defaultFileConflict,
		ProtonExe:    defaultProtonExe,
		Explicit:     rt.Explicit,
		ConfigPath:   rt.ConfigPath,
		ProfileName:  rt.ProfileName,
	}

	// Layer 1: Apply config file "defaults" section
	if cf.Defaults != nil {
		if cf.Defaults.DirConflict != "" {
			result.DirConflict = cf.Defaults.DirConflict
		}
		if cf.Defaults.FileConflict != "" {
			result.FileConflict = cf.Defaults.FileConflict
		}
		if cf.Defaults.BrowserPath != "" {
			result.BrowserPath = cf.Defaults.BrowserPath
		}
		if cf.Defaults.ProtonExe != "" {
			result.ProtonExe = cf.Defaults.ProtonExe
		}
		if cf.Defaults.Verbose {
			result.Verbose = cf.Defaults.Verbose
		}
		if cf.Defaults.Exclude != "" {
			result.Exclude = cf.Defaults.Exclude
		}
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

	// Layer 2: Apply profile values (override config file defaults)
	if profileFound {
		if profile.Local != "" {
			result.Local = profile.Local
		}
		if profile.Remote != "" {
			result.Remote = profile.Remote
		}
		if profile.DirConflict != "" {
			result.DirConflict = profile.DirConflict
		}
		if profile.FileConflict != "" {
			result.FileConflict = profile.FileConflict
		}
		if profile.BrowserPath != "" {
			result.BrowserPath = profile.BrowserPath
		}
		if profile.ProtonExe != "" {
			result.ProtonExe = profile.ProtonExe
		}
		if profile.Verbose {
			result.Verbose = profile.Verbose
		}
		if profile.Exclude != "" {
			result.Exclude = profile.Exclude
		}
	}

	// Layer 3: Apply explicit command-line flags (override everything)
	if rt.Explicit.Local {
		result.Local = rt.Local
	}
	if rt.Explicit.Remote {
		result.Remote = rt.Remote
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
	if rt.Explicit.Exclude {
		result.Exclude = rt.Exclude
	}

	return result, nil
}

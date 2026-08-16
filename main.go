// main.go
package main

import (
	"flag"
	"fmt"
	"os"
)

// usage prints the help text for protonshift.
func usage() {
	fmt.Fprintln(os.Stderr, `protonshift — a wrapper for the Proton Drive CLI

Usage:
  protonshift push <source> <destination> [flags]
  protonshift pull <source> <destination> [flags]
  protonshift list <path> [flags]

Subcommands:
  push   Upload files from a local source to Proton Drive
  pull   Download files from Proton Drive to a local destination
  list   List files and folders on Proton Drive

Flags:
  --browser <path>       Browser executable to open auth URL
  --dir-conflict <str>   Directory conflict strategy (default: merge)
  --file-conflict <str>  File conflict strategy (default: skip)
  --binary <path>        Path to proton-drive executable
                         (default: proton-drive, assumes PATH)
  --profile <name>       Use a named profile from config file
  --config <path>        Path to config file (default: searched)
  --verbose              Show raw proton-drive CLI output

Examples:
  protonshift push D:\DCIM /my-files
  protonshift pull /my-files/docs C:\Downloads
  protonshift list /my-files
  protonshift push --profile dcim
  protonshift push D:\DCIM /my-files --dir-conflict merge --file-conflict skip`)
}

func main() {
	args := os.Args[1:]

	// Need at least a subcommand
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	subcommand := args[0]
	rest := args[1:]

	switch subcommand {
	case "push", "pull", "list":
		if err := run(subcommand, rest); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", subcommand)
		usage()
		os.Exit(1)
	}
}

// run executes a subcommand with the given flags.
// It parses flags, resolves configuration, performs auth if needed,
// and dispatches to the appropriate operation handler.
func run(subcommand string, args []string) error {
	fs := flag.NewFlagSet(subcommand, flag.ExitOnError)

	rt := &RuntimeConfig{
		Subcommand: subcommand,
	}

	var dirConflict, fileConflict, browserPath, protonExe, profile, configPath string
	var verbose bool

	fs.StringVar(&dirConflict, "dir-conflict", "", "Directory conflict strategy")
	fs.StringVar(&fileConflict, "file-conflict", "", "File conflict strategy")
	fs.StringVar(&browserPath, "browser", "", "Browser executable path for auth")
	fs.StringVar(&protonExe, "binary", "", "Path to proton-drive executable")
	fs.StringVar(&profile, "profile", "", "Named profile from config file")
	fs.StringVar(&configPath, "config", "", "Path to config file")
	fs.BoolVar(&verbose, "verbose", false, "Show raw proton-drive output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	positional := fs.Args()

	// Map parsed flags into RuntimeConfig and track which were explicit
	rt.DirConflict = dirConflict
	rt.FileConflict = fileConflict
	rt.BrowserPath = browserPath
	rt.ProtonExe = protonExe
	rt.ProfileName = profile
	rt.ConfigPath = configPath
	rt.Verbose = verbose

	// Track explicit flags (non-empty string or flag visited)
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "dir-conflict":
			rt.Explicit.DirConflict = true
		case "file-conflict":
			rt.Explicit.FileConflict = true
		case "browser":
			rt.Explicit.BrowserPath = true
		case "binary":
			rt.Explicit.ProtonExe = true
		case "verbose":
			rt.Explicit.Verbose = true
		}
	})

	// Handle positional args (source, destination)
	// For "list", only one positional arg (path) is expected
	// For "push"/"pull", two positional args (source, destination) expected
	if subcommand == "list" {
		if len(positional) >= 1 {
			rt.Source = positional[0]
			rt.Explicit.Source = true
		}
	} else {
		if len(positional) >= 1 {
			rt.Source = positional[0]
			rt.Explicit.Source = true
		}
		if len(positional) >= 2 {
			rt.Destination = positional[1]
			rt.Explicit.Destination = true
		}
	}

	// Validate: push and pull require source and destination
	// (either from positional args or from config profile)
	config, err := resolveConfig(rt)
	if err != nil {
		return err
	}

	if subcommand == "push" || subcommand == "pull" {
		if config.Source == "" || config.Destination == "" {
			if config.ProfileName == "" {
				return fmt.Errorf(
					"%s requires <source> <destination>, "+
						"or a --profile with those values set",
					subcommand,
				)
			}
			return fmt.Errorf(
				"profile '%s' is missing source or destination",
				config.ProfileName,
			)
		}
	}

	if subcommand == "list" && config.Source == "" {
		if config.ProfileName == "" {
			return fmt.Errorf("list requires <path>")
		}
		return fmt.Errorf(
			"profile '%s' is missing source path for list",
			config.ProfileName,
		)
	}

	// Ensure auth is valid (may trigger browser-based login)
	if err := ensureAuth(config); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Dispatch to the appropriate operation
	switch subcommand {
	case "push":
		return doPush(config)
	case "pull":
		return doPull(config)
	case "list":
		return doList(config)
	}

	return nil
}

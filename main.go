// main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// stringFlags is the set of flags that take a value argument.
var stringFlags = map[string]bool{
	"--browser":       true,
	"--dir-conflict":  true,
	"--file-conflict": true,
	"--binary":        true,
	"--profile":       true,
	"--config":        true,
	"--exclude":       true,
}

// separateArgs splits a mixed argument list into flag arguments
// and positional arguments, allowing flags to appear anywhere in
// the command line (before or after positional args).
// This works around Go's flag package, which stops parsing flags
// at the first non-flag argument.
func separateArgs(args []string) (flags []string, positionals []string) {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			flags = append(flags, args[i])
			// If this flag takes a value and one is provided, grab it
			if stringFlags[args[i]] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else if strings.HasPrefix(args[i], "-") && args[i] != "-" {
			flags = append(flags, args[i])
		} else {
			positionals = append(positionals, args[i])
		}
	}
	return flags, positionals
}

// usage prints the help text for protonshift.
func usage() {
	fmt.Fprintln(os.Stderr, `protonshift — a wrapper for the Proton Drive CLI

Usage:
  protonshift push <local> <remote> [flags]
  protonshift pull <remote> <local> [flags]
  protonshift list <path> [flags]

Subcommands:
  push   Upload files from a local source to Proton Drive
  pull   Download files from Proton Drive to a local destination
  list   List files and folders on Proton Drive

Flags:
  --browser <path>       Browser executable to open auth URL
  --dir-conflict <str>   Folder conflict strategy: merge, rename,
                         replace, skip (default: merge)
  --file-conflict <str>  File conflict strategy: create-new-revision,
                         rename, replace, skip (default: skip)
  --binary <path>        Path to proton-drive executable
                         (default: proton-drive, assumes PATH)
  --profile <name>       Use a named profile from config file
  --config <path>        Path to config file (default: searched)
  --exclude <patterns>   Comma-separated glob patterns to skip
                         (e.g. "*.tmp,thumbnails/,*.cache")
  --verbose              Show raw proton-drive CLI output

Examples:
  protonshift push D:\DCIM /my-files
  protonshift pull /my-files/docs C:\Downloads
  protonshift list /my-files
  protonshift push --profile dcim
  protonshift pull --profile dcim
  protonshift push D:\DCIM /my-files --dir-conflict merge --file-conflict skip
  protonshift push D:\DCIM /my-files --exclude "*.tmp,thumbnails/"`)
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
func run(subcommand string, args []string) error {
	fs := flag.NewFlagSet(subcommand, flag.ExitOnError)

	rt := &RuntimeConfig{
		Subcommand: subcommand,
	}

	var dirConflict, fileConflict, browserPath, protonExe, profile, configPath, exclude string
	var verbose bool

	fs.StringVar(&dirConflict, "dir-conflict", "", "Directory conflict strategy")
	fs.StringVar(&fileConflict, "file-conflict", "", "File conflict strategy")
	fs.StringVar(&browserPath, "browser", "", "Browser executable path for auth")
	fs.StringVar(&protonExe, "binary", "", "Path to proton-drive executable")
	fs.StringVar(&profile, "profile", "", "Named profile from config file")
	fs.StringVar(&configPath, "config", "", "Path to config file")
	fs.StringVar(&exclude, "exclude", "", "Comma-separated glob patterns to exclude")
	fs.BoolVar(&verbose, "verbose", false, "Show raw proton-drive output")

	// Separate flags from positional args so flags can appear
	// anywhere in the command line (before or after paths).
	flagArgs, positional := separateArgs(args)

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	// Map parsed flags into RuntimeConfig and track which were explicit
	rt.DirConflict = dirConflict
	rt.FileConflict = fileConflict
	rt.BrowserPath = browserPath
	rt.ProtonExe = protonExe
	rt.ProfileName = profile
	rt.ConfigPath = configPath
	rt.Verbose = verbose
	rt.Exclude = exclude

	// Track explicit flags
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
		case "exclude":
			rt.Explicit.Exclude = true
		case "verbose":
			rt.Explicit.Verbose = true
		}
	})

	// Handle positional args
	// For "list", only one positional arg (path) is expected
	// For "push"/"pull", two positional args expected
	if subcommand == "list" {
		if len(positional) >= 1 {
			rt.Remote = positional[0]
			rt.Explicit.Remote = true
		}
	} else {
		if len(positional) >= 1 {
			if subcommand == "push" {
				rt.Local = positional[0]
				rt.Explicit.Local = true
			} else { // pull
				rt.Remote = positional[0]
				rt.Explicit.Remote = true
			}
		}
		if len(positional) >= 2 {
			if subcommand == "push" {
				rt.Remote = positional[1]
				rt.Explicit.Remote = true
			} else { // pull
				rt.Local = positional[1]
				rt.Explicit.Local = true
			}
		}
	}

	// Validate: push and pull require local and remote
	config, err := resolveConfig(rt)
	if err != nil {
		return err
	}

	if subcommand == "push" || subcommand == "pull" {
		if config.Local == "" || config.Remote == "" {
			if config.ProfileName == "" {
				return fmt.Errorf(
					"%s requires <local> <remote>, "+
						"or a --profile with those values set",
					subcommand,
				)
			}
			return fmt.Errorf(
				"profile '%s' is missing local or remote",
				config.ProfileName,
			)
		}
	}

	if subcommand == "list" && config.Remote == "" {
		if config.ProfileName == "" {
			return fmt.Errorf("list requires <path>")
		}
		return fmt.Errorf(
			"profile '%s' is missing remote path for list",
			config.ProfileName,
		)
	}

	// Ensure auth is valid
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

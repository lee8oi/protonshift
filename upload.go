// upload.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// doPush handles the "push" subcommand — uploading files from
// a local source to Proton Drive.
//
// If the source is a single file, it uploads that file directly.
// If the source is a directory, it walks the directory tree
// sequentially (one subdirectory at a time) to avoid exhausting
// file handles (EMFILE errors). After processing subdirectories,
// it uploads any loose files in the root of the source directory.
//
// This sequential approach mirrors the original PowerShell script's
// pattern and is the primary defense against EMFILE errors on Windows.
func doPush(cfg *RuntimeConfig) error {
	info, err := os.Stat(cfg.Source)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	if info.IsDir() {
		return pushDirectory(cfg)
	}
	return pushFile(cfg)
}

// pushFile uploads a single file to Proton Drive.
func pushFile(cfg *RuntimeConfig) error {
	dest := cfg.Destination

	fmt.Printf("Uploading: %s → %s\n", cfg.Source, dest)

	args := []string{
		"filesystem", "upload",
		cfg.Source, dest,
		"-d", cfg.DirConflict,
		"-f", cfg.FileConflict,
	}

	return runProtonDrive(cfg, args)
}

// pushDirectory uploads a directory to Proton Drive by processing
// each subdirectory sequentially, then handling loose files in the
// root. This mirrors the original PowerShell script's approach:
//
//  1. Enumerate subdirectories in the source path
//  2. For each subdirectory, run a single upload command for
//     that directory's contents
//  3. After subdirectories are done, upload loose files in root
//
// Each upload command runs as a separate process, ensuring clean
// file handle state between operations.
func pushDirectory(cfg *RuntimeConfig) error {
	baseLocal := cfg.Source
	baseRemote := cfg.Destination

	fmt.Printf("=== Pushing %s → %s ===\n", baseLocal, baseRemote)

	// Step 1: Process subdirectories sequentially
	entries, err := os.ReadDir(baseLocal)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	subdirs := []string{}
	looseFiles := []string{}

	for _, entry := range entries {
		if entry.IsDir() {
			subdirs = append(subdirs, entry.Name())
		} else {
			looseFiles = append(looseFiles, entry.Name())
		}
	}

	// Upload each subdirectory individually
	for _, name := range subdirs {
		localPath := filepath.Join(baseLocal, name)
		remotePath := fmt.Sprintf("%s/%s", baseRemote, name)

		fmt.Printf("\nUploading directory: %s\n", name)

		args := []string{
			"filesystem", "upload",
			filepath.Join(localPath, "*"),
			remotePath,
			"-d", cfg.DirConflict,
			"-f", cfg.FileConflict,
		}

		if err := runProtonDrive(cfg, args); err != nil {
			fmt.Fprintf(os.Stderr, "Errors in: %s (%v)\n", name, err)
			// Continue to next directory rather than aborting
			continue
		}
		fmt.Printf("Done: %s\n", name)
	}

	// Step 2: Upload loose files in the root
	if len(looseFiles) > 0 {
		fmt.Println("\nUploading loose files in root...")

		args := []string{
			"filesystem", "upload",
			filepath.Join(baseLocal, "*"),
			baseRemote,
			"-d", cfg.DirConflict,
			"-f", cfg.FileConflict,
		}

		if err := runProtonDrive(cfg, args); err != nil {
			fmt.Fprintf(os.Stderr, "Errors in loose files (%v)\n", err)
		} else {
			fmt.Println("Loose files done.")
		}
	}

	fmt.Println("\nPush complete.")
	return nil
}

// doPull handles the "pull" subcommand — downloading files from
// Proton Drive to a local destination.
//
// This is simpler than push because the proton-drive CLI handles
// the remote-to-local direction natively. We just need to ensure
// the local destination directory exists.
func doPull(cfg *RuntimeConfig) error {
	src := cfg.Source
	dest := cfg.Destination

	// Ensure the local destination directory exists
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	fmt.Printf("Pulling: %s → %s\n", src, dest)

	args := []string{
		"filesystem", "download",
		src, dest,
		"-d", cfg.DirConflict,
		"-f", cfg.FileConflict,
	}

	if err := runProtonDrive(cfg, args); err != nil {
		return err
	}

	fmt.Println("Pull complete.")
	return nil
}

// doList handles the "list" subcommand — listing files and folders
// on Proton Drive at the given path.
func doList(cfg *RuntimeConfig) error {
	path := cfg.Source

	args := []string{
		"filesystem", "list",
		path,
	}

	return runProtonDrive(cfg, args)
}

// runProtonDrive executes the proton-drive CLI with the given
// arguments. Output is streamed to the terminal in real-time
// so the user can see progress. If --verbose is enabled, the
// raw output is displayed; otherwise a simplified status is shown.
//
// This function is the core execution wrapper — every operation
// (push, pull, list) goes through here.
func runProtonDrive(cfg *RuntimeConfig, args []string) error {
	cmd := exec.Command(cfg.ProtonExe, args...)

	// Stream output to the terminal in real-time
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if cfg.Verbose {
		fmt.Printf("[DEBUG] Running: %s %v\n", cfg.ProtonExe, args)
	}

	return cmd.Run()
}

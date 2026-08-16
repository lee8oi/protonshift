// upload.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// batchSize controls how many files are passed to a single
// proton-drive upload command. Keeping this bounded prevents
// EMFILE errors when a single directory contains thousands of files.
const batchSize = 250

// doPush handles the "push" subcommand — uploading files from
// a local source to Proton Drive.
//
// If the source is a single file, it uploads that file directly.
// If the source is a directory, it walks the directory tree
// sequentially (one subdirectory at a time) to avoid exhausting
// file handles (EMFILE errors). Within each subdirectory, files
// are uploaded in batches of batchSize to further limit file
// handle pressure.
//
// This sequential + batched approach mirrors and improves upon
// the original PowerShell script's pattern.
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
// root. Within each directory, if the file count exceeds batchSize,
// files are split into batches and uploaded as separate commands.
func pushDirectory(cfg *RuntimeConfig) error {
	baseLocal := cfg.Source
	baseRemote := cfg.Destination

	fmt.Printf("=== Pushing %s → %s ===\n", baseLocal, baseRemote)

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

		if err := pushSubdirectory(cfg, localPath, remotePath, name); err != nil {
			fmt.Fprintf(os.Stderr, "Errors in: %s (%v)\n", name, err)
			continue
		}
		fmt.Printf("Done: %s\n", name)
	}

	// Upload loose files in the root
	if len(looseFiles) > 0 {
		fmt.Println("\nUploading loose files in root...")
		if err := uploadFilesInBatches(cfg, baseLocal, looseFiles, baseRemote); err != nil {
			fmt.Fprintf(os.Stderr, "Errors in loose files (%v)\n", err)
		} else {
			fmt.Println("Loose files done.")
		}
	}

	fmt.Println("\nPush complete.")
	return nil
}

// pushSubdirectory uploads the contents of a single subdirectory.
// It reads the directory, enumerates files (recursing into nested
// subdirs), and uploads them in batches. Nested subdirectories are
// handled as their own remote destinations with merge strategy.
func pushSubdirectory(cfg *RuntimeConfig, localPath, remotePath, name string) error {
	entries, err := os.ReadDir(localPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", localPath, err)
	}

	files := []string{}
	nestedDirs := []string{}

	for _, entry := range entries {
		if entry.IsDir() {
			nestedDirs = append(nestedDirs, entry.Name())
		} else {
			files = append(files, entry.Name())
		}
	}

	// Upload files in this directory in batches
	if len(files) > 0 {
		if err := uploadFilesInBatches(cfg, localPath, files, remotePath); err != nil {
			return err
		}
	}

	// Handle nested subdirectories recursively
	for _, nested := range nestedDirs {
		nestedLocal := filepath.Join(localPath, nested)
		nestedRemote := fmt.Sprintf("%s/%s", remotePath, nested)

		fmt.Printf("  Sub-directory: %s/%s\n", name, nested)

		if err := pushSubdirectory(cfg, nestedLocal, nestedRemote, fmt.Sprintf("%s/%s", name, nested)); err != nil {
			fmt.Fprintf(os.Stderr, "  Errors in: %s/%s (%v)\n", name, nested, err)
		}
	}

	return nil
}

// uploadFilesInBatches uploads a list of files from a local directory
// to a remote destination. If the file count exceeds batchSize,
// files are split into chunks and each chunk is uploaded as a
// separate proton-drive process. This prevents a single upload
// command from opening too many file handles at once (EMFILE).
//
// Each file is passed as an individual path argument to:
//
//	proton-drive filesystem upload <file1> <file2> ... <parentPath>
func uploadFilesInBatches(cfg *RuntimeConfig, localDir string, files []string, remotePath string) error {
	// In uploadFilesInBatches
	total := len(files)
	if total > 0 {
		fmt.Printf("  Found %d files in %s\n", total, localDir)
	}
	if total <= batchSize {
		// Small enough to upload in one shot
		paths := make([]string, total)
		for i, f := range files {
			paths[i] = filepath.Join(localDir, f)
		}
		args := []string{
			"filesystem", "upload",
		}
		args = append(args, paths...)
		args = append(args, remotePath, "-d", cfg.DirConflict, "-f", cfg.FileConflict)

		return runProtonDrive(cfg, args)
	}

	// Split into batches
	batchCount := (total + batchSize - 1) / batchSize
	fmt.Printf("  %d files — splitting into %d batches of %d\n", total, batchCount, batchSize)

	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}

		batchNum := (i / batchSize) + 1
		fmt.Printf("  Batch %d/%d (%d files)...\n", batchNum, batchCount, end-i)

		paths := make([]string, end-i)
		for j, f := range files[i:end] {
			paths[j] = filepath.Join(localDir, f)
		}

		args := []string{
			"filesystem", "upload",
		}
		args = append(args, paths...)
		args = append(args, remotePath, "-d", cfg.DirConflict, "-f", cfg.FileConflict)

		if err := runProtonDrive(cfg, args); err != nil {
			fmt.Fprintf(os.Stderr, "  Batch %d failed (%v)\n", batchNum, err)
			// Continue to next batch rather than aborting
		}
	}

	return nil
}

// doPull handles the "pull" subcommand — downloading files from
// Proton Drive to a local destination.
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

	fmt.Println("Run complete.")
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
// arguments. Output is streamed to the terminal in real-time.
func runProtonDrive(cfg *RuntimeConfig, args []string) error {
	cmd := exec.Command(cfg.ProtonExe, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if cfg.Verbose {
		fmt.Printf("[DEBUG] Running: %s %v\n", cfg.ProtonExe, args)
	}

	return cmd.Run()
}

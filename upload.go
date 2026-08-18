// upload.go
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// batchSize controls how many files are passed to a single
// proton-drive upload/download command. Keeping this bounded prevents
// EMFILE errors when a single directory contains thousands of files.
const batchSize = 250

// doPush handles the "push" subcommand — uploading files from
// a local source to Proton Drive.
func doPush(cfg *RuntimeConfig) error {
	info, err := os.Stat(cfg.Local)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	if info.IsDir() {
		return pushDirectory(cfg)
	}
	return pushFile(cfg)
}

func pushFile(cfg *RuntimeConfig) error {
	fmt.Printf("Uploading: %s → %s\n", cfg.Local, cfg.Remote)

	args := []string{
		"filesystem", "upload",
		"-d", cfg.DirConflict,
		"-f", cfg.FileConflict,
		cfg.Local, cfg.Remote,
	}

	return runProtonDrive(cfg, args)
}

// pushDirectory uploads a directory to Proton Drive by processing
// each subdirectory sequentially, then handling loose files in the
// root. Within each directory, if the file count exceeds batchSize,
// files are split into batches and uploaded as separate commands.
// Files and directories matching exclusion patterns (.psignore or
// --exclude flag) are skipped.
func pushDirectory(cfg *RuntimeConfig) error {
	baseLocal := cfg.Local
	baseRemote := cfg.Remote

	fmt.Printf("=== Pushing %s → %s ===\n", baseLocal, baseRemote)

	// Load exclusion patterns: .psignore from source root + --exclude flag
	ignorePatterns, err := loadIgnoreFile(baseLocal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read .psignore: %v\n", err)
	}
	flagPatterns := parseExcludeFlag(cfg.Exclude)
	patterns := mergePatterns(flagPatterns, ignorePatterns)

	if len(patterns) > 0 && cfg.Verbose {
		fmt.Printf("[DEBUG] Exclude patterns: %d active\n", len(patterns))
	}

	// Create the base remote folder before processing subdirectories.
	if err := createRemoteFolder(cfg, baseRemote); err != nil {
		return fmt.Errorf("failed to create base remote folder %s: %w", baseRemote, err)
	}

	entries, err := os.ReadDir(baseLocal)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	subdirs := []string{}
	looseFiles := []string{}

	for _, entry := range entries {
		if entry.IsDir() {
			if isExcluded(entry.Name(), true, patterns) {
				if cfg.Verbose {
					fmt.Printf("  [excluded] %s/\n", entry.Name())
				}
				continue
			}
			subdirs = append(subdirs, entry.Name())
		} else {
			if isExcluded(entry.Name(), false, patterns) {
				if cfg.Verbose {
					fmt.Printf("  [excluded] %s\n", entry.Name())
				}
				continue
			}
			looseFiles = append(looseFiles, entry.Name())
		}
	}

	// Upload each subdirectory individually
	for _, name := range subdirs {
		localPath := filepath.Join(baseLocal, name)
		remotePath := fmt.Sprintf("%s/%s", baseRemote, name)

		fmt.Printf("\nUploading directory: %s\n", name)

		if err := pushSubdirectory(cfg, localPath, remotePath, name, patterns); err != nil {
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
// Exclusion patterns are applied at every level of recursion.
func pushSubdirectory(cfg *RuntimeConfig, localPath, remotePath, name string, patterns []excludePattern) error {
	entries, err := os.ReadDir(localPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", localPath, err)
	}

	files := []string{}
	nestedDirs := []string{}

	for _, entry := range entries {
		if entry.IsDir() {
			if isExcluded(entry.Name(), true, patterns) {
				if cfg.Verbose {
					fmt.Printf("  [excluded] %s/%s/\n", name, entry.Name())
				}
				continue
			}
			nestedDirs = append(nestedDirs, entry.Name())
		} else {
			if isExcluded(entry.Name(), false, patterns) {
				if cfg.Verbose {
					fmt.Printf("  [excluded] %s/%s\n", name, entry.Name())
				}
				continue
			}
			files = append(files, entry.Name())
		}
	}

	// Create the remote folder before uploading files to it.
	// When passing individual file paths (instead of a glob),
	// proton-drive does not auto-create the parent folder.
	if len(files) > 0 || len(nestedDirs) > 0 {
		if err := createRemoteFolder(cfg, remotePath); err != nil {
			return fmt.Errorf("failed to create remote folder %s: %w", remotePath, err)
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

		if err := pushSubdirectory(cfg, nestedLocal, nestedRemote, fmt.Sprintf("%s/%s", name, nested), patterns); err != nil {
			fmt.Fprintf(os.Stderr, "  Errors in: %s/%s (%v)\n", name, nested, err)
		}
	}

	return nil
}

// createRemoteFolder creates a folder on Proton Drive at the given
// remote path. It first splits the path into parent and name, then
// calls `proton-drive filesystem create-folder parentPath name`.
// If the folder already exists, the error is ignored (merge behavior).
func createRemoteFolder(cfg *RuntimeConfig, remotePath string) error {
	// Split remote path into parent and name
	idx := strings.LastIndex(remotePath, "/")
	if idx <= 0 || idx == len(remotePath)-1 {
		// Can't split root or malformed path — skip folder creation
		return nil
	}

	parent := remotePath[:idx]
	name := remotePath[idx+1:]

	args := []string{
		"filesystem", "create-folder",
		parent, name,
	}

	cmd := exec.Command(cfg.ProtonExe, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Run()

	// Ignore errors — if the folder already exists, that's fine.
	// If it genuinely can't be created, the upload will fail with
	// a more descriptive error anyway.
	return nil
}

// uploadFilesInBatches uploads a list of files from a local directory
// to a remote destination. If the file count exceeds batchSize,
// files are split into chunks and each chunk is uploaded as a
// separate proton-drive process. This prevents a single upload
// command from opening too many file handles at once (EMFILE).
func uploadFilesInBatches(cfg *RuntimeConfig, localDir string, files []string, remotePath string) error {
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
			"-d", cfg.DirConflict,
			"-f", cfg.FileConflict,
		}
		args = append(args, paths...)
		args = append(args, remotePath)

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
			"-d", cfg.DirConflict,
			"-f", cfg.FileConflict,
		}
		args = append(args, paths...)
		args = append(args, remotePath)

		if err := runProtonDrive(cfg, args); err != nil {
			fmt.Fprintf(os.Stderr, "  Batch %d failed (%v)\n", batchNum, err)
			// Continue to next batch rather than aborting
		}
	}

	return nil
}

// doPull handles the "pull" subcommand — downloading files from
// Proton Drive to a local destination. The proton-drive CLI manages
// its own download queue and file handle concurrency internally,
// so no batching or enumeration is needed on our side.
func doPull(cfg *RuntimeConfig) error {
	src := cfg.Remote
	dest := cfg.Local

	// Create parent directory only — let proton-drive create
	// the leaf folder to avoid nesting when the destination
	// doesn't already exist.
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	fmt.Printf("=== Pulling %s → %s ===\n", src, dest)

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
	path := cfg.Remote

	args := []string{
		"filesystem", "list",
		path,
	}

	return runProtonDrive(cfg, args)
}

// runProtonDrive executes the proton-drive CLI with the given
// arguments. Output is streamed directly to the terminal.
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

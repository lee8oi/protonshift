# ProtonShift

> **ProtonShift is an unofficial community tool and is not affiliated with or endorsed by Proton AG.**

A lightweight Go wrapper around the Proton Drive CLI that handles browser-based authentication and bidirectional file transfers with EMFILE-safe sequential processing and intra-directory batch uploads.

## Overview

ProtonShift wraps the `proton-drive` CLI binary, intercepting its OAuth authentication URL and opening it in your browser of choice, then managing file uploads and downloads with sequential directory processing and batched file uploads to avoid Windows EMFILE (too many open files) errors.

Built with Go standard library only — no external dependencies.

## Features

- Browser-based auth interception — captures the auth URL from `proton-drive auth login` and opens it in a specified or system-default browser
- Session reuse — skips auth if a valid session is already stored in the OS credential manager
- Bidirectional transfers — push (upload) and pull (download) via simple subcommands
- EMFILE-safe processing — directories are processed one subdirectory at a time, each as a fresh process, preserving clean file handle state
- Intra-directory batching — large directories are automatically split into batches of 250 files per process, preventing file handle exhaustion within a single directory
- Recursive subdirectory walking — nested directories are discovered and uploaded as their own remote destinations
- Dual conflict strategy support — separate directory (`-d`) and file (`-f`) conflict handling passed through to the CLI
- JSON config profiles — save recurring transfer configurations for repeated use
- Top-level config defaults — share binary path, conflict strategies, and browser across all profiles
- Cross-platform — builds for Windows, macOS, and Linux

## Installation

### Prerequisites

- Go 1.21 or later
- The `proton-drive` CLI binary (download from proton.me/drive/download)

### Build from source

    git clone https://github.com/lee8oi/protonshift.git
    cd protonshift
    go build -o protonshift

On Windows:

    go build -o protonshift.exe

### Make proton-drive available

ProtonShift defaults to calling `proton-drive` from your PATH. If your binary is elsewhere, specify it via config (see below) or the `--binary` flag.

## Usage

    protonshift push <local> <remote> [flags]
    protonshift pull <remote> <local> [flags]
    protonshift list <path> [flags]

### Examples

Upload a directory:

    protonshift push D:/DCIM /my-files

Upload a single file:

    protonshift push C:/Users/Lee/report.pdf /my-files/docs

Download from Proton Drive:

    protonshift pull /my-files/docs C:/Users/Lee/Downloads

List remote contents:

    protonshift list /my-files

Use a saved profile:

    protonshift push --profile dcim
    protonshift pull --profile dcim

Use profile with a flag override:

    protonshift push --profile dcim --verbose

Use the default profile (no args needed):

    protonshift push

### Flags

    --browser <path>        Browser executable to open auth URL
    --dir-conflict <str>     Directory conflict strategy (default: merge)
    --file-conflict <str>    File conflict strategy (default: skip)
    --binary <path>          Path to proton-drive executable
                            (default: proton-drive, assumes PATH)
    --profile <name>         Use a named profile from config file
    --config <path>          Path to config file
    --verbose                Show raw proton-drive CLI output

## Configuration

ProtonShift looks for a JSON config file in the following locations (first match wins):

1. Path specified via `--config` flag
2. `./protonshift.json` in the current directory
3. `~/.protonshift/config.json` in the user home directory

### Config file format

    {
        "defaults": {
            "binary": "./proton-drive.exe",
            "dir_conflict": "merge",
            "file_conflict": "skip",
            "browser": "C:/Program Files/Firefox/firefox.exe"
        },
        "profiles": {
            "dcim": {
                "local": "D:/DCIM",
                "remote": "/my-files"
            },
            "projects": {
                "local": "C:/Users/Lee/Projects",
                "remote": "/my-files/projects-backup",
                "file_conflict": "overwrite"
            }
        },
        "default_profile": "dcim"
    }

### Resolution priority

Configuration values are resolved in the following order (highest priority first):

1. Command-line flags
2. Named profile values
3. Config file `defaults` section
4. Built-in defaults (`dir_conflict=merge`, `file_conflict=skip`, `binary=proton-drive`)

### Config fields

| Field           | Profile key     | Description                              |
|-----------------|-----------------|------------------------------------------|
| local           | local           | Local path (push: source, pull: dest)   |
| remote          | remote          | Remote path (push: dest, pull: source)  |
| dir_conflict    | dir_conflict    | Directory conflict strategy              |
| file_conflict   | file_conflict   | File conflict strategy                   |
| browser         | browser         | Browser executable path for auth         |
| binary          | binary          | Path to proton-drive CLI executable      |
| verbose         | verbose         | Show raw CLI output (true/false)         |

The `defaults` section accepts all the same fields except `local` and `remote`. Profiles inherit from `defaults` and can override any field.

## How EMFILE protection works

On Windows, uploading directories with many files can exhaust available file handles (EMFILE error). This typically occurs when a single `proton-drive` process attempts to open hundreds or thousands of files simultaneously. ProtonShift prevents this with a two-layer approach:

### Layer 1: Sequential directory processing

1. Enumerate subdirectories in the source path
2. For each subdirectory, process its contents as a separate operation
3. Nested subdirectories are discovered recursively and uploaded as their own remote destinations
4. After subdirectories, upload loose files in the root directory
5. If a subdirectory fails, the error is logged and processing continues to the next

### Layer 2: Intra-directory batch uploads

Within each directory, if the file count exceeds the batch size (default: 250), files are split into batches:

1. Enumerate all files in the directory
2. Split into batches of 250 files
3. Pass individual file paths to each `proton-drive filesystem upload` command
4. Each batch runs as a fresh process, releasing all file handles before the next batch starts
5. Failed batches are logged; processing continues to the next batch

Example output for a 2,382-file directory:

    Uploading directory: Camera
      Found 2382 files in D:/DCIM/Camera
      2382 files — splitting into 10 batches of 250
      Batch 1/10 (250 files)...
      Batch 2/10 (250 files)...
      ...
      Batch 10/10 (232 files)...
    Done: Camera

### Tuning batch size

The batch size is defined as a constant in `upload.go`:

    const batchSize = 250

If EMFILE errors persist on your system, decrease this value. Typical safe ranges:

- 250 — works for most Windows systems
- 100 — conservative, suitable for systems with lower handle limits
- 50 — very conservative, use if 100 still triggers EMFILE

## Project structure

    protonshift/
    ├── main.go                  # Entry point, subcommand dispatch, flag parsing
    ├── config.go                # Config file loading, profile resolution, defaults merging
    ├── auth.go                  # Session check, auth URL interception, browser launch
    ├── upload.go                # Push/pull/list operations, sequential + batched processing
    ├── go.mod                   # Module definition
    └── protonshift.json.example # Example config file

## Acknowledgment

- Proton AG for the Proton Drive CLI and their broader open-source ecosystem

## License

MIT
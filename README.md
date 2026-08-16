# ProtonShift

A lightweight Go wrapper around the Proton Drive CLI that handles browser-based authentication and bidirectional file transfers with EMFILE-safe sequential processing.

## Overview

ProtonShift wraps the `proton-drive` CLI binary, intercepting its OAuth authentication URL and opening it in your browser of choice, then managing file uploads and downloads with sequential directory processing to avoid Windows EMFILE (too many open files) errors.

Built with Go standard library only — no external dependencies.

## Features

- Browser-based auth interception — captures the auth URL from `proton-drive auth login` and opens it in a specified or system-default browser
- Session reuse — skips auth if a valid session is already stored in the OS credential manager
- Bidirectional transfers — push (upload) and pull (download) via simple subcommands
- EMFILE-safe processing — directories are uploaded one subdirectory at a time, each as a fresh process, preserving clean file handle state
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

    protonshift push <source> <destination> [flags]
    protonshift pull <source> <destination> [flags]
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

Use profile with a flag override:

    protonshift push --profile dcim --verbose

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
                "source": "D:/DCIM",
                "destination": "/my-files"
            },
            "projects": {
                "source": "C:/Users/Lee/Projects",
                "destination": "/my-files/projects-backup",
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
| source          | source          | Local path (push) or remote path (pull)  |
| destination     | destination     | Remote path (push) or local path (pull)  |
| dir_conflict    | dir_conflict    | Directory conflict strategy               |
| file_conflict   | file_conflict   | File conflict strategy                   |
| browser         | browser         | Browser executable path for auth          |
| binary          | binary          | Path to proton-drive CLI executable      |
| verbose         | verbose         | Show raw CLI output (true/false)         |

The `defaults` section accepts all the same fields except `source` and `destination`. Profiles inherit from `defaults` and can override any field.

## How EMFILE protection works

On Windows, uploading directories with many files can exhaust available file handles (EMFILE error). ProtonShift avoids this by processing directories sequentially:

1. Enumerate subdirectories in the source path
2. For each subdirectory, run a single `proton-drive filesystem upload` command for that directory's contents
3. Each upload runs as a fresh process, ensuring clean file handle state
4. After subdirectories, upload loose files in the root directory
5. If a subdirectory fails, the error is logged and processing continues to the next

This mirrors the pattern from the original PowerShell script that inspired the project.

## Project structure

    protonshift/
    ├── main.go                  # Entry point, subcommand dispatch, flag parsing
    ├── config.go                # Config file loading, profile resolution, defaults merging
    ├── auth.go                  # Session check, auth URL interception, browser launch
    ├── upload.go                # Push/pull/list operations, sequential processing
    ├── go.mod                   # Module definition
    └── protonshift.json.example # Example config file

## License

MIT
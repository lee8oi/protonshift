// auth.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
)

// urlPattern matches http and https URLs in arbitrary text.
// We use this to detect the auth URL that proton-drive prints
// during the login flow.
var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

// ensureAuth checks whether the current session is valid.
// If not, it triggers the browser-based auth flow by running
// `proton-drive auth login`, intercepting the printed auth URL,
// and opening it in the user's configured (or system default) browser.
//
// The proton-drive CLI handles OAuth by:
//  1. Starting a local HTTP listener
//  2. Printing an auth URL to stdout/stderr
//  3. Waiting for the browser callback to hit the listener
//  4. Storing the session token in the OS credential manager
//
// Our wrapper's job is to capture that URL and launch the browser
// so the user doesn't have to copy-paste it manually.
func ensureAuth(cfg *RuntimeConfig) error {
	// First, attempt a lightweight command to check if we're
	// already authenticated. If `proton-drive filesystem list`
	// succeeds on the root, we have a valid session.
	if checkSession(cfg) {
		if cfg.Verbose {
			fmt.Println("Session active.")
		}
		return nil
	}

	if cfg.Verbose {
		fmt.Println("No active session. Starting auth flow...")
	}

	return login(cfg)
}

// checkSession runs a quick `proton-drive filesystem list /my-files`
// command to see if the current session is valid. Returns true if
// the command succeeds (exit code 0), false otherwise.
func checkSession(cfg *RuntimeConfig) bool {
	cmd := exec.Command(cfg.ProtonExe, "filesystem", "list", "/my-files")
	// Suppress output during session check
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return err == nil
}

// login runs `proton-drive auth login` and intercepts the auth URL
// from the command's stdout/stderr output. When a URL is detected,
// it opens the user's configured browser (or system default).
// The function blocks until the auth command completes (success
// or failure).
func login(cfg *RuntimeConfig) error {
	cmd := exec.Command(cfg.ProtonExe, "auth", "login")

	// Get pipes for stdout and stderr so we can scan output in real-time
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start auth command: %w", err)
	}

	urlOpened := false

	// scanOutput reads lines from a reader, optionally prints them
	// (when verbose), and looks for a URL to open in the browser.
	scanOutput := func(reader io.Reader) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			if cfg.Verbose {
				fmt.Println(line)
			}
			if !urlOpened {
				if url := urlPattern.FindString(line); url != "" {
					if err := openBrowser(cfg.BrowserPath, url); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not open browser: %v\n", err)
						fmt.Printf("Please open this URL manually: %s\n", url)
					} else {
						fmt.Println("Opened authentication URL in browser.")
					}
					urlOpened = true
				}
			}
		}
		// Check for scanner errors (e.g., line too long, I/O error).
		// Scan exits silently on error — without this check we'd
		// miss failures that could cause us to never see the auth URL.
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error reading auth output: %v\n", err)
		}
	}

	// Read stdout and stderr concurrently
	go scanOutput(stdoutPipe)
	go scanOutput(stderrPipe)

	// Wait for the auth command to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("auth command failed: %w", err)
	}

	if !urlOpened {
		fmt.Println("Auth completed. (No URL detected — you may have already been authenticated.)")
	}

	return nil
}

// openBrowser launches the specified browser (or system default)
// with the given URL. If browserPath is empty, falls back to the
// OS-default browser handler.
func openBrowser(browserPath string, url string) error {
	if browserPath != "" {
		cmd := exec.Command(browserPath, url)
		return cmd.Start()
	}

	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		return cmd.Start()
	case "darwin":
		cmd := exec.Command("open", url)
		return cmd.Start()
	default:
		cmd := exec.Command("xdg-open", url)
		return cmd.Start()
	}
}

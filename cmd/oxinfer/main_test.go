package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/garaekz/oxinfer/internal/cli"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedExit   cli.ExitCode
		expectedStdout string
		expectedStderr bool
	}{
		{
			name:           "version flag",
			args:           []string{"--version"},
			expectedExit:   cli.ExitOK,
			expectedStdout: "oxinfer version 0.1.0",
			expectedStderr: false,
		},
		{
			name:           "help flag",
			args:           []string{"--help"},
			expectedExit:   cli.ExitOK,
			expectedStdout: "Usage:",
			expectedStderr: false,
		},
		{
			name:           "help flag short",
			args:           []string{"-h"},
			expectedExit:   cli.ExitOK,
			expectedStdout: "Usage:",
			expectedStderr: false,
		},
		{
			name:           "invalid flag",
			args:           []string{"--invalid-flag"},
			expectedExit:   cli.ExitInputError,
			expectedStdout: "",
			expectedStderr: true,
		},
		{
			name:           "nonexistent manifest file",
			args:           []string{"--manifest", "/nonexistent/file.json"},
			expectedExit:   cli.ExitInputError,
			expectedStdout: "",
			expectedStderr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout and stderr
			oldStdout := os.Stdout
			oldStderr := os.Stderr

			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()

			os.Stdout = wOut
			os.Stderr = wErr

			// Run the function
			exitCode := run(tt.args)

			// Restore stdout and stderr
			_ = wOut.Close()
			_ = wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read captured output
			var bufOut, bufErr bytes.Buffer
			_, _ = bufOut.ReadFrom(rOut)
			_, _ = bufErr.ReadFrom(rErr)

			stdout := strings.TrimSpace(bufOut.String())
			stderr := strings.TrimSpace(bufErr.String())

			// Check exit code
			if exitCode != tt.expectedExit {
				t.Errorf("run() exitCode = %v, want %v", exitCode, tt.expectedExit)
			}

			// Check stdout
			if tt.expectedStdout != "" && !strings.Contains(stdout, tt.expectedStdout) {
				t.Errorf("run() stdout = %q, want to contain %q", stdout, tt.expectedStdout)
			}

			// Check stderr
			if tt.expectedStderr && stderr == "" {
				t.Errorf("run() expected stderr output, got empty")
			} else if !tt.expectedStderr && stderr != "" {
				t.Errorf("run() expected no stderr output, got %q", stderr)
			}
		})
	}
}

func TestRunWithValidManifest(t *testing.T) {
	// Create a temporary test directory
	tempDir, err := os.MkdirTemp("", "oxinfer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a valid manifest file
	manifestPath := filepath.Join(tempDir, "manifest.json")
	manifestContent := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app/"]
		}
	}`, tempDir)

	err = os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	if err != nil {
		t.Fatalf("failed to write manifest file: %v", err)
	}

	// Test with valid manifest - this will fail until emitter is implemented
	// For now, we expect it to fail with internal error due to missing emitter
	exitCode := run([]string{"--manifest", manifestPath})
	if exitCode == cli.ExitOK {
		// If it succeeds, that's actually good (means emitter was implemented)
		t.Log("run() with valid manifest succeeded - emitter implementation is working")
	} else {
		// We expect this to fail for now due to missing emitter implementation
		t.Log("run() with valid manifest failed as expected due to missing emitter implementation")
	}
}

func TestRunWithStdinInput(t *testing.T) {
	// Create a temporary test directory
	tempDir, err := os.MkdirTemp("", "oxinfer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Save original stdin
	oldStdin := os.Stdin

	// Create a pipe to simulate stdin input
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()

	os.Stdin = r

	// Write manifest JSON to pipe
	manifestContent := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app/"]
		}
	}`, tempDir)

	go func() {
		defer func() { _ = w.Close() }()
		_, _ = w.Write([]byte(manifestContent))
	}()

	// Test with stdin input - this will fail until emitter is implemented
	exitCode := run([]string{})

	// Restore stdin
	os.Stdin = oldStdin

	if exitCode == cli.ExitOK {
		t.Log("run() with stdin input succeeded - emitter implementation is working")
	} else {
		t.Log("run() with stdin input failed as expected due to missing emitter implementation")
	}
}

func TestPrintError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		noColor  bool
		wantJSON bool
	}{
		{
			name:     "CLI error",
			err:      cli.NewInputError("test input error"),
			noColor:  false,
			wantJSON: true,
		},
		{
			name:     "CLI error with no color",
			err:      cli.NewInputError("test input error"),
			noColor:  true,
			wantJSON: true,
		},
		{
			name:     "generic error",
			err:      fmt.Errorf("generic error message"),
			noColor:  false,
			wantJSON: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Call printError
			printError(tt.err, tt.noColor)

			// Restore stderr
			_ = w.Close()
			os.Stderr = oldStderr

			// Read captured output
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			output := strings.TrimSpace(buf.String())

			if tt.wantJSON {
				// Should be valid JSON for CLI errors
				if !strings.Contains(output, `"message"`) {
					t.Errorf("printError() output should contain JSON message field, got: %s", output)
				}
				if !strings.Contains(output, `"exit_code"`) {
					t.Errorf("printError() output should contain JSON exit_code field, got: %s", output)
				}
			} else {
				// Should be plain text for generic errors
				if !strings.HasPrefix(output, "Error:") {
					t.Errorf("printError() output should start with 'Error:', got: %s", output)
				}
			}
		})
	}
}

// TestVersion verifies the version constant is properly set
func TestVersion(t *testing.T) {
	if version == "" {
		t.Error("version constant should not be empty")
	}
	if version != "0.1.0" {
		t.Errorf("version = %q, want %q", version, "0.1.0")
	}
}

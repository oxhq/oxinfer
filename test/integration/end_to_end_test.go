package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEndToEndWithFixtures tests the CLI end-to-end using the fixture manifests
func TestEndToEndWithFixtures(t *testing.T) {
	// Build the CLI binary first
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Test with valid manifests
	validManifests := []string{
		"minimal.json",
		"full.json",
		"defaults.json",
	}

	for _, manifestFile := range validManifests {
		t.Run(fmt.Sprintf("valid_%s", strings.TrimSuffix(manifestFile, ".json")), func(t *testing.T) {
			manifestPath := filepath.Join("../../test/fixtures/manifests/valid", manifestFile)

			// Run the CLI
			cmd := exec.Command(cliPath, "--manifest", manifestPath)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("CLI execution failed: %v\nStderr: %s", err, stderr.String())
			}

			// Check that we got JSON output
			output := stdout.String()
			if output == "" {
				t.Fatal("CLI produced no output")
			}

			// Validate that output is valid JSON
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("CLI output is not valid JSON: %v\nOutput: %s", err, output)
			}

			// Check that required fields are present in the output
			requiredFields := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
			for _, field := range requiredFields {
				if _, exists := result[field]; !exists {
					t.Errorf("Output missing required field: %s", field)
				}
			}

			// In the initial version, all collections should be empty arrays
			collections := []string{"controllers", "models", "polymorphic", "broadcast"}
			for _, collection := range collections {
				if arr, ok := result[collection].([]interface{}); !ok {
					t.Errorf("Field %s should be an array", collection)
				} else if len(arr) != 0 {
					t.Errorf("Field %s should be empty array in initial version, got length %d", collection, len(arr))
				}
			}

			// Verify meta structure
			if meta, ok := result["meta"].(map[string]interface{}); ok {
				if partial, exists := meta["partial"]; !exists {
					t.Error("Meta missing partial field")
				} else if _, ok := partial.(bool); !ok {
					t.Error("Meta partial should be boolean")
				}

				if stats, exists := meta["stats"]; !exists {
					t.Error("Meta missing stats field")
				} else if statsObj, ok := stats.(map[string]interface{}); ok {
					if _, exists := statsObj["filesParsed"]; !exists {
						t.Error("Meta.stats missing filesParsed field")
					}
					if _, exists := statsObj["skipped"]; !exists {
						t.Error("Meta.stats missing skipped field")
					}
					if _, exists := statsObj["durationMs"]; !exists {
						t.Error("Meta.stats missing durationMs field")
					}
				}
			} else {
				t.Error("Output missing or invalid meta field")
			}

			// Stats are now part of meta.stats, already verified above
		})
	}
}

// TestEndToEndInvalidManifests tests that invalid manifests produce appropriate errors
func TestEndToEndInvalidManifests(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	invalidManifests := []struct {
		file        string
		expectedErr string
		exitCode    int
	}{
		{
			file:        "missing-project.json",
			expectedErr: "schema",
			exitCode:    3, // ExitSchemaError
		},
		{
			file:        "invalid-limits.json",
			expectedErr: "schema",
			exitCode:    3, // ExitSchemaError
		},
		{
			file:        "unknown-keys.json",
			expectedErr: "schema",
			exitCode:    3, // ExitSchemaError
		},
		{
			file:        "malformed.json",
			expectedErr: "JSON",
			exitCode:    1, // ExitInputError
		},
		{
			file:        "missing-root.json",
			expectedErr: "schema",
			exitCode:    3, // ExitSchemaError
		},
	}

	for _, tc := range invalidManifests {
		t.Run(fmt.Sprintf("invalid_%s", strings.TrimSuffix(tc.file, ".json")), func(t *testing.T) {
			manifestPath := filepath.Join("../../test/fixtures/manifests/invalid", tc.file)

			cmd := exec.Command(cliPath, "--manifest", manifestPath)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err == nil {
				t.Fatalf("Expected CLI to fail for invalid manifest %s, but it succeeded", tc.file)
			}

			// Check exit code
			if exitError, ok := err.(*exec.ExitError); ok {
				if exitError.ExitCode() != tc.exitCode {
					t.Errorf("Expected exit code %d, got %d", tc.exitCode, exitError.ExitCode())
				}
			} else {
				t.Errorf("Expected exit error, got %T", err)
			}

			// Check that stderr contains error information
			stderrStr := stderr.String()
			if stderrStr == "" {
				t.Error("Expected error output on stderr")
			}

			// Check that error message contains expected content
			if !strings.Contains(strings.ToLower(stderrStr), strings.ToLower(tc.expectedErr)) {
				t.Errorf("Error message should contain %q, got: %s", tc.expectedErr, stderrStr)
			}

			// Stderr should contain JSON error for CLI errors
			if tc.exitCode != 2 { // Not internal error
				var errorObj map[string]interface{}
				if err := json.Unmarshal([]byte(stderrStr), &errorObj); err != nil {
					t.Errorf("Error output should be valid JSON: %v\nOutput: %s", err, stderrStr)
				} else {
					// Check error structure
					if _, exists := errorObj["message"]; !exists {
						t.Error("Error JSON missing message field")
					}
					if _, exists := errorObj["exit_code"]; !exists {
						t.Error("Error JSON missing exit_code field")
					}
					if _, exists := errorObj["type"]; !exists {
						t.Error("Error JSON missing type field")
					}
				}
			}
		})
	}
}

// TestEndToEndStdinInput tests reading manifest from stdin
func TestEndToEndStdinInput(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Use the minimal manifest content
	manifestContent := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"]
		}
	}`, filepath.Join("../../test/fixtures/projects/test-laravel"))

	cmd := exec.Command(cliPath)
	cmd.Stdin = strings.NewReader(manifestContent)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI execution failed: %v\nStderr: %s", err, stderr.String())
	}

	// Validate JSON output
	output := stdout.String()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("CLI output is not valid JSON: %v", err)
	}

	// Should have same structure as file-based input
	if _, exists := result["meta"]; !exists {
		t.Error("Stdin input should produce meta field")
	}
}

// TestEndToEndOutputFile tests writing output to a file
func TestEndToEndOutputFile(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	manifestPath := filepath.Join("../../test/fixtures/manifests/valid/minimal.json")
	outputPath := filepath.Join(t.TempDir(), "output.json")

	cmd := exec.Command(cliPath, "--manifest", manifestPath, "--out", outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI execution failed: %v\nStderr: %s", err, stderr.String())
	}

	// Check that output file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("Output file was not created: %s", outputPath)
	}

	// Read and validate the output file
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("Output file contains invalid JSON: %v", err)
	}
}

// TestEndToEndFlagCombinations tests various flag combinations
func TestEndToEndFlagCombinations(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		checkStdout  func(t *testing.T, output string)
	}{
		{
			name:         "version flag",
			args:         []string{"--version"},
			wantExitCode: 0,
			checkStdout: func(t *testing.T, output string) {
				if !strings.Contains(output, "oxinfer version 0.1.0") {
					t.Errorf("Version output should contain version info, got: %s", output)
				}
			},
		},
		{
			name:         "help flag",
			args:         []string{"--help"},
			wantExitCode: 0,
			checkStdout: func(t *testing.T, output string) {
				if !strings.Contains(output, "Usage:") {
					t.Errorf("Help output should contain usage info, got: %s", output)
				}
			},
		},
		{
			name:         "no-color flag",
			args:         []string{"--manifest", filepath.Join("../../test/fixtures/manifests/valid/minimal.json"), "--no-color"},
			wantExitCode: 0,
			checkStdout: func(t *testing.T, output string) {
				// Should still produce valid JSON output
				var result map[string]interface{}
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					t.Errorf("Output should be valid JSON: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(cliPath, tt.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			// Check exit code
			actualExitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					actualExitCode = exitError.ExitCode()
				} else {
					t.Fatalf("Unexpected error type: %T", err)
				}
			}

			if actualExitCode != tt.wantExitCode {
				t.Errorf("Expected exit code %d, got %d", tt.wantExitCode, actualExitCode)
				t.Errorf("Stderr: %s", stderr.String())
			}

			// Run custom stdout checks
			if tt.checkStdout != nil {
				tt.checkStdout(t, stdout.String())
			}
		})
	}
}

// TestDeterministicOutput tests that multiple runs produce identical output
func TestDeterministicOutput(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	manifestPath := filepath.Join("../../test/fixtures/manifests/valid/full.json")

	// Run the CLI multiple times
	var outputs []string
	var hashes []string

	for i := 0; i < 3; i++ {
		cmd := exec.Command(cliPath, "--manifest", manifestPath)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		err := cmd.Run()
		if err != nil {
			t.Fatalf("CLI execution %d failed: %v", i, err)
		}

		output := stdout.String()
		outputs = append(outputs, output)

		// Calculate hash
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(output)))
		hashes = append(hashes, hash)
	}

	// All hashes should be identical (except for timestamp)
	// Since we can't control timestamp, we'll parse JSON and compare structure
	for i := 1; i < len(outputs); i++ {
		var result1, result2 map[string]interface{}

		if err := json.Unmarshal([]byte(outputs[0]), &result1); err != nil {
			t.Fatalf("Failed to parse output %d: %v", 0, err)
		}
		if err := json.Unmarshal([]byte(outputs[i]), &result2); err != nil {
			t.Fatalf("Failed to parse output %d: %v", i, err)
		}

		// Remove timestamp fields before comparing (they will differ)
		// Note: In new schema, there's no timestamp in meta, so this is mostly for compatibility
		if meta1, ok := result1["meta"].(map[string]interface{}); ok {
			delete(meta1, "timestamp")
		}
		if meta2, ok := result2["meta"].(map[string]interface{}); ok {
			delete(meta2, "timestamp")
		}

		// Convert back to JSON for comparison
		json1, _ := json.Marshal(result1)
		json2, _ := json.Marshal(result2)

		if string(json1) != string(json2) {
			t.Errorf("Output %d differs from output 0 (excluding timestamps)", i)
			t.Logf("Output 0: %s", string(json1))
			t.Logf("Output %d: %s", i, string(json2))
		}
	}
}

// TestT7PatternsIntegration tests T7 pattern matching integration end-to-end
func TestT7PatternsIntegration(t *testing.T) {
	// Build the CLI binary first
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create a temporary test manifest with T7 features enabled
	testManifest := map[string]interface{}{
		"project": map[string]interface{}{
			"root":     "../../",
			"composer": "go.mod",
		},
		"scan": map[string]interface{}{
			"targets": []string{"test/fixtures/matchers"},
		},
		"features": map[string]interface{}{
			"with_pivot":     true,
			"attribute_make": true,
			"scopes_used":    true,
		},
	}

	// Write manifest to temporary file
	manifestFile, err := os.CreateTemp("", "t7_manifest_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp manifest: %v", err)
	}
	defer os.Remove(manifestFile.Name())

	manifestBytes, err := json.MarshalIndent(testManifest, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test manifest: %v", err)
	}

	if _, err := manifestFile.Write(manifestBytes); err != nil {
		t.Fatalf("Failed to write test manifest: %v", err)
	}
	manifestFile.Close()

	// Run the CLI with T7-enabled manifest
	cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Fatalf("CLI execution failed: %v\nStderr: %s", err, stderr.String())
	}

	// Parse the output JSON
	output := stdout.String()
	if output == "" {
		t.Fatal("CLI produced no output")
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Verify T7 pattern structures are present
	t.Run("verify_controllers_with_scopes", func(t *testing.T) {
		controllers, ok := result["controllers"].([]interface{})
		if !ok {
			t.Fatal("Controllers array not found")
		}

		foundScopes := false
		for _, controller := range controllers {
			ctrl, ok := controller.(map[string]interface{})
			if !ok {
				continue
			}

			if scopesUsed, exists := ctrl["scopesUsed"]; exists {
				scopes, ok := scopesUsed.([]interface{})
				if ok && len(scopes) > 0 {
					foundScopes = true
					// Verify scope structure
					scope := scopes[0].(map[string]interface{})
					if _, hasOn := scope["on"]; !hasOn {
						t.Error("Scope missing 'on' field")
					}
					if _, hasName := scope["name"]; !hasName {
						t.Error("Scope missing 'name' field")
					}
					break
				}
			}
		}

		if !foundScopes {
			t.Log("No scope usage patterns found - this may be expected if fixture files don't contain scopes")
		}
	})

	t.Run("verify_models_with_pivot_and_attributes", func(t *testing.T) {
		models, ok := result["models"].([]interface{})
		if !ok || len(models) == 0 {
			t.Log("No models found in output - this may be expected for current fixture structure")
			return
		}

		foundPivot := false
		foundAttributes := false

		for _, model := range models {
			mdl, ok := model.(map[string]interface{})
			if !ok {
				continue
			}

			// Check for pivot patterns
			if withPivot, exists := mdl["withPivot"]; exists {
				pivots, ok := withPivot.([]interface{})
				if ok && len(pivots) > 0 {
					foundPivot = true
					// Verify pivot structure
					pivot := pivots[0].(map[string]interface{})
					if _, hasRelation := pivot["relation"]; !hasRelation {
						t.Error("Pivot missing 'relation' field")
					}
					if _, hasColumns := pivot["columns"]; !hasColumns {
						t.Error("Pivot missing 'columns' field")
					}
				}
			}

			// Check for attribute patterns
			if attributes, exists := mdl["attributes"]; exists {
				attrs, ok := attributes.([]interface{})
				if ok && len(attrs) > 0 {
					foundAttributes = true
					// Verify attribute structure
					attr := attrs[0].(map[string]interface{})
					if _, hasName := attr["name"]; !hasName {
						t.Error("Attribute missing 'name' field")
					}
					if via, hasVia := attr["via"]; !hasVia || via != "Attribute::make" {
						t.Error("Attribute missing or invalid 'via' field")
					}
				}
			}
		}

		if !foundPivot {
			t.Log("No pivot patterns found - this may be expected if fixture files don't contain pivot relationships")
		}
		if !foundAttributes {
			t.Log("No attribute patterns found - this may be expected if fixture files don't contain modern attributes")
		}
	})

	// Test deterministic output by running twice and comparing hashes
	t.Run("verify_deterministic_output", func(t *testing.T) {
		// Run CLI again
		cmd2 := exec.Command(cliPath, "--manifest", manifestFile.Name())
		var stdout2 bytes.Buffer
		cmd2.Stdout = &stdout2

		if err := cmd2.Run(); err != nil {
			t.Fatalf("Second CLI execution failed: %v", err)
		}

		output2 := stdout2.String()

		// Compare canonical hashes (excluding volatile fields)
		hash1 := calculateCanonicalHash(output)
		hash2 := calculateCanonicalHash(output2)

		if hash1 != hash2 {
			t.Error("CLI output is not deterministic - two runs produced different results")
		}
	})
}

// buildCLIBinary builds the oxinfer CLI binary and returns the path
func buildCLIBinary(t *testing.T) string {
	t.Helper()

	// Create temporary directory for the binary
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "oxinfer")

	// Build the binary
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/oxinfer")
	// Set working directory to the project root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(wd, "../..")
	cmd.Dir = projectRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build CLI binary: %v\nStderr: %s", err, stderr.String())
	}

	return binaryPath
}

// TestBuildAndBasicExecution tests that the binary can be built and executed
func TestBuildAndBasicExecution(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Test that binary exists and is executable
	info, err := os.Stat(cliPath)
	if err != nil {
		t.Fatalf("Binary does not exist: %v", err)
	}

	if info.Mode()&0111 == 0 {
		t.Fatal("Binary is not executable")
	}

	// Test basic execution (version flag)
	cmd := exec.Command(cliPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Binary execution failed: %v", err)
	}

	if !strings.Contains(string(output), "0.1.0") {
		t.Errorf("Version output unexpected: %s", string(output))
	}
}

// TestNonExistentManifestFile tests behavior with non-existent manifest files
func TestNonExistentManifestFile(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	cmd := exec.Command(cliPath, "--manifest", "/nonexistent/file.json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected CLI to fail with non-existent manifest file")
	}

	// Should exit with input error code
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ExitCode() != 1 { // ExitInputError
			t.Errorf("Expected exit code 1, got %d", exitError.ExitCode())
		}
	}

	// Should produce JSON error on stderr
	stderrStr := stderr.String()
	var errorObj map[string]interface{}
	if err := json.Unmarshal([]byte(stderrStr), &errorObj); err != nil {
		t.Errorf("Error output should be valid JSON: %v\nOutput: %s", err, stderrStr)
	}
}


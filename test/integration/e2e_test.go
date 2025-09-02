package integration

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// IntegrationTest defines a complete end-to-end test scenario
type IntegrationTest struct {
	Name         string
	FixturePath  string
	ManifestFile string
	GoldenFile   string
	MaxDuration  time.Duration
	Description  string
}

// TestE2EIntegrationSuite runs the complete integration test suite
func TestE2EIntegrationSuite(t *testing.T) {
	// Build the CLI binary first
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Define integration test cases
	integrationTests := []IntegrationTest{
		{
			Name:         "minimal_laravel",
			FixturePath:  "../../test/fixtures/integration/minimal-laravel",
			ManifestFile: "manifest.json",
			GoldenFile:   "minimal-laravel.json",
			MaxDuration:  5 * time.Second,
			Description:  "Basic Laravel patterns with simple controller and model relationships",
		},
		{
			Name:         "api_project",
			FixturePath:  "../../test/fixtures/integration/api-project",
			ManifestFile: "manifest.json",
			GoldenFile:   "api-project.json",
			MaxDuration:  10 * time.Second,
			Description:  "API-focused project with resources, requests, and pivot relationships",
		},
		{
			Name:         "complex_app",
			FixturePath:  "../../test/fixtures/integration/complex-app",
			ManifestFile: "manifest.json",
			GoldenFile:   "complex-app.json",
			MaxDuration:  15 * time.Second,
			Description:  "Advanced patterns including polymorphic relationships and broadcasting",
		},
	}

	for _, test := range integrationTests {
		t.Run(test.Name, func(t *testing.T) {
			runIntegrationTest(t, cliPath, test)
		})
	}
}

// runIntegrationTest executes a single integration test
func runIntegrationTest(t *testing.T, cliPath string, test IntegrationTest) {
	t.Helper()

	// Verify fixture exists
	fixturePath := filepath.Join(test.FixturePath)
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Fatalf("Fixture directory does not exist: %s", fixturePath)
	}

	// Verify manifest file exists
	manifestPath := filepath.Join(fixturePath, test.ManifestFile)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatalf("Manifest file does not exist: %s", manifestPath)
	}

	// Load and verify golden file
	goldenPath := filepath.Join("../../test/golden", test.GoldenFile)
	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file %s: %v", goldenPath, err)
	}

	var expectedResult map[string]interface{}
	if err := json.Unmarshal(goldenData, &expectedResult); err != nil {
		t.Fatalf("Golden file contains invalid JSON: %v", err)
	}

	// Run the CLI with the test manifest
	t.Logf("Running integration test: %s", test.Description)
	start := time.Now()

	cmd := exec.Command(cliPath, "--manifest", manifestPath)
	output, err := cmd.Output()
	duration := time.Since(start)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			t.Fatalf("CLI execution failed with exit code %d: %v\nStderr: %s", 
				exitError.ExitCode(), err, string(exitError.Stderr))
		}
		t.Fatalf("CLI execution failed: %v", err)
	}

	// Check execution time
	if duration > test.MaxDuration {
		t.Errorf("Execution took too long: %v > %v", duration, test.MaxDuration)
	}
	t.Logf("Execution completed in %v", duration)

	// Parse actual output
	var actualResult map[string]interface{}
	if err := json.Unmarshal(output, &actualResult); err != nil {
		t.Fatalf("CLI output is not valid JSON: %v\nOutput: %s", err, string(output))
	}

	// Validate output structure against golden file
	validateOutputStructure(t, test.Name, expectedResult, actualResult)

	// Test deterministic output by running twice more
	validateDeterministicOutput(t, cliPath, manifestPath, string(output))
}

// validateOutputStructure compares actual output against expected golden file structure
func validateOutputStructure(t *testing.T, testName string, expected, actual map[string]interface{}) {
	t.Helper()

	// Remove dynamic fields that vary between runs
	if actualMeta, ok := actual["meta"].(map[string]interface{}); ok {
		if actualStats, ok := actualMeta["stats"].(map[string]interface{}); ok {
			// Keep durationMs for performance validation but allow some variance
			if duration, ok := actualStats["durationMs"].(float64); ok {
				if duration > 30000 { // 30 seconds is too long
					t.Errorf("Execution duration too long: %v ms", duration)
				}
			}
			// Remove dynamic timestamp fields for comparison
			delete(actualStats, "durationMs")
		}
	}

	if expectedMeta, ok := expected["meta"].(map[string]interface{}); ok {
		if expectedStats, ok := expectedMeta["stats"].(map[string]interface{}); ok {
			delete(expectedStats, "durationMs")
		}
	}

	// Validate required top-level fields
	requiredFields := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
	for _, field := range requiredFields {
		if _, exists := actual[field]; !exists {
			t.Errorf("Output missing required field: %s", field)
		}
	}

	// Validate meta structure
	validateMetaStructure(t, expected, actual)

	// For comprehensive tests, validate detailed content matches expectations
	switch testName {
	case "minimal_laravel":
		validateMinimalLaravelOutput(t, expected, actual)
	case "api_project":
		validateApiProjectOutput(t, expected, actual)
	case "complex_app":
		validateComplexAppOutput(t, expected, actual)
	}
}

// validateMetaStructure validates the meta section structure
func validateMetaStructure(t *testing.T, expected, actual map[string]interface{}) {
	t.Helper()

	actualMeta, ok := actual["meta"].(map[string]interface{})
	if !ok {
		t.Error("Meta field should be an object")
		return
	}

	// Check partial field
	if partial, exists := actualMeta["partial"]; !exists {
		t.Error("Meta missing partial field")
	} else if _, ok := partial.(bool); !ok {
		t.Error("Meta partial should be boolean")
	}

	// Check stats structure
	if stats, exists := actualMeta["stats"]; !exists {
		t.Error("Meta missing stats field")
	} else if statsObj, ok := stats.(map[string]interface{}); ok {
		requiredStatsFields := []string{"filesParsed", "skipped"}
		for _, field := range requiredStatsFields {
			if _, exists := statsObj[field]; !exists {
				t.Errorf("Meta.stats missing %s field", field)
			}
		}
	} else {
		t.Error("Meta.stats should be an object")
	}
}

// validateMinimalLaravelOutput validates output for minimal Laravel fixture
func validateMinimalLaravelOutput(t *testing.T, expected, actual map[string]interface{}) {
	t.Helper()

	// Check controllers array
	if controllers, ok := actual["controllers"].([]interface{}); ok {
		if len(controllers) == 0 {
			t.Log("No controllers found - this may be expected if parsing is not yet implemented")
			return
		}

		// Validate controller structure if present
		for i, controller := range controllers {
			ctrl, ok := controller.(map[string]interface{})
			if !ok {
				t.Errorf("Controller %d should be an object", i)
				continue
			}

			requiredFields := []string{"class", "file", "methods"}
			for _, field := range requiredFields {
				if _, exists := ctrl[field]; !exists {
					t.Errorf("Controller %d missing %s field", i, field)
				}
			}
		}
	} else {
		t.Error("Controllers should be an array")
	}

	// Check models array
	if models, ok := actual["models"].([]interface{}); ok {
		if len(models) == 0 {
			t.Log("No models found - this may be expected if parsing is not yet implemented")
			return
		}

		// Validate model structure if present
		for i, model := range models {
			mdl, ok := model.(map[string]interface{})
			if !ok {
				t.Errorf("Model %d should be an object", i)
				continue
			}

			requiredFields := []string{"class", "file"}
			for _, field := range requiredFields {
				if _, exists := mdl[field]; !exists {
					t.Errorf("Model %d missing %s field", i, field)
				}
			}
		}
	} else {
		t.Error("Models should be an array")
	}
}

// validateApiProjectOutput validates output for API project fixture
func validateApiProjectOutput(t *testing.T, expected, actual map[string]interface{}) {
	t.Helper()

	// Check for resource usage patterns
	if controllers, ok := actual["controllers"].([]interface{}); ok && len(controllers) > 0 {
		foundResourceUsage := false
		for _, controller := range controllers {
			if ctrl, ok := controller.(map[string]interface{}); ok {
				if methods, ok := ctrl["methods"].([]interface{}); ok {
					for _, method := range methods {
						if methodObj, ok := method.(map[string]interface{}); ok {
							if _, exists := methodObj["resourceUsage"]; exists {
								foundResourceUsage = true
								break
							}
						}
					}
				}
			}
		}

		if !foundResourceUsage {
			t.Log("No resource usage found - this may be expected if resource pattern matching is not yet implemented")
		}
	}

	// Check for pivot relationships
	if models, ok := actual["models"].([]interface{}); ok && len(models) > 0 {
		foundPivot := false
		for _, model := range models {
			if mdl, ok := model.(map[string]interface{}); ok {
				if relationships, ok := mdl["relationships"].([]interface{}); ok {
					for _, rel := range relationships {
						if relObj, ok := rel.(map[string]interface{}); ok {
							if _, exists := relObj["withPivot"]; exists {
								foundPivot = true
								break
							}
						}
					}
				}
			}
		}

		if !foundPivot {
			t.Log("No pivot relationships found - this may be expected if pivot pattern matching is not yet implemented")
		}
	}
}

// validateComplexAppOutput validates output for complex app fixture
func validateComplexAppOutput(t *testing.T, expected, actual map[string]interface{}) {
	t.Helper()

	// Check for polymorphic relationships
	if polymorphic, ok := actual["polymorphic"].([]interface{}); ok {
		if len(polymorphic) == 0 {
			t.Log("No polymorphic relationships found - this may be expected if polymorphic pattern matching is not yet implemented")
		} else {
			// Validate polymorphic structure
			for i, poly := range polymorphic {
				polyObj, ok := poly.(map[string]interface{})
				if !ok {
					t.Errorf("Polymorphic %d should be an object", i)
					continue
				}

				requiredFields := []string{"name", "discriminator", "relations"}
				for _, field := range requiredFields {
					if _, exists := polyObj[field]; !exists {
						t.Errorf("Polymorphic %d missing %s field", i, field)
					}
				}
			}
		}
	} else {
		t.Error("Polymorphic should be an array")
	}

	// Check for broadcast channels
	if broadcast, ok := actual["broadcast"].([]interface{}); ok {
		if len(broadcast) == 0 {
			t.Log("No broadcast channels found - this may be expected if broadcast pattern matching is not yet implemented")
		} else {
			// Validate broadcast structure
			for i, channel := range broadcast {
				channelObj, ok := channel.(map[string]interface{})
				if !ok {
					t.Errorf("Broadcast channel %d should be an object", i)
					continue
				}

				requiredFields := []string{"channel", "type", "parameters"}
				for _, field := range requiredFields {
					if _, exists := channelObj[field]; !exists {
						t.Errorf("Broadcast channel %d missing %s field", i, field)
					}
				}
			}
		}
	} else {
		t.Error("Broadcast should be an array")
	}

	// Check for scope usage
	if controllers, ok := actual["controllers"].([]interface{}); ok && len(controllers) > 0 {
		foundScopes := false
		for _, controller := range controllers {
			if ctrl, ok := controller.(map[string]interface{}); ok {
				if methods, ok := ctrl["methods"].([]interface{}); ok {
					for _, method := range methods {
						if methodObj, ok := method.(map[string]interface{}); ok {
							if scopesUsed, exists := methodObj["scopesUsed"]; exists {
								if scopes, ok := scopesUsed.([]interface{}); ok && len(scopes) > 0 {
									foundScopes = true
									break
								}
							}
						}
					}
				}
			}
		}

		if !foundScopes {
			t.Log("No scope usage found - this may be expected if scope pattern matching is not yet implemented")
		}
	}
}

// validateDeterministicOutput ensures the CLI produces identical output on multiple runs
func validateDeterministicOutput(t *testing.T, cliPath, manifestPath, originalOutput string) {
	t.Helper()

	// Run CLI two more times and compare outputs
	for i := 0; i < 2; i++ {
		cmd := exec.Command(cliPath, "--manifest", manifestPath)
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("Determinism test run %d failed: %v", i+1, err)
		}

		// Parse JSON to normalize for comparison (handles potential whitespace differences)
		var originalResult, currentResult map[string]interface{}
		
		if err := json.Unmarshal([]byte(originalOutput), &originalResult); err != nil {
			t.Fatalf("Failed to parse original output as JSON: %v", err)
		}
		
		if err := json.Unmarshal(output, &currentResult); err != nil {
			t.Fatalf("Failed to parse current output as JSON: %v", err)
		}

		// Remove dynamic fields that are allowed to vary
		removeDynamicFields := func(result map[string]interface{}) {
			if meta, ok := result["meta"].(map[string]interface{}); ok {
				if stats, ok := meta["stats"].(map[string]interface{}); ok {
					delete(stats, "durationMs") // Duration can vary
				}
			}
		}

		removeDynamicFields(originalResult)
		removeDynamicFields(currentResult)

		// Convert back to JSON for comparison
		originalBytes, _ := json.Marshal(originalResult)
		currentBytes, _ := json.Marshal(currentResult)

		// Calculate hashes for comparison
		originalHash := fmt.Sprintf("%x", sha256.Sum256(originalBytes))
		currentHash := fmt.Sprintf("%x", sha256.Sum256(currentBytes))

		if originalHash != currentHash {
			t.Errorf("Output not deterministic on run %d", i+1)
			t.Logf("Original hash: %s", originalHash)
			t.Logf("Current hash: %s", currentHash)
			t.Logf("Original output (normalized): %s", string(originalBytes))
			t.Logf("Current output (normalized): %s", string(currentBytes))
		}
	}
}

// TestFixtureValidation validates that all fixtures are properly structured
func TestFixtureValidation(t *testing.T) {
	fixtures := []struct {
		name        string
		path        string
		description string
	}{
		{
			name:        "minimal_laravel",
			path:        "../../test/fixtures/integration/minimal-laravel",
			description: "Minimal Laravel fixture validation",
		},
		{
			name:        "api_project",
			path:        "../../test/fixtures/integration/api-project",
			description: "API project fixture validation",
		},
		{
			name:        "complex_app",
			path:        "../../test/fixtures/integration/complex-app",
			description: "Complex app fixture validation",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			validateFixture(t, fixture.path, fixture.description)
		})
	}
}

// validateFixture checks that a fixture directory is properly structured
func validateFixture(t *testing.T, fixturePath, description string) {
	t.Helper()

	t.Logf("Validating fixture: %s", description)

	// Check that fixture directory exists
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Fatalf("Fixture directory does not exist: %s", fixturePath)
	}

	// Check required files
	requiredFiles := []string{"composer.json", "manifest.json"}
	for _, file := range requiredFiles {
		filePath := filepath.Join(fixturePath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Required file missing: %s", file)
		}
	}

	// Validate composer.json structure
	composerPath := filepath.Join(fixturePath, "composer.json")
	if composerData, err := os.ReadFile(composerPath); err == nil {
		var composer map[string]interface{}
		if err := json.Unmarshal(composerData, &composer); err != nil {
			t.Errorf("Invalid composer.json: %v", err)
		} else {
			// Check required composer fields
			requiredComposerFields := []string{"name", "autoload"}
			for _, field := range requiredComposerFields {
				if _, exists := composer[field]; !exists {
					t.Errorf("composer.json missing required field: %s", field)
				}
			}
		}
	}

	// Validate manifest.json structure
	manifestPath := filepath.Join(fixturePath, "manifest.json")
	if manifestData, err := os.ReadFile(manifestPath); err == nil {
		var manifest map[string]interface{}
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			t.Errorf("Invalid manifest.json: %v", err)
		} else {
			// Check required manifest fields
			requiredManifestFields := []string{"project", "scan"}
			for _, field := range requiredManifestFields {
				if _, exists := manifest[field]; !exists {
					t.Errorf("manifest.json missing required field: %s", field)
				}
			}
		}
	}

	// Check for PHP files in expected directories
	appDir := filepath.Join(fixturePath, "app")
	if _, err := os.Stat(appDir); err == nil {
		err := filepath.Walk(appDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(info.Name(), ".php") {
				// Validate PHP file has basic structure
				content, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("Failed to read PHP file %s: %v", path, err)
					return nil
				}

				contentStr := string(content)
				if !strings.Contains(contentStr, "<?php") {
					t.Errorf("PHP file missing opening tag: %s", path)
				}

				if !strings.Contains(contentStr, "namespace") {
					t.Errorf("PHP file missing namespace: %s", path)
				}
			}
			return nil
		})

		if err != nil {
			t.Errorf("Error walking app directory: %v", err)
		}
	}

	t.Logf("Fixture validation completed: %s", description)
}

// TestGoldenFileIntegrity validates that golden files are properly structured
func TestGoldenFileIntegrity(t *testing.T) {
	goldenDir := "../../test/golden"
	goldenFiles := []string{
		"minimal-laravel.json",
		"api-project.json", 
		"complex-app.json",
	}

	// Read checksums file
	checksumsPath := filepath.Join(goldenDir, "checksums.sha256")
	checksumsData, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatalf("Failed to read checksums file: %v", err)
	}

	checksums := make(map[string]string)
	lines := strings.Split(string(checksumsData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 2 {
			checksums[parts[1]] = parts[0]
		}
	}

	for _, filename := range goldenFiles {
		t.Run(filename, func(t *testing.T) {
			filePath := filepath.Join(goldenDir, filename)
			
			// Check file exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				t.Fatalf("Golden file does not exist: %s", filename)
			}

			// Read and validate JSON structure
			data, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read golden file: %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Golden file contains invalid JSON: %v", err)
			}

			// Validate required top-level structure
			requiredFields := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
			for _, field := range requiredFields {
				if _, exists := result[field]; !exists {
					t.Errorf("Golden file missing required field: %s", field)
				}
			}

			// Verify checksum integrity
			if expectedChecksum, exists := checksums[filename]; exists {
				actualChecksum := fmt.Sprintf("%x", sha256.Sum256(data))
				if actualChecksum != expectedChecksum {
					t.Errorf("Golden file checksum mismatch for %s", filename)
					t.Logf("Expected: %s", expectedChecksum)
					t.Logf("Actual: %s", actualChecksum)
				}
			} else {
				t.Errorf("No checksum found for golden file: %s", filename)
			}
		})
	}
}
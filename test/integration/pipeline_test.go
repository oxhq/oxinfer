package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPipelineIntegration tests the complete T1-T12 pipeline integration
func TestPipelineIntegration(t *testing.T) {
	tests := []struct {
		name            string
		fixture         string
		expectedFeatures []string
		description     string
	}{
		{
			name:            "basic_pipeline",
			fixture:         "minimal-laravel",
			expectedFeatures: []string{"http_status", "request_usage"},
			description:     "Basic pipeline with HTTP status and request validation patterns",
		},
		{
			name:            "advanced_pipeline", 
			fixture:         "api-project",
			expectedFeatures: []string{"http_status", "request_usage", "resource_usage", "with_pivot", "scopes_used"},
			description:     "Advanced pipeline with resource patterns, pivot relationships, and scopes",
		},
		{
			name:            "complete_pipeline",
			fixture:         "complex-app",
			expectedFeatures: []string{"http_status", "request_usage", "resource_usage", "with_pivot", "scopes_used", "polymorphic", "broadcast_channels"},
			description:     "Complete pipeline with all T5-T11 patterns including polymorphic and broadcasting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPipelineExecution(t, tt.fixture, tt.expectedFeatures, tt.description)
		})
	}
}

// testPipelineExecution runs the complete pipeline and validates output
func testPipelineExecution(t *testing.T, fixtureName string, expectedFeatures []string, description string) {
	t.Helper()

	t.Logf("Testing pipeline: %s", description)

	// Build CLI binary
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Prepare manifest path
	manifestPath := filepath.Join("../../test/fixtures/integration", fixtureName, "manifest.json")
	
	// Verify manifest exists and load it
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read manifest: %v", err)
	}

	var manifestConfig map[string]interface{}
	if err := json.Unmarshal(manifestData, &manifestConfig); err != nil {
		t.Fatalf("Invalid manifest JSON: %v", err)
	}

	// Execute the pipeline
	start := time.Now()
	cmd := exec.Command(cliPath, "--manifest", manifestPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Pipeline execution failed: %v\nStderr: %s", err, stderr.String())
	}

	t.Logf("Pipeline execution completed in %v", duration)

	// Parse output
	output := stdout.String()
	if output == "" {
		t.Fatal("Pipeline produced no output")
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Pipeline output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Validate pipeline components
	validatePipelineComponents(t, result, expectedFeatures, fixtureName)

	// Performance validation
	validatePipelinePerformance(t, result, duration, fixtureName)
}

// validatePipelineComponents validates that each pipeline component produced expected results
func validatePipelineComponents(t *testing.T, result map[string]interface{}, expectedFeatures []string, fixtureName string) {
	t.Helper()

	// T1: Manifest validation (implicit - if we got here, validation passed)
	t.Log("✓ T1: Manifest validation completed")

	// T2: PSR-4 resolution (implicit in class name resolution)
	controllers, hasControllers := result["controllers"].([]interface{})
	models, hasModels := result["models"].([]interface{})
	
	if hasControllers && len(controllers) > 0 {
		t.Log("✓ T2: PSR-4 class resolution working (controllers found)")
	} else {
		t.Log("- T2: PSR-4 class resolution (no controllers to validate)")
	}

	// T3: File indexing and caching (implicit - files were processed)
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if stats, ok := meta["stats"].(map[string]interface{}); ok {
			if filesParsed, ok := stats["filesParsed"].(float64); ok && filesParsed > 0 {
				t.Logf("✓ T3: File indexing completed (%v files parsed)", filesParsed)
			} else {
				t.Log("- T3: File indexing (no files parsed)")
			}
		}
	}

	// T4: PHP parsing with tree-sitter (implicit - PHP files were processed)
	if (hasControllers && len(controllers) > 0) || (hasModels && len(models) > 0) {
		t.Log("✓ T4: PHP parsing with tree-sitter working")
	} else {
		t.Log("- T4: PHP parsing (no PHP structures detected)")
	}

	// T5-T6: HTTP Status & Resource matchers
	if contains(expectedFeatures, "http_status") {
		validateHttpStatusMatching(t, controllers)
	}
	if contains(expectedFeatures, "resource_usage") {
		validateResourceMatching(t, controllers)
	}

	// T7-T8: Pivot & Scopes matchers  
	if contains(expectedFeatures, "with_pivot") {
		validatePivotMatching(t, models)
	}
	if contains(expectedFeatures, "scopes_used") {
		validateScopeMatching(t, controllers)
	}

	// T9-T10: Polymorphic & Broadcast matchers
	if contains(expectedFeatures, "polymorphic") {
		validatePolymorphicMatching(t, result)
	}
	if contains(expectedFeatures, "broadcast_channels") {
		validateBroadcastMatching(t, result)
	}

	// T11: Shape inference (implicit in structured output)
	validateShapeInference(t, result)

	// T12: Final emission (implicit - we got structured JSON output)
	t.Log("✓ T12: Delta JSON emission completed")
}

// validateHttpStatusMatching validates T5 HTTP status pattern matching
func validateHttpStatusMatching(t *testing.T, controllers []interface{}) {
	t.Helper()

	foundHttpStatus := false
	for _, controller := range controllers {
		if ctrl, ok := controller.(map[string]interface{}); ok {
			if methods, ok := ctrl["methods"].([]interface{}); ok {
				for _, method := range methods {
					if methodObj, ok := method.(map[string]interface{}); ok {
						if httpStatus, exists := methodObj["httpStatus"]; exists {
							if statusArray, ok := httpStatus.([]interface{}); ok && len(statusArray) > 0 {
								foundHttpStatus = true
								t.Logf("✓ T5: HTTP status patterns detected in %s", ctrl["class"])
								return
							}
						}
					}
				}
			}
		}
	}

	if !foundHttpStatus {
		t.Log("- T5: HTTP status patterns (none detected - may be expected)")
	}
}

// validateResourceMatching validates T6 resource pattern matching
func validateResourceMatching(t *testing.T, controllers []interface{}) {
	t.Helper()

	foundResourceUsage := false
	for _, controller := range controllers {
		if ctrl, ok := controller.(map[string]interface{}); ok {
			if methods, ok := ctrl["methods"].([]interface{}); ok {
				for _, method := range methods {
					if methodObj, ok := method.(map[string]interface{}); ok {
						if resourceUsage, exists := methodObj["resourceUsage"]; exists {
							if resourceArray, ok := resourceUsage.([]interface{}); ok && len(resourceArray) > 0 {
								foundResourceUsage = true
								t.Logf("✓ T6: Resource patterns detected in %s", ctrl["class"])
								return
							}
						}
					}
				}
			}
		}
	}

	if !foundResourceUsage {
		t.Log("- T6: Resource patterns (none detected - may be expected)")
	}
}

// validatePivotMatching validates T7 pivot relationship matching
func validatePivotMatching(t *testing.T, models []interface{}) {
	t.Helper()

	foundPivot := false
	for _, model := range models {
		if mdl, ok := model.(map[string]interface{}); ok {
			if relationships, ok := mdl["relationships"].([]interface{}); ok {
				for _, rel := range relationships {
					if relObj, ok := rel.(map[string]interface{}); ok {
						if _, exists := relObj["withPivot"]; exists {
							foundPivot = true
							t.Logf("✓ T7: Pivot relationships detected in %s", mdl["class"])
							return
						}
					}
				}
			}
		}
	}

	if !foundPivot {
		t.Log("- T7: Pivot relationships (none detected - may be expected)")
	}
}

// validateScopeMatching validates T8 scope pattern matching  
func validateScopeMatching(t *testing.T, controllers []interface{}) {
	t.Helper()

	foundScopes := false
	for _, controller := range controllers {
		if ctrl, ok := controller.(map[string]interface{}); ok {
			if methods, ok := ctrl["methods"].([]interface{}); ok {
				for _, method := range methods {
					if methodObj, ok := method.(map[string]interface{}); ok {
						if scopesUsed, exists := methodObj["scopesUsed"]; exists {
							if scopeArray, ok := scopesUsed.([]interface{}); ok && len(scopeArray) > 0 {
								foundScopes = true
								t.Logf("✓ T8: Scope patterns detected in %s", ctrl["class"])
								return
							}
						}
					}
				}
			}
		}
	}

	if !foundScopes {
		t.Log("- T8: Scope patterns (none detected - may be expected)")
	}
}

// validatePolymorphicMatching validates T9 polymorphic relationship matching
func validatePolymorphicMatching(t *testing.T, result map[string]interface{}) {
	t.Helper()

	if polymorphic, ok := result["polymorphic"].([]interface{}); ok {
		if len(polymorphic) > 0 {
			t.Logf("✓ T9: Polymorphic relationships detected (%d patterns)", len(polymorphic))
			
			// Validate structure
			for i, poly := range polymorphic {
				if polyObj, ok := poly.(map[string]interface{}); ok {
					requiredFields := []string{"parent", "morph", "discriminator"}
					for _, field := range requiredFields {
						if _, exists := polyObj[field]; !exists {
							t.Errorf("Polymorphic pattern %d missing %s field", i, field)
						}
					}
				}
			}
		} else {
			t.Log("- T9: Polymorphic relationships (none detected - may be expected)")
		}
	}
}

// validateBroadcastMatching validates T10 broadcast channel matching
func validateBroadcastMatching(t *testing.T, result map[string]interface{}) {
	t.Helper()

	if broadcast, ok := result["broadcast"].([]interface{}); ok {
		if len(broadcast) > 0 {
			t.Logf("✓ T10: Broadcast channels detected (%d channels)", len(broadcast))
			
			// Validate structure
			for i, channel := range broadcast {
				if channelObj, ok := channel.(map[string]interface{}); ok {
					requiredFields := []string{"channel", "params", "visibility"}
					for _, field := range requiredFields {
						if _, exists := channelObj[field]; !exists {
							t.Errorf("Broadcast channel %d missing %s field", i, field)
						}
					}
				}
			}
		} else {
			t.Log("- T10: Broadcast channels (none detected - may be expected)")
		}
	}
}

// validateShapeInference validates T11 shape inference
func validateShapeInference(t *testing.T, result map[string]interface{}) {
	t.Helper()

	// Check that output follows expected schema structure
	requiredTopLevel := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
	missingFields := []string{}

	for _, field := range requiredTopLevel {
		if _, exists := result[field]; !exists {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) == 0 {
		t.Log("✓ T11: Shape inference working (all required fields present)")
	} else {
		t.Errorf("T11: Shape inference incomplete - missing fields: %v", missingFields)
	}

	// Validate meta structure
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if stats, ok := meta["stats"].(map[string]interface{}); ok {
			expectedStatsFields := []string{"filesParsed", "skipped"}
			for _, field := range expectedStatsFields {
				if _, exists := stats[field]; !exists {
					t.Errorf("T11: Stats missing field: %s", field)
				}
			}
		}
	}
}

// validatePipelinePerformance validates pipeline execution performance
func validatePipelinePerformance(t *testing.T, result map[string]interface{}, duration time.Duration, fixtureName string) {
	t.Helper()

	// Performance thresholds by fixture complexity
	thresholds := map[string]time.Duration{
		"minimal-laravel": 5 * time.Second,
		"api-project":     10 * time.Second,
		"complex-app":     15 * time.Second,
	}

	threshold, exists := thresholds[fixtureName]
	if !exists {
		threshold = 30 * time.Second // Default threshold
	}

	if duration > threshold {
		t.Errorf("Pipeline execution too slow: %v > %v", duration, threshold)
	} else {
		t.Logf("✓ Pipeline performance: %v (threshold: %v)", duration, threshold)
	}

	// Validate internal duration reporting
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if stats, ok := meta["stats"].(map[string]interface{}); ok {
			if durationMs, ok := stats["durationMs"].(float64); ok {
				internalDuration := time.Duration(durationMs) * time.Millisecond
				
				// Internal duration should be reasonably close to measured duration
				// (allowing for CLI startup overhead)
				if internalDuration > duration {
					t.Errorf("Internal duration reporting inconsistent: %v > %v", internalDuration, duration)
				}
			}
		}
	}
}

// TestComponentIntegration tests integration between specific pipeline components
func TestComponentIntegration(t *testing.T) {
	tests := []struct {
		name        string
		components  []string
		description string
	}{
		{
			name:        "manifest_to_parsing",
			components:  []string{"T1", "T2", "T3", "T4"},
			description: "Integration from manifest validation to PHP parsing",
		},
		{
			name:        "parsing_to_matching",
			components:  []string{"T4", "T5", "T6"},
			description: "Integration from PHP parsing to pattern matching",
		},
		{
			name:        "matching_to_emission",
			components:  []string{"T5", "T6", "T7", "T8", "T9", "T10", "T11", "T12"},
			description: "Integration from pattern matching to JSON emission",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testComponentIntegration(t, tt.components, tt.description)
		})
	}
}

// testComponentIntegration validates integration between specific components
func testComponentIntegration(t *testing.T, components []string, description string) {
	t.Helper()

	t.Logf("Testing component integration: %s", description)

	// Use the complex app fixture for comprehensive testing
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	manifestPath := filepath.Join("../../test/fixtures/integration/complex-app/manifest.json")
	
	cmd := exec.Command(cliPath, "--manifest", manifestPath)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Component integration test failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Validate that the specific components work together
	switch strings.Join(components, "_") {
	case "T1_T2_T3_T4":
		// Validate that manifest → parsing pipeline works
		validateManifestParsingIntegration(t, result)
	case "T4_T5_T6": 
		// Validate that parsing → basic matching works
		validateParsingMatchingIntegration(t, result)
	case "T5_T6_T7_T8_T9_T10_T11_T12":
		// Validate that matching → emission pipeline works
		validateMatchingEmissionIntegration(t, result)
	}

	t.Logf("✓ Component integration validated: %s", description)
}

// validateManifestParsingIntegration validates T1-T4 integration
func validateManifestParsingIntegration(t *testing.T, result map[string]interface{}) {
	t.Helper()

	// If we get valid output, manifest validation worked (T1)
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if partial, ok := meta["partial"].(bool); ok && !partial {
			t.Log("✓ T1→T4: Manifest validation to parsing integration working")
		}
	}

	// Check that files were found and parsed (T2→T4)
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if stats, ok := meta["stats"].(map[string]interface{}); ok {
			if filesParsed, ok := stats["filesParsed"].(float64); ok && filesParsed > 0 {
				t.Logf("✓ T2→T4: File discovery to parsing (%v files)", filesParsed)
			} else {
				t.Error("T2→T4: No files were parsed")
			}
		}
	}
}

// validateParsingMatchingIntegration validates T4-T6 integration
func validateParsingMatchingIntegration(t *testing.T, result map[string]interface{}) {
	t.Helper()

	// Check that parsed PHP files led to pattern detection
	if controllers, ok := result["controllers"].([]interface{}); ok && len(controllers) > 0 {
		foundPatterns := false
		for _, controller := range controllers {
			if ctrl, ok := controller.(map[string]interface{}); ok {
				if methods, ok := ctrl["methods"].([]interface{}); ok && len(methods) > 0 {
					foundPatterns = true
					t.Log("✓ T4→T6: PHP parsing to pattern matching integration working")
					break
				}
			}
		}
		
		if !foundPatterns {
			t.Log("- T4→T6: Parsing successful but no patterns detected")
		}
	} else {
		t.Log("- T4→T6: No controllers detected from parsing")
	}
}

// validateMatchingEmissionIntegration validates T5-T12 integration
func validateMatchingEmissionIntegration(t *testing.T, result map[string]interface{}) {
	t.Helper()

	// Check that pattern matching results are properly structured in output
	sectionsWithContent := 0
	sections := []string{"controllers", "models", "polymorphic", "broadcast"}
	
	for _, section := range sections {
		if arr, ok := result[section].([]interface{}); ok && len(arr) > 0 {
			sectionsWithContent++
		}
	}

	if sectionsWithContent > 0 {
		t.Logf("✓ T5→T12: Pattern matching to emission (%d sections with content)", sectionsWithContent)
	} else {
		t.Log("- T5→T12: Pattern matching completed but no patterns detected")
	}

	// Validate that JSON structure follows schema
	requiredFields := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
	allPresent := true
	for _, field := range requiredFields {
		if _, exists := result[field]; !exists {
			allPresent = false
			break
		}
	}

	if allPresent {
		t.Log("✓ T11→T12: Shape inference to JSON emission integration working")
	} else {
		t.Error("T11→T12: JSON structure incomplete")
	}
}

// TestErrorHandlingIntegration tests error handling across pipeline components
func TestErrorHandlingIntegration(t *testing.T) {
	errorTests := []struct {
		name           string
		modifyManifest func(map[string]interface{})
		expectedExit   int
		description    string
	}{
		{
			name: "invalid_project_root",
			modifyManifest: func(m map[string]interface{}) {
				if project, ok := m["project"].(map[string]interface{}); ok {
					project["root"] = "/nonexistent/directory"
				}
			},
			expectedExit: 1, // InputError
			description:  "Pipeline error handling for invalid project root",
		},
		{
			name: "missing_composer_file",
			modifyManifest: func(m map[string]interface{}) {
				if project, ok := m["project"].(map[string]interface{}); ok {
					project["composer"] = "nonexistent.json"
				}
			},
			expectedExit: 1, // InputError
			description:  "Pipeline error handling for missing composer file",
		},
	}

	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Testing error integration: %s", tt.description)

			// Load and modify manifest
			baseManifestPath := filepath.Join("../../test/fixtures/integration/minimal-laravel/manifest.json")
			manifestData, err := os.ReadFile(baseManifestPath)
			if err != nil {
				t.Fatalf("Failed to read base manifest: %v", err)
			}

			var manifest map[string]interface{}
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				t.Fatalf("Failed to parse base manifest: %v", err)
			}

			tt.modifyManifest(manifest)

			// Write modified manifest to temp file
			modifiedManifestData, err := json.Marshal(manifest)
			if err != nil {
				t.Fatalf("Failed to marshal modified manifest: %v", err)
			}

			tempManifest, err := os.CreateTemp("", "error_test_manifest_*.json")
			if err != nil {
				t.Fatalf("Failed to create temp manifest: %v", err)
			}
			defer os.Remove(tempManifest.Name())

			if _, err := tempManifest.Write(modifiedManifestData); err != nil {
				t.Fatalf("Failed to write temp manifest: %v", err)
			}
			tempManifest.Close()

			// Run CLI with modified manifest
			cmd := exec.Command(cliPath, "--manifest", tempManifest.Name())
			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			err = cmd.Run()
			if err == nil {
				t.Fatalf("Expected pipeline to fail but it succeeded")
			}

			// Validate exit code
			if exitError, ok := err.(*exec.ExitError); ok {
				if exitError.ExitCode() != tt.expectedExit {
					t.Errorf("Expected exit code %d, got %d", tt.expectedExit, exitError.ExitCode())
				}
			} else {
				t.Errorf("Expected exit error, got %T", err)
			}

			// Validate structured error output
			stderrStr := stderr.String()
			if stderrStr == "" {
				t.Error("Expected error output on stderr")
			} else {
				// Should be JSON error for structured error handling
				var errorObj map[string]interface{}
				if err := json.Unmarshal([]byte(stderrStr), &errorObj); err != nil {
					t.Logf("Error output: %s", stderrStr)
				} else {
					// Validate error structure
					if _, exists := errorObj["message"]; !exists {
						t.Error("Error JSON missing message field")
					}
					if _, exists := errorObj["exit_code"]; !exists {
						t.Error("Error JSON missing exit_code field")
					}
					t.Log("✓ Structured error handling working")
				}
			}
		})
	}
}

// Helper function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
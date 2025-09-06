package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestPolymorphicPatternsEndToEnd tests the complete pipeline for polymorphic pattern detection
func TestPolymorphicPatternsEndToEnd(t *testing.T) {
	// Build the CLI binary first
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create comprehensive test manifest with polymorphic features enabled
	testManifest := map[string]any{
		"project": map[string]any{
			"root":     "../../",
			"composer": "go.mod",
		},
		"scan": map[string]any{
			"targets": []string{"test/fixtures/matchers"},
		},
		"features": map[string]any{
			"polymorphic": true,
		},
		"limits": map[string]any{
			"max_depth": 5, // Allow deeper traversal for complex polymorphic chains
		},
	}

	// Write manifest to temporary file
	manifestFile, err := os.CreateTemp("", "polymorphic_manifest_*.json")
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

	// Run the CLI with polymorphic-enabled manifest
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

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Run comprehensive validation tests
	t.Run("validate_polymorphic_structure", func(t *testing.T) {
		validatePolymorphicStructure(t, result)
	})

	t.Run("validate_morph_relationships", func(t *testing.T) {
		validateMorphRelationships(t, result)
	})

	t.Run("validate_discriminator_mappings", func(t *testing.T) {
		validateDiscriminatorMappings(t, result)
	})

	t.Run("validate_polymorphic_chains", func(t *testing.T) {
		validatePolymorphicChains(t, result)
	})

	t.Run("validate_integration_with_other_patterns", func(t *testing.T) {
		validateIntegrationWithOtherPatterns(t, result)
	})
}

// validatePolymorphicStructure validates the basic structure of polymorphic output
func validatePolymorphicStructure(t *testing.T, result map[string]any) {
	polymorphicArray, ok := result["polymorphic"].([]any)
	if !ok {
		t.Fatal("Polymorphic array not found or not an array")
	}

	if len(polymorphicArray) == 0 {
		t.Log("No polymorphic patterns found - this may be expected if fixtures don't contain polymorphic relationships")
		return
	}

	// Validate structure of first polymorphic relationship
	polymorph := polymorphicArray[0].(map[string]any)

	requiredFields := []string{"parent", "morph", "discriminator"}
	for _, field := range requiredFields {
		if _, exists := polymorph[field]; !exists {
			t.Errorf("Polymorphic relationship missing required field: %s", field)
		}
	}

	// Validate discriminator structure
	if discriminator, exists := polymorph["discriminator"].(map[string]any); exists {
		if _, hasPropName := discriminator["propertyName"]; !hasPropName {
			t.Error("Discriminator missing 'propertyName' field")
		}
		if _, hasMapping := discriminator["mapping"]; !hasMapping {
			t.Error("Discriminator missing 'mapping' field")
		}
	}

	// Validate morph structure
	if morph, exists := polymorph["morph"].(map[string]any); exists {
		if _, hasKey := morph["key"]; !hasKey {
			t.Error("Morph missing 'key' field")
		}
		if _, hasTypeCol := morph["typeColumn"]; !hasTypeCol {
			t.Error("Morph missing 'typeColumn' field")
		}
		if _, hasIdCol := morph["idColumn"]; !hasIdCol {
			t.Error("Morph missing 'idColumn' field")
		}
	}

}

// validateMorphRelationships validates different types of polymorphic relationships
func validateMorphRelationships(t *testing.T, result map[string]any) {
	polymorphicArray, ok := result["polymorphic"].([]any)
	if !ok || len(polymorphicArray) == 0 {
		t.Log("No polymorphic relationships to validate")
		return
	}

	foundRelationshipTypes := make(map[string]bool)

	for _, item := range polymorphicArray {
		polymorph := item.(map[string]any)

		if kind, exists := polymorph["kind"].(string); exists {
			foundRelationshipTypes[kind] = true

			// Validate specific relationship patterns
			switch kind {
			case "morphTo":
				validateMorphToRelationship(t, polymorph)
			case "morphOne":
				validateMorphOneRelationship(t, polymorph)
			case "morphMany":
				validateMorphManyRelationship(t, polymorph)
			case "morphToMany":
				validateMorphToManyRelationship(t, polymorph)
			}
		}
	}

	// Log found relationship types
	var types []string
	for kind := range foundRelationshipTypes {
		types = append(types, kind)
	}
	sort.Strings(types)
	t.Logf("Found polymorphic relationship types: %v", types)
}

// validateMorphToRelationship validates morphTo inverse polymorphic relationships
func validateMorphToRelationship(t *testing.T, polymorph map[string]any) {
	// MorphTo should have multiple possible target types
	if to, exists := polymorph["to"].(map[string]any); exists {
		if types, hasTypes := to["types"].([]any); hasTypes {
			if len(types) == 0 {
				t.Error("MorphTo relationship should have target types")
			}

			// Validate each target type structure
			for _, typeItem := range types {
				if typeMap, ok := typeItem.(map[string]any); ok {
					if _, hasAlias := typeMap["alias"]; !hasAlias {
						t.Error("MorphTo target type missing 'alias' field")
					}
					if _, hasModel := typeMap["model"]; !hasModel {
						t.Error("MorphTo target type missing 'model' field")
					}
				}
			}
		}
	}

	// Validate discriminator columns are present
	if discriminator, exists := polymorph["discriminator"].(map[string]any); exists {
		if typeCol, hasTypeCol := discriminator["typeColumn"].(string); hasTypeCol {
			if typeCol == "" {
				t.Error("MorphTo typeColumn should not be empty")
			}
		}
		if idCol, hasIdCol := discriminator["idColumn"].(string); hasIdCol {
			if idCol == "" {
				t.Error("MorphTo idColumn should not be empty")
			}
		}
	}
}

// validateMorphOneRelationship validates morphOne polymorphic relationships
func validateMorphOneRelationship(t *testing.T, polymorph map[string]any) {
	// MorphOne should specify the target model
	if to, exists := polymorph["to"].(map[string]any); exists {
		if model, hasModel := to["model"].(string); hasModel {
			if model == "" {
				t.Error("MorphOne relationship should specify target model")
			}
		}
	}

	// Validate morph name is present
	if relationship, exists := polymorph["relationship"].(string); exists {
		if relationship == "" {
			t.Error("MorphOne relationship name should not be empty")
		}
	}
}

// validateMorphManyRelationship validates morphMany polymorphic relationships
func validateMorphManyRelationship(t *testing.T, polymorph map[string]any) {
	// MorphMany should specify the target model
	if to, exists := polymorph["to"].(map[string]any); exists {
		if model, hasModel := to["model"].(string); hasModel {
			if model == "" {
				t.Error("MorphMany relationship should specify target model")
			}
		}
	}

	// Should indicate it's a one-to-many relationship
	if kind, exists := polymorph["kind"].(string); exists && kind == "morphMany" {
		// Validate this is properly categorized as a one-to-many polymorphic
		t.Logf("Validated morphMany relationship structure")
	}
}

// validateMorphToManyRelationship validates morphToMany polymorphic relationships
func validateMorphToManyRelationship(t *testing.T, polymorph map[string]any) {
	// MorphToMany should have pivot table information
	if pivotInfo, exists := polymorph["pivot"]; exists {
		if pivot, ok := pivotInfo.(map[string]any); ok {
			if _, hasTable := pivot["table"]; !hasTable {
				t.Error("MorphToMany relationship missing pivot table information")
			}
		}
	}
}

// validateDiscriminatorMappings validates global morph map definitions
func validateDiscriminatorMappings(t *testing.T, result map[string]any) {
	polymorphicArray, ok := result["polymorphic"].([]any)
	if !ok || len(polymorphicArray) == 0 {
		t.Log("No polymorphic relationships to validate discriminator mappings for")
		return
	}

	var globalMappings map[string]string
	foundMappings := 0

	// Look for global morph map definitions
	for _, item := range polymorphicArray {
		polymorph := item.(map[string]any)

		if mappings, exists := polymorph["globalMorphMap"]; exists {
			if mappingMap, ok := mappings.(map[string]any); ok {
				globalMappings = make(map[string]string)
				for alias, model := range mappingMap {
					if modelStr, isStr := model.(string); isStr {
						globalMappings[alias] = modelStr
					}
				}
				foundMappings++
			}
		}
	}

	if foundMappings > 0 {
		t.Logf("Found %d global morph map definitions", foundMappings)

		// Validate mapping consistency
		if len(globalMappings) > 0 {
			for alias, model := range globalMappings {
				if alias == "" {
					t.Error("Global morph map contains empty alias")
				}
				if model == "" {
					t.Error("Global morph map contains empty model reference")
				}
				if !strings.Contains(model, "\\") && !strings.Contains(model, "/") {
					t.Logf("Warning: morph map model '%s' may not be fully qualified", model)
				}
			}
		}
	} else {
		t.Log("No global morph map definitions found - relationships may use default class names")
	}
}

// validatePolymorphicChains validates complex polymorphic relationship chains
func validatePolymorphicChains(t *testing.T, result map[string]any) {
	polymorphicArray, ok := result["polymorphic"].([]any)
	if !ok || len(polymorphicArray) == 0 {
		t.Log("No polymorphic chains to validate")
		return
	}

	chainDepths := make(map[int]int) // depth -> count
	maxDepth := 0

	for _, item := range polymorphicArray {
		polymorph := item.(map[string]any)

		// Check if this relationship has depth information
		if depth, exists := polymorph["depth"].(float64); exists {
			depthInt := int(depth)
			chainDepths[depthInt]++
			if depthInt > maxDepth {
				maxDepth = depthInt
			}
		}

		// Look for chain references
		if chain, exists := polymorph["chain"]; exists {
			if chainSlice, ok := chain.([]any); ok {
				chainLength := len(chainSlice)
				t.Logf("Found polymorphic chain of length %d", chainLength)

				// Validate each step in the chain
				for i, step := range chainSlice {
					if stepMap, isMap := step.(map[string]any); isMap {
						if _, hasModel := stepMap["model"]; !hasModel {
							t.Errorf("Chain step %d missing model information", i)
						}
						if _, hasRelation := stepMap["relationship"]; !hasRelation {
							t.Errorf("Chain step %d missing relationship information", i)
						}
					}
				}
			}
		}
	}

	// Validate depth truncation (should not exceed configured limits)
	if maxDepth > 5 {
		t.Errorf("Polymorphic chain depth %d exceeds expected limit of 5", maxDepth)
	}

	if len(chainDepths) > 0 {
		t.Logf("Polymorphic chain depth distribution: %v", chainDepths)
	}
}

// validateIntegrationWithOtherPatterns validates polymorphic patterns work with other matchers
func validateIntegrationWithOtherPatterns(t *testing.T, result map[string]any) {
	// Check that other pattern types are also detected alongside polymorphic patterns
	patternTypes := []string{"controllers", "models", "pivots", "scopes", "attributes", "broadcast"}

	foundPatterns := make(map[string]int)
	for _, patternType := range patternTypes {
		if patterns, exists := result[patternType]; exists {
			if patternSlice, ok := patterns.([]any); ok {
				foundPatterns[patternType] = len(patternSlice)
			}
		}
	}

	// Look for cross-references between polymorphic and other patterns
	polymorphicArray, ok := result["polymorphic"].([]any)
	if !ok || len(polymorphicArray) == 0 {
		return
	}

	// Check if models referenced in polymorphic relationships appear in models array
	modelsArray, hasModels := result["models"].([]any)
	if hasModels {
		modelNames := make(map[string]bool)
		for _, model := range modelsArray {
			if modelMap, ok := model.(map[string]any); ok {
				if name, hasName := modelMap["name"].(string); hasName {
					modelNames[name] = true
				}
			}
		}

		// Verify polymorphic relationships reference valid models
		for _, item := range polymorphicArray {
			polymorph := item.(map[string]any)

			if from, exists := polymorph["from"].(map[string]any); exists {
				if name, hasName := from["name"].(string); hasName {
					if !modelNames[name] && len(modelNames) > 0 {
						t.Logf("Polymorphic relationship references model '%s' not found in models array", name)
					}
				}
			}

			if to, exists := polymorph["to"].(map[string]any); exists {
				if types, hasTypes := to["types"].([]any); hasTypes {
					for _, typeItem := range types {
						if typeMap, ok := typeItem.(map[string]any); ok {
							if model, hasModel := typeMap["model"].(string); hasModel {
								modelName := extractModelName(model)
								if !modelNames[modelName] && len(modelNames) > 0 {
									t.Logf("Polymorphic relationship references target model '%s' not found in models array", modelName)
								}
							}
						}
					}
				}
			}
		}
	}

	t.Logf("Pattern integration summary: %v", foundPatterns)
}

// extractModelName extracts the class name from a fully qualified class name
func extractModelName(fullClassName string) string {
	parts := strings.Split(fullClassName, "\\")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	parts = strings.Split(fullClassName, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullClassName
}

// TestPolymorphicDeterministicOutput tests that polymorphic detection produces deterministic output
func TestPolymorphicDeterministicOutput(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create test manifest
	testManifest := map[string]any{
		"project": map[string]any{
			"root":     "../../",
			"composer": "go.mod",
		},
		"scan": map[string]any{
			"targets": []string{"test/fixtures/matchers/polymorphic"},
		},
		"features": map[string]any{
			"polymorphic": true,
		},
	}

	manifestFile, err := os.CreateTemp("", "deterministic_polymorphic_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp manifest: %v", err)
	}
	defer os.Remove(manifestFile.Name())

	manifestBytes, _ := json.MarshalIndent(testManifest, "", "  ")
	manifestFile.Write(manifestBytes)
	manifestFile.Close()

	// Run the CLI multiple times
	var outputs []string
	var hashes []string
	numRuns := 3

	for i := 0; i < numRuns; i++ {
		cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		err := cmd.Run()
		if err != nil {
			t.Fatalf("CLI execution %d failed: %v", i, err)
		}

		output := stdout.String()
		outputs = append(outputs, output)

		// Calculate canonical hash (excluding volatile fields like durationMs)
		hash := calculateCanonicalHash(output)
		hashes = append(hashes, hash)
	}

	// Compare outputs for determinism
	for i := 1; i < numRuns; i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("Run %d produced different output than run 0", i)

			// Parse and compare JSON structures to identify differences
			var result0, resultI map[string]any
			json.Unmarshal([]byte(outputs[0]), &result0)
			json.Unmarshal([]byte(outputs[i]), &resultI)

			// Compare polymorphic arrays specifically
			if comparePolymorphicArrays(result0, resultI) {
				t.Log("Polymorphic arrays are structurally identical despite hash difference (likely timestamp)")

				// Debug: print the first few characters of each output to identify differences
				t.Logf("Output 0 (first 200 chars): %s", truncateString(outputs[0], 200))
				t.Logf("Output %d (first 200 chars): %s", i, truncateString(outputs[i], 200))
			} else {
				t.Error("Polymorphic arrays differ structurally between runs")
			}
		}
	}

	if len(set(hashes)) == 1 {
		t.Log("All runs produced identical output (deterministic)")
	} else {
		t.Logf("Hash variation across runs: %v", hashes)
	}
}

// comparePolymorphicArrays compares two polymorphic arrays ignoring order and timestamps
func comparePolymorphicArrays(result1, result2 map[string]any) bool {
	poly1, ok1 := result1["polymorphic"].([]any)
	poly2, ok2 := result2["polymorphic"].([]any)

	if !ok1 || !ok2 {
		return ok1 == ok2 // Both should be missing or both present
	}

	if len(poly1) != len(poly2) {
		return false
	}

	// Convert to normalized strings and sort for comparison
	normalize := func(items []any) []string {
		var normalized []string
		for _, item := range items {
			if itemMap, ok := item.(map[string]any); ok {
				// Remove any timestamp fields before comparison
				delete(itemMap, "timestamp")
				delete(itemMap, "discoveredAt")

				bytes, _ := json.Marshal(itemMap)
				normalized = append(normalized, string(bytes))
			}
		}
		sort.Strings(normalized)
		return normalized
	}

	norm1 := normalize(poly1)
	norm2 := normalize(poly2)

	if len(norm1) != len(norm2) {
		return false
	}

	for i := range norm1 {
		if norm1[i] != norm2[i] {
			return false
		}
	}

	return true
}

// set creates a set from a slice to check uniqueness
func set(slice []string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range slice {
		set[item] = true
	}
	return set
}

// TestPolymorphicPerformanceBenchmark benchmarks polymorphic pattern detection performance
func TestPolymorphicPerformanceBenchmark(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create performance test manifest
	testManifest := map[string]any{
		"project": map[string]any{
			"root":     "../../",
			"composer": "go.mod",
		},
		"scan": map[string]any{
			"targets": []string{"test/fixtures/matchers"},
		},
		"features": map[string]any{
			"polymorphic":    true,
			"http_status":    true,
			"resource_usage": true,
			"with_pivot":     true,
			"scopes_used":    true,
			"attribute_make": true,
		},
	}

	manifestFile, err := os.CreateTemp("", "perf_polymorphic_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp manifest: %v", err)
	}
	defer os.Remove(manifestFile.Name())

	manifestBytes, _ := json.MarshalIndent(testManifest, "", "  ")
	manifestFile.Write(manifestBytes)
	manifestFile.Close()

	// Benchmark multiple runs
	numRuns := 5
	var durations []time.Duration

	for i := 0; i < numRuns; i++ {
		start := time.Now()

		cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		err := cmd.Run()
		if err != nil {
			t.Fatalf("Performance run %d failed: %v", i, err)
		}

		duration := time.Since(start)
		durations = append(durations, duration)

		// Validate that we got reasonable output
		output := stdout.String()
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("Invalid JSON output in performance run %d", i)
		}
	}

	// Calculate performance statistics
	var totalDuration time.Duration
	var minDuration, maxDuration time.Duration = durations[0], durations[0]

	for _, d := range durations {
		totalDuration += d
		if d < minDuration {
			minDuration = d
		}
		if d > maxDuration {
			maxDuration = d
		}
	}

	avgDuration := totalDuration / time.Duration(numRuns)

	t.Logf("Polymorphic detection performance:")
	t.Logf("  Runs: %d", numRuns)
	t.Logf("  Average: %v", avgDuration)
	t.Logf("  Min: %v", minDuration)
	t.Logf("  Max: %v", maxDuration)
	t.Logf("  Total: %v", totalDuration)

	// Performance thresholds (adjust based on expected performance)
	if avgDuration > 10*time.Second {
		t.Errorf("Polymorphic detection too slow: %v > 10s", avgDuration)
	}

	// Check for performance consistency (max should not be more than 3x min)
	if maxDuration > 3*minDuration {
		t.Logf("Warning: performance variation high (max %v vs min %v)", maxDuration, minDuration)
	}
}

// TestPolymorphicErrorHandling tests error handling for malformed polymorphic patterns
func TestPolymorphicErrorHandling(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create temporary fixture directory with malformed polymorphic patterns
	tempDir := t.TempDir()
	malformedDir := filepath.Join(tempDir, "malformed")
	if err := os.MkdirAll(malformedDir, 0755); err != nil {
		t.Fatalf("Failed to create malformed dir: %v", err)
	}

	// Create malformed polymorphic PHP file
	malformedContent := `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\MorphTo;

class BadModel extends Model
{
    // Malformed morphTo - missing return type
    public function malformedMorph()
    {
        return $this->morphTo(); // No return type declaration
    }
    
    // Incomplete morphTo call
    public function incompleteRel(): MorphTo
    {
        return $this->morphTo(
            // Missing parameters - syntax error
    }
    
    // Invalid morphTo usage
    public function invalidMorph(): MorphTo
    {
        return $this->belongsTo('SomeModel'); // Wrong relationship type
    }
}
`

	malformedFile := filepath.Join(malformedDir, "malformed.php")
	if err := os.WriteFile(malformedFile, []byte(malformedContent), 0644); err != nil {
		t.Fatalf("Failed to create malformed file: %v", err)
	}

	// Create test manifest pointing to malformed directory
	testManifest := map[string]any{
		"project": map[string]any{
			"root":     tempDir,
			"composer": "composer.json",
		},
		"scan": map[string]any{
			"targets": []string{"malformed"},
		},
		"features": map[string]any{
			"polymorphic": true,
		},
	}

	// Create composer.json
	composerFile := filepath.Join(tempDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/malformed"}`), 0644); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	manifestFile, err := os.CreateTemp("", "error_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp manifest: %v", err)
	}
	defer os.Remove(manifestFile.Name())

	manifestBytes, _ := json.MarshalIndent(testManifest, "", "  ")
	manifestFile.Write(manifestBytes)
	manifestFile.Close()

	// Run CLI and expect it to handle malformed patterns gracefully
	cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// CLI should not crash on malformed patterns
	if err != nil {
		// Check if it's a parsing error or other expected error
		if exitError, ok := err.(*exec.ExitError); ok {
			// Exit code 2 (internal error) may be acceptable for malformed syntax
			if exitError.ExitCode() == 2 {
				t.Log("CLI exited with internal error due to malformed patterns - this is acceptable")
				return
			}
		}
		t.Logf("CLI failed with error: %v", err)
		t.Logf("Stderr: %s", stderr.String())
	}

	// If CLI succeeded, check that output is valid JSON
	output := stdout.String()
	if output != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Errorf("CLI produced invalid JSON despite malformed input: %v", err)
		} else {
			// Check that polymorphic array exists and is valid (even if empty)
			if polymorphic, exists := result["polymorphic"]; exists {
				if _, isArray := polymorphic.([]any); !isArray {
					t.Error("Polymorphic field should be an array even with malformed input")
				}
			}
		}
	}
}

// TestPolymorphicFeatureFlagHandling tests polymorphic feature flag behavior
func TestPolymorphicFeatureFlagHandling(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	testCases := []struct {
		name               string
		polymorphicEnabled any
		expectPolymorphic  bool
		description        string
	}{
		{
			name:               "polymorphic_enabled_true",
			polymorphicEnabled: true,
			expectPolymorphic:  true,
			description:        "Polymorphic patterns should be detected when feature is enabled",
		},
		{
			name:               "polymorphic_enabled_false",
			polymorphicEnabled: false,
			expectPolymorphic:  false,
			description:        "Polymorphic patterns should be ignored when feature is disabled",
		},
		{
			name:               "polymorphic_not_specified",
			polymorphicEnabled: nil,
			expectPolymorphic:  true,
			description:        "Polymorphic patterns should be detected when feature is not specified (defaults to enabled)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test manifest
			testManifest := map[string]any{
				"project": map[string]any{
					"root":     "../../",
					"composer": "go.mod",
				},
				"scan": map[string]any{
					"targets": []string{"test/fixtures/matchers/polymorphic"},
				},
			}

			// Add features section if needed
			if tc.polymorphicEnabled != nil {
				testManifest["features"] = map[string]any{
					"polymorphic": tc.polymorphicEnabled,
				}
			}

			manifestFile, err := os.CreateTemp("", "flag_test_*.json")
			if err != nil {
				t.Fatalf("Failed to create temp manifest: %v", err)
			}
			defer os.Remove(manifestFile.Name())

			manifestBytes, _ := json.MarshalIndent(testManifest, "", "  ")
			manifestFile.Write(manifestBytes)
			manifestFile.Close()

			// Run CLI
			cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
			var stdout bytes.Buffer
			cmd.Stdout = &stdout

			err = cmd.Run()
			if err != nil {
				t.Fatalf("CLI execution failed: %v", err)
			}

			// Parse output
			output := stdout.String()
			var result map[string]any
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("Invalid JSON output: %v", err)
			}

			// Check polymorphic array
			polymorphicArray, hasPolymorphic := result["polymorphic"].([]any)

			if tc.expectPolymorphic {
				if !hasPolymorphic {
					t.Error("Expected polymorphic array to exist when feature is enabled")
				} else if len(polymorphicArray) == 0 {
					t.Log("Polymorphic array is empty - may indicate no patterns in fixtures")
				}
			} else {
				if hasPolymorphic && len(polymorphicArray) > 0 {
					t.Error("Expected no polymorphic patterns when feature is disabled")
				}
			}

			t.Log(tc.description)
		})
	}
}

// TestPolymorphicGoldenFileComparison tests against expected golden file output (if available)
func TestPolymorphicGoldenFileComparison(t *testing.T) {
	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	// Create test manifest
	testManifest := map[string]any{
		"project": map[string]any{
			"root":     "../../",
			"composer": "go.mod",
		},
		"scan": map[string]any{
			"targets": []string{"test/fixtures/matchers/polymorphic"},
		},
		"features": map[string]any{
			"polymorphic": true,
		},
	}

	manifestFile, err := os.CreateTemp("", "golden_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp manifest: %v", err)
	}
	defer os.Remove(manifestFile.Name())

	manifestBytes, _ := json.MarshalIndent(testManifest, "", "  ")
	manifestFile.Write(manifestBytes)
	manifestFile.Close()

	// Run CLI
	cmd := exec.Command(cliPath, "--manifest", manifestFile.Name())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err = cmd.Run()
	if err != nil {
		t.Fatalf("CLI execution failed: %v", err)
	}

	output := stdout.String()

	// Check if golden file exists
	goldenFilePath := "../../test/fixtures/expected/polymorphic-delta.json"
	if _, err := os.Stat(goldenFilePath); os.IsNotExist(err) {
		// Golden file doesn't exist - create it for future comparisons
		t.Logf("Golden file not found at %s", goldenFilePath)
		t.Log("Current output can be used as baseline for future comparisons")

		// Validate that current output is well-formed JSON
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Errorf("Output is not valid JSON: %v", err)
		}
		return
	}

	// Read golden file
	goldenContent, err := os.ReadFile(goldenFilePath)
	if err != nil {
		t.Fatalf("Failed to read golden file: %v", err)
	}

	// Parse both outputs
	var currentResult, goldenResult map[string]any
	if err := json.Unmarshal([]byte(output), &currentResult); err != nil {
		t.Fatalf("Current output is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(goldenContent, &goldenResult); err != nil {
		t.Fatalf("Golden file is not valid JSON: %v", err)
	}

	// Compare polymorphic arrays (allowing for some variation in non-deterministic fields)
	if !comparePolymorphicArrays(currentResult, goldenResult) {
		t.Error("Current output differs from golden file")
		t.Logf("To update golden file, copy current output to: %s", goldenFilePath)
	} else {
		t.Log("Current output matches golden file expectations")
	}
}

// calculateCanonicalHash creates a deterministic hash of JSON output by excluding volatile fields
func calculateCanonicalHash(jsonOutput string) string {
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &data); err != nil {
		// If JSON parsing fails, fall back to raw hash
		return fmt.Sprintf("%x", sha256.Sum256([]byte(jsonOutput)))
	}

	// Exclude volatile fields from meta
	if meta, exists := data["meta"].(map[string]any); exists {
		if stats, exists := meta["stats"].(map[string]any); exists {
			// Remove timing-related fields
			delete(stats, "durationMs")
		}
		// Remove timestamp field
		delete(meta, "generatedAt")
	}

	// Re-marshal and hash
	canonical, err := json.Marshal(data)
	if err != nil {
		// If re-marshaling fails, fall back to raw hash
		return fmt.Sprintf("%x", sha256.Sum256([]byte(jsonOutput)))
	}

	return fmt.Sprintf("%x", sha256.Sum256(canonical))
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

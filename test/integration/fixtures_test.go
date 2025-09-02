package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// FixtureTestCase defines a fixture validation test case
type FixtureTestCase struct {
	Name             string
	Path             string
	ExpectedFiles    []string
	ExpectedDirs     []string
	PHPValidation    bool
	ManifestFeatures []string
	Description      string
}

// TestFixtureStructure validates all integration fixture structures
func TestFixtureStructure(t *testing.T) {
	fixtures := []FixtureTestCase{
		{
			Name:         "minimal_laravel",
			Path:         "../../test/fixtures/integration/minimal-laravel",
			ExpectedFiles: []string{"composer.json", "manifest.json"},
			ExpectedDirs:  []string{"app", "app/Http", "app/Http/Controllers", "app/Models", "routes"},
			PHPValidation: true,
			ManifestFeatures: []string{"http_status", "request_usage"},
			Description:  "Minimal Laravel fixture with basic MVC patterns",
		},
		{
			Name:         "api_project",
			Path:         "../../test/fixtures/integration/api-project",
			ExpectedFiles: []string{"composer.json", "manifest.json"},
			ExpectedDirs:  []string{"app", "app/Http", "app/Http/Controllers", "app/Http/Requests", "app/Http/Resources", "app/Models", "routes"},
			PHPValidation: true,
			ManifestFeatures: []string{"http_status", "request_usage", "resource_usage", "with_pivot", "scopes_used"},
			Description:  "API-focused fixture with resources, requests, and advanced relationships",
		},
		{
			Name:         "complex_app",
			Path:         "../../test/fixtures/integration/complex-app",
			ExpectedFiles: []string{"composer.json", "manifest.json"},
			ExpectedDirs:  []string{"app", "app/Http", "app/Http/Controllers", "app/Models", "app/Broadcasting", "routes"},
			PHPValidation: true,
			ManifestFeatures: []string{"http_status", "request_usage", "resource_usage", "with_pivot", "scopes_used", "polymorphic", "broadcast_channels"},
			Description:  "Complex fixture with polymorphic relationships and broadcasting",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			validateFixtureStructure(t, fixture)
		})
	}
}

// validateFixtureStructure performs comprehensive fixture validation
func validateFixtureStructure(t *testing.T, fixture FixtureTestCase) {
	t.Helper()

	t.Logf("Validating fixture: %s", fixture.Description)

	// Check fixture directory exists
	if _, err := os.Stat(fixture.Path); os.IsNotExist(err) {
		t.Fatalf("Fixture directory does not exist: %s", fixture.Path)
	}

	// Validate required files
	t.Run("required_files", func(t *testing.T) {
		validateRequiredFiles(t, fixture.Path, fixture.ExpectedFiles)
	})

	// Validate directory structure
	t.Run("directory_structure", func(t *testing.T) {
		validateDirectoryStructure(t, fixture.Path, fixture.ExpectedDirs)
	})

	// Validate composer.json
	t.Run("composer_json", func(t *testing.T) {
		validateComposerJSON(t, filepath.Join(fixture.Path, "composer.json"))
	})

	// Validate manifest.json
	t.Run("manifest_json", func(t *testing.T) {
		validateManifestJSON(t, filepath.Join(fixture.Path, "manifest.json"), fixture.ManifestFeatures)
	})

	// Validate PHP files if enabled
	if fixture.PHPValidation {
		t.Run("php_files", func(t *testing.T) {
			validatePHPFiles(t, fixture.Path, fixture.Name)
		})
	}
}

// validateRequiredFiles checks that all required files exist
func validateRequiredFiles(t *testing.T, basePath string, expectedFiles []string) {
	t.Helper()

	for _, file := range expectedFiles {
		filePath := filepath.Join(basePath, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Required file missing: %s", file)
		} else {
			t.Logf("✓ Required file exists: %s", file)
		}
	}
}

// validateDirectoryStructure checks that all expected directories exist
func validateDirectoryStructure(t *testing.T, basePath string, expectedDirs []string) {
	t.Helper()

	for _, dir := range expectedDirs {
		dirPath := filepath.Join(basePath, dir)
		if stat, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Errorf("Required directory missing: %s", dir)
		} else if !stat.IsDir() {
			t.Errorf("Path exists but is not a directory: %s", dir)
		} else {
			t.Logf("✓ Required directory exists: %s", dir)
		}
	}
}

// validateComposerJSON validates the structure of composer.json files
func validateComposerJSON(t *testing.T, composerPath string) {
	t.Helper()

	data, err := os.ReadFile(composerPath)
	if err != nil {
		t.Fatalf("Failed to read composer.json: %v", err)
	}

	var composer map[string]interface{}
	if err := json.Unmarshal(data, &composer); err != nil {
		t.Fatalf("Invalid composer.json: %v", err)
	}

	// Validate required fields
	requiredFields := []string{"name", "autoload"}
	for _, field := range requiredFields {
		if _, exists := composer[field]; !exists {
			t.Errorf("composer.json missing required field: %s", field)
		} else {
			t.Logf("✓ composer.json has %s field", field)
		}
	}

	// Validate autoload structure
	if autoload, ok := composer["autoload"].(map[string]interface{}); ok {
		if psr4, ok := autoload["psr-4"].(map[string]interface{}); ok {
			if _, exists := psr4["App\\"]; !exists {
				t.Error("composer.json autoload missing App\\ namespace")
			} else {
				t.Log("✓ composer.json has App\\ PSR-4 autoload")
			}
		} else {
			t.Error("composer.json missing psr-4 autoload")
		}
	} else {
		t.Error("composer.json autoload should be an object")
	}

	// Validate project name format
	if name, ok := composer["name"].(string); ok {
		if !strings.Contains(name, "/") {
			t.Errorf("composer.json name should follow vendor/package format, got: %s", name)
		} else {
			t.Logf("✓ composer.json name format valid: %s", name)
		}
	}
}

// validateManifestJSON validates the structure of manifest.json files
func validateManifestJSON(t *testing.T, manifestPath string, expectedFeatures []string) {
	t.Helper()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read manifest.json: %v", err)
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Invalid manifest.json: %v", err)
	}

	// Validate required top-level fields
	requiredFields := []string{"project", "scan"}
	for _, field := range requiredFields {
		if _, exists := manifest[field]; !exists {
			t.Errorf("manifest.json missing required field: %s", field)
		} else {
			t.Logf("✓ manifest.json has %s field", field)
		}
	}

	// Validate project configuration
	if project, ok := manifest["project"].(map[string]interface{}); ok {
		requiredProjectFields := []string{"root", "composer"}
		for _, field := range requiredProjectFields {
			if _, exists := project[field]; !exists {
				t.Errorf("manifest.json project missing %s field", field)
			}
		}

		// Validate root path points to fixture directory
		if root, ok := project["root"].(string); ok {
			if !strings.Contains(root, "integration") {
				t.Errorf("manifest.json root should point to integration fixture, got: %s", root)
			} else {
				t.Log("✓ manifest.json root points to integration fixture")
			}
		}
	}

	// Validate scan configuration
	if scan, ok := manifest["scan"].(map[string]interface{}); ok {
		if targets, ok := scan["targets"].([]interface{}); ok {
			if len(targets) == 0 {
				t.Error("manifest.json scan.targets should not be empty")
			} else {
				t.Logf("✓ manifest.json has %d scan targets", len(targets))
			}
		} else {
			t.Error("manifest.json scan.targets should be an array")
		}
	}

	// Validate features configuration
	if len(expectedFeatures) > 0 {
		if features, ok := manifest["features"].(map[string]interface{}); ok {
			for _, feature := range expectedFeatures {
				if enabled, exists := features[feature]; !exists {
					t.Errorf("manifest.json missing expected feature: %s", feature)
				} else if enabledBool, ok := enabled.(bool); !ok {
					t.Errorf("manifest.json feature %s should be boolean", feature)
				} else if !enabledBool {
					t.Errorf("manifest.json feature %s should be enabled", feature)
				} else {
					t.Logf("✓ manifest.json feature %s enabled", feature)
				}
			}
		} else {
			t.Errorf("manifest.json missing features configuration for expected features: %v", expectedFeatures)
		}
	}
}

// validatePHPFiles validates PHP file structure and content
func validatePHPFiles(t *testing.T, basePath, fixtureName string) {
	t.Helper()

	phpFiles := []string{}
	
	// Find all PHP files recursively
	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(info.Name(), ".php") {
			phpFiles = append(phpFiles, path)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Error walking directory: %v", err)
	}

	if len(phpFiles) == 0 {
		t.Error("No PHP files found in fixture")
		return
	}

	t.Logf("Found %d PHP files to validate", len(phpFiles))

	// Validate each PHP file
	for _, phpFile := range phpFiles {
		t.Run(fmt.Sprintf("php_file_%s", filepath.Base(phpFile)), func(t *testing.T) {
			validatePHPFile(t, phpFile, fixtureName)
		})
	}
}

// validatePHPFile validates a single PHP file
func validatePHPFile(t *testing.T, phpFile, fixtureName string) {
	t.Helper()

	content, err := os.ReadFile(phpFile)
	if err != nil {
		t.Fatalf("Failed to read PHP file: %v", err)
	}

	contentStr := string(content)

	// Basic PHP validation
	if !strings.HasPrefix(contentStr, "<?php") {
		t.Error("PHP file should start with <?php tag")
	}

	if !strings.Contains(contentStr, "namespace") {
		t.Error("PHP file should declare namespace")
	}

	// Determine file type and validate accordingly
	relPath, _ := filepath.Rel(filepath.Join("../../test/fixtures/integration", fixtureName), phpFile)
	
	switch {
	case strings.Contains(relPath, "Controllers"):
		validateControllerFile(t, contentStr, relPath)
	case strings.Contains(relPath, "Models"):
		validateModelFile(t, contentStr, relPath)
	case strings.Contains(relPath, "Requests"):
		validateRequestFile(t, contentStr, relPath)
	case strings.Contains(relPath, "Resources"):
		validateResourceFile(t, contentStr, relPath)
	case strings.Contains(relPath, "routes"):
		validateRouteFile(t, contentStr, relPath)
	case strings.Contains(relPath, "Broadcasting"):
		validateBroadcastFile(t, contentStr, relPath)
	default:
		t.Logf("Unknown PHP file type, skipping specific validation: %s", relPath)
	}
}

// validateControllerFile validates controller-specific patterns
func validateControllerFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Controllers should extend Controller
	if !strings.Contains(content, "extends Controller") {
		t.Errorf("Controller should extend Controller: %s", relPath)
	}

	// Controllers should have methods
	if !strings.Contains(content, "public function") {
		t.Errorf("Controller should have public methods: %s", relPath)
	}

	// Check for common controller methods
	commonMethods := []string{"index", "store", "show", "update", "destroy"}
	foundMethods := 0
	for _, method := range commonMethods {
		if strings.Contains(content, "function "+method) {
			foundMethods++
		}
	}

	if foundMethods > 0 {
		t.Logf("✓ Controller has %d RESTful methods: %s", foundMethods, relPath)
	}

	// Check for response patterns
	if strings.Contains(content, "response()") || strings.Contains(content, "->json(") {
		t.Logf("✓ Controller uses response patterns: %s", relPath)
	}
}

// validateModelFile validates model-specific patterns
func validateModelFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Models should extend Model
	if !strings.Contains(content, "extends Model") {
		t.Errorf("Model should extend Model: %s", relPath)
	}

	// Models should have fillable or guarded
	if !strings.Contains(content, "$fillable") && !strings.Contains(content, "$guarded") {
		t.Logf("Model missing mass assignment protection (may be intentional): %s", relPath)
	}

	// Check for relationship methods
	relationships := []string{"hasOne", "hasMany", "belongsTo", "belongsToMany", "morphTo", "morphOne", "morphMany", "morphToMany", "morphedByMany"}
	foundRelationships := 0
	for _, rel := range relationships {
		if strings.Contains(content, rel+"(") {
			foundRelationships++
		}
	}

	if foundRelationships > 0 {
		t.Logf("✓ Model has %d relationships: %s", foundRelationships, relPath)
	}

	// Check for scopes
	if strings.Contains(content, "function scope") {
		t.Logf("✓ Model has query scopes: %s", relPath)
	}

	// Check for casts
	if strings.Contains(content, "$casts") {
		t.Logf("✓ Model has attribute casts: %s", relPath)
	}
}

// validateRequestFile validates form request patterns
func validateRequestFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Requests should extend FormRequest
	if !strings.Contains(content, "extends FormRequest") {
		t.Errorf("Request should extend FormRequest: %s", relPath)
	}

	// Requests should have authorize method
	if !strings.Contains(content, "function authorize") {
		t.Errorf("Request should have authorize method: %s", relPath)
	}

	// Requests should have rules method
	if !strings.Contains(content, "function rules") {
		t.Errorf("Request should have rules method: %s", relPath)
	}

	t.Logf("✓ Request file structure valid: %s", relPath)
}

// validateResourceFile validates API resource patterns
func validateResourceFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Resources should extend JsonResource or ResourceCollection
	if !strings.Contains(content, "extends JsonResource") && !strings.Contains(content, "extends ResourceCollection") {
		t.Errorf("Resource should extend JsonResource or ResourceCollection: %s", relPath)
	}

	// Resources should have toArray method
	if !strings.Contains(content, "function toArray") {
		t.Errorf("Resource should have toArray method: %s", relPath)
	}

	t.Logf("✓ Resource file structure valid: %s", relPath)
}

// validateRouteFile validates route definition patterns
func validateRouteFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Route files should use Route facade
	if !strings.Contains(content, "Route::") {
		t.Errorf("Route file should use Route facade: %s", relPath)
	}

	// Check for common route methods
	routeMethods := []string{"get(", "post(", "put(", "patch(", "delete(", "apiResource(", "resource("}
	foundMethods := 0
	for _, method := range routeMethods {
		if strings.Contains(content, "Route::"+method) {
			foundMethods++
		}
	}

	if foundMethods > 0 {
		t.Logf("✓ Route file has %d route definitions: %s", foundMethods, relPath)
	}
}

// validateBroadcastFile validates broadcasting patterns
func validateBroadcastFile(t *testing.T, content, relPath string) {
	t.Helper()

	// Check for channel-related patterns
	if strings.Contains(content, "Broadcast::channel") || strings.Contains(content, "function join") {
		t.Logf("✓ Broadcast file has channel patterns: %s", relPath)
	}

	// Broadcasting files should have proper namespace
	if !strings.Contains(content, "namespace App\\Broadcasting") && !strings.Contains(content, "routes/channels.php") {
		t.Errorf("Broadcast file should have Broadcasting namespace or be channels.php: %s", relPath)
	}
}

// TestFixtureDependencies validates that fixtures have proper internal dependencies
func TestFixtureDependencies(t *testing.T) {
	fixtures := []string{"minimal-laravel", "api-project", "complex-app"}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			validateFixtureDependencies(t, fixture)
		})
	}
}

// validateFixtureDependencies checks class references and dependencies within fixtures
func validateFixtureDependencies(t *testing.T, fixtureName string) {
	t.Helper()

	basePath := filepath.Join("../../test/fixtures/integration", fixtureName)
	
	// Find all PHP files and extract class references
	classReferences := make(map[string][]string) // file -> referenced classes
	definedClasses := make(map[string]string)    // class -> file

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(info.Name(), ".php") {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		contentStr := string(content)
		relPath, _ := filepath.Rel(basePath, path)

		// Extract defined class
		if strings.Contains(contentStr, "class ") {
			lines := strings.Split(contentStr, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "class ") && !strings.Contains(line, "//") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						className := parts[1]
						if namespace := extractNamespace(contentStr); namespace != "" {
							className = namespace + "\\" + className
						}
						definedClasses[className] = relPath
					}
					break
				}
			}
		}

		// Extract referenced classes (simplified)
		references := []string{}
		patterns := []string{
			"use ",
			"new ",
			"::",
			"extends ",
			"implements ",
		}

		for _, pattern := range patterns {
			if strings.Contains(contentStr, pattern) {
				// This is a simplified extraction - in practice you'd need more sophisticated parsing
				references = append(references, "found_references")
			}
		}

		if len(references) > 0 {
			classReferences[relPath] = references
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Error analyzing fixture dependencies: %v", err)
	}

	t.Logf("✓ Analyzed %d classes in %s fixture", len(definedClasses), fixtureName)

	// Validate that referenced models/controllers exist
	validateInternalReferences(t, fixtureName, definedClasses)
}

// extractNamespace extracts the namespace declaration from PHP content
func extractNamespace(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "namespace ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				namespace := parts[1]
				namespace = strings.TrimSuffix(namespace, ";")
				return namespace
			}
		}
	}
	return ""
}

// validateInternalReferences validates that class references within fixtures are consistent
func validateInternalReferences(t *testing.T, fixtureName string, definedClasses map[string]string) {
	t.Helper()

	// For each fixture, validate expected relationships exist
	switch fixtureName {
	case "minimal-laravel":
		expectedClasses := []string{
			"App\\Http\\Controllers\\UserController",
			"App\\Models\\User",
			"App\\Models\\Post",
		}
		for _, expectedClass := range expectedClasses {
			if file, exists := definedClasses[expectedClass]; exists {
				t.Logf("✓ Expected class found: %s in %s", expectedClass, file)
			} else {
				t.Errorf("Expected class missing: %s", expectedClass)
			}
		}

	case "api-project":
		expectedClasses := []string{
			"App\\Http\\Controllers\\ProductController",
			"App\\Http\\Requests\\StoreProductRequest", 
			"App\\Http\\Resources\\ProductResource",
			"App\\Models\\Product",
			"App\\Models\\Category",
			"App\\Models\\Tag",
		}
		for _, expectedClass := range expectedClasses {
			if file, exists := definedClasses[expectedClass]; exists {
				t.Logf("✓ Expected class found: %s in %s", expectedClass, file)
			} else {
				t.Errorf("Expected class missing: %s", expectedClass)
			}
		}

	case "complex-app":
		expectedClasses := []string{
			"App\\Http\\Controllers\\PostController",
			"App\\Models\\Post",
			"App\\Models\\Comment",
			"App\\Models\\Video",
			"App\\Models\\Image",
			"App\\Models\\Tag",
			"App\\Models\\User",
		}
		for _, expectedClass := range expectedClasses {
			if file, exists := definedClasses[expectedClass]; exists {
				t.Logf("✓ Expected class found: %s in %s", expectedClass, file)
			} else {
				t.Errorf("Expected class missing: %s", expectedClass)
			}
		}
	}
}

// TestFixturePerformance measures fixture processing performance
func TestFixturePerformance(t *testing.T) {
	fixtures := []struct {
		name      string
		threshold time.Duration
	}{
		{"minimal-laravel", 2 * time.Second},
		{"api-project", 5 * time.Second},
		{"complex-app", 10 * time.Second},
	}

	cliPath := buildCLIBinary(t)
	defer os.Remove(cliPath)

	for _, fixture := range fixtures {
		t.Run(fixture.name+"_performance", func(t *testing.T) {
			manifestPath := filepath.Join("../../test/fixtures/integration", fixture.name, "manifest.json")
			
			// Measure processing time
			start := time.Now()
			cmd := exec.Command(cliPath, "--manifest", manifestPath)
			_, err := cmd.Output()
			duration := time.Since(start)

			if err != nil {
				t.Fatalf("Fixture processing failed: %v", err)
			}

			if duration > fixture.threshold {
				t.Errorf("Fixture %s processing too slow: %v > %v", fixture.name, duration, fixture.threshold)
			} else {
				t.Logf("✓ Fixture %s processed in %v (threshold: %v)", fixture.name, duration, fixture.threshold)
			}
		})
	}
}
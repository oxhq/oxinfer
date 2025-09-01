package manifest_test

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// ExampleManifestLoader_LoadFromReader demonstrates how to use the manifest loader
func ExampleManifestLoader_LoadFromReader() {
	// Create a temporary directory for the example
	tmpDir, err := os.MkdirTemp("", "oxinfer-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create necessary files
	composerFile := tmpDir + "/composer.json"
	if err := os.WriteFile(composerFile, []byte(`{"name": "example/project"}`), 0644); err != nil {
		log.Fatal(err)
	}

	appDir := tmpDir + "/app"
	if err := os.Mkdir(appDir, 0755); err != nil {
		log.Fatal(err)
	}

	// Create a manifest JSON string
	manifestJSON := `{
		"project": {
			"root": "` + tmpDir + `",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app/"],
			"globs": ["**/*.php"]
		},
		"limits": {
			"max_files": 5000,
			"max_file_size": 1048576
		},
		"cache": {
			"enabled": true,
			"dir": ".oxinfer/cache",
			"ttl": 86400
		},
		"features": {
			"routes": true,
			"controllers": true,
			"models": false
		}
	}`

	// Create validator and loader
	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	// Load the manifest
	reader := strings.NewReader(manifestJSON)
	m, err := loader.LoadFromReader(reader)
	if err != nil {
		log.Fatal(err)
	}

	// Access the loaded manifest data
	fmt.Printf("Project root exists: %t\n", m.Project.Root != "")
	fmt.Printf("Scan targets: %v\n", m.Scan.Targets)
	fmt.Printf("Max files: %d\n", *m.Limits.MaxFiles)
	fmt.Printf("Cache enabled: %t\n", m.Cache.Enabled)
	fmt.Printf("Routes feature: %t\n", *m.Features.Routes)
	fmt.Printf("Models feature: %t\n", *m.Features.Models)

	// Output:
	// Project root exists: true
	// Scan targets: [app/]
	// Max files: 5000
	// Cache enabled: true
	// Routes feature: true
	// Models feature: false
}

// ExampleManifestLoader_LoadFromFile demonstrates loading from a file
func ExampleManifestLoader_LoadFromFile() {
	// Create a temporary directory and manifest file
	tmpDir, err := os.MkdirTemp("", "oxinfer-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create necessary files
	composerFile := tmpDir + "/composer.json"
	if err := os.WriteFile(composerFile, []byte(`{"name": "example/project"}`), 0644); err != nil {
		log.Fatal(err)
	}

	appDir := tmpDir + "/app"
	if err := os.Mkdir(appDir, 0755); err != nil {
		log.Fatal(err)
	}

	// Create manifest file
	manifestFile := tmpDir + "/oxinfer.manifest.json"
	manifestContent := `{
		"project": {
			"root": "` + tmpDir + `",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app/"]
		}
	}`

	if err := os.WriteFile(manifestFile, []byte(manifestContent), 0644); err != nil {
		log.Fatal(err)
	}

	// Create validator and loader
	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	// Load the manifest from file
	m, err := loader.LoadFromFile(manifestFile)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Loaded from: %s\n", manifestFile)
	fmt.Printf("Project root: %s\n", m.Project.Root)
	fmt.Printf("Scan targets: %v\n", m.Scan.Targets)

	// Output will vary based on temporary directory paths
}
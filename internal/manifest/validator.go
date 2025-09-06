package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/garaekz/oxinfer/internal/cli"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// validatorSchemaJSON contains the embedded manifest JSON schema
const validatorSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://oxcribe.dev/schema/manifest.schema.json",
  "title": "Oxinfer Manifest",
  "type": "object",
  "additionalProperties": false,
  "required": ["project", "scan"],
  "properties": {
    "project": {
      "type": "object",
      "additionalProperties": false,
      "required": ["root", "composer"],
      "properties": {
        "root": { "type": "string", "minLength": 1 },
        "composer": { "type": "string", "minLength": 1 }
      }
    },
    "scan": {
      "type": "object",
      "additionalProperties": false,
      "required": ["targets"],
      "properties": {
        "targets": {
          "type": "array",
          "minItems": 1,
          "items": { "type": "string", "minLength": 1 }
        },
        "vendor_whitelist": {
          "type": "array",
          "items": { "type": "string", "minLength": 1 },
          "default": []
        },
        "globs": {
          "type": "array",
          "items": { "type": "string", "minLength": 1 },
          "default": ["app/**/*.php", "routes/**/*.php"]
        }
      }
    },
    "limits": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "max_workers": { "type": "integer", "minimum": 1, "default": 8 },
        "max_files": { "type": "integer", "minimum": 1, "default": 500 },
        "max_depth": { "type": "integer", "minimum": 0, "default": 2 }
      },
      "default": {}
    },
    "cache": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "enabled": { "type": "boolean", "default": true },
        "kind": {
          "type": "string",
          "enum": ["sha256+mtime", "mtime"],
          "default": "sha256+mtime"
        }
      },
      "default": {}
    },
    "features": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "http_status": { "type": "boolean", "default": true },
        "request_usage": { "type": "boolean", "default": true },
        "resource_usage": { "type": "boolean", "default": true },
        "with_pivot": { "type": "boolean", "default": true },
        "attribute_make": { "type": "boolean", "default": true },
        "scopes_used": { "type": "boolean", "default": true },
        "polymorphic": { "type": "boolean", "default": true },
        "broadcast_channels": { "type": "boolean", "default": true }
      },
      "default": {}
    }
  }
}`

// ManifestValidator interface defines methods for validating manifest data
type ManifestValidator interface {
	ValidateSchema(data []byte) error
	ValidatePaths(m *Manifest) error
}

// manifestValidator is the concrete implementation of ManifestValidator
type manifestValidator struct{}

// NewValidator creates a new ManifestValidator instance
func NewValidator() ManifestValidator {
	return &manifestValidator{}
}

// ValidateSchema validates the raw JSON data against the manifest schema
func (v *manifestValidator) ValidateSchema(data []byte) error {
	// Load the manifest schema
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	// Load schema from embedded file
	if err := compiler.AddResource("manifest.schema.json", strings.NewReader(validatorSchemaJSON)); err != nil {
		return cli.WrapSchemaError("failed to load manifest schema", err)
	}

	schema, err := compiler.Compile("manifest.schema.json")
	if err != nil {
		return cli.WrapSchemaError("failed to compile manifest schema", err)
	}

	// Parse the JSON data for validation
	var jsonData any
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return cli.WrapInputError("invalid JSON structure", err)
	}

	// Validate against schema
	if err := schema.Validate(jsonData); err != nil {
		return cli.WrapSchemaError(fmt.Sprintf("manifest validation failed: %v", err), err)
	}

	return nil
}

// ValidatePaths performs business logic validation on a loaded manifest
// This includes path validation and normalization
func (v *manifestValidator) ValidatePaths(m *Manifest) error {
	if err := v.validateProject(m); err != nil {
		return err
	}

	if err := v.validateScan(m); err != nil {
		return err
	}

	if err := v.validateLimits(m); err != nil {
		return err
	}

	return nil
}

// validateProject validates the project configuration
func (v *manifestValidator) validateProject(m *Manifest) error {
	// Validate and normalize the root path
	if m.Project.Root == "" {
		return cli.NewInputError("project.root cannot be empty")
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(m.Project.Root)
	if err != nil {
		return cli.WrapInputError(fmt.Sprintf("failed to resolve absolute path for %q", m.Project.Root), err)
	}

	// Check if the path exists
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return cli.NewInputError(fmt.Sprintf("project root path does not exist: %q", absPath))
		}
		return cli.WrapInputError(fmt.Sprintf("failed to access project root path %q", absPath), err)
	}

	// Check if it's a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return cli.WrapInputError(fmt.Sprintf("failed to stat project root path %q", absPath), err)
	}
	if !info.IsDir() {
		return cli.NewInputError(fmt.Sprintf("project root must be a directory, got file: %q", absPath))
	}

	// Update the manifest with the normalized absolute path
	m.Project.Root = absPath

	// Validate composer.json path
	if err := v.validateComposerPath(m); err != nil {
		return err
	}

	return nil
}

// validateComposerPath validates that the composer.json file exists
func (v *manifestValidator) validateComposerPath(m *Manifest) error {
	var composerPath string

	// Handle relative and absolute paths for composer.json
	if filepath.IsAbs(m.Project.Composer) {
		composerPath = m.Project.Composer
	} else {
		composerPath = filepath.Join(m.Project.Root, m.Project.Composer)
	}

	// Check if the composer.json file exists
	if _, err := os.Stat(composerPath); err != nil {
		if os.IsNotExist(err) {
			return cli.NewInputError(fmt.Sprintf("composer.json file does not exist: %q", composerPath))
		}
		return cli.WrapInputError(fmt.Sprintf("failed to access composer.json file %q", composerPath), err)
	}

	// Check if it's a file (not a directory)
	info, err := os.Stat(composerPath)
	if err != nil {
		return cli.WrapInputError(fmt.Sprintf("failed to stat composer.json file %q", composerPath), err)
	}
	if info.IsDir() {
		return cli.NewInputError(fmt.Sprintf("composer.json must be a file, got directory: %q", composerPath))
	}

	// Store the normalized absolute path
	m.Project.Composer = composerPath

	return nil
}

// validateScan validates the scan configuration
func (v *manifestValidator) validateScan(m *Manifest) error {
	// Validate targets
	if len(m.Scan.Targets) == 0 {
		return cli.NewInputError("scan.targets cannot be empty")
	}

	// Validate that target directories exist (relative to project root)
	for _, target := range m.Scan.Targets {
		var targetPath string
		if filepath.IsAbs(target) {
			targetPath = target
		} else {
			targetPath = filepath.Join(m.Project.Root, target)
		}

		if _, err := os.Stat(targetPath); err != nil {
			if os.IsNotExist(err) {
				return cli.NewInputError(fmt.Sprintf("scan target directory does not exist: %q", targetPath))
			}
			return cli.WrapInputError(fmt.Sprintf("failed to access scan target directory %q", targetPath), err)
		}

		// Check if it's a directory
		info, err := os.Stat(targetPath)
		if err != nil {
			return cli.WrapInputError(fmt.Sprintf("failed to stat scan target %q", targetPath), err)
		}
		if !info.IsDir() {
			return cli.NewInputError(fmt.Sprintf("scan target must be a directory, got file: %q", targetPath))
		}
	}

	// Validate globs if provided
	if len(m.Scan.Globs) == 0 {
		return cli.NewInputError("scan.globs cannot be empty")
	}

	return nil
}

// validateLimits validates the limits configuration
func (v *manifestValidator) validateLimits(m *Manifest) error {
	if m.Limits == nil {
		return nil // Limits are optional
	}

	// Validate max_workers limits
	if m.Limits.MaxWorkers != nil {
		if *m.Limits.MaxWorkers < 1 {
			return cli.NewInputError("limits.max_workers must be at least 1")
		}
	}

	// Validate max_files limits
	if m.Limits.MaxFiles != nil {
		if *m.Limits.MaxFiles < 1 {
			return cli.NewInputError("limits.max_files must be at least 1")
		}
	}

	// Validate max_depth limits
	if m.Limits.MaxDepth != nil {
		if *m.Limits.MaxDepth < 0 {
			return cli.NewInputError("limits.max_depth must be at least 0")
		}
	}

	return nil
}

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
  "$schema": "https://json-schema.org/draft-07/schema#",
  "$id": "https://github.com/garaekz/oxinfer/schemas/manifest.schema.json",
  "title": "Oxinfer Manifest Schema",
  "description": "Schema for Oxinfer manifest configuration files",
  "type": "object",
  "additionalProperties": false,
  "required": ["project", "scan"],
  "properties": {
    "project": {
      "type": "object",
      "additionalProperties": false,
      "required": ["root", "composer"],
      "properties": {
        "root": {
          "type": "string",
          "description": "Root directory of the Laravel project to analyze"
        },
        "composer": {
          "type": "string",
          "description": "Path to composer.json file",
          "default": "composer.json"
        }
      }
    },
    "scan": {
      "type": "object",
      "additionalProperties": false,
      "required": ["targets"],
      "properties": {
        "targets": {
          "type": "array",
          "description": "Directories to scan for PHP files",
          "items": {
            "type": "string"
          },
          "minItems": 1,
          "default": ["app/"]
        },
        "vendor_whitelist": {
          "type": "array",
          "description": "Vendor packages to include in analysis",
          "items": {
            "type": "string"
          }
        },
        "globs": {
          "type": "array",
          "description": "File glob patterns to include",
          "items": {
            "type": "string"
          },
          "default": ["**/*.php"]
        }
      }
    },
    "limits": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "max_files": {
          "type": "integer",
          "description": "Maximum number of files to analyze",
          "minimum": 1,
          "maximum": 100000,
          "default": 10000
        },
        "max_file_size": {
          "type": "integer",
          "description": "Maximum file size in bytes",
          "minimum": 1024,
          "maximum": 104857600,
          "default": 5242880
        },
        "timeout": {
          "type": "integer",
          "description": "Analysis timeout in seconds",
          "minimum": 1,
          "maximum": 3600,
          "default": 300
        }
      }
    },
    "cache": {
      "type": "object",
      "additionalProperties": false,
      "required": ["enabled"],
      "properties": {
        "enabled": {
          "type": "boolean",
          "description": "Enable caching of analysis results",
          "default": true
        },
        "dir": {
          "type": "string",
          "description": "Cache directory path",
          "default": ".oxinfer/cache"
        },
        "ttl": {
          "type": "integer",
          "description": "Cache time-to-live in seconds",
          "minimum": 1,
          "maximum": 2592000,
          "default": 86400
        }
      }
    },
    "features": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "routes": {
          "type": "boolean",
          "description": "Analyze route definitions",
          "default": true
        },
        "controllers": {
          "type": "boolean",
          "description": "Analyze controller classes",
          "default": true
        },
        "models": {
          "type": "boolean",
          "description": "Analyze Eloquent models",
          "default": true
        },
        "middleware": {
          "type": "boolean",
          "description": "Analyze middleware classes",
          "default": true
        },
        "migrations": {
          "type": "boolean",
          "description": "Analyze database migrations",
          "default": true
        },
        "policies": {
          "type": "boolean",
          "description": "Analyze authorization policies",
          "default": true
        },
        "events": {
          "type": "boolean",
          "description": "Analyze event classes",
          "default": true
        },
        "jobs": {
          "type": "boolean",
          "description": "Analyze job classes",
          "default": true
        }
      }
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
	compiler.Draft = jsonschema.Draft7

	// Load schema from embedded file
	if err := compiler.AddResource("manifest.schema.json", strings.NewReader(validatorSchemaJSON)); err != nil {
		return cli.WrapSchemaError("failed to load manifest schema", err)
	}

	schema, err := compiler.Compile("manifest.schema.json")
	if err != nil {
		return cli.WrapSchemaError("failed to compile manifest schema", err)
	}

	// Parse the JSON data for validation
	var jsonData interface{}
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

	// Validate file limits (already constrained by schema, but double-check)
	if m.Limits.MaxFiles != nil {
		if *m.Limits.MaxFiles < 1 || *m.Limits.MaxFiles > 100000 {
			return cli.NewInputError("limits.max_files must be between 1 and 100000")
		}
	}

	// Validate file size limits (1KB to 100MB)
	if m.Limits.MaxFileSize != nil {
		if *m.Limits.MaxFileSize < 1024 || *m.Limits.MaxFileSize > 104857600 {
			return cli.NewInputError("limits.max_file_size must be between 1024 and 104857600 bytes")
		}
	}

	// Validate timeout limits (1 second to 1 hour)
	if m.Limits.Timeout != nil {
		if *m.Limits.Timeout < 1 || *m.Limits.Timeout > 3600 {
			return cli.NewInputError("limits.timeout must be between 1 and 3600 seconds")
		}
	}

	return nil
}
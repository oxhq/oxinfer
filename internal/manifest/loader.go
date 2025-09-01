package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/garaekz/oxinfer/internal/cli"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// manifestSchemaJSON contains the embedded manifest JSON schema
const manifestSchemaJSON = `{
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

// ManifestLoader interface defines methods for loading manifest files
type ManifestLoader interface {
	LoadFromFile(path string) (*Manifest, error)
	LoadFromReader(r io.Reader) (*Manifest, error)
}

// manifestLoader is the concrete implementation of ManifestLoader
type manifestLoader struct {
	validator ManifestValidator
}

// NewLoader creates a new ManifestLoader instance
func NewLoader(validator ManifestValidator) ManifestLoader {
	return &manifestLoader{
		validator: validator,
	}
}

// LoadFromFile loads and validates a manifest from a file path
func (l *manifestLoader) LoadFromFile(path string) (*Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, cli.WrapInputError(fmt.Sprintf("failed to open manifest file %q", path), err)
	}
	defer file.Close()

	return l.LoadFromReader(file)
}

// LoadFromReader loads and validates a manifest from an io.Reader
func (l *manifestLoader) LoadFromReader(r io.Reader) (*Manifest, error) {
	// Read all data from the reader
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, cli.WrapInputError("failed to read manifest data", err)
	}

	// Validate against JSON schema first
	if err := l.validator.ValidateSchema(data); err != nil {
		return nil, err
	}

	// Parse JSON into manifest struct
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, cli.WrapInputError("invalid JSON in manifest", err)
	}

	// Apply schema defaults
	applyDefaults(&manifest)

	// Validate paths and business logic
	if err := l.validator.ValidatePaths(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// applyDefaults applies default values to manifest fields that are not set
// This matches the defaults specified in the JSON schema
func applyDefaults(m *Manifest) {
	// Project defaults - composer.json default handled by schema
	if m.Project.Composer == "" {
		m.Project.Composer = "composer.json"
	}

	// Scan defaults
	if len(m.Scan.Targets) == 0 {
		m.Scan.Targets = []string{"app/"}
	}
	if len(m.Scan.Globs) == 0 {
		m.Scan.Globs = []string{"**/*.php"}
	}

	// Limits defaults
	if m.Limits != nil {
		if m.Limits.MaxFiles == nil {
			defaultMaxFiles := 10000
			m.Limits.MaxFiles = &defaultMaxFiles
		}
		if m.Limits.MaxFileSize == nil {
			defaultMaxFileSize := 5242880 // 5MB
			m.Limits.MaxFileSize = &defaultMaxFileSize
		}
		if m.Limits.Timeout == nil {
			defaultTimeout := 300 // 5 minutes
			m.Limits.Timeout = &defaultTimeout
		}
	}

	// Cache defaults
	if m.Cache != nil {
		if m.Cache.Dir == "" {
			m.Cache.Dir = ".oxinfer/cache"
		}
		if m.Cache.TTL == nil {
			defaultTTL := 86400 // 24 hours
			m.Cache.TTL = &defaultTTL
		}
	}

	// Features defaults - all enabled by default
	if m.Features != nil {
		if m.Features.Routes == nil {
			defaultTrue := true
			m.Features.Routes = &defaultTrue
		}
		if m.Features.Controllers == nil {
			defaultTrue := true
			m.Features.Controllers = &defaultTrue
		}
		if m.Features.Models == nil {
			defaultTrue := true
			m.Features.Models = &defaultTrue
		}
		if m.Features.Middleware == nil {
			defaultTrue := true
			m.Features.Middleware = &defaultTrue
		}
		if m.Features.Migrations == nil {
			defaultTrue := true
			m.Features.Migrations = &defaultTrue
		}
		if m.Features.Policies == nil {
			defaultTrue := true
			m.Features.Policies = &defaultTrue
		}
		if m.Features.Events == nil {
			defaultTrue := true
			m.Features.Events = &defaultTrue
		}
		if m.Features.Jobs == nil {
			defaultTrue := true
			m.Features.Jobs = &defaultTrue
		}
	}
}

// validateWithSchema validates the raw JSON data against the manifest schema
func validateWithSchema(data []byte) error {
	// Load the manifest schema
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	// Load schema from embedded file
	if err := compiler.AddResource("manifest.schema.json", strings.NewReader(manifestSchemaJSON)); err != nil {
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
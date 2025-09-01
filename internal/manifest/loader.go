package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/garaekz/oxinfer/internal/cli"
)

// manifestSchemaJSON contains the embedded manifest JSON schema
const manifestSchemaJSON = `{
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
	if len(m.Scan.Globs) == 0 {
		m.Scan.Globs = []string{"app/**/*.php", "routes/**/*.php"}
	}

	// Limits defaults
	if m.Limits != nil {
		if m.Limits.MaxWorkers == nil {
			defaultMaxWorkers := 8
			m.Limits.MaxWorkers = &defaultMaxWorkers
		}
		if m.Limits.MaxFiles == nil {
			defaultMaxFiles := 500
			m.Limits.MaxFiles = &defaultMaxFiles
		}
		if m.Limits.MaxDepth == nil {
			defaultMaxDepth := 2
			m.Limits.MaxDepth = &defaultMaxDepth
		}
	}

	// Cache defaults
	if m.Cache != nil {
		if m.Cache.Enabled == nil {
			defaultEnabled := true
			m.Cache.Enabled = &defaultEnabled
		}
		if m.Cache.Kind == nil {
			defaultKind := "sha256+mtime"
			m.Cache.Kind = &defaultKind
		}
	}

	// Features defaults - all enabled by default
	if m.Features != nil {
		if m.Features.HTTPStatus == nil {
			defaultTrue := true
			m.Features.HTTPStatus = &defaultTrue
		}
		if m.Features.RequestUsage == nil {
			defaultTrue := true
			m.Features.RequestUsage = &defaultTrue
		}
		if m.Features.ResourceUsage == nil {
			defaultTrue := true
			m.Features.ResourceUsage = &defaultTrue
		}
		if m.Features.WithPivot == nil {
			defaultTrue := true
			m.Features.WithPivot = &defaultTrue
		}
		if m.Features.AttributeMake == nil {
			defaultTrue := true
			m.Features.AttributeMake = &defaultTrue
		}
		if m.Features.ScopesUsed == nil {
			defaultTrue := true
			m.Features.ScopesUsed = &defaultTrue
		}
		if m.Features.Polymorphic == nil {
			defaultTrue := true
			m.Features.Polymorphic = &defaultTrue
		}
		if m.Features.BroadcastChannels == nil {
			defaultTrue := true
			m.Features.BroadcastChannels = &defaultTrue
		}
	}
}


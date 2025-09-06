//go:build goexperiment.jsonv2

package manifest

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"

	"github.com/garaekz/oxinfer/internal/cli"
)

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
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, cli.WrapInputError("failed to read manifest data", err)
	}

	if err := l.validator.ValidateSchema(data); err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, cli.WrapInputError("invalid JSON in manifest", err)
	}

	applyDefaults(&manifest)

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

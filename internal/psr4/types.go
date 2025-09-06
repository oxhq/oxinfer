package psr4

import (
	"fmt"
	"sort"
	"strings"
)

// Data structures for composer.json parsing and PSR-4 autoloading configuration.

// ComposerData represents the complete structure of a composer.json file
// with all fields relevant to PSR-4 autoloading and package metadata.
type ComposerData struct {
	// Name is the package name (e.g., "laravel/framework")
	Name string `json:"name,omitempty"`

	// Description provides a brief description of the package
	Description string `json:"description,omitempty"`

	// Type specifies the package type (e.g., "library", "project")
	Type string `json:"type,omitempty"`

	// Version is the package version
	Version string `json:"version,omitempty"`

	// Autoload contains production autoloading rules
	Autoload PSR4Config `json:"autoload,omitempty"`

	// AutoloadDev contains development-only autoloading rules
	AutoloadDev PSR4Config `json:"autoload-dev,omitempty"`

	// Require lists production dependencies
	Require map[string]string `json:"require,omitempty"`

	// RequireDev lists development dependencies
	RequireDev map[string]string `json:"require-dev,omitempty"`

	// Config contains additional configuration options
	Config map[string]any `json:"config,omitempty"`

	// Extra contains extra data for specific use cases
	Extra map[string]any `json:"extra,omitempty"`
}

// PSR4Config represents the autoloading configuration section of composer.json.
// It supports PSR-4, PSR-0, classmap, and files autoloading strategies.
type PSR4Config struct {
	// PSR4 maps namespace prefixes to directory paths
	// Value can be string, []string, or []interface{} to handle various formats
	PSR4 map[string]any `json:"psr-4,omitempty"`

	// PSR0 maps namespace prefixes to directory paths (legacy support)
	PSR0 map[string]any `json:"psr-0,omitempty"`

	// Classmap lists directories and files to scan for classes
	Classmap []string `json:"classmap,omitempty"`

	// Files lists files to include directly
	Files []string `json:"files,omitempty"`
}

// PackageInfo contains metadata about a composer package.
// Used for organizing and understanding package structure.
type PackageInfo struct {
	// Name is the package name
	Name string

	// Description provides package description
	Description string

	// Type specifies the package type
	Type string

	// Version is the package version
	Version string

	// IsRootPackage indicates if this is the root project package
	IsRootPackage bool
}

// NamespaceMapping represents a PSR-4 namespace to directory mapping.
// Used for processing and validating PSR-4 autoload rules.
type NamespaceMapping struct {
	// Namespace is the namespace prefix (e.g., "App\\")
	Namespace string

	// Paths are the directory paths for this namespace
	// Multiple paths are supported for a single namespace
	Paths []string

	// IsDevDependency indicates if this mapping comes from autoload-dev
	IsDevDependency bool
}

// GetPSR4Mappings extracts and normalizes PSR-4 namespace mappings from composer data.
// Handles various formats of PSR-4 path specifications (string, []string, []interface{}).
// Returns deterministically sorted mappings for consistent output.
func (c *ComposerData) GetPSR4Mappings() ([]NamespaceMapping, error) {
	var mappings []NamespaceMapping

	// Process production autoload mappings
	if c.Autoload.PSR4 != nil {
		prodMappings, err := extractPSR4Mappings(c.Autoload.PSR4, false)
		if err != nil {
			return nil, fmt.Errorf("failed to extract production PSR-4 mappings: %w", err)
		}
		mappings = append(mappings, prodMappings...)
	}

	// Process development autoload mappings
	if c.AutoloadDev.PSR4 != nil {
		devMappings, err := extractPSR4Mappings(c.AutoloadDev.PSR4, true)
		if err != nil {
			return nil, fmt.Errorf("failed to extract development PSR-4 mappings: %w", err)
		}
		mappings = append(mappings, devMappings...)
	}

	// Sort mappings by namespace for deterministic output
	sortNamespaceMappings(mappings)

	return mappings, nil
}

// GetPackageInfo extracts package metadata from composer data.
func (c *ComposerData) GetPackageInfo() PackageInfo {
	return PackageInfo{
		Name:          c.Name,
		Description:   c.Description,
		Type:          c.Type,
		Version:       c.Version,
		IsRootPackage: true, // This implementation focuses on root package
	}
}

// HasPSR4Config returns true if the composer data contains any PSR-4 configuration.
func (c *ComposerData) HasPSR4Config() bool {
	return len(c.Autoload.PSR4) > 0 ||
		len(c.AutoloadDev.PSR4) > 0
}

// extractPSR4Mappings processes a PSR-4 configuration map and extracts namespace mappings.
// Handles multiple path formats: string, []string, []interface{}.
func extractPSR4Mappings(psr4Config map[string]any, isDevDependency bool) ([]NamespaceMapping, error) {
	var mappings []NamespaceMapping

	for namespace, pathsValue := range psr4Config {
		mapping := NamespaceMapping{
			Namespace:       namespace,
			IsDevDependency: isDevDependency,
		}

		// Handle different path value types
		switch paths := pathsValue.(type) {
		case string:
			// Single path as string
			mapping.Paths = []string{paths}

		case []string:
			// Multiple paths as string slice
			mapping.Paths = paths

		case []any:
			// Multiple paths as interface slice (from JSON unmarshaling)
			for _, p := range paths {
				if pathStr, ok := p.(string); ok {
					mapping.Paths = append(mapping.Paths, pathStr)
				} else {
					return nil, NewInvalidNamespaceError(namespace,
						fmt.Errorf("path must be string, got %T", p))
				}
			}

		default:
			return nil, NewInvalidNamespaceError(namespace,
				fmt.Errorf("paths must be string or []string, got %T", pathsValue))
		}

		// Validate namespace format
		if err := validateNamespaceFormat(namespace); err != nil {
			return nil, NewInvalidNamespaceError(namespace, err)
		}

		mappings = append(mappings, mapping)
	}

	return mappings, nil
}

// validateNamespaceFormat checks if a PSR-4 namespace prefix is properly formatted.
func validateNamespaceFormat(namespace string) error {
	// Empty namespace is valid in PSR-4 (represents global/root namespace)
	if namespace == "" {
		return nil
	}

	// Non-empty PSR-4 namespaces should end with backslash
	if !strings.HasSuffix(namespace, "\\") {
		return fmt.Errorf("PSR-4 namespace must end with backslash")
	}

	return nil
}

// sortNamespaceMappings sorts namespace mappings deterministically by namespace prefix.
func sortNamespaceMappings(mappings []NamespaceMapping) {
	sort.Slice(mappings, func(i, j int) bool {
		return mappings[i].Namespace < mappings[j].Namespace
	})
}

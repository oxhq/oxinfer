// Package psr4 provides PSR-4 autoloading resolution for PHP projects.
//
// This package implements composer.json parsing, FQCN to file path mapping,
// and filesystem resolution following the PSR-4 autoloading specification.
//
// Interface contracts are stable and should not be modified without
// careful consideration of backward compatibility and system integration.
package psr4

import "context"

// ComposerConfig represents the structure of a composer.json file
// with focus on autoloading configuration.
type ComposerConfig struct {
	// Autoload contains production autoloading rules
	Autoload AutoloadSection `json:"autoload"`
	// AutoloadDev contains development-only autoloading rules  
	AutoloadDev AutoloadSection `json:"autoload-dev"`
	// Name is the package name (optional)
	Name string `json:"name,omitempty"`
}

// AutoloadSection represents an autoload configuration section
type AutoloadSection struct {
	// PSR4 maps namespace prefixes to directory paths
	PSR4 map[string]interface{} `json:"psr-4,omitempty"`
	// PSR0 maps namespace prefixes to directory paths (legacy)
	PSR0 map[string]interface{} `json:"psr-0,omitempty"`
	// Classmap lists directories/files to scan for classes
	Classmap []string `json:"classmap,omitempty"`
	// Files lists files to include directly
	Files []string `json:"files,omitempty"`
}

// ComposerLoader loads and validates composer.json files.
//
// Implementations must handle malformed JSON gracefully and provide
// meaningful error messages for validation failures.
type ComposerLoader interface {
	// LoadComposer loads a composer.json file from the specified path.
	// Returns ComposerConfig or error if file cannot be loaded/parsed.
	LoadComposer(path string) (*ComposerConfig, error)
	
	// ValidateConfig validates a ComposerConfig for PSR-4 compliance.
	// Checks namespace format, directory paths, and structural requirements.
	ValidateConfig(config *ComposerConfig) error
}

// ClassMapper maps fully qualified class names to potential file paths
// using PSR-4 autoloading rules.
//
// Implementations must follow PSR-4 specification exactly, including
// namespace prefix matching and file path transformation rules.
type ClassMapper interface {
	// MapClass maps a fully qualified class name to potential file paths.
	// Returns ordered list of candidates (most specific namespace first).
	// FQCN format: "App\\Http\\Controllers\\UserController"
	MapClass(fqcn string) ([]string, error)
	
	// GetNamespaces returns all registered namespace prefixes.
	// Useful for enumerating available namespaces.
	GetNamespaces() []string
}

// PathResolver resolves class file candidates to actual filesystem paths.
//
// Implementations must handle both absolute and relative paths,
// support context cancellation, and work across platforms.
type PathResolver interface {
	// ResolvePath finds the first existing file from candidates list.
	// baseDir is the directory containing composer.json (project root).
	// Returns absolute path to file or error if none found.
	ResolvePath(ctx context.Context, candidates []string, baseDir string) (string, error)
	
	// FileExists checks if a file exists at the given path.
	// Handles both absolute and relative paths efficiently.
	FileExists(path string) bool
}

// PSR4Resolver provides complete PSR-4 class resolution combining
// composer parsing, class mapping, and filesystem resolution.
//
// Implementations must maintain deterministic behavior and support
// efficient caching for large projects.
type PSR4Resolver interface {
	// ResolveClass resolves a fully qualified class name to file path.
	// This is the primary method that orchestrates the complete resolution.
	ResolveClass(ctx context.Context, fqcn string) (string, error)
	
	// GetAllClasses returns a map of all discoverable classes to their file paths.
	// Results must be deterministically ordered for consistent delta.json output.
	GetAllClasses(ctx context.Context) (map[string]string, error)
	
	// Refresh reloads composer configuration and clears caches.
	// Used when composer.json is modified during analysis.
	Refresh() error
}

// ClassInfo represents metadata about a discovered class.
type ClassInfo struct {
	// FQCN is the fully qualified class name
	FQCN string
	// FilePath is the absolute path to the class file
	FilePath string
	// Namespace is the namespace prefix used for resolution
	Namespace string
	// IsDevDependency indicates if class comes from autoload-dev
	IsDevDependency bool
}

// ResolverOptions configures PSR-4 resolver behavior.
type ResolverOptions struct {
	// IncludeDev whether to include autoload-dev classes
	IncludeDev bool
	// CacheSize maximum number of resolved classes to cache
	CacheSize int
	// BaseDir override for composer.json location
	BaseDir string
}

// Error types for PSR-4 resolution failures.

// ComposerError represents composer.json related errors.
type ComposerError struct {
	Path    string
	Message string
	Cause   error
}

func (e *ComposerError) Error() string {
	return e.Message
}

func (e *ComposerError) Unwrap() error {
	return e.Cause
}

// MappingError represents class mapping failures.
type MappingError struct {
	FQCN    string
	Message string
	Cause   error
}

func (e *MappingError) Error() string {
	return e.Message
}

func (e *MappingError) Unwrap() error {
	return e.Cause
}

// ResolutionError represents file resolution failures.
type ResolutionError struct {
	Candidates []string
	BaseDir    string
	Message    string
	Cause      error
}

func (e *ResolutionError) Error() string {
	return e.Message
}

func (e *ResolutionError) Unwrap() error {
	return e.Cause
}
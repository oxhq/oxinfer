package indexer

import "time"

// DiscoveryConfig contains configuration parameters for file discovery operations
type DiscoveryConfig struct {
	// Targets are the directories to scan for PHP files (relative to ProjectRoot)
	Targets []string

	// Globs are the glob patterns to match files (e.g., "**/*.php", "app/**/*.php")
	Globs []string

	// ProjectRoot is the base project directory (absolute path)
	ProjectRoot string

	// MaxDepth is the maximum directory traversal depth (0 = no limit)
	MaxDepth int
}

// DiscoveryResult contains the results of a file discovery operation
type DiscoveryResult struct {
	// Files are the discovered PHP files, sorted deterministically
	Files []FileInfo

	// Stats contains performance and diagnostic information
	Stats DiscoveryStats

	// Truncated indicates if discovery was limited by MaxDepth
	Truncated bool
}

// DiscoveryStats provides metrics about the file discovery process
type DiscoveryStats struct {
	// FilesScanned is the total number of files examined during discovery
	FilesScanned int

	// DirectoriesScanned is the total number of directories traversed
	DirectoriesScanned int

	// Duration is the time spent performing file discovery
	Duration time.Duration

	// TruncatedPaths are directories that were skipped due to MaxDepth limits
	TruncatedPaths []string

	// PHPFilesFound is the number of PHP files discovered
	PHPFilesFound int

	// NonPHPFilesSkipped is the number of non-PHP files that were skipped
	NonPHPFilesSkipped int
}

// DefaultDiscoveryConfig returns a discovery configuration with sensible defaults
func DefaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		Targets:     []string{"app", "routes"},
		Globs:       []string{"**/*.php"},
		ProjectRoot: "",
		MaxDepth:    0, // No limit by default
	}
}

// Validate checks that the DiscoveryConfig is valid and complete
func (c DiscoveryConfig) Validate() error {
	if c.ProjectRoot == "" {
		return NewDiscoveryError("Validate", "", ErrInvalidPath).
			WithMetadata("field", "ProjectRoot").
			WithMetadata("reason", "empty project root")
	}

	if len(c.Targets) == 0 {
		return NewDiscoveryError("Validate", "", ErrInvalidPath).
			WithMetadata("field", "Targets").
			WithMetadata("reason", "no targets specified")
	}

	if len(c.Globs) == 0 {
		return NewDiscoveryError("Validate", "", ErrInvalidGlob).
			WithMetadata("field", "Globs").
			WithMetadata("reason", "no globs specified")
	}

	if c.MaxDepth < 0 {
		return NewDiscoveryError("Validate", "", ErrInvalidPath).
			WithMetadata("field", "MaxDepth").
			WithMetadata("reason", "negative max depth")
	}

	return nil
}

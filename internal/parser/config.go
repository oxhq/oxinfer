// Package parser provides configuration for PHP project parsing operations.
// Defines default configurations, validation, and helper functions for
// integrating with manifest configuration.
package parser

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// DefaultProjectParserConfig returns sensible defaults for PHP project parsing.
// These defaults work well for typical Laravel projects and provide a good baseline.
func DefaultProjectParserConfig() ProjectParserConfig {
	return ProjectParserConfig{
		// Project settings - will be overridden by manifest
		ProjectRoot:  ".",
		ComposerPath: "composer.json",

		// Discovery settings - conservative defaults
		Targets:  []string{"app", "routes"},
		Globs:    []string{"**/*.php"},
		MaxFiles: 1000,
		MaxDepth: 10,

		// Parsing settings - balanced for performance and thoroughness
		MaxWorkers:   4,
		ParseTimeout: 30 * time.Second,

		// Caching settings - enable for performance
		CacheEnabled: true,
		CacheKind:    "mtime",

		// Feature flags - enable all for comprehensive analysis
		ExtractClasses:    true,
		ExtractMethods:    true,
		ExtractNamespaces: true,
		ExtractTraits:     true,
		ExtractInterfaces: true,
	}
}

// ConfigFromManifest creates ProjectParserConfig from Oxinfer manifest.
// Integrates with manifest system to extract relevant configuration.
func ConfigFromManifest(m *manifest.Manifest) (ProjectParserConfig, error) {
	if m == nil {
		return ProjectParserConfig{}, fmt.Errorf("manifest cannot be nil")
	}

	config := DefaultProjectParserConfig()

	// Project settings
	config.ProjectRoot = m.Project.Root
	if m.Project.Composer != "" {
		config.ComposerPath = m.Project.Composer
	} else {
		// Default composer.json path relative to project root
		config.ComposerPath = filepath.Join(m.Project.Root, "composer.json")
	}

	// Discovery settings
	if len(m.Scan.Targets) > 0 {
		config.Targets = m.Scan.Targets
	}
	if len(m.Scan.Globs) > 0 {
		config.Globs = m.Scan.Globs
	} else {
		// Default globs for PHP files if not specified
		config.Globs = []string{"**/*.php"}
	}

	// Apply limits from manifest
	if m.Limits != nil {
		if m.Limits.MaxWorkers != nil {
			config.MaxWorkers = *m.Limits.MaxWorkers
		}
		if m.Limits.MaxFiles != nil {
			config.MaxFiles = *m.Limits.MaxFiles
		}
		if m.Limits.MaxDepth != nil {
			config.MaxDepth = *m.Limits.MaxDepth
		}
	}

	// Apply cache settings from manifest
	if m.Cache != nil {
		if m.Cache.Enabled != nil {
			config.CacheEnabled = *m.Cache.Enabled
		}
		if m.Cache.Kind != nil {
			config.CacheKind = *m.Cache.Kind
		}
	}

	// Apply feature flags from manifest
	if m.Features != nil {
		// All features are enabled by default, but can be disabled via manifest
		// Features control what constructs are extracted during analysis

		// Note: The manifest features are for specific Laravel patterns,
		// but we enable basic PHP construct extraction by default.
		// Future development will map these features to specific extractors.
	}

	return config, nil
}

// ValidateProjectParserConfig validates ProjectParserConfig for consistency and feasibility.
// Returns error if configuration contains invalid or incompatible settings.
func ValidateProjectParserConfig(config ProjectParserConfig) error {
	// Validate project settings
	if config.ProjectRoot == "" {
		return fmt.Errorf("project root cannot be empty")
	}
	if config.ComposerPath == "" {
		return fmt.Errorf("composer path cannot be empty")
	}

	// Validate discovery settings
	if len(config.Targets) == 0 {
		return fmt.Errorf("at least one scan target must be specified")
	}
	if len(config.Globs) == 0 {
		return fmt.Errorf("at least one glob pattern must be specified")
	}
	if config.MaxFiles <= 0 {
		return fmt.Errorf("max files must be positive, got %d", config.MaxFiles)
	}
	if config.MaxDepth <= 0 {
		return fmt.Errorf("max depth must be positive, got %d", config.MaxDepth)
	}

	// Validate parsing settings
	if config.MaxWorkers <= 0 {
		return fmt.Errorf("max workers must be positive, got %d", config.MaxWorkers)
	}
	if config.MaxWorkers > 32 {
		return fmt.Errorf("max workers too high (%d), limit is 32 for stability", config.MaxWorkers)
	}
	if config.ParseTimeout <= 0 {
		return fmt.Errorf("parse timeout must be positive, got %v", config.ParseTimeout)
	}
	if config.ParseTimeout > 5*time.Minute {
		return fmt.Errorf("parse timeout too high (%v), limit is 5 minutes", config.ParseTimeout)
	}

	// Validate cache settings (only when cache is enabled)
	if config.CacheEnabled {
		if config.CacheKind != "mtime" && config.CacheKind != "sha256+mtime" {
			return fmt.Errorf("invalid cache kind '%s', must be 'mtime' or 'sha256+mtime'", config.CacheKind)
		}
	}

	return nil
}

// DefaultProjectParserProgress returns initial progress state for project parsing.
// Used to initialize progress tracking before parsing begins.
func DefaultProjectParserProgress() ProjectParserProgress {
	return ProjectParserProgress{
		Phase:       ProjectParserPhaseInitializing,
		PhaseStatus: "Initializing parser...",

		// All counters start at zero
		FilesDiscovered: 0,
		FilesParsed:     0,
		FilesExtracted:  0,
		FilesFailed:     0,

		ClassesFound:    0,
		MethodsFound:    0,
		TraitsFound:     0,
		InterfacesFound: 0,

		ElapsedTime:        0,
		EstimatedRemaining: 0,
		ThroughputPerSec:   0,

		CurrentMemoryUsage: 0,
		ActiveWorkers:      0,

		IsComplete: false,
		HasErrors:  false,
	}
}

// ProjectParserPhaseString returns human-readable phase names for logging and UI.
func ProjectParserPhaseString(phase ProjectParserPhase) string {
	switch phase {
	case ProjectParserPhaseInitializing:
		return "Initializing"
	case ProjectParserPhaseDiscovering:
		return "Discovering Files"
	case ProjectParserPhaseResolving:
		return "Resolving Namespaces"
	case ProjectParserPhaseParsing:
		return "Parsing PHP Files"
	case ProjectParserPhaseExtracting:
		return "Extracting Constructs"
	case ProjectParserPhaseCompleted:
		return "Completed"
	case ProjectParserPhaseFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// EstimateMemoryUsage provides rough memory usage estimates for configuration validation.
// Helps prevent OOM conditions with large projects or aggressive worker settings.
func EstimateMemoryUsage(config ProjectParserConfig) int64 {
	// Rough estimates in bytes:
	// - Each worker: ~2MB for parser instance + buffers
	// - Each file cached: ~1KB metadata + content size estimation
	// - Base overhead: ~10MB for indexer and PSR-4 resolver

	baseOverhead := int64(10 * 1024 * 1024) // 10MB base

	workerMemory := int64(config.MaxWorkers) * 2 * 1024 * 1024 // 2MB per worker

	// Estimate file cache memory (assume average 10KB per PHP file)
	cacheMemory := int64(config.MaxFiles) * 11 * 1024 // 11KB per file (content + metadata)

	totalEstimate := baseOverhead + workerMemory + cacheMemory

	return totalEstimate
}

// WarnOnHighResourceUsage logs warnings for configurations that may cause performance issues.
// Returns warnings as strings that can be logged or displayed to users.
func WarnOnHighResourceUsage(config ProjectParserConfig) []string {
	var warnings []string

	// Check memory usage
	estimatedMemory := EstimateMemoryUsage(config)
	if estimatedMemory > 512*1024*1024 { // 512MB
		warnings = append(warnings, fmt.Sprintf(
			"Estimated memory usage is high (%d MB). Consider reducing MaxFiles or MaxWorkers",
			estimatedMemory/(1024*1024),
		))
	}

	// Check worker count vs CPU
	if config.MaxWorkers > 8 {
		warnings = append(warnings, fmt.Sprintf(
			"High worker count (%d) may cause contention. Optimal range is typically 2-8",
			config.MaxWorkers,
		))
	}

	// Check file limits
	if config.MaxFiles > 5000 {
		warnings = append(warnings, fmt.Sprintf(
			"Large file limit (%d) may cause slow analysis. Consider breaking into smaller batches",
			config.MaxFiles,
		))
	}

	// Check timeout settings
	if config.ParseTimeout < 5*time.Second {
		warnings = append(warnings, fmt.Sprintf(
			"Short parse timeout (%v) may cause failures on large files",
			config.ParseTimeout,
		))
	}

	return warnings
}

// Package pipeline provides orchestration for the complete oxinfer analysis pipeline.
// It coordinates all components from T1-T11 to produce the final delta.json output.
package pipeline

import (
	"context"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/indexer"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/matchers"
	"github.com/garaekz/oxinfer/internal/parser"
	"github.com/garaekz/oxinfer/internal/psr4"
)

// PipelinePhase represents the current phase of pipeline execution.
type PipelinePhase int

const (
	PipelinePhaseInitializing PipelinePhase = iota
	PipelinePhaseIndexing
	PipelinePhaseParsing
	PipelinePhaseMatching
	PipelinePhaseInference
	PipelinePhaseAssembly
	PipelinePhaseCompleted
	PipelinePhaseFailed
)

// String returns a human-readable name for the pipeline phase.
func (p PipelinePhase) String() string {
	switch p {
	case PipelinePhaseInitializing:
		return "initializing"
	case PipelinePhaseIndexing:
		return "indexing"
	case PipelinePhaseParsing:
		return "parsing"
	case PipelinePhaseMatching:
		return "matching"
	case PipelinePhaseInference:
		return "inference"
	case PipelinePhaseAssembly:
		return "assembly"
	case PipelinePhaseCompleted:
		return "completed"
	case PipelinePhaseFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// PipelineConfig contains configuration for the entire analysis pipeline.
type PipelineConfig struct {
	// Project settings
	ProjectRoot  string
	ComposerPath string

	// Indexing settings
	Targets     []string
	Globs       []string
	MaxFiles    int
	MaxWorkers  int
	CacheConfig indexer.IndexConfig

	// Parser settings
	ParserConfig parser.ParserConfig

	// Matcher settings
	MatcherConfig *matchers.MatcherConfig

	// Inference settings
	InferenceConfig *infer.InferenceConfig

	// Output settings
	EnableStamp   bool
	OutputPartial bool
}

// PipelineResults aggregates results from all pipeline phases.
type PipelineResults struct {
	// Phase results
	IndexResult    *indexer.IndexResult
	ParseResults   *ParseResults
	MatchResults   *MatchResults
	InferenceResults *InferenceResults

	// Final output
	Delta *emitter.Delta

	// Processing metadata
	StartTime      time.Time
	EndTime        time.Time
	ProcessingTime time.Duration
	Phase          PipelinePhase
	Partial        bool
	TruncatedBy    []string
}

// ParseResults contains results from the parsing phase.
type ParseResults struct {
	// Successfully parsed files
	ParsedFiles []ParsedFile

	// Files that failed parsing
	FailedFiles []FailedFile

	// Aggregated PHP constructs
	Classes    []parser.PHPClass
	Methods    []parser.PHPMethod
	Namespaces []parser.PHPNamespace
	Traits     []parser.PHPTrait
	Interfaces []parser.PHPInterface

	// Statistics
	FilesProcessed int
	ParseErrors    int
	ParseDuration  time.Duration
}

// ParsedFile represents a successfully parsed PHP file.
type ParsedFile struct {
	FilePath        string
	RelativePath    string
	Namespace       string
	FileStructure   *parser.PHPFileStructure
	LaravelPatterns *parser.LaravelPatterns
	ParsedFromCache bool
	ParseDuration   time.Duration
}

// FailedFile represents a file that failed parsing.
type FailedFile struct {
	FilePath string
	Error    error
}

// MatchResults contains results from the pattern matching phase.
type MatchResults struct {
	// Pattern matches by file
	FilePatterns map[string]*matchers.LaravelPatterns

	// Aggregated matches by type
	HTTPStatusMatches   []*matchers.HTTPStatusMatch
	RequestUsageMatches []*matchers.RequestUsageMatch
	ResourceMatches     []*matchers.ResourceMatch
	PivotMatches        []*matchers.PivotMatch
	AttributeMatches    []*matchers.AttributeMatch
	ScopeMatches        []*matchers.ScopeMatch
	PolymorphicMatches  []*matchers.PolymorphicMatch
	BroadcastMatches    []*matchers.BroadcastMatch

	// Statistics
	FilesMatched     int
	TotalMatches     int
	MatchingDuration time.Duration
}

// InferenceResults contains results from the shape inference phase.
type InferenceResults struct {
	// Inferred request shapes by controller method
	RequestShapes map[string]*infer.RequestInfo

	// Consolidated patterns
	ConsolidatedRequests map[string]*infer.ConsolidatedRequest

	// Statistics
	PatternsProcessed int
	ShapesInferred    int
	InferenceDuration time.Duration
}

// PipelineStats provides comprehensive statistics about pipeline execution.
type PipelineStats struct {
	// Overall metrics
	TotalDuration   time.Duration
	FilesDiscovered int
	FilesProcessed  int
	FilesSkipped    int
	FilesFailed     int

	// Phase durations
	IndexingDuration time.Duration
	ParsingDuration  time.Duration
	MatchingDuration time.Duration
	InferenceDuration time.Duration
	AssemblyDuration time.Duration

	// Pattern detection
	PatternsDetected int
	ShapesInferred   int

	// Error handling
	RecoverableErrors int
	FatalErrors       int

	// Resource usage
	PeakMemoryUsage int64
	CacheHitRate    float64
}

// PipelineProgress tracks real-time progress through the pipeline.
type PipelineProgress struct {
	// Current state
	Phase       PipelinePhase
	PhaseStatus string
	Progress    float64 // 0.0 to 1.0

	// File progress
	FilesDiscovered int
	FilesParsed     int
	FilesMatched    int
	FilesInferred   int
	FilesFailed     int

	// Pattern progress
	PatternsFound int
	ShapesInferred int

	// Timing
	ElapsedTime        time.Duration
	EstimatedRemaining time.Duration
	ThroughputPerSec   float64

	// Status flags
	IsComplete bool
	HasErrors  bool
	Partial    bool
}

// PipelineError represents errors that occur during pipeline execution.
type PipelineError struct {
	Type    string
	Message string
	Phase   PipelinePhase
	Context string
	Cause   error
}

func (e *PipelineError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *PipelineError) Unwrap() error {
	return e.Cause
}

// Pipeline error types
const (
	ErrorTypeInitialization = "INITIALIZATION"
	ErrorTypeIndexing       = "INDEXING"
	ErrorTypeParsing        = "PARSING"
	ErrorTypeMatching       = "MATCHING"
	ErrorTypeInference      = "INFERENCE"
	ErrorTypeAssembly       = "ASSEMBLY"
	ErrorTypeConfiguration  = "CONFIGURATION"
	ErrorTypeResourceLimit  = "RESOURCE_LIMIT"
)

// NewPipelineError creates a new pipeline error.
func NewPipelineError(errorType string, message string, phase PipelinePhase, context string, cause error) *PipelineError {
	return &PipelineError{
		Type:    errorType,
		Message: message,
		Phase:   phase,
		Context: context,
		Cause:   cause,
	}
}

// PipelineOrchestrator defines the main interface for pipeline orchestration.
type PipelineOrchestrator interface {
	// ProcessProject executes the complete pipeline from manifest to delta.json
	ProcessProject(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error)

	// Phase execution methods for granular control
	RunIndexingPhase(ctx context.Context, manifest *manifest.Manifest) (*indexer.IndexResult, error)
	RunParsingPhase(ctx context.Context, files []indexer.FileInfo) (*ParseResults, error)
	RunMatchingPhase(ctx context.Context, parseResults *ParseResults) (*MatchResults, error)
	RunInferencePhase(ctx context.Context, matchResults *MatchResults) (*InferenceResults, error)

	// Configuration and monitoring
	SetConfiguration(config *PipelineConfig)
	GetConfiguration() *PipelineConfig
	GetProgress() *PipelineProgress
	GetStats() *PipelineStats

	// Resource management
	SetProgressCallback(callback func(*PipelineProgress))
	ClearCaches() // Clear all internal caches and reset state
	Close() error
}

// DeltaAssembler defines the interface for assembling final delta.json from pipeline results.
type DeltaAssembler interface {
	// AssembleDelta creates the final delta.json from all pipeline results
	AssembleDelta(ctx context.Context, results *PipelineResults) (*emitter.Delta, error)

	// Component integration methods
	AssembleControllers(parseResults *ParseResults, matchResults *MatchResults, inferenceResults *InferenceResults) ([]emitter.Controller, error)
	AssembleModels(parseResults *ParseResults, matchResults *MatchResults) ([]emitter.Model, error)
	AssemblePolymorphic(matchResults *MatchResults) ([]emitter.Polymorphic, error)
	AssembleBroadcast(matchResults *MatchResults) ([]emitter.Broadcast, error)
	AssembleMetadata(results *PipelineResults, stats *PipelineStats) (emitter.MetaInfo, error)
}

// ComponentRegistry manages access to all pipeline components.
type ComponentRegistry struct {
	// Core components
	PSR4Resolver    psr4.PSR4Resolver
	FileIndexer     indexer.FileIndexer
	PHPParser       parser.PHPParser
	PatternMatcher  matchers.CompositePatternMatcher
	ShapeInferencer infer.ShapeInferencer
	DeltaEmitter    emitter.DeltaEmitter

	// Component configurations
	IndexerConfig   *indexer.IndexConfig
	ParserConfig    *parser.ParserConfig
	MatcherConfig   *matchers.MatcherConfig
	InferenceConfig *infer.InferenceConfig
}

// NewComponentRegistry creates a new component registry with default configurations.
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{
		IndexerConfig:   &indexer.IndexConfig{},
		ParserConfig:    &parser.ParserConfig{},
		MatcherConfig:   matchers.DefaultMatcherConfig(),
		InferenceConfig: infer.DefaultInferenceConfig(),
	}
}

// ClearCaches clears all component caches and resets state.
func (r *ComponentRegistry) ClearCaches() {
	// Clear PSR-4 resolver cache if it has one
	// PSR4Resolver interface would need to define this method
	
	// Clear parser caches if available
	// PHPParser interface would need to define this method
	
	// Clear pattern matcher caches if available
	// CompositePatternMatcher interface would need to define this method
	
	// For now, we'll reset component references to force re-initialization
	// This is a safe approach that ensures clean state
	r.PSR4Resolver = nil
	r.FileIndexer = nil
	r.PHPParser = nil
	r.PatternMatcher = nil
	r.ShapeInferencer = nil
	r.DeltaEmitter = nil
}

// Close releases all component resources.
func (r *ComponentRegistry) Close() error {
	var lastErr error

	// PSR4Resolver doesn't have a Close method currently
	// if r.PSR4Resolver != nil {
	//     if err := r.PSR4Resolver.Close(); err != nil {
	//         lastErr = err
	//     }
	// }

	if r.PHPParser != nil {
		if err := r.PHPParser.Close(); err != nil {
			lastErr = err
		}
	}

	if r.PatternMatcher != nil {
		if err := r.PatternMatcher.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// DefaultPipelineConfig returns sensible defaults for pipeline configuration.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		Targets:    []string{"app", "routes"},
		Globs:      []string{"**/*.php"},
		MaxFiles:   10000,
		MaxWorkers: 4,
		CacheConfig: indexer.IndexConfig{
			CacheEnabled: true,
			CacheKind:    "sha256+mtime",
		},
		ParserConfig: parser.ParserConfig{
			MaxFileSize:           1024 * 1024, // 1MB
			MaxParseTime:          30 * time.Second,
			PoolSize:              4,
			EnableLaravelPatterns: true,
			EnableDocBlocks:       true,
			EnableDetailedErrors:  true,
		},
		MatcherConfig:   matchers.DefaultMatcherConfig(),
		InferenceConfig: infer.DefaultInferenceConfig(),
		EnableStamp:     true,
		OutputPartial:   true,
	}
}

// ConfigureFromManifest updates pipeline configuration based on manifest settings.
func (config *PipelineConfig) ConfigureFromManifest(manifest *manifest.Manifest) error {
	if manifest == nil {
		return NewPipelineError(ErrorTypeConfiguration, "manifest cannot be nil", PipelinePhaseInitializing, "ConfigureFromManifest", nil)
	}

	// Update basic settings
	if manifest.Project.Root != "" {
		config.ProjectRoot = manifest.Project.Root
	}

	// Update scanning targets
	if len(manifest.Scan.Targets) > 0 {
		config.Targets = manifest.Scan.Targets
	}

	// Update file limits
	if manifest.Limits != nil {
		if manifest.Limits.MaxFiles != nil && *manifest.Limits.MaxFiles > 0 {
			config.MaxFiles = *manifest.Limits.MaxFiles
		}

		if manifest.Limits.MaxWorkers != nil && *manifest.Limits.MaxWorkers > 0 {
			config.MaxWorkers = *manifest.Limits.MaxWorkers
		}
	}

	// Update caching settings
	if manifest.Cache != nil {
		if manifest.Cache.Enabled != nil {
			config.CacheConfig.CacheEnabled = *manifest.Cache.Enabled
		}

		if manifest.Cache.Kind != nil && *manifest.Cache.Kind != "" {
			config.CacheConfig.CacheKind = *manifest.Cache.Kind
		}
	}

	// Configure feature flags
	if manifest.Features != nil {
		// Initialize MatcherConfig if it's nil
		if config.MatcherConfig == nil {
			config.MatcherConfig = matchers.DefaultMatcherConfig()
		}
		
		featureConfig := &matchers.FeatureConfig{
			HTTPStatus:        manifest.Features.HTTPStatus,
			RequestUsage:      manifest.Features.RequestUsage,
			ResourceUsage:     manifest.Features.ResourceUsage,
			WithPivot:         manifest.Features.WithPivot,
			AttributeMake:     manifest.Features.AttributeMake,
			ScopesUsed:        manifest.Features.ScopesUsed,
			Polymorphic:       manifest.Features.Polymorphic,
			BroadcastChannels: manifest.Features.BroadcastChannels,
		}
		config.MatcherConfig.ApplyFeatureFlags(featureConfig)
	}

	return nil
}

// Validate checks if the pipeline configuration is valid.
func (config *PipelineConfig) Validate() error {
	if config.ProjectRoot == "" {
		return NewPipelineError(ErrorTypeConfiguration, "project root cannot be empty", PipelinePhaseInitializing, "Validate", nil)
	}

	if len(config.Targets) == 0 {
		return NewPipelineError(ErrorTypeConfiguration, "scan targets cannot be empty", PipelinePhaseInitializing, "Validate", nil)
	}

	if config.MaxFiles <= 0 {
		return NewPipelineError(ErrorTypeConfiguration, "max files must be positive", PipelinePhaseInitializing, "Validate", nil)
	}

	if config.MaxWorkers <= 0 {
		return NewPipelineError(ErrorTypeConfiguration, "max workers must be positive", PipelinePhaseInitializing, "Validate", nil)
	}

	return nil
}
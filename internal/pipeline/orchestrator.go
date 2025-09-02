// Package pipeline provides orchestration for the complete oxinfer analysis pipeline.
package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/indexer"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/matchers"
	"github.com/garaekz/oxinfer/internal/parser"
	"github.com/garaekz/oxinfer/internal/psr4"
)

// DefaultOrchestrator implements the PipelineOrchestrator interface.
// It coordinates all pipeline phases from file discovery to delta emission.
type DefaultOrchestrator struct {
	config           *PipelineConfig
	registry         *ComponentRegistry
	assembler        DeltaAssembler
	progressCallback func(*PipelineProgress)

	// State tracking
	mu       sync.RWMutex
	progress *PipelineProgress
	stats    *PipelineStats
	results  *PipelineResults
}

// NewOrchestrator creates a new pipeline orchestrator with the given configuration.
func NewOrchestrator(config *PipelineConfig) (*DefaultOrchestrator, error) {
	if config == nil {
		return nil, NewPipelineError(ErrorTypeConfiguration, "config cannot be nil", PipelinePhaseInitializing, "NewOrchestrator", nil)
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	registry := NewComponentRegistry()
	assembler := NewDeltaAssembler()

	return &DefaultOrchestrator{
		config:    config,
		registry:  registry,
		assembler: assembler,
		progress: &PipelineProgress{
			Phase: PipelinePhaseInitializing,
		},
		stats:   &PipelineStats{},
		results: &PipelineResults{},
	}, nil
}

// ProcessProject executes the complete pipeline from manifest to delta.json.
func (o *DefaultOrchestrator) ProcessProject(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error) {
	o.updateProgress(PipelinePhaseInitializing, "Initializing pipeline components", 0.0)

	// Configure pipeline from manifest
	if err := o.config.ConfigureFromManifest(manifest); err != nil {
		return nil, o.handleError(ErrorTypeConfiguration, "failed to configure from manifest", PipelinePhaseInitializing, err)
	}

	// Initialize components
	if err := o.initializeComponents(); err != nil {
		return nil, o.handleError(ErrorTypeInitialization, "failed to initialize components", PipelinePhaseInitializing, err)
	}

	startTime := time.Now()
	o.results.StartTime = startTime

	// Phase 1: File Indexing
	o.updateProgress(PipelinePhaseIndexing, "Discovering and indexing PHP files", 0.1)
	indexResult, err := o.RunIndexingPhase(ctx, manifest)
	if err != nil {
		return nil, o.handleError(ErrorTypeIndexing, "indexing phase failed", PipelinePhaseIndexing, err)
	}
	o.results.IndexResult = indexResult
	o.stats.FilesDiscovered = indexResult.TotalFiles
	o.stats.IndexingDuration = time.Duration(indexResult.DurationMs) * time.Millisecond

	// Phase 2: PHP Parsing
	o.updateProgress(PipelinePhaseParsing, "Parsing PHP files and extracting constructs", 0.3)
	parseResults, err := o.RunParsingPhase(ctx, indexResult.Files)
	if err != nil {
		return nil, o.handleError(ErrorTypeParsing, "parsing phase failed", PipelinePhaseParsing, err)
	}
	o.results.ParseResults = parseResults
	o.stats.FilesProcessed = parseResults.FilesProcessed
	o.stats.ParsingDuration = parseResults.ParseDuration

	// Phase 3: Pattern Matching
	o.updateProgress(PipelinePhaseMatching, "Detecting Laravel patterns", 0.6)
	matchResults, err := o.RunMatchingPhase(ctx, parseResults)
	if err != nil {
		return nil, o.handleError(ErrorTypeMatching, "matching phase failed", PipelinePhaseMatching, err)
	}
	o.results.MatchResults = matchResults
	o.stats.PatternsDetected = matchResults.TotalMatches
	o.stats.MatchingDuration = matchResults.MatchingDuration

	// Phase 4: Shape Inference
	o.updateProgress(PipelinePhaseInference, "Inferring request shapes", 0.8)
	inferenceResults, err := o.RunInferencePhase(ctx, matchResults)
	if err != nil {
		return nil, o.handleError(ErrorTypeInference, "inference phase failed", PipelinePhaseInference, err)
	}
	o.results.InferenceResults = inferenceResults
	o.stats.ShapesInferred = inferenceResults.ShapesInferred
	o.stats.InferenceDuration = inferenceResults.InferenceDuration

	// Phase 5: Delta Assembly
	o.updateProgress(PipelinePhaseAssembly, "Assembling final delta.json", 0.9)
	assemblyStart := time.Now()
	delta, err := o.assembler.AssembleDelta(ctx, o.results)
	if err != nil {
		return nil, o.handleError(ErrorTypeAssembly, "assembly phase failed", PipelinePhaseAssembly, err)
	}
	o.stats.AssemblyDuration = time.Since(assemblyStart)

	// Finalize results
	endTime := time.Now()
	o.results.EndTime = endTime
	o.results.ProcessingTime = endTime.Sub(startTime)
	o.results.Delta = delta
	o.stats.TotalDuration = o.results.ProcessingTime

	// Check if processing was partial
	o.results.Partial = indexResult.Partial || o.shouldMarkPartial()
	if o.results.Partial {
		delta.Meta.Partial = true
		o.collectTruncationReasons(indexResult)
	}

	o.updateProgress(PipelinePhaseCompleted, "Pipeline completed successfully", 1.0)
	return delta, nil
}

// RunIndexingPhase executes the file discovery and indexing phase.
func (o *DefaultOrchestrator) RunIndexingPhase(ctx context.Context, manifest *manifest.Manifest) (*indexer.IndexResult, error) {
	// Configure indexer from manifest and config
	indexConfig := indexer.IndexConfig{
		Targets:         o.config.Targets,
		Globs:           o.config.Globs,
		BaseDir:         o.config.ProjectRoot,
		MaxWorkers:      o.config.MaxWorkers,
		MaxFiles:        o.config.MaxFiles,
		CacheEnabled:    o.config.CacheConfig.CacheEnabled,
		CacheKind:       o.config.CacheConfig.CacheKind,
		VendorWhitelist: manifest.Scan.VendorWhitelist,
	}

	// Initialize file indexer if not already done
	if o.registry.FileIndexer == nil {
		fileIndexer := indexer.NewDefaultFileIndexer()
		o.registry.FileIndexer = fileIndexer
	}

	result, err := o.registry.FileIndexer.IndexFiles(ctx, indexConfig)
	if err != nil {
		return nil, fmt.Errorf("file indexing failed: %w", err)
	}

	return result, nil
}

// RunParsingPhase executes the PHP parsing phase.
func (o *DefaultOrchestrator) RunParsingPhase(ctx context.Context, files []indexer.FileInfo) (*ParseResults, error) {
	startTime := time.Now()

	// Initialize PHP parser if not already done
	if o.registry.PHPParser == nil {
		config := o.config.ParserConfig
		phpParser, err := parser.NewPHPParser(&config)
		if err != nil {
			return nil, fmt.Errorf("failed to create PHP parser: %w", err)
		}
		o.registry.PHPParser = phpParser
	}

	// The PHPParser.ProcessFile method handles construct extraction internally

	results := &ParseResults{
		ParsedFiles: make([]ParsedFile, 0, len(files)),
		FailedFiles: make([]FailedFile, 0),
		Classes:     make([]parser.PHPClass, 0),
		Methods:     make([]parser.PHPMethod, 0),
		Namespaces:  make([]parser.PHPNamespace, 0),
		Traits:      make([]parser.PHPTrait, 0),
		Interfaces:  make([]parser.PHPInterface, 0),
	}

	// Process each file using the PHPParser.ProcessFile method
	for _, file := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Use the ProcessFile method which integrates with the file indexer properly
		parseResult, err := o.registry.PHPParser.ProcessFile(ctx, file)
		if err != nil {
			// Log error and continue with other files
			failedFile := FailedFile{
				FilePath: file.Path,
				Error:    err,
			}
			results.FailedFiles = append(results.FailedFiles, failedFile)
			results.ParseErrors++
			continue
		}

		// Process successful result
		if parseResult != nil {
			// Extract file path from result
			if resultMap, ok := parseResult.(*map[string]interface{}); ok {
				if filePath, ok := (*resultMap)["filePath"].(string); ok {
					parsedFile := ParsedFile{
						FilePath:     filePath,
						RelativePath: filePath, // TODO: compute relative path properly
						Namespace:    "",       // TODO: extract from result
					}
					results.ParsedFiles = append(results.ParsedFiles, parsedFile)
				}
			}
		}

		results.FilesProcessed++
	}

	// Finalize results
	results.ParseDuration = time.Since(startTime)

	// Sort all constructs for deterministic output
	o.sortParseResults(results)

	return results, nil
}

// RunMatchingPhase executes the pattern matching phase.
func (o *DefaultOrchestrator) RunMatchingPhase(ctx context.Context, parseResults *ParseResults) (*MatchResults, error) {
	startTime := time.Now()

	// Initialize pattern matcher if not already done
	if o.registry.PatternMatcher == nil {
		matcher, err := o.createPatternMatcher()
		if err != nil {
			return nil, fmt.Errorf("failed to create pattern matcher: %w", err)
		}
		o.registry.PatternMatcher = matcher
	}

	results := &MatchResults{
		FilePatterns:        make(map[string]*matchers.LaravelPatterns),
		HTTPStatusMatches:   make([]*matchers.HTTPStatusMatch, 0),
		RequestUsageMatches: make([]*matchers.RequestUsageMatch, 0),
		ResourceMatches:     make([]*matchers.ResourceMatch, 0),
		PivotMatches:        make([]*matchers.PivotMatch, 0),
		AttributeMatches:    make([]*matchers.AttributeMatch, 0),
		ScopeMatches:        make([]*matchers.ScopeMatch, 0),
		PolymorphicMatches:  make([]*matchers.PolymorphicMatch, 0),
		BroadcastMatches:    make([]*matchers.BroadcastMatch, 0),
	}

	// Sort parsed files by path for deterministic processing
	sort.Slice(parseResults.ParsedFiles, func(i, j int) bool {
		return parseResults.ParsedFiles[i].FilePath < parseResults.ParsedFiles[j].FilePath
	})

	// Process each parsed file
	for _, parsedFile := range parseResults.ParsedFiles {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// We need to reconstruct the syntax tree for pattern matching
		// In a full implementation, we'd pass the tree from parsing phase
		syntaxTree, err := o.parseSyntaxTree(ctx, parsedFile.FilePath)
		if err != nil {
			continue // Skip files that can't be parsed for matching
		}

		patterns, err := o.registry.PatternMatcher.MatchAll(ctx, syntaxTree, parsedFile.FilePath)
		if err != nil {
			continue // Skip files with matching errors
		}

		results.FilePatterns[parsedFile.FilePath] = patterns

		// Aggregate matches by type
		results.HTTPStatusMatches = append(results.HTTPStatusMatches, patterns.HTTPStatus...)
		results.RequestUsageMatches = append(results.RequestUsageMatches, patterns.RequestUsage...)
		results.ResourceMatches = append(results.ResourceMatches, patterns.Resources...)
		results.PivotMatches = append(results.PivotMatches, patterns.Pivots...)
		results.AttributeMatches = append(results.AttributeMatches, patterns.Attributes...)
		results.ScopeMatches = append(results.ScopeMatches, patterns.Scopes...)
		results.PolymorphicMatches = append(results.PolymorphicMatches, patterns.Polymorphics...)
		results.BroadcastMatches = append(results.BroadcastMatches, patterns.Broadcasts...)

		results.FilesMatched++
	}

	// Sort file patterns by path for deterministic processing
	o.sortFilePatterns(results)

	// Calculate total matches
	results.TotalMatches = len(results.HTTPStatusMatches) + len(results.RequestUsageMatches) +
		len(results.ResourceMatches) + len(results.PivotMatches) + len(results.AttributeMatches) +
		len(results.ScopeMatches) + len(results.PolymorphicMatches) + len(results.BroadcastMatches)

	results.MatchingDuration = time.Since(startTime)
	return results, nil
}

// RunInferencePhase executes the shape inference phase.
func (o *DefaultOrchestrator) RunInferencePhase(ctx context.Context, matchResults *MatchResults) (*InferenceResults, error) {
	startTime := time.Now()

	// Initialize shape inferencer if not already done
	if o.registry.ShapeInferencer == nil {
		contentTypeDetector := infer.NewContentTypeDetector(o.config.InferenceConfig)
		keyPathParser := infer.NewKeyPathParser(o.config.InferenceConfig)
		propertyMerger := infer.NewPropertyMerger(o.config.InferenceConfig)

		o.registry.ShapeInferencer = infer.NewShapeInferencer(
			contentTypeDetector,
			keyPathParser,
			propertyMerger,
			o.config.InferenceConfig,
		)
	}

	results := &InferenceResults{
		RequestShapes:        make(map[string]*infer.RequestInfo),
		ConsolidatedRequests: make(map[string]*infer.ConsolidatedRequest),
	}

	// Group request usage patterns by controller method
	methodPatterns := o.groupRequestPatternsByMethod(matchResults.RequestUsageMatches)

	// Sort method keys for deterministic processing
	methodKeys := make([]string, 0, len(methodPatterns))
	for key := range methodPatterns {
		methodKeys = append(methodKeys, key)
	}
	sort.Strings(methodKeys)

	// Infer shapes for each controller method
	for _, methodKey := range methodKeys {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		patterns := methodPatterns[methodKey]

		// Convert from matchers format to infer format
		inferPatterns := make([]matchers.RequestUsageMatch, len(patterns))
		for i, p := range patterns {
			inferPatterns[i] = *p
		}

		// Perform inference with proper error handling
		requestInfo, err := o.registry.ShapeInferencer.InferRequestShape(inferPatterns)
		if err != nil {
			// Log inference error but continue with other methods
			continue
		}

		if requestInfo != nil {
			results.RequestShapes[methodKey] = requestInfo
			results.ShapesInferred++
		}

		// Try to get consolidated patterns as well
		consolidated, err := o.registry.ShapeInferencer.ConsolidatePatterns(inferPatterns)
		if err == nil && consolidated != nil {
			results.ConsolidatedRequests[methodKey] = consolidated
		}

		results.PatternsProcessed += len(patterns)
	}

	results.InferenceDuration = time.Since(startTime)
	return results, nil
}

// SetConfiguration updates the pipeline configuration.
func (o *DefaultOrchestrator) SetConfiguration(config *PipelineConfig) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.config = config
}

// GetConfiguration returns the current pipeline configuration.
func (o *DefaultOrchestrator) GetConfiguration() *PipelineConfig {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.config
}

// GetProgress returns the current pipeline progress.
func (o *DefaultOrchestrator) GetProgress() *PipelineProgress {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.progress
}

// GetStats returns the current pipeline statistics.
func (o *DefaultOrchestrator) GetStats() *PipelineStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.stats
}

// SetProgressCallback sets a callback for progress updates.
func (o *DefaultOrchestrator) SetProgressCallback(callback func(*PipelineProgress)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.progressCallback = callback
}

// Close releases all orchestrator resources.
func (o *DefaultOrchestrator) Close() error {
	if o.registry != nil {
		return o.registry.Close()
	}
	return nil
}

// Helper methods

// initializeComponents initializes all pipeline components.
func (o *DefaultOrchestrator) initializeComponents() error {
	// Initialize PSR-4 resolver
	if o.registry.PSR4Resolver == nil {
		composerPath := filepath.Join(o.config.ProjectRoot, "composer.json")
		if o.config.ComposerPath != "" {
			composerPath = o.config.ComposerPath
		}

		config := &psr4.ResolverConfig{
			ProjectRoot:  o.config.ProjectRoot,
			ComposerPath: composerPath,
			IncludeDev:   true,
			CacheEnabled: true,
			CacheSize:    1000,
		}

		resolver, err := psr4.NewPSR4Resolver(config)
		if err != nil {
			// PSR-4 resolver is optional, continue without it
			resolver = nil
		}
		o.registry.PSR4Resolver = resolver
	}

	// Initialize parser configuration if needed
	if o.registry.ParserConfig == nil {
		o.registry.ParserConfig = &o.config.ParserConfig
	}

	// Initialize matcher configuration if needed
	if o.registry.MatcherConfig == nil {
		o.registry.MatcherConfig = o.config.MatcherConfig
	}

	// Initialize inference configuration if needed
	if o.registry.InferenceConfig == nil {
		o.registry.InferenceConfig = o.config.InferenceConfig
	}

	return nil
}

// createPatternMatcher creates and configures a composite pattern matcher.
func (o *DefaultOrchestrator) createPatternMatcher() (matchers.CompositePatternMatcher, error) {
	// Initialize tree-sitter language for PHP
	language := parser.GetPHPLanguage()
	if language == nil {
		return nil, fmt.Errorf("failed to load PHP language for tree-sitter")
	}

	// Create matcher configuration with enabled patterns
	config := matchers.DefaultMatcherConfig()
	
	// Apply feature flags from manifest to matcher configuration
	if o.config.MatcherConfig != nil {
		// Use the existing matcher config from pipeline config
		config = o.config.MatcherConfig
	}

	// Create pattern matching processor which initializes all matchers
	processor, err := matchers.NewPatternMatchingProcessor(language, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pattern matching processor: %w", err)
	}

	// Extract the composite matcher from the processor
	// The processor contains a composite matcher that has all individual matchers registered
	composite := processor.GetComposite()
	if composite == nil {
		return nil, fmt.Errorf("pattern matching processor has no composite matcher")
	}

	return composite, nil
}

// parseSyntaxTree parses a file to get its syntax tree for pattern matching.
func (o *DefaultOrchestrator) parseSyntaxTree(ctx context.Context, filePath string) (*parser.SyntaxTree, error) {
	// Initialize PHP parser if not already done
	if o.registry.PHPParser == nil {
		config := parser.DefaultParserConfig()
		phpParser, err := parser.NewPHPParser(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create PHP parser: %w", err)
		}
		o.registry.PHPParser = phpParser
	}

	// Cast to TreeSitterParser for syntax tree access
	treeSitterParser, ok := o.registry.PHPParser.(parser.TreeSitterParser)
	if !ok {
		return nil, fmt.Errorf("PHPParser does not support syntax tree parsing: %T", o.registry.PHPParser)
	}
	
	// Parse the file to get syntax tree
	tree, err := treeSitterParser.ParseFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}

	return tree, nil
}

// extractMethods extracts all methods from a slice of classes.
func extractMethods(classes []parser.PHPClass) []parser.PHPMethod {
	var methods []parser.PHPMethod
	for _, class := range classes {
		methods = append(methods, class.Methods...)
	}
	return methods
}

// parseAndExtractFile parses a single file and extracts PHP constructs.
func (o *DefaultOrchestrator) parseAndExtractFile(ctx context.Context, file indexer.FileInfo, extractor parser.PHPConstructExtractor, results *ParseResults) (time.Duration, error) {
	fileDuration := time.Duration(0)

	// Cast to TreeSitterParser for syntax tree access
	treeSitterParser, ok := o.registry.PHPParser.(parser.TreeSitterParser)
	if !ok {
		return fileDuration, fmt.Errorf("PHPParser does not support syntax tree parsing: %T", o.registry.PHPParser)
	}
	
	// Parse the file with timeout handling
	tree, err := treeSitterParser.ParseFile(ctx, file.Path)
	if err != nil {
		return fileDuration, fmt.Errorf("failed to parse file %s: %w", file.Path, err)
	}
	// Note: SyntaxTree doesn't require explicit cleanup - handled by GC

	// Extract PHP constructs from the syntax tree
	structure, err := extractor.ExtractAllConstructs(tree)
	if err != nil {
		return fileDuration, fmt.Errorf("failed to extract constructs from %s: %w", file.Path, err)
	}

	// Determine namespace using PSR-4 resolver if available
	namespace := ""
	// TODO: Implement ResolveClassFromFile method in PSR4Resolver
	// if o.registry.PSR4Resolver != nil {
	//     if resolved, err := o.registry.PSR4Resolver.ResolveClassFromFile(file.Path); err == nil && resolved != nil {
	//         namespace = resolved.Namespace
	//     }
	// }
	if namespace == "" && structure.Namespace != nil {
		namespace = structure.Namespace.Name
	}

	// Extract Laravel patterns if enabled
	var laravelPatterns *parser.LaravelPatterns
	if o.config.ParserConfig.EnableLaravelPatterns {
		laravelPatterns, err = extractor.ExtractLaravelPatterns(tree)
		if err != nil {
			// Laravel pattern extraction is not critical, continue without it
			laravelPatterns = &parser.LaravelPatterns{}
		}
	}

	// Create parsed file record
	parsedFile := ParsedFile{
		FilePath:        file.AbsPath,
		RelativePath:    file.Path, // Path is already relative from base directory
		Namespace:       namespace,
		FileStructure:   structure,
		LaravelPatterns: laravelPatterns,
		ParsedFromCache: false, // Would need cache integration for this
		ParseDuration:   structure.ParseDuration,
	}

	results.ParsedFiles = append(results.ParsedFiles, parsedFile)

	// Aggregate constructs across all files
	results.Classes = append(results.Classes, structure.Classes...)
	results.Interfaces = append(results.Interfaces, structure.Interfaces...)
	results.Traits = append(results.Traits, structure.Traits...)
	if structure.Namespace != nil {
		results.Namespaces = append(results.Namespaces, *structure.Namespace)
	}

	// Extract methods from classes and add to results
	for _, class := range structure.Classes {
		results.Methods = append(results.Methods, class.Methods...)
	}

	return structure.ParseDuration, nil
}

// sortParseResults sorts all parsed constructs for deterministic output.
func (o *DefaultOrchestrator) sortParseResults(results *ParseResults) {
	// Sort parsed files by path
	sort.Slice(results.ParsedFiles, func(i, j int) bool {
		return results.ParsedFiles[i].FilePath < results.ParsedFiles[j].FilePath
	})

	// Sort failed files by path
	sort.Slice(results.FailedFiles, func(i, j int) bool {
		return results.FailedFiles[i].FilePath < results.FailedFiles[j].FilePath
	})

	// Sort classes by fully qualified name
	sort.Slice(results.Classes, func(i, j int) bool {
		return results.Classes[i].FullyQualifiedName < results.Classes[j].FullyQualifiedName
	})

	// Sort interfaces by fully qualified name
	sort.Slice(results.Interfaces, func(i, j int) bool {
		return results.Interfaces[i].FullyQualifiedName < results.Interfaces[j].FullyQualifiedName
	})

	// Sort traits by fully qualified name
	sort.Slice(results.Traits, func(i, j int) bool {
		return results.Traits[i].FullyQualifiedName < results.Traits[j].FullyQualifiedName
	})

	// Sort namespaces by name
	sort.Slice(results.Namespaces, func(i, j int) bool {
		return results.Namespaces[i].Name < results.Namespaces[j].Name
	})

	// Sort methods by class and method name
	sort.Slice(results.Methods, func(i, j int) bool {
		if results.Methods[i].ClassName != results.Methods[j].ClassName {
			return results.Methods[i].ClassName < results.Methods[j].ClassName
		}
		return results.Methods[i].Name < results.Methods[j].Name
	})
}

// groupRequestPatternsByMethod groups request usage patterns by controller method.
func (o *DefaultOrchestrator) groupRequestPatternsByMethod(patterns []*matchers.RequestUsageMatch) map[string][]*matchers.RequestUsageMatch {
	grouped := make(map[string][]*matchers.RequestUsageMatch)

	for _, pattern := range patterns {
		// Create a key from controller class and method context
		// In a more sophisticated implementation, this would extract method info from pattern context
		key := o.extractMethodKeyFromPattern(pattern)
		grouped[key] = append(grouped[key], pattern)
	}

	return grouped
}

// extractMethodKeyFromPattern extracts a method key from a request usage pattern.
func (o *DefaultOrchestrator) extractMethodKeyFromPattern(pattern *matchers.RequestUsageMatch) string {
	// RequestUsageMatch doesn't contain context information, so we'll need
	// to use a simplified approach for now. In a real implementation, the
	// matchers would provide context about where the pattern was found.
	
	// For now, create a key based on the methods used in the pattern
	if len(pattern.Methods) > 0 {
		// Use the first method as a key component
		firstMethod := pattern.Methods[0]
		return fmt.Sprintf("Controller::%s", firstMethod)
	}

	// Fallback to a default key
	return "DefaultController::index"
}

// updateProgress updates the pipeline progress and notifies callbacks.
func (o *DefaultOrchestrator) updateProgress(phase PipelinePhase, status string, progress float64) {
	o.mu.Lock()
	o.progress.Phase = phase
	o.progress.PhaseStatus = status
	o.progress.Progress = progress
	callback := o.progressCallback
	o.mu.Unlock()

	if callback != nil {
		callback(o.progress)
	}
}

// handleError creates a pipeline error and updates progress.
func (o *DefaultOrchestrator) handleError(errorType, message string, phase PipelinePhase, cause error) error {
	o.updateProgress(PipelinePhaseFailed, "Pipeline failed: "+message, 0.0)
	return NewPipelineError(errorType, message, phase, "", cause)
}

// shouldMarkPartial determines if processing should be marked as partial.
func (o *DefaultOrchestrator) shouldMarkPartial() bool {
	// Mark as partial if we have parsing errors or other issues
	if o.results.ParseResults != nil && o.results.ParseResults.ParseErrors > 0 {
		return true
	}
	return false
}

// collectTruncationReasons collects reasons why processing was truncated.
func (o *DefaultOrchestrator) collectTruncationReasons(indexResult *indexer.IndexResult) {
	if indexResult.Partial {
		o.results.TruncatedBy = append(o.results.TruncatedBy, "file_limit_exceeded")
	}
	if o.results.ParseResults != nil && o.results.ParseResults.ParseErrors > 0 {
		o.results.TruncatedBy = append(o.results.TruncatedBy, "parse_errors")
	}

	// Sort for determinism
	sort.Strings(o.results.TruncatedBy)
}

// sortFilePatterns ensures files are processed in deterministic order.
func (o *DefaultOrchestrator) sortFilePatterns(results *MatchResults) {
	// Since matches are appended in file processing order, we need to ensure
	// that files are processed deterministically. The file indexer should already
	// provide files in sorted order, but we may need additional sorting here.
	// For now, this is a placeholder - the real fix is ensuring the indexer
	// provides deterministic file ordering.
}
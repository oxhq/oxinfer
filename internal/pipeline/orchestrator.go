// Package pipeline provides orchestration for the complete oxinfer analysis pipeline.
package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/indexer"
	"github.com/oxhq/oxinfer/internal/infer"
	"github.com/oxhq/oxinfer/internal/logging"
	"github.com/oxhq/oxinfer/internal/manifest"
	"github.com/oxhq/oxinfer/internal/matchers"
	"github.com/oxhq/oxinfer/internal/parser"
	"github.com/oxhq/oxinfer/internal/pipeline/optimizations"
	"github.com/oxhq/oxinfer/internal/psr4"
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

	// Initialize Laravel parser pool for thread-safe parsing
	if o.registry.LaravelParser == nil {
		laravelParser, err := parser.NewLaravelParser(false)
		if err != nil {
			return nil, fmt.Errorf("failed to create Laravel parser: %w", err)
		}
		o.registry.LaravelParser = laravelParser
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
		ModelScopes: make(map[string][]string),
	}

	// Right-size parsing workers for CPU-bound workload
	cpus := runtime.NumCPU()
	parseWorkerCount := int(float64(cpus) * 1.5)
	if parseWorkerCount < cpus {
		parseWorkerCount = cpus
	}
	if o.config.MaxWorkers > 0 && parseWorkerCount > o.config.MaxWorkers {
		parseWorkerCount = o.config.MaxWorkers
	}
	if parseWorkerCount > len(files) {
		parseWorkerCount = len(files) // Don't create more workers than files
	}
	if parseWorkerCount < 2 {
		parseWorkerCount = 2
	}

	// Create a parser pool for thread-safe concurrent parsing
	parserPool := make(chan *parser.LaravelParser, parseWorkerCount)
	allParsers := make([]*parser.LaravelParser, 0, parseWorkerCount) // Keep track of all parsers
	for i := 0; i < parseWorkerCount; i++ {
		// Create individual parser for each worker to avoid concurrent access
		laravelParser, err := parser.NewLaravelParser(false)
		if err != nil {
			return nil, fmt.Errorf("failed to create Laravel parser for pool: %w", err)
		}
		parserPool <- laravelParser
		allParsers = append(allParsers, laravelParser) // Store for later aggregation
	}

	// Channels for parallel parsing
	fileJobs := make(chan indexer.FileInfo, len(files))
	parseResults := make(chan struct {
		ParsedFile *ParsedFile
		FailedFile *FailedFile
	}, len(files))

	// Launch parsing workers
	var parseWg sync.WaitGroup
	for i := 0; i < parseWorkerCount; i++ {
		parseWg.Add(1)
		go func() {
			defer parseWg.Done()

			// Get a parser from the pool for this worker
			workerParser := <-parserPool
			defer func() {
				// Return parser to pool when worker finishes
				parserPool <- workerParser
			}()

			for file := range fileJobs {
				select {
				case <-ctx.Done():
					parseResults <- struct {
						ParsedFile *ParsedFile
						FailedFile *FailedFile
					}{
						FailedFile: &FailedFile{
							FilePath: file.Path,
							Error:    ctx.Err(),
						},
					}
					return
				default:
				}

				// Parse Laravel patterns using absolute path and get syntax tree
				// Each worker uses its own parser instance for thread safety
				syntaxTree, err := workerParser.ParseFile(ctx, file.AbsPath)
				if err != nil {
					parseResults <- struct {
						ParsedFile *ParsedFile
						FailedFile *FailedFile
					}{
						FailedFile: &FailedFile{
							FilePath: file.Path,
							Error:    err,
						},
					}
					continue
				}

				parseResults <- struct {
					ParsedFile *ParsedFile
					FailedFile *FailedFile
				}{
					ParsedFile: &ParsedFile{
						FilePath:     file.Path,    // Keep relative for display
						AbsPath:      file.AbsPath, // Add absolute for file operations
						RelativePath: file.Path,
						SyntaxTree:   syntaxTree, // Store syntax tree to avoid reparsing
					},
				}
			}
		}()
	}

	// Submit all files for parsing; large files first to reduce tail latency
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	for _, file := range files {
		fileJobs <- file
	}
	close(fileJobs)

	// Close results channel when all workers complete
	go func() {
		parseWg.Wait()
		close(parseResults)
	}()

	// Collect results from all parsing workers
	for result := range parseResults {
		if result.FailedFile != nil {
			results.FailedFiles = append(results.FailedFiles, *result.FailedFile)
			results.ParseErrors++
		} else if result.ParsedFile != nil {
			results.ParsedFiles = append(results.ParsedFiles, *result.ParsedFile)
			results.FilesProcessed++
		}
	}

	// Finalize results
	results.ParseDuration = time.Since(startTime)

	// Aggregate model scopes, controllers, and models from all worker parsers
	aggregatedModelScopes := make(map[string][]string)
	aggregatedControllers := make(map[string][]string)
	aggregatedModels := make(map[string]parser.ModelInfo)

	for _, workerParser := range allParsers {
		// Merge model scopes
		for fqcn, scopes := range workerParser.GetModelScopes() {
			if existing, exists := aggregatedModelScopes[fqcn]; exists {
				// Merge scopes, avoiding duplicates
				for _, scope := range scopes {
					found := false
					for _, existingScope := range existing {
						if existingScope == scope {
							found = true
							break
						}
					}
					if !found {
						existing = append(existing, scope)
					}
				}
				aggregatedModelScopes[fqcn] = existing
			} else {
				aggregatedModelScopes[fqcn] = scopes
			}
		}

		// Merge controllers
		for fqcn, methods := range workerParser.GetControllers() {
			if existing, exists := aggregatedControllers[fqcn]; exists {
				// Merge methods, avoiding duplicates
				for _, method := range methods {
					found := false
					for _, existingMethod := range existing {
						if existingMethod == method {
							found = true
							break
						}
					}
					if !found {
						existing = append(existing, method)
					}
				}
				aggregatedControllers[fqcn] = existing
			} else {
				aggregatedControllers[fqcn] = methods
			}
		}

		// Merge models
		for fqcn, modelInfo := range workerParser.GetModels() {
			aggregatedModels[fqcn] = modelInfo // Models are unique by FQCN
		}
	}

	// Optional PSR-4 validation (non-fatal): verify classes resolve via resolver
	if r := o.registry.PSR4Resolver; r != nil && !reflect.ValueOf(r).IsNil() {
		// Validate models
		for fqcn, mi := range aggregatedModels {
			if _, err := r.ResolveClass(ctx, fqcn); err != nil {
				// Keep non-fatal: log and continue; do not include clearly invalid
				logging.VerboseFromContext(ctx, "orchestrator", "PSR-4 failed for model %s (%s): %v", fqcn, mi.FilePath, err)
			}
		}
		// Validate controllers
		for fqcn := range aggregatedControllers {
			if _, err := r.ResolveClass(ctx, fqcn); err != nil {
				logging.VerboseFromContext(ctx, "orchestrator", "PSR-4 failed for controller %s: %v", fqcn, err)
			}
		}
	}

	// Set the aggregated results
	results.ModelScopes = aggregatedModelScopes
	results.Controllers = aggregatedControllers
	results.Models = aggregatedModels

	// Sort all constructs for deterministic output
	o.sortParseResults(results)

	return results, nil
}

// RunMatchingPhase executes the pattern matching phase.
func (o *DefaultOrchestrator) RunMatchingPhase(ctx context.Context, parseResults *ParseResults) (*MatchResults, error) {
	startTime := time.Now()

	// Inject config into context for component access
	if o.config != nil {
		ctx = context.WithValue(ctx, "oxinfer.config", o.config)
	}

	// Initialize pattern matcher FIRST to ensure ScopeRegistry exists
	if o.registry.PatternMatcher == nil {
		matcher, err := o.createPatternMatcher()
		if err != nil {
			return nil, fmt.Errorf("failed to create pattern matcher: %w", err)
		}
		o.registry.PatternMatcher = matcher
	}

	// THEN populate ScopeRegistry from ParseResults
	if parseResults.ModelScopes != nil && o.registry.ScopeRegistry != nil {
		for modelFQCN, scopes := range parseResults.ModelScopes {
			for _, scopeName := range scopes {
				o.registry.ScopeRegistry.AddModelScope(modelFQCN, scopeName)
			}
		}
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

	// Use parallel pattern matching for aggressive performance
	parsedFiles := make([]*optimizations.ParsedFile, 0, len(parseResults.ParsedFiles))
	for _, pf := range parseResults.ParsedFiles {
		if pf.SyntaxTree != nil {
			parsedFiles = append(parsedFiles, &optimizations.ParsedFile{
				FilePath:   pf.FilePath,
				SyntaxTree: pf.SyntaxTree,
			})
		}
	}

	// Create parallel pattern matcher with aggressive worker count
	maxWorkers := runtime.NumCPU() * 2
	if maxWorkers < 8 {
		maxWorkers = 8 // Minimum aggressive parallelism
	}
	parallelMatcher := optimizations.NewParallelPatternMatcher(o.registry.PatternMatcher, maxWorkers)

	// Process all files in parallel
	filePatterns, err := parallelMatcher.MatchAllFiles(ctx, parsedFiles)
	if err != nil {
		return nil, fmt.Errorf("parallel pattern matching failed: %w", err)
	}

	// Aggregate results using the parallel matcher's aggregation
	aggregated := parallelMatcher.AggregateResults(filePatterns)

	// Transfer aggregated results to match results
	results.FilePatterns = aggregated.FilePatterns
	results.HTTPStatusMatches = aggregated.HTTPStatusMatches
	results.RequestUsageMatches = aggregated.RequestUsageMatches
	results.ResourceMatches = aggregated.ResourceMatches
	results.PivotMatches = aggregated.PivotMatches
	results.AttributeMatches = aggregated.AttributeMatches
	results.ScopeMatches = aggregated.ScopeMatches
	results.PolymorphicMatches = aggregated.PolymorphicMatches
	results.BroadcastMatches = aggregated.BroadcastMatches
	results.FilesMatched = int(aggregated.FilesMatched)

	// Sort file patterns by path for deterministic processing
	o.sortFilePatterns(results)

	// Calculate total matches
	results.TotalMatches = int(aggregated.TotalMatches)

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

// ClearCaches clears all internal caches and resets state.
func (o *DefaultOrchestrator) ClearCaches() {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Clear results and stats
	o.results = &PipelineResults{}
	o.stats = &PipelineStats{}
	o.progress = &PipelineProgress{
		Phase: PipelinePhaseInitializing,
	}

	// Clear component registry caches
	if o.registry != nil {
		o.registry.ClearCaches()
	}
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
	// Initialize ScopeRegistry first (shared between parser and matcher)
	if o.registry.ScopeRegistry == nil {
		o.registry.ScopeRegistry = matchers.NewScopeRegistry()
	}

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

	// Set the shared scope registry
	if o.registry.ScopeRegistry != nil {
		processor.SetScopeRegistry(o.registry.ScopeRegistry)
	}

	// Extract the composite matcher from the processor
	// The processor contains a composite matcher that has all individual matchers registered
	composite := processor.GetComposite()

	return composite, nil
}

// parseSyntaxTree parses a file to get its syntax tree for pattern matching.
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
		if key, ok := o.extractMethodKeyFromPattern(pattern); ok {
			grouped[key] = append(grouped[key], pattern)
		}
		// Skip patterns that cannot be resolved to valid method keys
	}

	return grouped
}

// extractMethodKeyFromPattern extracts a method key from a request usage pattern.
// Returns empty string and false if cannot resolve to a valid method key.
func (o *DefaultOrchestrator) extractMethodKeyFromPattern(pattern *matchers.RequestUsageMatch) (string, bool) {
	// RequestUsageMatch doesn't contain sufficient context information for reliable extraction.
	// In a real implementation, the matchers would provide context about where the pattern was found,
	// including controller class name and method name from the AST context.

	// Without proper AST context, we cannot generate valid Controller::method keys.
	// Rather than inventing placeholder keys, skip these patterns cleanly.

	// TODO: Enhance RequestUsageMatch to include controller context from tree-sitter analysis
	// The matchers should extract and provide:
	// - Controller FQCN from class declaration AST node
	// - Method name from method declaration AST node
	// - File path context for proper FQCN resolution
	// This requires coordination between pattern matching and AST context extraction.

	return "", false
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
	// Normalise per-file pattern slices so downstream consumers see deterministic
	// ordering regardless of parallel execution.
	for _, patterns := range results.FilePatterns {
		optimizations.NormalizeLaravelPatterns(patterns)
	}
}

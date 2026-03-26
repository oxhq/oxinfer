// Package matchers provides integration between pattern matchers and the parser/emitter system.
package matchers

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultCompositePatternMatcher orchestrates multiple pattern matchers.
type DefaultCompositePatternMatcher struct {
	matchers map[PatternType]PatternMatcher
	config   *MatcherConfig
	language *sitter.Language
	stats    *ProcessingStats
}

// NewCompositePatternMatcher creates a new composite pattern matcher.
func NewCompositePatternMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultCompositePatternMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	return &DefaultCompositePatternMatcher{
		matchers: make(map[PatternType]PatternMatcher),
		config:   config,
		language: language,
		stats:    &ProcessingStats{},
	}, nil
}

// AddMatcher registers a new pattern matcher.
func (c *DefaultCompositePatternMatcher) AddMatcher(matcher PatternMatcher) error {
	if matcher == nil {
		return fmt.Errorf("matcher cannot be nil")
	}

	patternType := matcher.GetType()
	c.matchers[patternType] = matcher
	return nil
}

// RemoveMatcher unregisters a pattern matcher by type.
func (c *DefaultCompositePatternMatcher) RemoveMatcher(patternType PatternType) error {
	if matcher, exists := c.matchers[patternType]; exists {
		matcher.Close() // Clean up resources
		delete(c.matchers, patternType)
		return nil
	}
	return fmt.Errorf("matcher of type %s not found", patternType)
}

// MatchAll runs all registered matchers on the syntax tree with aggressive parallelization.
func (c *DefaultCompositePatternMatcher) MatchAll(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*LaravelPatterns, error) {
	if tree == nil {
		return nil, fmt.Errorf("syntax tree cannot be nil")
	}

	startTime := time.Now()
	patterns := &LaravelPatterns{
		FilePath:     filePath,
		HTTPStatus:   make([]*HTTPStatusMatch, 0),
		RequestUsage: make([]*RequestUsageMatch, 0),
		Resources:    make([]*ResourceMatch, 0),
		Pivots:       make([]*PivotMatch, 0),
		Attributes:   make([]*AttributeMatch, 0),
		Scopes:       make([]*ScopeMatch, 0),
		Polymorphics: make([]*PolymorphicMatch, 0),
		Broadcasts:   make([]*BroadcastMatch, 0),
		ProcessedAt:  startTime.Unix(),
	}

	// Collect enabled matchers for parallel execution
	var enabledMatchers []struct {
		patternType PatternType
		matcher     PatternMatcher
	}

	for patternType, matcher := range c.matchers {
		if c.isMatcherEnabled(patternType) {
			enabledMatchers = append(enabledMatchers, struct {
				patternType PatternType
				matcher     PatternMatcher
			}{patternType, matcher})
		}
	}

	// Early return if no matchers enabled
	if len(enabledMatchers) == 0 {
		processingTime := time.Since(startTime)
		patterns.ProcessingMs = processingTime.Milliseconds()
		c.stats.FilesProcessed++
		return patterns, nil
	}

	// Determine worker count - use configured limit but cap at number of enabled matchers
	maxWorkers := c.config.MaxConcurrentMatchers
	if maxWorkers > len(enabledMatchers) {
		maxWorkers = len(enabledMatchers)
	}

	// Result structures for parallel execution
	type matcherResult struct {
		patternType PatternType
		results     []*MatchResult
		err         error
	}

	// Create buffered channels for aggressive throughput
	matcherJobs := make(chan struct {
		patternType PatternType
		matcher     PatternMatcher
	}, len(enabledMatchers))

	matcherResults := make(chan matcherResult, len(enabledMatchers))

	// Launch worker goroutines
	for i := 0; i < maxWorkers; i++ {
		go func() {
			for job := range matcherJobs {
				select {
				case <-ctx.Done():
					matcherResults <- matcherResult{
						patternType: job.patternType,
						err:         ctx.Err(),
					}
					return
				default:
				}

				// Execute matcher with timeout
				matchCtx, cancel := context.WithTimeout(ctx, time.Duration(c.config.MatchTimeoutMs)*time.Millisecond)
				results, err := job.matcher.Match(matchCtx, tree, filePath)
				cancel()

				matcherResults <- matcherResult{
					patternType: job.patternType,
					results:     results,
					err:         err,
				}
			}
		}()
	}

	// Submit all jobs
	for _, enabledMatcher := range enabledMatchers {
		matcherJobs <- struct {
			patternType PatternType
			matcher     PatternMatcher
		}{enabledMatcher.patternType, enabledMatcher.matcher}
	}
	close(matcherJobs)

	// Collect results from all workers
	var totalMatches int64
	for i := 0; i < len(enabledMatchers); i++ {
		result := <-matcherResults

		if result.err != nil {
			// Log error but continue with other matchers
			continue
		}

		// Process results by type (sequential aggregation to avoid race conditions)
		c.processMatchResults(result.patternType, result.results, patterns)
		totalMatches += int64(len(result.results))
	}

	// Update processing statistics
	processingTime := time.Since(startTime)
	patterns.ProcessingMs = processingTime.Milliseconds()
	c.stats.FilesProcessed++
	c.stats.ProcessingTimeMs += processingTime.Milliseconds()
	c.stats.TotalMatches += totalMatches

	// Update pattern detection count
	patternCount := int64(len(patterns.HTTPStatus) + len(patterns.RequestUsage) + len(patterns.Resources) +
		len(patterns.Pivots) + len(patterns.Attributes) + len(patterns.Scopes) + len(patterns.Polymorphics) + len(patterns.Broadcasts))
	c.stats.PatternsDetected += patternCount

	return patterns, nil
}

// GetMatchers returns all registered matchers.
func (c *DefaultCompositePatternMatcher) GetMatchers() map[PatternType]PatternMatcher {
	// Return copy to prevent external modification
	matchers := make(map[PatternType]PatternMatcher)
	maps.Copy(matchers, c.matchers)
	return matchers
}

// IsInitialized returns true if all matchers are ready.
func (c *DefaultCompositePatternMatcher) IsInitialized() bool {
	for _, matcher := range c.matchers {
		if !matcher.IsInitialized() {
			return false
		}
	}
	return true
}

// Close releases resources from all matchers.
func (c *DefaultCompositePatternMatcher) Close() error {
	var firstError error

	for _, matcher := range c.matchers {
		if err := matcher.Close(); err != nil && firstError == nil {
			firstError = err
		}
	}

	c.matchers = make(map[PatternType]PatternMatcher)
	return firstError
}

// isMatcherEnabled checks if a matcher type is enabled in configuration.
func (c *DefaultCompositePatternMatcher) isMatcherEnabled(patternType PatternType) bool {
	switch patternType {
	case PatternTypeHTTPStatus:
		return c.config.EnableHTTPStatusMatching
	case PatternTypeRequestUsage:
		return c.config.EnableRequestUsageMatching || c.config.EnableRequestMatching
	case PatternTypeResource:
		return c.config.EnableResourceMatching
	case PatternTypePivot:
		return c.config.EnablePivotMatching
	case PatternTypeAttribute:
		return c.config.EnableAttributeMatching
	case PatternTypeScope:
		return c.config.EnableScopeMatching
	case PatternTypePolymorphic:
		return c.config.EnablePolymorphicMatching
	case PatternTypeBroadcast:
		return c.config.EnableBroadcastMatching
	default:
		return false // Unknown pattern types are disabled by default
	}
}

// processMatchResults processes matcher results and adds them to patterns.
func (c *DefaultCompositePatternMatcher) processMatchResults(patternType PatternType, results []*MatchResult, patterns *LaravelPatterns) {
	for _, result := range results {
		switch patternType {
		case PatternTypeHTTPStatus:
			if httpMatch, ok := result.Data.(*HTTPStatusMatch); ok {
				patterns.HTTPStatus = append(patterns.HTTPStatus, httpMatch)
			}
		case PatternTypeRequestUsage:
			if reqMatch, ok := result.Data.(*RequestUsageMatch); ok {
				patterns.RequestUsage = append(patterns.RequestUsage, reqMatch)
			}
		case PatternTypeResource:
			if resMatch, ok := result.Data.(*ResourceMatch); ok {
				patterns.Resources = append(patterns.Resources, resMatch)
			}
		case PatternTypePivot:
			if pivotMatch, ok := result.Data.(*PivotMatch); ok {
				patterns.Pivots = append(patterns.Pivots, pivotMatch)
			}
		case PatternTypeAttribute:
			if attrMatch, ok := result.Data.(*AttributeMatch); ok {
				patterns.Attributes = append(patterns.Attributes, attrMatch)
			}
		case PatternTypeScope:
			if scopeMatch, ok := result.Data.(*ScopeMatch); ok {
				patterns.Scopes = append(patterns.Scopes, scopeMatch)
			}
		case PatternTypePolymorphic:
			if polyMatch, ok := result.Data.(*PolymorphicMatch); ok {
				patterns.Polymorphics = append(patterns.Polymorphics, polyMatch)
			}
		case PatternTypeBroadcast:
			if broadcastMatch, ok := result.Data.(*BroadcastMatch); ok {
				patterns.Broadcasts = append(patterns.Broadcasts, broadcastMatch)
			}
		}
	}
}

// DefaultPatternMatchingProcessor integrates pattern matching with parser and emitter.
type DefaultPatternMatchingProcessor struct {
	composite     *DefaultCompositePatternMatcher
	config        *MatcherConfig
	stats         *ProcessingStats
	scopeRegistry *ScopeRegistry
}

// NewPatternMatchingProcessor creates a new pattern matching processor.
func NewPatternMatchingProcessor(language *sitter.Language, config *MatcherConfig) (*DefaultPatternMatchingProcessor, error) {
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create composite matcher: %w", err)
	}

	// Initialize enabled matchers
	processor := &DefaultPatternMatchingProcessor{
		composite:     composite,
		config:        config,
		stats:         &ProcessingStats{},
		scopeRegistry: nil, // Will be set via SetScopeRegistry
	}

	if err := processor.initializeMatchers(language, config); err != nil {
		return nil, fmt.Errorf("failed to initialize matchers: %w", err)
	}

	return processor, nil
}

// SetScopeRegistry sets the scope registry for scope matching.
func (p *DefaultPatternMatchingProcessor) SetScopeRegistry(registry *ScopeRegistry) {
	p.scopeRegistry = registry
	// Update the scope matcher if it exists
	if matchers := p.composite.GetMatchers(); matchers != nil {
		if scopeMatcher, ok := matchers[PatternTypeScope]; ok {
			if sm, ok := scopeMatcher.(*DefaultScopeMatcher); ok {
				sm.SetScopeRegistry(registry)
			}
		}
	}
}

// initializeMatchers creates and registers enabled matchers.
func (p *DefaultPatternMatchingProcessor) initializeMatchers(language *sitter.Language, config *MatcherConfig) error {
	// Initialize HTTP status matcher if enabled
	if config.EnableHTTPStatusMatching {
		httpMatcher, err := NewHTTPStatusMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create HTTP status matcher: %w", err)
		}
		if err := p.composite.AddMatcher(httpMatcher); err != nil {
			return fmt.Errorf("failed to add HTTP status matcher: %w", err)
		}
	}

	// Initialize request usage matcher if enabled
	if config.EnableRequestUsageMatching || config.EnableRequestMatching {
		requestMatcher, err := NewRequestUsageMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create request usage matcher: %w", err)
		}
		if err := p.composite.AddMatcher(requestMatcher); err != nil {
			return fmt.Errorf("failed to add request usage matcher: %w", err)
		}
	}

	// Initialize resource matcher if enabled
	if config.EnableResourceMatching {
		resourceMatcher, err := NewResourceMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create resource matcher: %w", err)
		}
		if err := p.composite.AddMatcher(resourceMatcher); err != nil {
			return fmt.Errorf("failed to add resource matcher: %w", err)
		}
	}

	// Initialize pivot relationship pattern matchers if enabled

	// Pivot matcher
	if config.EnablePivotMatching {
		pivotMatcher, err := NewPivotMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create pivot matcher: %w", err)
		}
		if err := p.composite.AddMatcher(pivotMatcher); err != nil {
			return fmt.Errorf("failed to add pivot matcher: %w", err)
		}
	}

	// Attribute matcher
	if config.EnableAttributeMatching {
		attributeMatcher, err := NewAttributeMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create attribute matcher: %w", err)
		}
		if err := p.composite.AddMatcher(attributeMatcher); err != nil {
			return fmt.Errorf("failed to add attribute matcher: %w", err)
		}
	}

	// Scope matcher
	if config.EnableScopeMatching {
		scopeMatcher, err := NewScopeMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create scope matcher: %w", err)
		}
		if err := p.composite.AddMatcher(scopeMatcher); err != nil {
			return fmt.Errorf("failed to add scope matcher: %w", err)
		}
	}

	// Initialize scope pattern matchers if enabled

	// Polymorphic matcher
	if config.EnablePolymorphicMatching {
		polymorphicMatcher, err := NewPolymorphicMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create polymorphic matcher: %w", err)
		}
		if err := p.composite.AddMatcher(polymorphicMatcher); err != nil {
			return fmt.Errorf("failed to add polymorphic matcher: %w", err)
		}
	}

	// Initialize broadcast channel pattern matchers if enabled

	// Broadcast matcher
	if config.EnableBroadcastMatching {
		broadcastMatcher, err := NewBroadcastMatcher(language, config)
		if err != nil {
			return fmt.Errorf("failed to create broadcast matcher: %w", err)
		}
		if err := p.composite.AddMatcher(broadcastMatcher); err != nil {
			return fmt.Errorf("failed to add broadcast matcher: %w", err)
		}
	}

	return nil
}

// ProcessFile runs pattern matching on a single file.
func (p *DefaultPatternMatchingProcessor) ProcessFile(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*LaravelPatterns, error) {
	if tree == nil {
		return nil, fmt.Errorf("syntax tree cannot be nil")
	}

	patterns, err := p.composite.MatchAll(ctx, tree, filePath)
	if err != nil {
		return nil, fmt.Errorf("pattern matching failed: %w", err)
	}

	return patterns, nil
}

// ConvertToEmitterFormat converts patterns to emitter.Controller format.
func (p *DefaultPatternMatchingProcessor) ConvertToEmitterFormat(patterns *LaravelPatterns) (*emitter.Controller, error) {
	if patterns == nil {
		return nil, fmt.Errorf("patterns cannot be nil")
	}

	// Extract class and method names
	className, classOk := p.extractClassName(patterns)
	methodName, methodOk := p.extractMethodName(patterns)

	// Skip if we cannot extract valid identifiers
	if !classOk || !methodOk {
		return nil, nil // Return nil to indicate this should be skipped
	}

	controller := &emitter.Controller{
		FQCN:   className,
		Method: methodName,
	}

	// Convert HTTP status patterns
	if len(patterns.HTTPStatus) > 0 {
		httpMatch := patterns.HTTPStatus[0] // Use first match for primary status
		controller.HTTP = &emitter.HTTPInfo{
			Status:   &httpMatch.Status,
			Explicit: &httpMatch.Explicit,
		}
	}

	// Convert request usage patterns
	if len(patterns.RequestUsage) > 0 {
		reqMatch := patterns.RequestUsage[0] // Use first match for primary request info
		controller.Request = &emitter.RequestInfo{
			ContentTypes: reqMatch.ContentTypes,
			Body:         emitter.NewOrderedObjectFromMap(reqMatch.Body),
			Query:        emitter.NewOrderedObjectFromMap(reqMatch.Query),
			Files:        emitter.NewOrderedObjectFromMap(reqMatch.Files),
		}
	}

	// Convert resource patterns
	controller.Resources = make([]emitter.Resource, 0, len(patterns.Resources))
	for _, resMatch := range patterns.Resources {
		resource := emitter.Resource{
			Class:      p.shortClassName(resMatch.Class),
			Collection: resMatch.Collection,
		}
		controller.Resources = append(controller.Resources, resource)
	}

	// Convert scope usage patterns to controller.ScopesUsed
	controller.ScopesUsed = make([]emitter.ScopeUsed, 0, len(patterns.Scopes))
	scopeUsageCount := 0

	// Log scope processing if we have scope patterns
	if len(patterns.Scopes) > 0 {
		fmt.Printf("[SCOPE] Processing %d scope patterns for %s::%s\n",
			len(patterns.Scopes), controller.FQCN, controller.Method)
	}

	for _, scopeMatch := range patterns.Scopes {
		// Only include scope usage patterns, not definitions
		if scopeMatch.Pattern == "usage" || scopeMatch.Pattern == "model_usage" {
			scopeUsed := emitter.ScopeUsed{
				On:   scopeMatch.On,
				Name: scopeMatch.Name,
				Args: make([]string, 0, len(scopeMatch.Args)),
			}

			// Convert args from interface{} to string
			for _, arg := range scopeMatch.Args {
				if argStr, ok := arg.(string); ok {
					scopeUsed.Args = append(scopeUsed.Args, argStr)
				}
			}

			controller.ScopesUsed = append(controller.ScopesUsed, scopeUsed)
			scopeUsageCount++

			fmt.Printf("[SCOPE] Added scope usage: On=%s, Name=%s, Args=%v\n",
				scopeUsed.On, scopeUsed.Name, scopeUsed.Args)
		} else {
			fmt.Printf("[SCOPE] Skipped scope pattern: %s (not usage)\n", scopeMatch.Pattern)
		}
	}

	if len(patterns.Scopes) > 0 {
		fmt.Printf("[SCOPE] Final: %d scope usage patterns added to %s::%s\n",
			scopeUsageCount, controller.FQCN, controller.Method)
	}

	// Polymorphic relationships are now handled at top-level by AssemblePolymorphic()
	// No longer attached to individual controllers

	return controller, nil
}

// ConvertToBroadcastFormat converts broadcast patterns to emitter.Broadcast format.
func (p *DefaultPatternMatchingProcessor) ConvertToBroadcastFormat(patterns *LaravelPatterns) ([]emitter.Broadcast, error) {
	if patterns == nil {
		return nil, fmt.Errorf("patterns cannot be nil")
	}

	// Convert broadcast patterns to emitter format
	broadcasts := make([]emitter.Broadcast, 0, len(patterns.Broadcasts))
	for _, broadcastMatch := range patterns.Broadcasts {
		broadcast := emitter.Broadcast{
			Channel:    broadcastMatch.Channel,
			Visibility: broadcastMatch.Visibility,
		}

		// Add parameters if present
		if len(broadcastMatch.Params) > 0 {
			// Preserve literal route order - do NOT sort
			params := make([]string, len(broadcastMatch.Params))
			copy(params, broadcastMatch.Params)
			broadcast.Params = params
		}

		// Add file path if specified
		if broadcastMatch.File != "" {
			broadcast.File = &broadcastMatch.File
		}

		// Add payload literal flag if detected
		if broadcastMatch.PayloadLiteral {
			broadcast.PayloadLiteral = &broadcastMatch.PayloadLiteral
		}

		broadcasts = append(broadcasts, broadcast)
	}

	// Sort broadcasts for deterministic output
	sort.Slice(broadcasts, func(i, j int) bool {
		return broadcasts[i].Channel < broadcasts[j].Channel
	})

	return broadcasts, nil
}

// ConvertToModelFormat converts patterns to emitter.Model format for models.
func (p *DefaultPatternMatchingProcessor) ConvertToModelFormat(patterns *LaravelPatterns) (*emitter.Model, error) {
	if patterns == nil {
		return nil, fmt.Errorf("patterns cannot be nil")
	}

	// Skip if no model-specific patterns found
	if len(patterns.Pivots) == 0 && len(patterns.Attributes) == 0 && len(patterns.Polymorphics) == 0 {
		return nil, nil
	}

	// Extract class name for the model
	className, classOk := p.extractClassName(patterns)
	if !classOk {
		return nil, nil // Skip if cannot extract valid class name
	}

	model := &emitter.Model{
		FQCN: className,
	}

	// Convert pivot patterns
	model.WithPivot = make([]emitter.PivotInfo, 0, len(patterns.Pivots))
	for _, pivotMatch := range patterns.Pivots {
		pivotInfo := emitter.PivotInfo{
			Relation: pivotMatch.Relation,
			Columns:  make([]string, len(pivotMatch.Fields)),
		}
		copy(pivotInfo.Columns, pivotMatch.Fields)

		if pivotMatch.Alias != "" {
			pivotInfo.Alias = &pivotMatch.Alias
		}
		if pivotMatch.Timestamps {
			pivotInfo.Timestamps = &pivotMatch.Timestamps
		}

		model.WithPivot = append(model.WithPivot, pivotInfo)
	}

	// Convert attribute patterns
	model.Attributes = make([]emitter.Attribute, 0, len(patterns.Attributes))
	for _, attrMatch := range patterns.Attributes {
		// Only include modern attributes created via Attribute::make
		if attrMatch.IsModern {
			attribute := emitter.Attribute{
				Name: attrMatch.Name,
				Via:  "Attribute::make",
			}
			model.Attributes = append(model.Attributes, attribute)
		}
	}

	// Polymorphic relationships are now handled at top-level by AssemblePolymorphic()
	// No longer attached to individual models

	return model, nil
}

// GetStats returns processing statistics.
func (p *DefaultPatternMatchingProcessor) GetStats() *ProcessingStats {
	// Merge stats from composite matcher
	compositeStats := p.composite.stats
	return &ProcessingStats{
		FilesProcessed:    p.stats.FilesProcessed + compositeStats.FilesProcessed,
		PatternsDetected:  p.stats.PatternsDetected + compositeStats.PatternsDetected,
		TotalMatches:      p.stats.TotalMatches + compositeStats.TotalMatches,
		AverageConfidence: p.calculateAverageConfidence(),
		ProcessingTimeMs:  p.stats.ProcessingTimeMs + compositeStats.ProcessingTimeMs,
	}
}

// GetComposite returns the underlying composite pattern matcher.
func (p *DefaultPatternMatchingProcessor) GetComposite() CompositePatternMatcher {
	return p.composite
}

// Close releases processor resources.
func (p *DefaultPatternMatchingProcessor) Close() error {
	if p.composite != nil {
		return p.composite.Close()
	}
	return nil
}

// extractClassName attempts to extract class name from patterns.
// Returns empty string and false if cannot resolve to a valid class name.
func (p *DefaultPatternMatchingProcessor) extractClassName(patterns *LaravelPatterns) (string, bool) {
	if patterns == nil {
		return "", false
	}

	if patterns.ClassName != "" {
		return patterns.ClassName, true
	}

	if ctx, ok := p.extractContextClassAndMethod(patterns); ok && ctx.className != "" {
		return ctx.className, true
	}

	if patterns.FilePath != "" {
		base := filepath.Base(patterns.FilePath)
		className := strings.TrimSuffix(base, filepath.Ext(base))
		if className != "" {
			return className, true
		}
	}

	return "UnknownController", true
}

// extractMethodName attempts to extract method name from patterns.
// Returns empty string and false if cannot resolve to a valid method name.
func (p *DefaultPatternMatchingProcessor) extractMethodName(patterns *LaravelPatterns) (string, bool) {
	if patterns == nil {
		return "", false
	}

	if ctx, ok := p.extractContextClassAndMethod(patterns); ok && ctx.methodName != "" {
		return ctx.methodName, true
	}

	return "index", true
}

type extractedContext struct {
	className  string
	methodName string
}

func (p *DefaultPatternMatchingProcessor) extractContextClassAndMethod(patterns *LaravelPatterns) (extractedContext, bool) {
	var candidates []string

	for _, match := range patterns.HTTPStatus {
		if match != nil && match.Method != "" {
			candidates = append(candidates, match.Method)
		}
	}
	for _, match := range patterns.Resources {
		if match != nil && match.Method != "" {
			candidates = append(candidates, match.Method)
		}
	}
	for _, match := range patterns.Scopes {
		if match != nil {
			if match.Context != "" {
				candidates = append(candidates, match.Context)
			}
			if match.Method != "" {
				candidates = append(candidates, match.Method)
			}
		}
	}
	for _, match := range patterns.Polymorphics {
		if match != nil {
			if match.Context != "" {
				candidates = append(candidates, match.Context)
			}
			if match.Method != "" {
				candidates = append(candidates, match.Method)
			}
		}
	}

	for _, candidate := range candidates {
		if !strings.Contains(candidate, "::") {
			continue
		}
		parts := strings.SplitN(candidate, "::", 2)
		if parts[0] != "" && parts[1] != "" {
			return extractedContext{className: parts[0], methodName: parts[1]}, true
		}
	}

	return extractedContext{}, false
}

func (p *DefaultPatternMatchingProcessor) shortClassName(className string) string {
	if className == "" {
		return ""
	}
	if idx := strings.LastIndex(className, "\\"); idx >= 0 && idx < len(className)-1 {
		return className[idx+1:]
	}
	return className
}

// calculateAverageConfidence calculates average confidence from all processed patterns.
func (p *DefaultPatternMatchingProcessor) calculateAverageConfidence() float64 {
	// This would track confidence across all matches
	// For now, return a reasonable default
	return 0.85
}

// CreateDefaultMatcherSetup creates a complete matcher setup with all available matchers.
func CreateDefaultMatcherSetup(language *sitter.Language) (*DefaultPatternMatchingProcessor, error) {
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pattern matching processor: %w", err)
	}

	return processor, nil
}

// ValidateMatcherConfiguration validates matcher configuration settings.
func ValidateMatcherConfiguration(config *MatcherConfig) error {
	if config == nil {
		return fmt.Errorf("matcher configuration cannot be nil")
	}

	if config.MaxMatchesPerFile <= 0 {
		return fmt.Errorf("MaxMatchesPerFile must be positive, got %d", config.MaxMatchesPerFile)
	}

	if config.MinConfidenceThreshold < 0 || config.MinConfidenceThreshold > 1 {
		return fmt.Errorf("MinConfidenceThreshold must be between 0 and 1, got %f", config.MinConfidenceThreshold)
	}

	if config.MaxConcurrentMatchers <= 0 {
		return fmt.Errorf("MaxConcurrentMatchers must be positive, got %d", config.MaxConcurrentMatchers)
	}

	if config.MatchTimeoutMs <= 0 {
		return fmt.Errorf("MatchTimeoutMs must be positive, got %d", config.MatchTimeoutMs)
	}

	// At least one matcher type must be enabled
	if !config.EnableHTTPStatusMatching &&
		!(config.EnableRequestUsageMatching || config.EnableRequestMatching) &&
		!config.EnableResourceMatching &&
		!config.EnablePivotMatching && !config.EnableAttributeMatching && !config.EnableScopeMatching &&
		!config.EnablePolymorphicMatching && !config.EnableBroadcastMatching {
		return fmt.Errorf("at least one matcher type must be enabled")
	}

	return nil
}

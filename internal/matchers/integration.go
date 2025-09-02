// Package matchers provides integration between pattern matchers and the parser/emitter system.
package matchers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/parser"
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

// MatchAll runs all registered matchers on the syntax tree.
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

	// Execute enabled matchers
	for patternType, matcher := range c.matchers {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check if this matcher type is enabled
		if !c.isMatcherEnabled(patternType) {
			continue
		}

		// Execute the matcher
		results, err := matcher.Match(ctx, tree, filePath)
		if err != nil {
			// Log error but continue with other matchers
			continue
		}

		// Process results by type
		c.processMatchResults(patternType, results, patterns)
		c.stats.TotalMatches += int64(len(results))
	}

	// Update processing statistics
	processingTime := time.Since(startTime)
	patterns.ProcessingMs = processingTime.Milliseconds()
	c.stats.FilesProcessed++
	c.stats.ProcessingTimeMs += processingTime.Milliseconds()

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
	for k, v := range c.matchers {
		matchers[k] = v
	}
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
	composite *DefaultCompositePatternMatcher
	config    *MatcherConfig
	stats     *ProcessingStats
}

// NewPatternMatchingProcessor creates a new pattern matching processor.
func NewPatternMatchingProcessor(language *sitter.Language, config *MatcherConfig) (*DefaultPatternMatchingProcessor, error) {
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create composite matcher: %w", err)
	}

	// Initialize enabled matchers
	processor := &DefaultPatternMatchingProcessor{
		composite: composite,
		config:    config,
		stats:     &ProcessingStats{},
	}

	if err := processor.initializeMatchers(language, config); err != nil {
		return nil, fmt.Errorf("failed to initialize matchers: %w", err)
	}

	return processor, nil
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

	// Initialize T7 pattern matchers if enabled
	
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

	// Initialize T8 pattern matchers if enabled
	
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

	// Initialize T10 pattern matchers if enabled
	
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

	controller := &emitter.Controller{
		FQCN:   p.extractClassName(patterns),
		Method: p.extractMethodName(patterns),
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
			Class:      resMatch.Class,
			Collection: resMatch.Collection,
		}
		controller.Resources = append(controller.Resources, resource)
	}

	// Convert scope usage patterns to controller.ScopesUsed
	controller.ScopesUsed = make([]emitter.ScopeUsed, 0, len(patterns.Scopes))
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
		}
	}

	// Convert polymorphic relationship patterns to controller.Polymorphic
	controller.Polymorphic = make([]emitter.PolymorphicRelation, 0, len(patterns.Polymorphics))
	for _, polyMatch := range patterns.Polymorphics {
		// Create polymorphic relation with deterministic ordering
		polyRelation := emitter.PolymorphicRelation{
			Relation:  polyMatch.Relation,
			Type:      polyMatch.Type,
			MorphType: polyMatch.MorphType,
			MorphId:   polyMatch.MorphId,
		}
		
		// Add model if specified
		if polyMatch.Model != "" {
			polyRelation.Model = &polyMatch.Model
		}
		
		// Add discriminator if present
		if polyMatch.Discriminator != nil {
			polyRelation.Discriminator = &emitter.PolymorphicDiscriminator{
				PropertyName: polyMatch.Discriminator.PropertyName,
				Mapping:      make(map[string]string),
				Source:       polyMatch.Discriminator.Source,
				IsExplicit:   polyMatch.Discriminator.IsExplicit,
			}
			
			// Copy mapping with deterministic ordering
			for key, value := range polyMatch.Discriminator.Mapping {
				polyRelation.Discriminator.Mapping[key] = value
			}
			
			if polyMatch.Discriminator.DefaultType != "" {
				polyRelation.Discriminator.DefaultType = &polyMatch.Discriminator.DefaultType
			}
		}
		
		// Add related models with sorted order for determinism
		if len(polyMatch.RelatedModels) > 0 {
			sortedModels := make([]string, len(polyMatch.RelatedModels))
			copy(sortedModels, polyMatch.RelatedModels)
			sort.Strings(sortedModels)
			polyRelation.RelatedModels = sortedModels
		}
		
		// Add depth information if truncated
		if polyMatch.DepthTruncated {
			polyRelation.DepthTruncated = &polyMatch.DepthTruncated
			if polyMatch.MaxDepth > 0 {
				polyRelation.MaxDepth = &polyMatch.MaxDepth
			}
		}
		
		controller.Polymorphic = append(controller.Polymorphic, polyRelation)
	}

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
			// Sort params for deterministic output
			params := make([]string, len(broadcastMatch.Params))
			copy(params, broadcastMatch.Params)
			sort.Strings(params)
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

	model := &emitter.Model{
		FQCN: p.extractClassName(patterns),
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

	// Convert polymorphic relationship patterns to model.Polymorphic
	model.Polymorphic = make([]emitter.PolymorphicRelation, 0, len(patterns.Polymorphics))
	for _, polyMatch := range patterns.Polymorphics {
		// Only include polymorphic relationships defined in models
		if polyMatch.Context == "model" || polyMatch.Context == "relationship" {
			polyRelation := emitter.PolymorphicRelation{
				Relation:  polyMatch.Relation,
				Type:      polyMatch.Type,
				MorphType: polyMatch.MorphType,
				MorphId:   polyMatch.MorphId,
			}
			
			// Add model if specified (for morphOne/morphMany)
			if polyMatch.Model != "" {
				polyRelation.Model = &polyMatch.Model
			}
			
			// Add discriminator information
			if polyMatch.Discriminator != nil {
				polyRelation.Discriminator = &emitter.PolymorphicDiscriminator{
					PropertyName: polyMatch.Discriminator.PropertyName,
					Mapping:      make(map[string]string),
					Source:       polyMatch.Discriminator.Source,
					IsExplicit:   polyMatch.Discriminator.IsExplicit,
				}
				
				// Copy mapping with deterministic ordering
				for key, value := range polyMatch.Discriminator.Mapping {
					polyRelation.Discriminator.Mapping[key] = value
				}
				
				if polyMatch.Discriminator.DefaultType != "" {
					polyRelation.Discriminator.DefaultType = &polyMatch.Discriminator.DefaultType
				}
			}
			
			// Add related models with sorted order for determinism
			if len(polyMatch.RelatedModels) > 0 {
				sortedModels := make([]string, len(polyMatch.RelatedModels))
				copy(sortedModels, polyMatch.RelatedModels)
				sort.Strings(sortedModels)
				polyRelation.RelatedModels = sortedModels
			}
			
			// Add depth information if truncated
			if polyMatch.DepthTruncated {
				polyRelation.DepthTruncated = &polyMatch.DepthTruncated
				if polyMatch.MaxDepth > 0 {
					polyRelation.MaxDepth = &polyMatch.MaxDepth
				}
			}
			
			model.Polymorphic = append(model.Polymorphic, polyRelation)
		}
	}

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

// extractClassName attempts to extract class name from file path.
func (p *DefaultPatternMatchingProcessor) extractClassName(patterns *LaravelPatterns) string {
	if patterns.ClassName != "" {
		return patterns.ClassName
	}
	
	// Try to infer from file path - this is a simplification
	// In a real implementation, this would use PSR-4 resolution
	return "UnknownController" // Placeholder
}

// extractMethodName attempts to extract method name from patterns.
func (p *DefaultPatternMatchingProcessor) extractMethodName(patterns *LaravelPatterns) string {
	// This would typically be extracted from the actual method being analyzed
	// For now, return a default
	return "index" // Placeholder
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

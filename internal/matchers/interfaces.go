// Package matchers provides pattern detection for Laravel/PHP constructs.
// Implements tree-sitter query-based pattern matching with confidence scoring
// and integration with the existing parser and emitter infrastructure.
package matchers

import (
	"context"
	"runtime"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// PatternType represents the type of Laravel pattern detected.
type PatternType string

const (
	// Core Laravel patterns
	PatternTypeHTTPStatus   PatternType = "http_status"
	PatternTypeRequestUsage PatternType = "request_usage"
	PatternTypeResource     PatternType = "resource"

	// Advanced Laravel patterns
	PatternTypePivot       PatternType = "pivot"
	PatternTypeAttribute   PatternType = "attribute"
	PatternTypeScope       PatternType = "scope"
	PatternTypePolymorphic PatternType = "polymorphic"
	PatternTypeBroadcast   PatternType = "broadcast"
)

// MatchResult represents a single pattern match with confidence scoring.
type MatchResult struct {
	Type       PatternType   `json:"type"`
	Position   parser.Point  `json:"position"`
	Content    string        `json:"content"`
	Confidence float64       `json:"confidence"`
	Data       any           `json:"data"`
	Context    *MatchContext `json:"context,omitempty"`
}

// MatchContext provides additional context about the match location.
type MatchContext struct {
	ClassName  string `json:"className,omitempty"`
	MethodName string `json:"methodName,omitempty"`
	FilePath   string `json:"filePath,omitempty"`
	Explicit   bool   `json:"explicit"`
}

// HTTPStatusMatch represents detected HTTP status patterns.
type HTTPStatusMatch struct {
	Status   int    `json:"status"`
	Explicit bool   `json:"explicit"`
	Pattern  string `json:"pattern"`
	Method   string `json:"method,omitempty"` // Controller::method context
}

// RequestUsageMatch represents detected request usage patterns.
type RequestUsageMatch struct {
	ContentTypes []string       `json:"contentTypes"`
	Body         map[string]any `json:"body,omitempty"`
	Query        map[string]any `json:"query,omitempty"`
	Files        map[string]any `json:"files,omitempty"`
	Methods      []string       `json:"methods"`
}

// ResourceMatch represents detected Laravel Resource usage.
type ResourceMatch struct {
	Class      string `json:"class"`
	FQCN       string `json:"fqcn,omitempty"`
	Collection bool   `json:"collection"`
	Pattern    string `json:"pattern"`
	Method     string `json:"method,omitempty"`
}

// PivotMatch represents detected Laravel pivot relationship patterns.
type PivotMatch struct {
	Relation   string   `json:"relation"`
	Fields     []string `json:"fields,omitempty"`
	Timestamps bool     `json:"timestamps"`
	Alias      string   `json:"alias,omitempty"`
	Pattern    string   `json:"pattern"`
	Method     string   `json:"method"`
}

// AttributeMatch represents detected Laravel attribute accessor patterns.
type AttributeMatch struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	CastType string `json:"castType,omitempty"`
	Accessor bool   `json:"accessor"`
	Mutator  bool   `json:"mutator"`
	IsModern bool   `json:"isModern"`
	Pattern  string `json:"pattern"`
	Method   string `json:"method"`
}

// ScopeMatch represents detected Laravel query scope patterns.
type ScopeMatch struct {
	Name     string `json:"name"`
	On       string `json:"on"`                // Model class that defines or uses the scope
	Args     []any  `json:"args,omitempty"`    // Arguments passed to scope
	IsGlobal bool   `json:"isGlobal"`          // Whether this is a global scope
	IsLocal  bool   `json:"isLocal"`           // Whether this is a local scope
	Pattern  string `json:"pattern"`           // Pattern type (definition, usage, etc.)
	Method   string `json:"method"`            // Method name (scopeXxx or direct usage)
	Context  string `json:"context,omitempty"` // Usage context (model, query, etc.)
}

// PolymorphicMatch represents detected Laravel polymorphic relationship patterns.
type PolymorphicMatch struct {
	Relation       string                    `json:"relation"`                // Relationship method name
	Type           string                    `json:"type"`                    // Polymorphic type (morphTo, morphOne, morphMany)
	MorphType      string                    `json:"morphType,omitempty"`     // Morph type column (e.g., 'imageable_type')
	MorphId        string                    `json:"morphId,omitempty"`       // Morph ID column (e.g., 'imageable_id')
	Model          string                    `json:"model,omitempty"`         // Target model class for morphOne/morphMany
	Discriminator  *PolymorphicDiscriminator `json:"discriminator,omitempty"` // Discriminator mapping information
	DepthTruncated bool                      `json:"depthTruncated"`          // True if max depth reached
	MaxDepth       int                       `json:"maxDepth,omitempty"`      // Maximum traversal depth configured
	Pattern        string                    `json:"pattern"`                 // Pattern type that matched
	Method         string                    `json:"method"`                  // Method name (morphTo, morphOne, etc.)
	Context        string                    `json:"context,omitempty"`       // Context (model, relationship, etc.)
	RelatedModels  []string                  `json:"relatedModels,omitempty"` // Models that can be related through this polymorphic relationship
}

// PolymorphicDiscriminator contains discriminator mapping information for polymorphic relationships.
type PolymorphicDiscriminator struct {
	PropertyName string            `json:"propertyName"`          // Discriminator property name (usually morph type column)
	Mapping      map[string]string `json:"mapping"`               // Type mappings (type value -> model class)
	Source       string            `json:"source"`                // Source of mapping (morphMap, explicit, inferred)
	IsExplicit   bool              `json:"isExplicit"`            // Whether mapping is explicitly defined
	DefaultType  string            `json:"defaultType,omitempty"` // Default type if no mapping matches
}

// BroadcastMatch represents detected Laravel broadcast channel patterns.
type BroadcastMatch struct {
	Channel        string   `json:"channel"`        // Channel name with parameter placeholders
	Params         []string `json:"params"`         // Extracted parameters from channel name
	Visibility     string   `json:"visibility"`     // Channel visibility (public, private, presence)
	PayloadLiteral bool     `json:"payloadLiteral"` // Whether payload contains literal values
	Method         string   `json:"method"`         // Broadcast method used (channel, private, presence)
	Pattern        string   `json:"pattern"`        // Pattern type that matched
	File           string   `json:"file,omitempty"` // File path where channel is defined
}

// LaravelPatterns aggregates all detected Laravel patterns for a single file.
type LaravelPatterns struct {
	FilePath     string               `json:"filePath"`
	ClassName    string               `json:"className,omitempty"`
	HTTPStatus   []*HTTPStatusMatch   `json:"httpStatus,omitempty"`
	RequestUsage []*RequestUsageMatch `json:"requestUsage,omitempty"`
	Resources    []*ResourceMatch     `json:"resources,omitempty"`
	Pivots       []*PivotMatch        `json:"pivots,omitempty"`
	Attributes   []*AttributeMatch    `json:"attributes,omitempty"`
	Scopes       []*ScopeMatch        `json:"scopes,omitempty"`
	Polymorphics []*PolymorphicMatch  `json:"polymorphics,omitempty"`
	Broadcasts   []*BroadcastMatch    `json:"broadcasts,omitempty"`
	ProcessedAt  int64                `json:"processedAt"`
	ProcessingMs int64                `json:"processingMs"`
}

// PatternMatcher defines the interface for pattern detection implementations.
type PatternMatcher interface {
	// GetType returns the pattern type this matcher detects
	GetType() PatternType

	// Match finds all occurrences of the pattern in the given syntax tree
	Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error)

	// GetQueries returns the tree-sitter queries used by this matcher
	GetQueries() []*sitter.Query

	// IsInitialized returns true if the matcher is ready for use
	IsInitialized() bool

	// Close releases any resources held by the matcher
	Close() error
}

// HTTPStatusMatcher defines specialized interface for HTTP status detection.
type HTTPStatusMatcher interface {
	PatternMatcher

	// MatchHTTPStatus finds HTTP status patterns with confidence scoring
	MatchHTTPStatus(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*HTTPStatusMatch, error)
}

// RequestUsageMatcher defines specialized interface for request usage detection.
type RequestUsageMatcher interface {
	PatternMatcher

	// MatchRequestUsage finds request usage patterns and infers content types
	MatchRequestUsage(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*RequestUsageMatch, error)
}

// ResourceMatcher defines specialized interface for Laravel Resource detection.
type ResourceMatcher interface {
	PatternMatcher

	// MatchResources finds Laravel Resource usage patterns
	MatchResources(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*ResourceMatch, error)
}

// PivotMatcher defines specialized interface for Laravel pivot relationship detection.
type PivotMatcher interface {
	PatternMatcher

	// MatchPivots finds Laravel pivot relationship patterns
	MatchPivots(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*PivotMatch, error)
}

// AttributeMatcher defines specialized interface for Laravel attribute accessor detection.
type AttributeMatcher interface {
	PatternMatcher

	// MatchAttributes finds Laravel attribute accessor patterns
	MatchAttributes(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*AttributeMatch, error)
}

// ScopeMatcher defines specialized interface for Laravel query scope detection.
type ScopeMatcher interface {
	PatternMatcher

	// MatchScopes finds Laravel query scope patterns
	MatchScopes(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*ScopeMatch, error)
}

// PolymorphicMatcher defines specialized interface for Laravel polymorphic relationship detection.
type PolymorphicMatcher interface {
	PatternMatcher

	// MatchPolymorphic finds Laravel polymorphic relationship patterns
	MatchPolymorphic(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*PolymorphicMatch, error)

	// SetMaxDepth configures the maximum relationship traversal depth
	SetMaxDepth(maxDepth int)

	// GetMaxDepth returns the current maximum traversal depth
	GetMaxDepth() int
}

// BroadcastMatcher defines specialized interface for Laravel broadcast channel detection.
type BroadcastMatcher interface {
	PatternMatcher

	// MatchBroadcast finds Laravel broadcast channel patterns
	MatchBroadcast(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*BroadcastMatch, error)
}

// CompositePatternMatcher orchestrates multiple pattern matchers.
type CompositePatternMatcher interface {
	// AddMatcher registers a new pattern matcher
	AddMatcher(matcher PatternMatcher) error

	// RemoveMatcher unregisters a pattern matcher by type
	RemoveMatcher(patternType PatternType) error

	// MatchAll runs all registered matchers on the syntax tree
	MatchAll(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*LaravelPatterns, error)

	// GetMatchers returns all registered matchers
	GetMatchers() map[PatternType]PatternMatcher

	// IsInitialized returns true if all matchers are ready
	IsInitialized() bool

	// Close releases resources from all matchers
	Close() error
}

// PatternMatchingProcessor integrates pattern matching with parser and emitter.
type PatternMatchingProcessor interface {
	// ProcessFile runs pattern matching on a single file
	ProcessFile(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*LaravelPatterns, error)

	// ConvertToEmitterFormat converts patterns to emitter.Controller format
	ConvertToEmitterFormat(patterns *LaravelPatterns) (*emitter.Controller, error)

	// GetStats returns processing statistics
	GetStats() *ProcessingStats

	// Close releases processor resources
	Close() error
}

// ProcessingStats tracks pattern matching performance and results.
type ProcessingStats struct {
	FilesProcessed    int64   `json:"filesProcessed"`
	PatternsDetected  int64   `json:"patternsDetected"`
	TotalMatches      int64   `json:"totalMatches"`
	AverageConfidence float64 `json:"averageConfidence"`
	ProcessingTimeMs  int64   `json:"processingTimeMs"`
}

// MatcherConfig configures pattern matcher behavior.
type MatcherConfig struct {
	// Resource limits
	MaxMatchesPerFile      int     `json:"maxMatchesPerFile"`
	MinConfidenceThreshold float64 `json:"minConfidenceThreshold"`

	// Feature flags - Core patterns
	EnableHTTPStatusMatching   bool `json:"enableHTTPStatusMatching"`
	EnableRequestUsageMatching bool `json:"enableRequestUsageMatching"`
	EnableRequestMatching      bool `json:"enableRequestMatching"` // Alias for EnableRequestUsageMatching
	EnableResourceMatching     bool `json:"enableResourceMatching"`

	// Feature flags - T7 patterns
	EnablePivotMatching     bool `json:"enablePivotMatching"`
	EnableAttributeMatching bool `json:"enableAttributeMatching"`
	EnableScopeMatching     bool `json:"enableScopeMatching"`

	// Feature flags - T8 patterns
	EnablePolymorphicMatching bool `json:"enablePolymorphicMatching"`

	// Feature flags - T10 patterns
	EnableBroadcastMatching bool `json:"enableBroadcastMatching"`

	// Concurrency and timeout settings
	MaxConcurrentMatchers int `json:"maxConcurrentMatchers"`
	MatchTimeoutMs        int `json:"matchTimeoutMs"`

	// Polymorphic relationship settings
	MaxRelationshipDepth int `json:"maxRelationshipDepth"`

	// Behavior control
	StrictExplicitOnly     bool `json:"strictExplicitOnly"`
	ResolveImportedClasses bool `json:"resolveImportedClasses"`
	DeduplicateMatches     bool `json:"deduplicateMatches"`
}

// DefaultMatcherConfig returns sensible defaults for pattern matching.
func DefaultMatcherConfig() *MatcherConfig {
	// Aggressive concurrent matcher configuration
	// Most matchers are independent and can run in parallel
	maxConcurrentMatchers := runtime.NumCPU()
	if maxConcurrentMatchers < 8 {
		maxConcurrentMatchers = 8 // Pattern matching is CPU-bound, use all cores
	}

	return &MatcherConfig{
		MaxMatchesPerFile:          100,
		MinConfidenceThreshold:     0.8,
		EnableHTTPStatusMatching:   true,
		EnableRequestUsageMatching: true,
		EnableRequestMatching:      true, // Keep both flags for compatibility
		EnableResourceMatching:     true,
		EnablePivotMatching:        true,
		EnableAttributeMatching:    true,
		EnableScopeMatching:        true,
		EnablePolymorphicMatching:  true,
		EnableBroadcastMatching:    true,
		MaxConcurrentMatchers:      maxConcurrentMatchers,
		MatchTimeoutMs:             5000,
		MaxRelationshipDepth:       5,
		StrictExplicitOnly:         false,
		ResolveImportedClasses:     true,
		DeduplicateMatches:         true,
	}
}

// ConfidenceLevel defines thresholds for pattern confidence scoring.
type ConfidenceLevel struct {
	Minimum float64
	Good    float64
	High    float64
	Maximum float64
}

// DefaultConfidenceLevels returns standard confidence thresholds.
func DefaultConfidenceLevels() *ConfidenceLevel {
	return &ConfidenceLevel{
		Minimum: 0.6,
		Good:    0.8,
		High:    0.9,
		Maximum: 1.0,
	}
}

// FeatureConfig represents feature flags from the manifest.
// This type mirrors internal/manifest.FeatureConfig to avoid circular imports.
type FeatureConfig struct {
	HTTPStatus        *bool `json:"http_status,omitempty"`
	RequestUsage      *bool `json:"request_usage,omitempty"`
	ResourceUsage     *bool `json:"resource_usage,omitempty"`
	WithPivot         *bool `json:"with_pivot,omitempty"`
	AttributeMake     *bool `json:"attribute_make,omitempty"`
	ScopesUsed        *bool `json:"scopes_used,omitempty"`
	Polymorphic       *bool `json:"polymorphic,omitempty"`
	BroadcastChannels *bool `json:"broadcast_channels,omitempty"`
}

// ApplyFeatureFlags updates matcher configuration based on manifest feature flags.
// If a feature flag is nil, the existing configuration value is preserved.
func (config *MatcherConfig) ApplyFeatureFlags(features *FeatureConfig) {
	if features == nil {
		return
	}

	// Apply core pattern flags
	if features.HTTPStatus != nil {
		config.EnableHTTPStatusMatching = *features.HTTPStatus
	}
	if features.RequestUsage != nil {
		config.EnableRequestUsageMatching = *features.RequestUsage
		config.EnableRequestMatching = *features.RequestUsage // Keep both for compatibility
	}
	if features.ResourceUsage != nil {
		config.EnableResourceMatching = *features.ResourceUsage
	}

	// Apply T7 pattern flags
	if features.WithPivot != nil {
		config.EnablePivotMatching = *features.WithPivot
	}
	if features.AttributeMake != nil {
		config.EnableAttributeMatching = *features.AttributeMake
	}
	if features.ScopesUsed != nil {
		config.EnableScopeMatching = *features.ScopesUsed
	}

	// Apply T8 pattern flags
	if features.Polymorphic != nil {
		config.EnablePolymorphicMatching = *features.Polymorphic
	}

	// Apply T10 pattern flags
	if features.BroadcastChannels != nil {
		config.EnableBroadcastMatching = *features.BroadcastChannels
	}
}

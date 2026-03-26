// Package parser provides PHP source code analysis using tree-sitter.
// This package implements concurrent PHP parsing, AST extraction, and Laravel pattern detection
// with integration with the file indexing system.
package parser

import (
	"context"
	"time"

	"github.com/oxhq/oxinfer/internal/manifest"
)

// TreeSitterParser provides low-level tree-sitter PHP parsing functionality.
// Handles tree-sitter C library integration, parser lifecycle, and memory management.
type TreeSitterParser interface {
	// ParseContent parses PHP content and returns the raw syntax tree.
	// Returns error for malformed PHP that cannot be parsed.
	ParseContent(content []byte) (*SyntaxTree, error)

	// ParseFile parses a PHP file and returns the syntax tree.
	// Handles file reading and content validation internally.
	ParseFile(ctx context.Context, filePath string) (*SyntaxTree, error)

	// IsInitialized returns true if the parser is ready for use.
	// Used to verify grammar loading and initialization status.
	IsInitialized() bool

	// Close releases parser resources and cleans up memory.
	// Must be called to prevent memory leaks in long-running processes.
	Close() error
}

// QueryEngine executes tree-sitter queries against PHP syntax trees.
// Provides high-level extraction of PHP language constructs using S-expression patterns.
type QueryEngine interface {
	// ExtractNamespaces finds all namespace declarations in the syntax tree.
	// Returns namespaces with their full qualified names and positions.
	ExtractNamespaces(tree *SyntaxTree) ([]PHPNamespace, error)

	// ExtractClasses finds all class definitions in the syntax tree.
	// Includes regular classes, abstract classes, and final classes with their metadata.
	ExtractClasses(tree *SyntaxTree) ([]PHPClass, error)

	// ExtractMethods finds all method definitions within classes.
	// Returns methods with signatures, visibility, and modifiers.
	ExtractMethods(tree *SyntaxTree) ([]PHPMethod, error)

	// ExtractTraits finds all trait definitions and usage.
	// Returns traits with their methods and usage contexts.
	ExtractTraits(tree *SyntaxTree) ([]PHPTrait, error)

	// ExtractFunctions finds all function definitions (global and closures).
	// Returns functions with signatures and parameter information.
	ExtractFunctions(tree *SyntaxTree) ([]PHPFunction, error)

	// ExtractInterfaces finds all interface definitions.
	// Returns interfaces with their method signatures and inheritance.
	ExtractInterfaces(tree *SyntaxTree) ([]PHPInterface, error)

	// ExtractUseStatements finds all use/import statements in the syntax tree.
	// Returns use statements with their fully qualified names and aliases.
	ExtractUseStatements(tree *SyntaxTree) ([]PHPUseStatement, error)
}

// ParserPool manages thread-safe access to tree-sitter parser instances.
// Provides parser reuse and resource management for concurrent parsing operations.
type ParserPool interface {
	// AcquireParser gets a parser instance from the pool.
	// Blocks if no parsers are available until timeout or context cancellation.
	AcquireParser(ctx context.Context) (TreeSitterParser, error)

	// ReleaseParser returns a parser instance to the pool.
	// Parser should not be used after being released.
	ReleaseParser(parser TreeSitterParser) error

	// Size returns the current pool size (total parser instances).
	Size() int

	// ActiveCount returns the number of parsers currently in use.
	ActiveCount() int

	// Close shuts down the pool and releases all parser resources.
	// All active parsers should be returned before calling Close.
	Close() error
}

// PHPConstructExtractor extracts and normalizes PHP language constructs from syntax trees.
// Combines QueryEngine results into structured representations for pattern analysis.
type PHPConstructExtractor interface {
	// ExtractAllConstructs performs comprehensive extraction of PHP constructs.
	// Returns all classes, methods, functions, traits, and interfaces found.
	ExtractAllConstructs(tree *SyntaxTree) (*PHPFileStructure, error)

	// ExtractLaravelPatterns identifies Laravel-specific patterns in PHP code.
	// Looks for controllers, models, requests, resources, and other Laravel constructs.
	ExtractLaravelPatterns(tree *SyntaxTree) (*LaravelPatterns, error)

	// ExtractDocBlocks parses PHPDoc comments and annotations.
	// Returns structured documentation with type hints and parameter descriptions.
	ExtractDocBlocks(tree *SyntaxTree) ([]PHPDocBlock, error)

	// ExtractUseStatements finds all use/import statements.
	// Returns qualified class names and their aliases.
	ExtractUseStatements(tree *SyntaxTree) ([]PHPUseStatement, error)
}

// PHPParser orchestrates the complete PHP parsing process.
// Implements the FileProcessor interface for integration with file indexer.
type PHPParser interface {
	// ProcessFile implements indexer.FileProcessor for system integration.
	// Performs complete PHP analysis and returns structured results.
	ProcessFile(ctx context.Context, file any) (any, error)

	// ParsePHPFile performs comprehensive PHP file analysis.
	// Returns detailed PHP structure information for pattern detection.
	ParsePHPFile(ctx context.Context, filePath string) (*PHPParseResult, error)

	// GetParserStats returns current parser performance and resource statistics.
	GetParserStats() ParserStats

	// SetConfiguration updates parser configuration and limits.
	SetConfiguration(config ParserConfig) error

	// Close releases all parser resources and shuts down worker pools.
	Close() error
}

// ConcurrentPHPParser provides thread-safe concurrent PHP parsing capabilities.
// Manages concurrent parsing operations with resource pooling and error recovery.
type ConcurrentPHPParser interface {
	// ParseConcurrently processes multiple PHP files concurrently and returns results via channel.
	// Respects context cancellation and manages resource allocation automatically.
	ParseConcurrently(ctx context.Context, files []ParseJob) (<-chan ParseJobResult, error)

	// SetMaxWorkers updates the maximum number of concurrent workers.
	// Dynamically adjusts resource allocation and parser pool size.
	SetMaxWorkers(maxWorkers int) error

	// GetActiveWorkers returns the current number of active parsing workers.
	// Used for monitoring and resource management decisions.
	GetActiveWorkers() int

	// Shutdown gracefully shuts down the concurrent parser and releases all resources.
	// Waits for active workers to finish or times out based on context.
	Shutdown(ctx context.Context) error
}

// ErrorHandler manages parser error reporting and recovery strategies.
// Provides structured error handling with context and recovery recommendations.
type ErrorHandler interface {
	// HandleParseError processes tree-sitter parsing errors.
	// Returns recoverable errors or escalates fatal errors.
	HandleParseError(filePath string, err error) error

	// HandleExtractionError processes PHP construct extraction errors.
	// Provides context about which constructs failed extraction.
	HandleExtractionError(filePath string, construct string, err error) error

	// IsRecoverableError determines if a parsing error can be handled gracefully.
	// Returns true for syntax errors that don't prevent partial analysis.
	IsRecoverableError(err error) bool

	// GetErrorStats returns statistics about parsing and extraction errors.
	GetErrorStats() ErrorStats
}

// PHPProjectParser orchestrates complete PHP project analysis integrating all core components.
// Combines file discovery, PSR-4 resolution, concurrent parsing,
// and construct extraction into a unified Laravel project analysis system.
type PHPProjectParser interface {
	// ParseProject performs complete PHP project analysis with progress monitoring.
	// Integrates file discovery, PSR-4 namespace resolution, concurrent parsing, and construct extraction.
	ParseProject(ctx context.Context, config ProjectParserConfig) (*ProjectParseResult, error)

	// LoadFromManifest configures the parser from Oxinfer manifest (from the manifest system).
	// Extracts scan targets, limits, caching settings, and feature flags.
	LoadFromManifest(manifest *Manifest) error

	// GetProgress returns real-time progress information during parsing operations.
	// Tracks files discovered, cached, parsed, extracted, failed across all phases.
	GetProgress() ProjectParserProgress

	// SetProgressCallback enables real-time progress monitoring during long operations.
	// Callback receives progress updates throughout the analysis pipeline.
	SetProgressCallback(callback func(ProjectParserProgress))
}

// SyntaxTree represents a parsed PHP syntax tree from tree-sitter.
// Wraps tree-sitter node structure with Go-friendly methods.
type SyntaxTree struct {
	// Root is the root node of the syntax tree
	Root *SyntaxNode

	// Source is the original PHP source code
	Source []byte

	// Language identifies the tree-sitter language (should be "php")
	Language string

	// ParsedAt is when this tree was created
	ParsedAt time.Time
}

// SyntaxNode represents a single node in the PHP syntax tree.
// Provides access to tree-sitter node properties and navigation.
type SyntaxNode struct {
	// Type is the tree-sitter node type (e.g., "class_declaration")
	Type string

	// Text is the source text covered by this node
	Text string

	// StartByte is the start position in the source
	StartByte int

	// EndByte is the end position in the source
	EndByte int

	// StartPoint is the start line/column position
	StartPoint Point

	// EndPoint is the end line/column position
	EndPoint Point

	// Children are the child nodes
	Children []*SyntaxNode

	// Parent is the parent node (nil for root)
	Parent *SyntaxNode
}

// Point represents a line/column position in source code.
type Point struct {
	Row    int // Line number (0-indexed)
	Column int // Column number (0-indexed)
}

// PHPNamespace represents a PHP namespace declaration.
type PHPNamespace struct {
	// Name is the fully qualified namespace name
	Name string

	// Position is the location in the source file
	Position SourcePosition

	// Classes are classes defined in this namespace
	Classes []string

	// Functions are functions defined in this namespace
	Functions []string

	// Constants are constants defined in this namespace
	Constants []string
}

// PHPClass represents a PHP class definition.
type PHPClass struct {
	// Name is the class name (not fully qualified)
	Name string

	// FullyQualifiedName includes namespace prefix
	FullyQualifiedName string

	// Namespace is the containing namespace
	Namespace string

	// Position is the location in the source file
	Position SourcePosition

	// Visibility is the class visibility (public, private, protected)
	Visibility string

	// IsAbstract indicates if the class is abstract
	IsAbstract bool

	// IsFinal indicates if the class is final
	IsFinal bool

	// Extends is the parent class name (if any)
	Extends string

	// Implements are the implemented interfaces
	Implements []string

	// Methods are the methods defined in this class
	Methods []PHPMethod

	// Properties are the properties defined in this class
	Properties []PHPProperty

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock

	// Traits are traits used by this class
	Traits []string
}

// PHPMethod represents a PHP method definition.
type PHPMethod struct {
	// Name is the method name
	Name string

	// ClassName is the containing class name
	ClassName string

	// Position is the location in the source file
	Position SourcePosition

	// Visibility is the method visibility (public, private, protected)
	Visibility string

	// IsStatic indicates if the method is static
	IsStatic bool

	// IsAbstract indicates if the method is abstract
	IsAbstract bool

	// IsFinal indicates if the method is final
	IsFinal bool

	// Parameters are the method parameters
	Parameters []PHPParameter

	// ReturnType is the declared return type (if any)
	ReturnType string

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock
}

// PHPFunction represents a PHP function definition.
type PHPFunction struct {
	// Name is the function name
	Name string

	// Namespace is the containing namespace
	Namespace string

	// Position is the location in the source file
	Position SourcePosition

	// Parameters are the function parameters
	Parameters []PHPParameter

	// ReturnType is the declared return type (if any)
	ReturnType string

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock

	// IsAnonymous indicates if this is a closure/anonymous function
	IsAnonymous bool
}

// PHPTrait represents a PHP trait definition.
type PHPTrait struct {
	// Name is the trait name
	Name string

	// FullyQualifiedName includes namespace prefix
	FullyQualifiedName string

	// Namespace is the containing namespace
	Namespace string

	// Position is the location in the source file
	Position SourcePosition

	// Methods are the methods defined in this trait
	Methods []PHPMethod

	// Properties are the properties defined in this trait
	Properties []PHPProperty

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock
}

// PHPInterface represents a PHP interface definition.
type PHPInterface struct {
	// Name is the interface name
	Name string

	// FullyQualifiedName includes namespace prefix
	FullyQualifiedName string

	// Namespace is the containing namespace
	Namespace string

	// Position is the location in the source file
	Position SourcePosition

	// Extends are the parent interfaces
	Extends []string

	// Methods are the method signatures defined in this interface
	Methods []PHPMethod

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock
}

// PHPParameter represents a method or function parameter.
type PHPParameter struct {
	// Name is the parameter name (including $ prefix)
	Name string

	// Type is the parameter type hint (if any)
	Type string

	// DefaultValue is the default value (if any)
	DefaultValue string

	// IsVariadic indicates if this is a variadic parameter (...)
	IsVariadic bool

	// IsByReference indicates if this is passed by reference (&)
	IsByReference bool
}

// PHPProperty represents a class property.
type PHPProperty struct {
	// Name is the property name (including $ prefix)
	Name string

	// ClassName is the containing class name
	ClassName string

	// Position is the location in the source file
	Position SourcePosition

	// Visibility is the property visibility (public, private, protected)
	Visibility string

	// IsStatic indicates if the property is static
	IsStatic bool

	// Type is the property type hint (if any)
	Type string

	// DefaultValue is the default value (if any)
	DefaultValue string

	// DocBlock is the PHPDoc comment (if any)
	DocBlock *PHPDocBlock
}

// PHPDocBlock represents a PHPDoc documentation comment.
type PHPDocBlock struct {
	// Summary is the short description
	Summary string

	// Description is the long description
	Description string

	// Tags are the PHPDoc tags (@param, @return, etc.)
	Tags []PHPDocTag

	// Position is the location in the source file
	Position SourcePosition
}

// PHPDocTag represents a single PHPDoc tag.
type PHPDocTag struct {
	// Name is the tag name (param, return, var, etc.)
	Name string

	// Type is the type information (if applicable)
	Type string

	// Variable is the variable name (for @param, @var)
	Variable string

	// Description is the tag description
	Description string
}

// PHPUseStatement represents a use/import statement.
type PHPUseStatement struct {
	// FullyQualifiedName is the imported class/function/constant
	FullyQualifiedName string

	// Alias is the local alias (if any)
	Alias string

	// Type is the use type (class, function, constant)
	Type string

	// Position is the location in the source file
	Position SourcePosition
}

// PHPFileStructure contains all PHP constructs found in a file.
type PHPFileStructure struct {
	// FilePath is the path to the analyzed file
	FilePath string

	// Namespace is the file's namespace (if any)
	Namespace *PHPNamespace

	// Classes are all class definitions in the file
	Classes []PHPClass

	// Interfaces are all interface definitions in the file
	Interfaces []PHPInterface

	// Traits are all trait definitions in the file
	Traits []PHPTrait

	// Functions are all function definitions in the file
	Functions []PHPFunction

	// UseStatements are all use/import statements
	UseStatements []PHPUseStatement

	// ParsedAt is when this analysis was performed
	ParsedAt time.Time

	// ParseDuration is how long the analysis took
	ParseDuration time.Duration
}

// LaravelPatterns contains Laravel-specific patterns found in PHP code.
type LaravelPatterns struct {
	// Controllers are detected Laravel controllers
	Controllers []LaravelController

	// Models are detected Eloquent models
	Models []LaravelModel

	// Requests are detected Form Request classes
	Requests []LaravelRequest

	// Resources are detected API Resource classes
	Resources []LaravelResource

	// Middlewares are detected middleware classes
	Middlewares []LaravelMiddleware

	// Routes are detected route definitions
	Routes []LaravelRoute
}

// LaravelController represents a Laravel controller class.
type LaravelController struct {
	// Class is the underlying PHP class
	Class PHPClass

	// Actions are public methods that can be route handlers
	Actions []PHPMethod

	// Middleware are middleware applied to this controller
	Middleware []string
}

// LaravelModel represents an Eloquent model class.
type LaravelModel struct {
	// Class is the underlying PHP class
	Class PHPClass

	// TableName is the database table (if specified)
	TableName string

	// Fillable are mass-assignable attributes
	Fillable []string

	// Hidden are attributes hidden from serialization
	Hidden []string

	// Relationships are detected model relationships
	Relationships []LaravelRelationship
}

// LaravelRequest represents a Form Request class.
type LaravelRequest struct {
	// Class is the underlying PHP class
	Class PHPClass

	// Rules are validation rules (if detectable)
	Rules map[string]string

	// AuthorizationMethod is the authorize method (if present)
	AuthorizationMethod *PHPMethod
}

// LaravelResource represents an API Resource class.
type LaravelResource struct {
	// Class is the underlying PHP class
	Class PHPClass

	// ToArrayMethod is the toArray transformation method
	ToArrayMethod *PHPMethod
}

// LaravelMiddleware represents a middleware class.
type LaravelMiddleware struct {
	// Class is the underlying PHP class
	Class PHPClass

	// HandleMethod is the middleware handle method
	HandleMethod *PHPMethod
}

// LaravelRoute represents a route definition.
type LaravelRoute struct {
	// Method is the HTTP method (GET, POST, etc.)
	Method string

	// URI is the route URI pattern
	URI string

	// Controller is the controller class (if any)
	Controller string

	// Action is the controller method (if any)
	Action string

	// Position is the location in the source file
	Position SourcePosition
}

// LaravelRelationship represents a model relationship.
type LaravelRelationship struct {
	// Name is the relationship method name
	Name string

	// Type is the relationship type (hasMany, belongsTo, etc.)
	Type string

	// RelatedModel is the related model class
	RelatedModel string
}

// PHPParseResult contains the complete result of PHP file parsing.
type PHPParseResult struct {
	// FileStructure contains all PHP constructs
	FileStructure *PHPFileStructure

	// LaravelPatterns contains detected Laravel patterns
	LaravelPatterns *LaravelPatterns

	// Errors contains any parsing or extraction errors
	Errors []error

	// ParsedFromCache indicates if results came from cache
	ParsedFromCache bool

	// Statistics contains parsing performance metrics
	Statistics ParseStatistics
}

// ParseStatistics contains metrics about the parsing operation.
type ParseStatistics struct {
	// FilePath is the parsed file path
	FilePath string

	// FileSize is the file size in bytes
	FileSize int64

	// ParseDuration is the total parsing time
	ParseDuration time.Duration

	// ExtractionDuration is the construct extraction time
	ExtractionDuration time.Duration

	// ConstructCount is the total number of constructs found
	ConstructCount int

	// ErrorCount is the number of parsing errors
	ErrorCount int

	// CacheHit indicates if the file was served from cache
	CacheHit bool
}

// ParserConfig contains configuration for the PHP parser.
type ParserConfig struct {
	// MaxFileSize is the maximum file size to parse (bytes)
	MaxFileSize int64

	// MaxParseTime is the maximum time to spend parsing a single file
	MaxParseTime time.Duration

	// PoolSize is the number of parser instances in the pool
	PoolSize int

	// EnableLaravelPatterns enables Laravel-specific pattern detection
	EnableLaravelPatterns bool

	// EnableDocBlocks enables PHPDoc parsing
	EnableDocBlocks bool

	// EnableDetailedErrors provides detailed error reporting
	EnableDetailedErrors bool
}

// ParserStats contains parser performance and resource statistics.
type ParserStats struct {
	// TotalFilesParsed is the total number of files processed
	TotalFilesParsed int64

	// ParsedFiles is the number of successfully parsed files
	ParsedFiles int64

	// FailedFiles is the number of files that failed to parse
	FailedFiles int64

	// TotalJobsProcessed is the total number of parsing jobs processed
	TotalJobsProcessed int64

	// TotalParseTime is the cumulative parsing time
	TotalParseTime time.Duration

	// AverageParseTime is the average time per file
	AverageParseTime time.Duration

	// CacheHitRate is the cache hit percentage
	CacheHitRate float64

	// ErrorRate is the parsing error percentage
	ErrorRate float64

	// PoolUtilization is the parser pool utilization percentage
	PoolUtilization float64

	// MemoryUsage is the estimated memory usage in bytes
	MemoryUsage int64

	// ActiveParsers is the current number of active parsers
	ActiveParsers int
}

// ErrorStats contains error reporting and recovery statistics.
type ErrorStats struct {
	// TotalErrors is the total number of parsing errors
	TotalErrors int64

	// RecoverableErrors is the number of errors that were handled gracefully
	RecoverableErrors int64

	// FatalErrors is the number of errors that prevented parsing
	FatalErrors int64

	// ErrorRate is the error percentage
	ErrorRate float64

	// RecoveryRate is the recovery percentage
	RecoveryRate float64

	// CommonErrors are the most frequently encountered errors
	CommonErrors map[string]int64
}

// SourcePosition represents a position in a source file.
type SourcePosition struct {
	// StartLine is the starting line number (1-indexed)
	StartLine int

	// StartColumn is the starting column number (1-indexed)
	StartColumn int

	// EndLine is the ending line number (1-indexed)
	EndLine int

	// EndColumn is the ending column number (1-indexed)
	EndColumn int

	// StartByte is the starting byte offset
	StartByte int

	// EndByte is the ending byte offset
	EndByte int
}

// Manifest represents the Oxinfer manifest configuration from the manifest system.
// Import alias to avoid circular dependencies with internal/manifest package.
type Manifest = manifest.Manifest

// ProjectParserConfig contains comprehensive configuration for PHP project parsing.
type ProjectParserConfig struct {
	// Project settings
	ProjectRoot  string // Project root directory
	ComposerPath string // Path to composer.json

	// Discovery settings (file indexer integration)
	Targets  []string // Target directories to scan
	Globs    []string // Glob patterns for PHP files
	MaxFiles int      // Maximum files to process
	MaxDepth int      // Maximum directory depth

	// Parsing settings
	MaxWorkers   int           // Maximum concurrent workers
	ParseTimeout time.Duration // Per-file parse timeout

	// Caching settings (file indexer integration)
	CacheEnabled bool   // Enable file caching
	CacheKind    string // "mtime" or "sha256+mtime"

	// Feature flags for construct extraction
	ExtractClasses    bool // Extract class information
	ExtractMethods    bool // Extract method information
	ExtractNamespaces bool // Extract namespace information
	ExtractTraits     bool // Extract trait information
	ExtractInterfaces bool // Extract interface information
}

// ProjectParseResult contains the comprehensive results of PHP project parsing.
type ProjectParseResult struct {
	// Discovered files (file indexer integration)
	DiscoveredFiles []any // Files found by indexer

	// Parse results
	ParsedFiles []ParsedFileResult // Successfully parsed files
	FailedFiles []FailedFileResult // Files that failed parsing

	// Extracted PHP constructs
	Classes    []PHPClass     // Extracted PHP classes
	Methods    []PHPMethod    // Extracted PHP methods
	Namespaces []PHPNamespace // Extracted namespaces
	Traits     []PHPTrait     // Extracted traits
	Interfaces []PHPInterface // Extracted interfaces

	// Project statistics
	Stats       ProjectParseStats // Performance metrics
	Partial     bool              // true if limits caused truncation
	TruncatedBy []string          // Which limits caused truncation
}

// ParsedFileResult contains results for a successfully parsed file.
type ParsedFileResult struct {
	FilePath     string        // PHP file path
	RelativePath string        // Path relative to project root
	Namespace    string        // File's namespace (resolved via PSR-4)
	Classes      []string      // Class names in file
	Methods      []string      // Method names in file
	ParseTime    time.Duration // Time to parse file
	ExtractTime  time.Duration // Time to extract constructs
	CacheHit     bool          // Whether result came from cache
}

// FailedFileResult contains information about files that failed parsing.
type FailedFileResult struct {
	FilePath string // PHP file path that failed
	Error    error  // Parsing error
}

// ProjectParseStats contains comprehensive statistics about project parsing.
type ProjectParseStats struct {
	// Discovery stats (file indexer integration)
	FilesDiscovered int // Total files found
	FilesSkipped    int // Files skipped by cache

	// Parse stats
	FilesParsed int // Successfully parsed files
	FilesFailed int // Files that failed parsing

	// Extract stats
	ClassesExtracted    int // Total classes found
	MethodsExtracted    int // Total methods found
	TraitsExtracted     int // Total traits found
	InterfacesExtracted int // Total interfaces found

	// Performance stats
	TotalDuration  time.Duration // Total processing time
	DiscoveryTime  time.Duration // Time spent discovering files
	ParseTime      time.Duration // Time spent parsing
	ExtractionTime time.Duration // Time spent extracting

	// Resource usage
	PeakMemoryUsage int64   // Peak memory usage
	CacheHitRate    float64 // Cache effectiveness %
}

// ProjectParserProgress tracks real-time progress through the parsing pipeline.
type ProjectParserProgress struct {
	// Current phase
	Phase       ProjectParserPhase // Current parsing phase
	PhaseStatus string             // Human-readable phase status

	// File progress
	FilesDiscovered int // Files discovered so far
	FilesParsed     int // Files parsed so far
	FilesExtracted  int // Files with constructs extracted
	FilesFailed     int // Files that failed processing

	// Construct progress
	ClassesFound    int // Classes extracted so far
	MethodsFound    int // Methods extracted so far
	TraitsFound     int // Traits extracted so far
	InterfacesFound int // Interfaces extracted so far

	// Performance metrics
	ElapsedTime        time.Duration // Time since parsing started
	EstimatedRemaining time.Duration // Estimated time remaining
	ThroughputPerSec   float64       // Files processed per second

	// Resource usage
	CurrentMemoryUsage int64 // Current memory usage
	ActiveWorkers      int   // Current active workers

	// Status flags
	IsComplete bool // Whether parsing is complete
	HasErrors  bool // Whether any errors occurred
}

// ProjectParserPhase represents the current phase of project parsing.
type ProjectParserPhase int

const (
	ProjectParserPhaseInitializing ProjectParserPhase = iota
	ProjectParserPhaseDiscovering
	ProjectParserPhaseResolving
	ProjectParserPhaseParsing
	ProjectParserPhaseExtracting
	ProjectParserPhaseCompleted
	ProjectParserPhaseFailed
)

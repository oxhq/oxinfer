// Package parser defines core data types for PHP parsing with tree-sitter.
// These types provide Go-friendly wrappers around tree-sitter C structures
// and organize parsing results into structured formats for analysis.
package parser

import (
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

// ParseResult represents the complete result of parsing PHP content.
// Wraps tree-sitter tree with metadata and error information for analysis.
type ParseResult struct {
	Tree       *sitter.Tree // Raw tree-sitter AST tree
	RootNode   *sitter.Node // Root AST node for traversal
	FilePath   string       // Source file path (if from file)
	Content    []byte       // Original PHP source content
	Stats      ParseStats   // Parsing performance statistics
	HasErrors  bool         // True if syntax errors were detected
	Errors     []ParseError // List of syntax errors found
	ParsedAt   time.Time    // When parsing was completed
}

// ParseStats contains performance metrics for parsing operations.
// Used for monitoring parser performance and resource usage.
type ParseStats struct {
	ParseTime    time.Duration // Time spent parsing content
	TreeSize     int           // Number of nodes in AST tree
	ContentSize  int64         // Size of source content in bytes
	ErrorCount   int           // Number of syntax errors detected
	MaxDepth     int           // Maximum tree depth reached
	NodeTypes    map[string]int // Count of each node type found
}

// ParseError represents a single PHP syntax error detected by tree-sitter.
// Contains position information and error context for debugging.
type ParseError struct {
	Type        string // Error type classification
	Message     string // Human-readable error description
	Line        int    // Error line number (1-indexed)
	Column      int    // Error column number (1-indexed)
	StartByte   uint32 // Start position in source bytes
	EndByte     uint32 // End position in source bytes
	NodeType    string // Tree-sitter node type where error occurred
	ExpectedText string // Expected syntax (if available)
	ActualText   string // Actual text that caused error
}

// DefaultParserConfig returns sensible default configuration values.
// Provides balanced performance and functionality for typical use cases.
// Uses the ParserConfig type defined in interfaces.go.
func DefaultParserConfig() *ParserConfig {
	return &ParserConfig{
		MaxFileSize:           1024 * 1024, // 1MB limit
		MaxParseTime:          10 * time.Second,
		PoolSize:              4, // Default pool size
		EnableLaravelPatterns: true,
		EnableDocBlocks:       true,
		EnableDetailedErrors:  true,
	}
}

// NodeVisitor defines a callback function for tree traversal operations.
// Used with tree walking functions to process specific node types.
type NodeVisitor func(node *sitter.Node, depth int) (continueTraversal bool, err error)

// TreeStatistics contains detailed information about parsed AST trees.
// Used for analyzing PHP code structure and complexity metrics.
type TreeStatistics struct {
	TotalNodes        int            // Total number of nodes in tree
	MaxDepth          int            // Maximum depth reached
	NodeTypeCounts    map[string]int // Count of each node type
	PHPConstructs     PHPConstructCounts // Count of PHP language constructs
	ComplexityScore   float64        // Estimated code complexity
	AnalysisTime      time.Duration  // Time spent analyzing tree
}

// PHPConstructCounts tracks counts of PHP language constructs found in code.
// Provides insights into code composition and Laravel usage patterns.
type PHPConstructCounts struct {
	Namespaces  int // Number of namespace declarations
	Classes     int // Number of class definitions
	Interfaces  int // Number of interface definitions
	Traits      int // Number of trait definitions
	Functions   int // Number of function definitions
	Methods     int // Number of method definitions
	Properties  int // Number of property definitions
	UseStatements int // Number of use/import statements
	Constants   int // Number of constant definitions
	Comments    int // Number of comment blocks
}

// ParseJob represents a single PHP parsing task for concurrent processing.
// Contains all information needed to parse a file independently.
type ParseJob struct {
	ID          string    // Unique job identifier
	FilePath    string    // Path to PHP file to parse
	Content     []byte    // File content (if already loaded)
	Priority    int       // Job priority (higher = more urgent)
	Config      *ParserConfig // Parser configuration for this job
	SubmittedAt time.Time // When job was created
	StartedAt   time.Time // When parsing began
	Deadline    time.Time // Latest acceptable completion time
}

// ParseJobResult contains the result of a completed parsing job.
// Used by concurrent parser pool to return results to callers.
type ParseJobResult struct {
	JobID     string       // Original job identifier
	Result    *ParseResult // Parse result (nil if error)
	Error     error        // Error that occurred during parsing
	Duration  time.Duration // Time spent processing job
	WorkerID  string       // Identifier of worker that processed job
	CacheHit  bool         // Whether result came from cache
}

// ParserMetrics tracks runtime performance and resource usage statistics.
// Used for monitoring parser health and optimizing performance.
type ParserMetrics struct {
	TotalParseJobs     int64         // Total number of parse jobs processed
	SuccessfulParses   int64         // Number of successful parses
	FailedParses       int64         // Number of failed parses
	TotalParseTime     time.Duration // Cumulative time spent parsing
	AverageParseTime   time.Duration // Average time per parse operation
	CacheHits          int64         // Number of cache hits
	CacheHitRate       float64       // Cache hit rate percentage
	MaxMemoryUsage     int64         // Peak memory usage observed
	CurrentMemoryUsage int64         // Current estimated memory usage
	ActiveParsers      int           // Number of active parser instances
	QueuedJobs         int           // Number of jobs waiting for processing
}

// TreeWalkResult contains results from tree traversal operations.
// Used when walking AST trees to collect specific information.
type TreeWalkResult struct {
	NodesVisited   int                     // Total nodes visited during walk
	NodesMatched   int                     // Nodes that matched criteria
	MatchedNodes   []*sitter.Node          // Nodes that matched (if collected)
	NodeData       map[*sitter.Node]interface{} // Additional data collected per node
	TraversalTime  time.Duration           // Time spent traversing tree
	EarlyTermination bool                  // Whether traversal was terminated early
	Error          error                   // Error that stopped traversal
}

// QueryPattern represents a tree-sitter query pattern for matching PHP constructs.
// Used to find specific language patterns in parsed AST trees.
type QueryPattern struct {
	Name        string // Human-readable pattern name
	Query       string // S-expression query pattern
	Description string // What this pattern matches
	Examples    []string // Example PHP code that matches
}

// Common PHP construct query patterns for tree-sitter matching.
var (
	ClassDeclarationPattern = &QueryPattern{
		Name:  "class_declaration",
		Query: "(class_declaration name: (name) @class.name)",
		Description: "Matches PHP class declarations",
		Examples: []string{"class MyClass {}", "abstract class BaseClass {}"},
	}

	FunctionDeclarationPattern = &QueryPattern{
		Name:  "function_declaration", 
		Query: "(function_definition name: (name) @function.name)",
		Description: "Matches PHP function declarations",
		Examples: []string{"function myFunction() {}", "public function test() {}"},
	}

	NamespaceDeclarationPattern = &QueryPattern{
		Name:  "namespace_declaration",
		Query: "(namespace_definition name: (namespace_name) @namespace.name)",
		Description: "Matches PHP namespace declarations", 
		Examples: []string{"namespace App\\Http\\Controllers;", "namespace MyVendor\\Package;"},
	}

	UseStatementPattern = &QueryPattern{
		Name:  "use_statement",
		Query: "(namespace_use_declaration (namespace_use_clause name: (qualified_name) @use.name))",
		Description: "Matches PHP use/import statements",
		Examples: []string{"use Illuminate\\Http\\Request;", "use App\\Models\\User as UserModel;"},
	}

	MethodDeclarationPattern = &QueryPattern{
		Name:  "method_declaration",
		Query: "(method_declaration name: (name) @method.name)",
		Description: "Matches PHP method declarations within classes",
		Examples: []string{"public function index() {}", "private static function helper() {}"},
	}
)

// NodeTypeClassifier provides semantic classification of tree-sitter nodes.
// Used to categorize nodes by their role in PHP language structure.
type NodeTypeClassifier struct {
	DeclarationTypes []string // Node types that declare new symbols
	ExpressionTypes  []string // Node types that represent expressions
	StatementTypes   []string // Node types that represent statements
	LiteralTypes     []string // Node types that represent literal values
	OperatorTypes    []string // Node types that represent operators
}

// PHPNodeClassifier returns a classifier configured for PHP language constructs.
func PHPNodeClassifier() *NodeTypeClassifier {
	return &NodeTypeClassifier{
		DeclarationTypes: []string{
			"class_declaration", "function_definition", "method_declaration",
			"property_declaration", "interface_declaration", "trait_declaration",
			"namespace_definition", "const_declaration", "parameter",
		},
		ExpressionTypes: []string{
			"binary_expression", "assignment_expression", "call_expression",
			"member_access_expression", "subscript_expression", "conditional_expression",
			"new_expression", "clone_expression", "instanceof_expression",
		},
		StatementTypes: []string{
			"expression_statement", "if_statement", "while_statement", "for_statement",
			"foreach_statement", "return_statement", "break_statement", "continue_statement",
			"try_statement", "throw_statement", "switch_statement", "declare_statement",
		},
		LiteralTypes: []string{
			"integer", "float", "string", "boolean", "null", "array", "heredoc",
		},
		OperatorTypes: []string{
			"=", "+", "-", "*", "/", "%", "==", "!=", "===", "!==", "<", ">", 
			"<=", ">=", "&&", "||", "!", "&", "|", "^", "<<", ">>", "??",
		},
	}
}

// IsDeclaration returns true if the node type represents a declaration.
func (c *NodeTypeClassifier) IsDeclaration(nodeType string) bool {
	for _, dt := range c.DeclarationTypes {
		if dt == nodeType {
			return true
		}
	}
	return false
}

// IsExpression returns true if the node type represents an expression.
func (c *NodeTypeClassifier) IsExpression(nodeType string) bool {
	for _, et := range c.ExpressionTypes {
		if et == nodeType {
			return true
		}
	}
	return false
}

// IsStatement returns true if the node type represents a statement.
func (c *NodeTypeClassifier) IsStatement(nodeType string) bool {
	for _, st := range c.StatementTypes {
		if st == nodeType {
			return true
		}
	}
	return false
}

// ValidationResult contains the result of validating a parsed PHP file.
// Used to check for common PHP syntax and semantic issues.
type ValidationResult struct {
	IsValid        bool              // Whether PHP code is valid
	SyntaxErrors   []ParseError      // Syntax errors found
	SemanticIssues []SemanticIssue   // Semantic problems detected
	Warnings       []ValidationWarning // Non-fatal issues found
	ValidatedAt    time.Time         // When validation was performed
}

// SemanticIssue represents a semantic problem in PHP code.
// Detected issues that are syntactically valid but potentially problematic.
type SemanticIssue struct {
	Type        string // Issue type (undefined_variable, unused_import, etc.)
	Message     string // Description of the issue
	Severity    string // Issue severity (error, warning, info)
	Line        int    // Line where issue occurs
	Column      int    // Column where issue occurs
	Suggestion  string // Suggested fix (if available)
}

// ValidationWarning represents a non-fatal issue in PHP code.
// Includes style violations and potential improvements.
type ValidationWarning struct {
	Type       string // Warning type
	Message    string // Warning description  
	Line       int    // Warning line number
	Column     int    // Warning column number
	Rule       string // Validation rule that triggered warning
	Confidence float64 // Confidence in warning (0.0-1.0)
}
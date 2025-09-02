// Package parser provides PHP source code analysis using tree-sitter.
// Implements the tree-sitter foundation for parsing PHP files with proper
// resource management, error handling, and thread safety.
package parser

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

// DefaultPHPParser implements TreeSitterParser interface using tree-sitter PHP grammar.
// Provides thread-safe access to tree-sitter parsing with proper resource management.
type DefaultPHPParser struct {
	parser     *sitter.Parser   // Tree-sitter parser instance
	language   *sitter.Language // PHP language grammar
	config     *ParserConfig    // Parser configuration
	mu         sync.Mutex       // Mutex for thread safety
	closed     bool             // Whether parser has been closed
	stats      *ParserMetrics   // Runtime metrics
	classifier *NodeTypeClassifier // Node type classifier
}

// NewPHPParser creates and initializes a new PHP parser instance.
// Returns error if tree-sitter PHP language cannot be loaded or parser initialization fails.
func NewPHPParser(config *ParserConfig) (*DefaultPHPParser, error) {
	if config == nil {
		config = DefaultParserConfig()
	}

	// Create new tree-sitter parser
	parser := sitter.NewParser()
	if parser == nil {
		return nil, NewInternalError("parser", "failed to create tree-sitter parser", nil)
	}

	// Get PHP language grammar
	language := php.GetLanguage()
	if language == nil {
		return nil, NewInternalError("language", "failed to load PHP language grammar", nil)
	}

	// Set language on parser
	parser.SetLanguage(language)

	return &DefaultPHPParser{
		parser:     parser,
		language:   language,
		config:     config,
		closed:     false,
		stats:      &ParserMetrics{},
		classifier: PHPNodeClassifier(),
	}, nil
}

// ParseContent parses PHP content and returns the raw syntax tree.
// Implements TreeSitterParser interface for content parsing.
func (p *DefaultPHPParser) ParseContent(content []byte) (*SyntaxTree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrParserClosed
	}

	if len(content) == 0 {
		return nil, NewParserError("empty content provided", ErrInvalidPHPContent)
	}

	if int64(len(content)) > p.config.MaxFileSize {
		return nil, NewParserError(
			fmt.Sprintf("content size %d exceeds limit %d", len(content), p.config.MaxFileSize),
			ErrContentTooLarge,
		)
	}

	startTime := time.Now()

	// Parse content with tree-sitter
	tree, err := p.parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, WrapTreeSitterError("parse content", err)
	}
	if tree == nil {
		return nil, NewParserError("tree-sitter parsing returned nil", ErrParsingFailed)
	}

	parseTime := time.Since(startTime)

	// Get root node
	rootNode := tree.RootNode()
	if rootNode == nil {
		tree.Close()
		return nil, NewParserError("parsed tree has no root node", ErrParsingFailed)
	}

	// Create syntax tree wrapper
	syntaxTree := &SyntaxTree{
		Root:     convertNode(rootNode, content),
		Source:   content,
		Language: "php",
		ParsedAt: time.Now(),
	}

	// Check for syntax errors
	hasErrors, _ := p.extractSyntaxErrors(rootNode, "", content)
	
	// Update parser metrics
	p.updateMetrics(parseTime, hasErrors)

	return syntaxTree, nil
}

// ParseFile parses a PHP file and returns the syntax tree.
// Implements TreeSitterParser interface for file parsing.
func (p *DefaultPHPParser) ParseFile(ctx context.Context, filePath string) (*SyntaxTree, error) {
	if p.closed {
		return nil, ErrParserClosed
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, WrapFileError(filePath, "read", err)
	}

	// Check file size limit
	if int64(len(content)) > p.config.MaxFileSize {
		return nil, NewParserErrorWithFile(filePath,
			fmt.Sprintf("file size %d exceeds limit %d", len(content), p.config.MaxFileSize),
			ErrContentTooLarge,
		)
	}

	// Parse content with context for timeout handling
	done := make(chan struct{})
	var syntaxTree *SyntaxTree
	var parseErr error

	go func() {
		defer close(done)
		syntaxTree, parseErr = p.ParseContent(content)
		if syntaxTree != nil {
			// Update syntax tree with file path
			syntaxTree.Root.Text = filePath // Store file path in root for reference
		}
	}()

	// Handle timeout and context cancellation
	select {
	case <-done:
		if parseErr != nil {
			return nil, WrapFileError(filePath, "parse", parseErr)
		}
		return syntaxTree, nil
	case <-ctx.Done():
		return nil, NewParserErrorWithFile(filePath, 
			"parsing cancelled or timed out", ctx.Err())
	case <-time.After(p.config.MaxParseTime):
		return nil, NewTimeoutError(filePath, 
			"parsing exceeded time limit",
			p.config.MaxParseTime.Milliseconds(),
			int64(len(content)))
	}
}

// IsInitialized returns true if the parser is ready for use.
// Checks that parser and language are properly initialized.
func (p *DefaultPHPParser) IsInitialized() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	return !p.closed && p.parser != nil && p.language != nil
}

// Close releases parser resources and cleans up memory.
// Must be called to prevent memory leaks in long-running processes.
func (p *DefaultPHPParser) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil // Already closed
	}

	p.closed = true

	// Note: tree-sitter parser cleanup is handled by GC
	// Individual trees must be closed by callers
	p.parser = nil
	p.language = nil

	return nil
}

// GetLanguage returns the PHP language grammar for query construction.
// Used by query engines to create tree-sitter queries.
func (p *DefaultPHPParser) GetLanguage() *sitter.Language {
	return p.language
}

// GetMetrics returns current parser performance metrics.
func (p *DefaultPHPParser) GetMetrics() *ParserMetrics {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return copy of metrics to prevent concurrent access
	metricsCopy := *p.stats
	return &metricsCopy
}

// convertNode converts a tree-sitter node to our SyntaxNode format.
// Recursively processes child nodes to build complete tree structure.
func convertNode(node *sitter.Node, source []byte) *SyntaxNode {
	if node == nil {
		return nil
	}

	syntaxNode := &SyntaxNode{
		Type:       node.Type(),
		Text:       string(node.Content(source)),
		StartByte:  int(node.StartByte()),
		EndByte:    int(node.EndByte()),
		StartPoint: Point{Row: int(node.StartPoint().Row), Column: int(node.StartPoint().Column)},
		EndPoint:   Point{Row: int(node.EndPoint().Row), Column: int(node.EndPoint().Column)},
		Children:   make([]*SyntaxNode, 0, node.ChildCount()),
	}

	// Convert child nodes
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			childNode := convertNode(child, source)
			if childNode != nil {
				childNode.Parent = syntaxNode
				syntaxNode.Children = append(syntaxNode.Children, childNode)
			}
		}
	}

	return syntaxNode
}

// calculateTreeDepth recursively calculates the maximum depth of the AST tree.
func calculateTreeDepth(node *sitter.Node) int {
	if node == nil {
		return 0
	}

	maxChildDepth := 0
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			childDepth := calculateTreeDepth(child)
			if childDepth > maxChildDepth {
				maxChildDepth = childDepth
			}
		}
	}

	return maxChildDepth + 1
}

// extractSyntaxErrors traverses the tree looking for ERROR nodes indicating syntax problems.
func (p *DefaultPHPParser) extractSyntaxErrors(node *sitter.Node, filePath string, source []byte) (bool, []ParseError) {
	if node == nil {
		return false, nil
	}

	var errors []ParseError
	hasErrors := false

	// Check if current node is an error
	if node.Type() == "ERROR" {
		hasErrors = true
		error := ParseError{
			Type:       "syntax_error",
			Message:    "PHP syntax error",
			Line:       int(node.StartPoint().Row) + 1, // Convert to 1-indexed
			Column:     int(node.StartPoint().Column) + 1,
			StartByte:  uint32(node.StartByte()),
			EndByte:    uint32(node.EndByte()),
			NodeType:   node.Type(),
			ActualText: string(node.Content(source)),
		}
		errors = append(errors, error)
	}

	// Recursively check child nodes
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			childHasErrors, childErrors := p.extractSyntaxErrors(child, filePath, source)
			if childHasErrors {
				hasErrors = true
				errors = append(errors, childErrors...)
			}
		}
	}

	return hasErrors, errors
}

// updateMetrics updates parser performance metrics.
func (p *DefaultPHPParser) updateMetrics(parseTime time.Duration, hasErrors bool) {
	p.stats.TotalParseJobs++
	p.stats.TotalParseTime += parseTime
	
	if hasErrors {
		p.stats.FailedParses++
	} else {
		p.stats.SuccessfulParses++
	}

	// Calculate averages
	if p.stats.TotalParseJobs > 0 {
		p.stats.AverageParseTime = p.stats.TotalParseTime / time.Duration(p.stats.TotalParseJobs)
	}
}

// ValidateConfig validates parser configuration values.
func ValidateConfig(config *ParserConfig) error {
	if config == nil {
		return NewParserError("parser configuration is nil", ErrParserNotInitialized)
	}

	if config.MaxFileSize <= 0 {
		return NewParserError("MaxFileSize must be positive", ErrInvalidPHPContent)
	}

	if config.MaxParseTime <= 0 {
		return NewParserError("MaxParseTime must be positive", ErrInvalidPHPContent)
	}

	if config.PoolSize <= 0 {
		return NewParserError("PoolSize must be positive", ErrInvalidPHPContent)
	}

	return nil
}
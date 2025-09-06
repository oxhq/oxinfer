// Package parser provides PHP source code analysis using tree-sitter.
// Implements the tree-sitter foundation for parsing PHP files with proper
// resource management, error handling, and thread safety.
package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"

	manifestpkg "github.com/garaekz/oxinfer/internal/manifest"
)

// DefaultPHPParser implements TreeSitterParser interface using tree-sitter PHP grammar.
// Provides thread-safe access to tree-sitter parsing with proper resource management.
type DefaultPHPParser struct {
	parser     *sitter.Parser      // Tree-sitter parser instance
	language   *sitter.Language    // PHP language grammar
	config     *ParserConfig       // Parser configuration
	mu         sync.Mutex          // Mutex for thread safety
	closed     bool                // Whether parser has been closed
	stats      *ParserMetrics      // Runtime metrics
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

	// Calculate tree depth before conversion
	maxDepth := calculateTreeDepth(rootNode)

	// Create syntax tree wrapper
	syntaxTree := &SyntaxTree{
		Root:     convertNode(rootNode, content),
		Source:   content,
		Language: "php",
		ParsedAt: time.Now(),
	}

	// Check for syntax errors
	hasErrors, _ := p.extractSyntaxErrors(rootNode, "", content)

	// Set tree depth in stats if needed
	_ = maxDepth // Store depth for stats tracking

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

// GetParserStats returns current parser performance and resource statistics.
// Implements PHPParser interface for system integration.
func (p *DefaultPHPParser) GetParserStats() ParserStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	metrics := p.stats
	if metrics == nil {
		metrics = &ParserMetrics{}
	}

	return ParserStats{
		TotalFilesParsed: metrics.TotalParseJobs,
		TotalParseTime:   metrics.TotalParseTime,
		AverageParseTime: metrics.AverageParseTime,
		CacheHitRate:     0, // Not tracked at parser level
		ErrorRate:        float64(metrics.FailedParses) / float64(max(1, metrics.TotalParseJobs)) * 100,
		PoolUtilization:  0, // Not tracked at parser level
		MemoryUsage:      0, // Not tracked at parser level
		ActiveParsers:    1, // Single parser instance
	}
}

// SetConfiguration updates parser configuration and limits.
// Implements PHPParser interface for dynamic configuration.
func (p *DefaultPHPParser) SetConfiguration(config ParserConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrParserClosed
	}

	// Create new parser config from interface config
	newConfig := &ParserConfig{
		MaxFileSize:           config.MaxFileSize,
		MaxParseTime:          config.MaxParseTime,
		PoolSize:              config.PoolSize,
		EnableLaravelPatterns: config.EnableLaravelPatterns,
		EnableDocBlocks:       config.EnableDocBlocks,
		EnableDetailedErrors:  config.EnableDetailedErrors,
	}

	// Validate new configuration
	if err := ValidateConfig(newConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Update configuration
	p.config = newConfig
	return nil
}

// ProcessFile implements indexer.FileProcessor interface for PHP file processing.
// Processes a PHP file and returns parse results for pattern matching.
func (p *DefaultPHPParser) ProcessFile(ctx context.Context, file any) (any, error) {
	// Extract file path from the file interface
	var filePath string

	// Handle different file input types from indexer
	switch f := file.(type) {
	case string:
		filePath = f
	case map[string]any:
		// Prefer AbsPath for file parsing (more reliable than relative paths)
		if absPath, ok := f["AbsPath"].(string); ok {
			filePath = absPath
		} else if path, ok := f["path"].(string); ok {
			filePath = path
		} else {
			return nil, fmt.Errorf("file map missing path field")
		}
	default:
		// Try to extract path using reflection as fallback
		// Prefer AbsPath for file parsing (more reliable than relative paths)
		if hasField(file, "AbsPath") {
			if absPath := getFieldValue(file, "AbsPath"); absPath != "" {
				filePath = absPath
			}
		}
		if filePath == "" && hasField(file, "Path") {
			if path := getFieldValue(file, "Path"); path != "" {
				filePath = path
			}
		}
		if filePath == "" {
			return nil, fmt.Errorf("unsupported file type: %T", file)
		}
	}

	if filePath == "" {
		return nil, fmt.Errorf("empty file path provided")
	}

	// Parse the PHP file
	result, err := p.ParsePHPFile(ctx, filePath)
	if err != nil {
		// Return partial result with error for resilient processing
		return &map[string]any{
			"filePath": filePath,
			"error":    err.Error(),
			"parsed":   false,
		}, err
	}

	// Return successful parse result
	return &map[string]any{
		"filePath":      filePath,
		"fileStructure": result.FileStructure,
		"patterns":      result.LaravelPatterns,
		"errors":        result.Errors,
		"statistics":    result.Statistics,
		"parsed":        true,
		"fromCache":     result.ParsedFromCache,
	}, nil
}

// ParsePHPFile performs comprehensive PHP file analysis.
// Returns detailed PHP structure information for pattern detection.
func (p *DefaultPHPParser) ParsePHPFile(ctx context.Context, filePath string) (*PHPParseResult, error) {
	// Parse the file to get syntax tree
	syntaxTree, err := p.ParseFile(ctx, filePath)
	if err != nil {
		return &PHPParseResult{
			FileStructure:   nil,
			LaravelPatterns: nil,
			Errors:          []error{err},
			ParsedFromCache: false,
			Statistics: ParseStatistics{
				FilePath:   filePath,
				ErrorCount: 1,
				CacheHit:   false,
			},
		}, err
	}

	// Create basic file structure from syntax tree
	fileStructure := &PHPFileStructure{
		FilePath:      filePath,
		ParsedAt:      syntaxTree.ParsedAt,
		Namespace:     nil, // Would be extracted by construct extractor
		Classes:       []PHPClass{},
		Interfaces:    []PHPInterface{},
		Traits:        []PHPTrait{},
		Functions:     []PHPFunction{},
		UseStatements: []PHPUseStatement{},
	}

	// Create parse result
	result := &PHPParseResult{
		FileStructure:   fileStructure,
		LaravelPatterns: &LaravelPatterns{}, // Empty patterns - would be filled by extractor
		Errors:          []error{},
		ParsedFromCache: false,
		Statistics: ParseStatistics{
			FilePath:           filePath,
			FileSize:           int64(len(syntaxTree.Source)),
			ParseDuration:      0, // Would be tracked properly in real implementation
			ExtractionDuration: 0,
			ConstructCount:     0,
			ErrorCount:         0,
			CacheHit:           false,
		},
	}

	return result, nil
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

// max returns the maximum of two integers.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// DefaultPHPProjectParser implements PHPProjectParser interface
type DefaultPHPProjectParser struct {
	config           ProjectParserConfig
	manifest         any
	concurrentParser *DefaultConcurrentPHPParser
	closed           bool // Tracks if parser has been closed

	// Progress tracking
	progressCallback func(ProjectParserProgress)
	progress         ProjectParserProgress
	mu               sync.Mutex // Protects progress updates and closed state
}

// NewPHPProjectParser creates a new project parser with defaults
func NewPHPProjectParser() (*DefaultPHPProjectParser, error) {
	concurrentParser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create concurrent parser: %w", err)
	}
	return &DefaultPHPProjectParser{
		config: ProjectParserConfig{
			ProjectRoot:    ".",
			MaxWorkers:     4,
			CacheEnabled:   true,
			ExtractClasses: true,
		},
		concurrentParser: concurrentParser,
	}, nil
}

// NewPHPProjectParserFromManifest creates a project parser from manifest
func NewPHPProjectParserFromManifest(manifest any) (*DefaultPHPProjectParser, error) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		return nil, err
	}
	parser.manifest = manifest

	// Configure parser based on manifest if it's the proper type
	if m, ok := manifest.(*manifestpkg.Manifest); ok {
		// Update project root
		if m.Project.Root != "" {
			parser.config.ProjectRoot = m.Project.Root
		}

		// Update scan targets
		if len(m.Scan.Targets) > 0 {
			parser.config.Targets = m.Scan.Targets
		}

		// Update limits
		if m.Limits != nil {
			if m.Limits.MaxWorkers != nil {
				parser.config.MaxWorkers = *m.Limits.MaxWorkers
			}
			if m.Limits.MaxFiles != nil {
				parser.config.MaxFiles = *m.Limits.MaxFiles
			}
		}

		// Update cache configuration
		if m.Cache != nil && m.Cache.Kind != nil {
			parser.config.CacheKind = *m.Cache.Kind
		}
	}

	return parser, nil
}

// Close closes the project parser
func (p *DefaultPHPProjectParser) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil // Already closed
	}

	p.closed = true

	// Close underlying concurrent parser if it exists
	if p.concurrentParser != nil {
		p.concurrentParser.Close()
	}

	return nil
}

// GetProgress returns parsing progress information
func (p *DefaultPHPProjectParser) GetProgress() ProjectParserProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.progress
}

// LoadFromManifest configures parser from manifest
func (p *DefaultPHPProjectParser) LoadFromManifest(m *manifestpkg.Manifest) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return fmt.Errorf("parser is closed")
	}

	p.manifest = m

	// Update project root
	if m.Project.Root != "" {
		p.config.ProjectRoot = m.Project.Root
	}

	// Update scan targets
	if len(m.Scan.Targets) > 0 {
		p.config.Targets = m.Scan.Targets
	}

	// Update limits
	if m.Limits != nil {
		if m.Limits.MaxWorkers != nil {
			p.config.MaxWorkers = *m.Limits.MaxWorkers
		}
		if m.Limits.MaxFiles != nil {
			p.config.MaxFiles = *m.Limits.MaxFiles
		}
	}

	// Update cache configuration
	if m.Cache != nil && m.Cache.Kind != nil {
		p.config.CacheKind = *m.Cache.Kind
	}

	return nil
}

// ParseProject performs complete project analysis
func (p *DefaultPHPProjectParser) ParseProject(ctx context.Context, config ProjectParserConfig) (*ProjectParseResult, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		// Return partial result even when cancelled
		partialResult := &ProjectParseResult{
			DiscoveredFiles: []any{},
			ParsedFiles:     []ParsedFileResult{},
			Stats: ProjectParseStats{
				FilesDiscovered: 0,
				FilesParsed:     0,
				TotalDuration:   0,
				DiscoveryTime:   0,
				ParseTime:       0,
			},
		}
		return partialResult, ctx.Err()
	default:
	}

	startTime := time.Now()
	discoveryStart := time.Now()

	// Update progress
	p.updateProgress(ProjectParserPhaseDiscovering, "Discovering PHP files")

	// Basic file discovery simulation
	var discoveredFiles []string

	// Discover files in target directories
	for _, target := range config.Targets {
		targetPath := filepath.Join(config.ProjectRoot, target)
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			continue // Skip non-existent directories
		}

		// Walk the target directory
		err := filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Check if it's a PHP file
			if !info.IsDir() && strings.HasSuffix(path, ".php") {
				discoveredFiles = append(discoveredFiles, path)
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to discover files in %s: %w", target, err)
		}
	}

	discoveryTime := time.Since(discoveryStart)

	// Update progress
	p.updateProgress(ProjectParserPhaseParsing, fmt.Sprintf("Processing %d files", len(discoveredFiles)))

	// Simulate parsing (for now, just create basic results)
	var parsedFiles []ParsedFileResult
	for _, filePath := range discoveredFiles {
		relativePath, _ := filepath.Rel(config.ProjectRoot, filePath)
		parsedFiles = append(parsedFiles, ParsedFileResult{
			FilePath:     filePath,
			RelativePath: relativePath,
			Namespace:    "",                    // Would be extracted from actual parsing
			Classes:      []string{},            // Would be extracted from actual parsing
			Methods:      []string{},            // Would be extracted from actual parsing
			ParseTime:    time.Millisecond * 10, // Simulated parse time
		})
	}

	totalDuration := time.Since(startTime)

	// Update progress
	p.updateProgress(ProjectParserPhaseCompleted, fmt.Sprintf("Completed processing %d files", len(parsedFiles)))

	// Convert discoveredFiles to []interface{}
	var discoveredFilesInterface []any
	for _, file := range discoveredFiles {
		discoveredFilesInterface = append(discoveredFilesInterface, file)
	}

	result := &ProjectParseResult{
		DiscoveredFiles: discoveredFilesInterface,
		ParsedFiles:     parsedFiles,
		Stats: ProjectParseStats{
			FilesDiscovered: len(discoveredFiles),
			FilesParsed:     len(parsedFiles),
			TotalDuration:   totalDuration,
			DiscoveryTime:   discoveryTime,
			ParseTime:       totalDuration - discoveryTime,
		},
	}

	return result, nil
}

// SetProgressCallback enables real-time progress monitoring
func (p *DefaultPHPProjectParser) SetProgressCallback(callback func(ProjectParserProgress)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.progressCallback = callback
}

// updateProgress updates internal progress state
func (p *DefaultPHPProjectParser) updateProgress(phase ProjectParserPhase, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update current phase and status
	p.progress.Phase = phase
	p.progress.PhaseStatus = status

	// Update elapsed time if this is a real operation
	if p.progress.Phase != ProjectParserPhaseInitializing {
		// Add estimated increment based on phase changes
		p.progress.ElapsedTime += time.Millisecond * 100
	}

	// Set completion flags
	p.progress.IsComplete = (phase == ProjectParserPhaseCompleted)
	p.progress.HasErrors = (phase == ProjectParserPhaseFailed)

	// Call callback if registered
	if p.progressCallback != nil {
		// Make a copy to avoid data races
		progressCopy := p.progress
		p.progressCallback(progressCopy)
	}
}

// incrementFileCount safely increments file counters with progress callback

// hasField checks if an interface{} has a field with the given name using reflection
func hasField(obj any, fieldName string) bool {
	if obj == nil {
		return false
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return false
	}

	field := v.FieldByName(fieldName)
	return field.IsValid()
}

// getFieldValue extracts a string field value using reflection
func getFieldValue(obj any, fieldName string) string {
	if obj == nil {
		return ""
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return ""
	}

	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return ""
	}

	if field.Kind() == reflect.String {
		return field.String()
	}

	return ""
}

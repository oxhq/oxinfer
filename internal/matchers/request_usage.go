// Package matchers provides request usage detection for Laravel controllers.
package matchers

import (
    "context"
    "fmt"
    "strings"
    "regexp"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultRequestUsageMatcher implements RequestUsageMatcher interface.
type DefaultRequestUsageMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewRequestUsageMatcher creates a new request usage matcher.
func NewRequestUsageMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultRequestUsageMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}
	
	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultRequestUsageMatcher{
		config:           config,
		queryDefs:        RequestUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize request usage matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for request usage detection.
func (m *DefaultRequestUsageMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile request usage queries: %w", err)
	}
	
	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultRequestUsageMatcher) GetType() PatternType {
	return PatternTypeRequestUsage
}

// Match finds all request usage patterns in the syntax tree.
func (m *DefaultRequestUsageMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("request usage matcher not initialized")
	}

	if tree == nil || tree.Root == nil {
		return nil, fmt.Errorf("invalid syntax tree provided")
	}

    var allResults []*MatchResult
    confSum := 0.0
    confCount := 0
	
	// Track request usage patterns across the file
	requestUsage := &RequestUsageMatch{
		ContentTypes: make([]string, 0),
		Body:         make(map[string]interface{}),
		Query:        make(map[string]interface{}),
		Files:        make(map[string]interface{}),
		Methods:      make([]string, 0),
	}

	// Execute each query
	for i, query := range m.queries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		queryDef := m.queryDefs[i]
		
		// Convert SyntaxTree back to tree-sitter node for querying
		sitterNode, sitterTree, err := m.convertToSitterNode(tree)
		if err != nil {
			continue // Skip this query on conversion error
		}
		defer sitterTree.Close() // Ensure tree is cleaned up

		cursor := sitter.NewQueryCursor()
		cursor.Exec(query, sitterNode)

        // Process matches
        for {
            match, ok := cursor.NextMatch()
            if !ok {
                break
            }

            // Process captures based on query type
            m.processRequestMatch(match, query, queryDef, tree, filePath, requestUsage, &allResults)
            // Count this matched query for confidence aggregation
            if len(match.Captures) > 0 {
                confSum += queryDef.Confidence
                confCount++
            }
        }
		cursor.Close()
		// Tree cleanup handled by defer statement
	}

	// Consolidate request usage into final results
	if len(requestUsage.Methods) > 0 || len(requestUsage.Body) > 0 || 
	   len(requestUsage.Query) > 0 || len(requestUsage.Files) > 0 {
		
		// Infer content types based on usage patterns
		m.inferContentTypes(requestUsage)
		
        overallConf := 0.8
        if confCount > 0 {
            overallConf = confSum / float64(confCount)
        }

        result := &MatchResult{
            Type:       PatternTypeRequestUsage,
            Position:   parser.Point{Row: 0, Column: 0}, // File-level match
            Content:    fmt.Sprintf("Request usage: %d methods", len(requestUsage.Methods)),
            Confidence: overallConf,
            Data:       requestUsage,
            Context: &MatchContext{
                FilePath: filePath,
                Explicit: len(requestUsage.Files) > 0, // File methods are explicit
            },
        }
		
		allResults = append(allResults, result)
	}

	// Apply confidence filtering and deduplication
	filteredResults := m.filterByConfidence(allResults)
	finalResults := m.deduplicateResults(filteredResults)

	return finalResults, nil
}

// MatchRequestUsage finds request usage patterns and infers content types.
func (m *DefaultRequestUsageMatcher) MatchRequestUsage(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*RequestUsageMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	requestMatches := make([]*RequestUsageMatch, 0, len(results))
	for _, result := range results {
		if reqMatch, ok := result.Data.(*RequestUsageMatch); ok {
			requestMatches = append(requestMatches, reqMatch)
		}
	}

	return requestMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultRequestUsageMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultRequestUsageMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultRequestUsageMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}
	
	m.initialized = false
	m.queries = nil
	return nil
}

// processRequestMatch processes individual request method matches.
func (m *DefaultRequestUsageMatcher) processRequestMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
	requestUsage *RequestUsageMatch,
	allResults *[]*MatchResult,
) {
    for _, capture := range match.Captures {
        captureName := query.CaptureNameForId(capture.Index)
        
        switch captureName {
		case "request":
			// Track that we found a request object
			continue
		case "method":
			methodNode := capture.Node
			methodName := string(methodNode.Content(tree.Source))
			
			// Add method to tracking
			if !contains(requestUsage.Methods, methodName) {
				requestUsage.Methods = append(requestUsage.Methods, methodName)
			}
			
			// Process specific method types
			m.processMethodByType(queryDef.Name, methodName, requestUsage, methodNode, tree)
			
        case "parameter":
            paramNode := capture.Node
            paramText := string(paramNode.Content(tree.Source))
            
            // Clean parameter text (remove quotes)
            paramText = strings.Trim(paramText, `"'`)
            
            // Add to appropriate collection based on method type
            m.addParameterByMethod(queryDef.Name, paramText, requestUsage)
        case "arr":
            // Extract string literals from array argument for only/except
            arrText := string(capture.Node.Content(tree.Source))
            for _, lit := range extractStringLiterals(arrText) {
                m.addParameterByMethod(queryDef.Name, lit, requestUsage)
            }
        }
    }
}

// extractStringLiterals finds simple quoted string literals in a snippet like ['a','b']
var strLitRe = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)

func extractStringLiterals(s string) []string {
    matches := strLitRe.FindAllStringSubmatch(s, -1)
    out := make([]string, 0, len(matches))
    for _, m := range matches {
        if len(m) >= 2 {
            val := m[1]
            if val == "" && len(m) >= 3 {
                val = m[2]
            }
            if val != "" {
                out = append(out, val)
            }
        }
    }
    return out
}

// processMethodByType processes different request method types.
func (m *DefaultRequestUsageMatcher) processMethodByType(
	queryName, methodName string,
	requestUsage *RequestUsageMatch,
	methodNode *sitter.Node,
	tree *parser.SyntaxTree,
) {
	switch queryName {
	case "request_json":
		// JSON method implies JSON content type
		m.addContentType(requestUsage, "application/json")
	case "request_file", "request_has_file":
		// File methods imply multipart content type
		m.addContentType(requestUsage, "multipart/form-data")
		// Add generic file parameter
		requestUsage.Files["upload"] = map[string]interface{}{}
	case "request_all":
		// All method could be any content type
		m.addContentType(requestUsage, "application/x-www-form-urlencoded")
		m.addContentType(requestUsage, "application/json")
	case "request_validate":
		// Validation typically involves form data
		m.addContentType(requestUsage, "application/x-www-form-urlencoded")
	}
}

// addParameterByMethod adds parameters to the appropriate collection.
func (m *DefaultRequestUsageMatcher) addParameterByMethod(queryName, paramName string, requestUsage *RequestUsageMatch) {
	switch queryName {
	case "request_input":
		// Input parameters go to body
		requestUsage.Body[paramName] = map[string]interface{}{}
	case "request_only", "request_except":
		// Only/except parameters go to body
		requestUsage.Body[paramName] = map[string]interface{}{}
	default:
		// Default to body parameters
		requestUsage.Body[paramName] = map[string]interface{}{}
	}
}

// addContentType adds a content type if not already present.
func (m *DefaultRequestUsageMatcher) addContentType(requestUsage *RequestUsageMatch, contentType string) {
	if !contains(requestUsage.ContentTypes, contentType) {
		requestUsage.ContentTypes = append(requestUsage.ContentTypes, contentType)
	}
}

// inferContentTypes infers content types based on detected patterns.
func (m *DefaultRequestUsageMatcher) inferContentTypes(requestUsage *RequestUsageMatch) {
	// If no content types detected, infer from usage
	if len(requestUsage.ContentTypes) == 0 {
		if len(requestUsage.Files) > 0 {
			m.addContentType(requestUsage, "multipart/form-data")
		} else if len(requestUsage.Body) > 0 {
			m.addContentType(requestUsage, "application/x-www-form-urlencoded")
			m.addContentType(requestUsage, "application/json")
		}
	}
}

// calculateOverallConfidence calculates average confidence across all matches.
func (m *DefaultRequestUsageMatcher) calculateOverallConfidence(results []*MatchResult) float64 {
	if len(results) == 0 {
		return 0.8 // Default confidence for consolidated request usage
	}
	
	totalConfidence := 0.0
	for _, result := range results {
		totalConfidence += result.Confidence
	}
	
	return totalConfidence / float64(len(results))
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultRequestUsageMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
	// Re-parse the content to get a tree-sitter node
	tempParser := sitter.NewParser()
	if tempParser == nil {
		return nil, nil, fmt.Errorf("failed to create temporary parser")
	}

	tempParser.SetLanguage(m.compiler.language)
	
	sitterTree, err := tempParser.ParseCtx(context.Background(), nil, tree.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to re-parse content: %w", err)
	}
	if sitterTree == nil {
		return nil, nil, fmt.Errorf("re-parsing returned nil tree")
	}

	rootNode := sitterTree.RootNode()
	if rootNode == nil {
		sitterTree.Close()
		return nil, nil, fmt.Errorf("re-parsed tree has no root node")
	}

	return rootNode, sitterTree, nil
}

// filterByConfidence removes matches below the minimum confidence threshold.
func (m *DefaultRequestUsageMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
	if m.config.MinConfidenceThreshold <= 0 {
		return results // No filtering
	}

	filtered := make([]*MatchResult, 0, len(results))
	for _, result := range results {
		if result.Confidence >= m.config.MinConfidenceThreshold {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// deduplicateResults removes duplicate matches by position and content.
func (m *DefaultRequestUsageMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]bool)
	deduplicated := make([]*MatchResult, 0, len(results))

	for _, result := range results {
		// Create a unique key based on content and data
		key := fmt.Sprintf("%s:%v", result.Content, result.Data)
		
		if !seen[key] {
			seen[key] = true
			deduplicated = append(deduplicated, result)
		}
	}

	return deduplicated
}

// contains checks if a string slice contains a specific string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetSupportedContentTypes returns commonly used content types in Laravel.
func GetSupportedContentTypes() []string {
	return []string{
		"application/json",
		"multipart/form-data", 
		"application/x-www-form-urlencoded",
	}
}

// GetRequestMethods returns commonly used request methods in Laravel controllers.
func GetRequestMethods() []string {
	return []string{
		"all", "json", "input", "only", "except", "get", "post",
		"file", "hasFile", "validate", "validated", "query",
	}
}

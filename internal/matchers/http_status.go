// Package matchers provides HTTP status detection for Laravel controllers.
package matchers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultHTTPStatusMatcher implements HTTPStatusMatcher interface.
type DefaultHTTPStatusMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewHTTPStatusMatcher creates a new HTTP status matcher.
func NewHTTPStatusMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultHTTPStatusMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultHTTPStatusMatcher{
		config:           config,
		queryDefs:        HTTPStatusQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize HTTP status matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for HTTP status detection.
func (m *DefaultHTTPStatusMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile HTTP status queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultHTTPStatusMatcher) GetType() PatternType {
	return PatternTypeHTTPStatus
}

// Match finds all HTTP status patterns in the syntax tree.
func (m *DefaultHTTPStatusMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("HTTP status matcher not initialized")
	}

	if tree == nil || tree.Root == nil {
		return nil, fmt.Errorf("invalid syntax tree provided")
	}

	var allResults []*MatchResult

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

			// Extract status code from captures
			for _, capture := range match.Captures {
				captureName := query.CaptureNameForId(capture.Index)

				if captureName == "status" {
					statusNode := capture.Node
					statusText := string(statusNode.Content(tree.Source))

					// Parse status code
					statusCode, err := strconv.Atoi(statusText)
					if err != nil {
						continue // Skip invalid status codes
					}

					// Validate HTTP status code range
					if statusCode < 100 || statusCode > 599 {
						continue
					}

					// Create match result
					result := &MatchResult{
						Type:       PatternTypeHTTPStatus,
						Position:   parser.Point{Row: int(statusNode.StartPoint().Row), Column: int(statusNode.StartPoint().Column)},
						Content:    statusText,
						Confidence: queryDef.Confidence,
						Data: &HTTPStatusMatch{
							Status:   statusCode,
							Explicit: m.determineExplicitness(queryDef.Name, statusCode),
							Pattern:  queryDef.Name,
						},
						Context: &MatchContext{
							FilePath: filePath,
							Explicit: m.determineExplicitness(queryDef.Name, statusCode),
						},
					}

					allResults = append(allResults, result)

					// Respect match limits
					if len(allResults) >= m.config.MaxMatchesPerFile {
						cursor.Close()
						return m.deduplicateResults(allResults), nil
					}
				}
			}
		}
		cursor.Close()
		// Tree cleanup handled by defer statement
	}

	// Apply confidence filtering
	filteredResults := m.filterByConfidence(allResults)

	// Deduplicate results
	finalResults := m.deduplicateResults(filteredResults)

	return finalResults, nil
}

// MatchHTTPStatus finds HTTP status patterns with confidence scoring.
func (m *DefaultHTTPStatusMatcher) MatchHTTPStatus(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*HTTPStatusMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	httpMatches := make([]*HTTPStatusMatch, 0, len(results))
	for _, result := range results {
		if httpMatch, ok := result.Data.(*HTTPStatusMatch); ok {
			httpMatches = append(httpMatches, httpMatch)
		}
	}

	return httpMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultHTTPStatusMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultHTTPStatusMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultHTTPStatusMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	return nil
}

// determineExplicitness determines if an HTTP status is explicitly set based on pattern and code.
func (m *DefaultHTTPStatusMatcher) determineExplicitness(patternName string, statusCode int) bool {
	// Strict explicit mode - only certain patterns count as explicit
	if m.config.StrictExplicitOnly {
		switch patternName {
		case "abort_call", "response_status_method", "return_response_json_status":
			return true
		default:
			return false
		}
	}

	// Standard explicitness rules
	switch patternName {
	case "abort_call":
		return true // abort() calls are always explicit
	case "response_status_method", "response_json_with_status", "return_response_json_status":
		return true // Direct status method calls
	case "response_direct_status":
		// Only non-200 status codes are considered explicit in direct response calls
		return statusCode != 200
	case "variable_response_status":
		return false // Variable assignments are less explicit
	default:
		return false
	}
}

// filterByConfidence removes matches below the minimum confidence threshold.
func (m *DefaultHTTPStatusMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultHTTPStatusMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
	// Re-parse the content to get a tree-sitter node
	// This is necessary because our SyntaxTree is a Go-friendly wrapper
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

// deduplicateResults removes duplicate matches by position and content.
func (m *DefaultHTTPStatusMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]bool)
	deduplicated := make([]*MatchResult, 0, len(results))

	for _, result := range results {
		// Create a unique key based on position and content
		key := fmt.Sprintf("%d:%d:%s", result.Position.Row, result.Position.Column, result.Content)

		if !seen[key] {
			seen[key] = true
			deduplicated = append(deduplicated, result)
		}
	}

	return deduplicated
}

// GetSupportedStatusCodes returns commonly used HTTP status codes for validation.
func GetSupportedStatusCodes() []int {
	return []int{
		200, 201, 202, 204, // 2xx Success
		300, 301, 302, 304, // 3xx Redirection
		400, 401, 403, 404, 405, 409, 410, 422, 429, // 4xx Client Error
		500, 501, 502, 503, 504, // 5xx Server Error
	}
}

// IsCommonStatusCode checks if a status code is commonly used in web applications.
func IsCommonStatusCode(code int) bool {
	supported := GetSupportedStatusCodes()
	for _, supported := range supported {
		if code == supported {
			return true
		}
	}
	return false
}

// GetStatusCodeMeaning returns human-readable meaning for HTTP status codes.
func GetStatusCodeMeaning(code int) string {
	meanings := map[int]string{
		200: "OK",
		201: "Created",
		202: "Accepted",
		204: "No Content",
		300: "Multiple Choices",
		301: "Moved Permanently",
		302: "Found",
		304: "Not Modified",
		400: "Bad Request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not Found",
		405: "Method Not Allowed",
		409: "Conflict",
		410: "Gone",
		422: "Unprocessable Entity",
		429: "Too Many Requests",
		500: "Internal Server Error",
		501: "Not Implemented",
		502: "Bad Gateway",
		503: "Service Unavailable",
		504: "Gateway Timeout",
	}

	if meaning, exists := meanings[code]; exists {
		return meaning
	}

	// Return generic meaning based on code class
	switch {
	case code >= 200 && code < 300:
		return "Success"
	case code >= 300 && code < 400:
		return "Redirection"
	case code >= 400 && code < 500:
		return "Client Error"
	case code >= 500 && code < 600:
		return "Server Error"
	default:
		return "Unknown"
	}
}

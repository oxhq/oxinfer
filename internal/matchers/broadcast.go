// Package matchers provides Laravel broadcast channel detection for real-time communication.
package matchers

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultBroadcastMatcher implements BroadcastMatcher interface.
type DefaultBroadcastMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewBroadcastMatcher creates a new Laravel broadcast channel matcher.
func NewBroadcastMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultBroadcastMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultBroadcastMatcher{
		config:           config,
		queryDefs:        BroadcastUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize broadcast matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for broadcast channel detection.
func (m *DefaultBroadcastMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile broadcast queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultBroadcastMatcher) GetType() PatternType {
	return PatternTypeBroadcast
}

// isBroadcastFile checks if the file should be processed for broadcast channels.
// Only processes files in routes/channels.php or routes/channels/ directory
func (m *DefaultBroadcastMatcher) isBroadcastFile(filePath string) bool {
	// Normalize path for consistent comparison
	normalized := filepath.ToSlash(strings.ToLower(filePath))
	
	// Valid patterns for broadcast channel files
	validPatterns := []string{
		"routes/channels.php",        // Main channels file
		"routes/channels/",            // Channels subdirectory
		"routes/broadcasting.php",    // Alternative naming
	}
	
	for _, pattern := range validPatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	
	return false
}

// isQueryBuilderMethod checks if a method name is a query builder method
// that should be filtered out from broadcast detection
func (m *DefaultBroadcastMatcher) isQueryBuilderMethod(methodName string) bool {
	queryBuilderMethods := map[string]bool{
		"where":        true,
		"whereIn":      true,
		"whereNotIn":   true,
		"whereBetween": true,
		"whereNull":    true,
		"whereNotNull": true,
		"whereHas":     true,
		"get":          true,
		"first":        true,
		"firstOrFail":  true,
		"find":         true,
		"findOrFail":   true,
		"all":          true,
		"orderBy":      true,
		"groupBy":      true,
		"having":       true,
		"select":       true,
		"distinct":     true,
		"join":         true,
		"leftJoin":     true,
		"rightJoin":    true,
		"limit":        true,
		"take":         true,
		"skip":         true,
		"offset":       true,
		"count":        true,
		"sum":          true,
		"avg":          true,
		"min":          true,
		"max":          true,
		"pluck":        true,
		"exists":       true,
		"doesntExist":  true,
		"with":         true,
		"withCount":    true,
		"load":         true,
		"latest":       true,
		"oldest":       true,
		"chunk":        true,
		"chunkById":    true,
		"cursor":       true,
		"lazy":         true,
	}
	
	return queryBuilderMethods[methodName]
}

// Match finds all Laravel broadcast channel patterns in the syntax tree.
func (m *DefaultBroadcastMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("broadcast matcher not initialized")
	}

	// CRITICAL: Only process broadcast channel files
	if !m.isBroadcastFile(filePath) {
		return []*MatchResult{}, nil // Return empty results, not an error
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

			// Process broadcast matches
			result := m.processBroadcastMatch(match, query, queryDef, tree, filePath)
			if result != nil {
				// Filter out query builder methods
				if broadcastMatch, ok := result.Data.(*BroadcastMatch); ok {
					if m.isQueryBuilderMethod(broadcastMatch.Method) {
						continue // Skip query builder methods
					}
				}
				
				allResults = append(allResults, result)

				// Respect match limits
				if len(allResults) >= m.config.MaxMatchesPerFile {
					cursor.Close()
					return m.sortAndDeduplicate(allResults), nil
				}
			}
		}
		cursor.Close()
		// Tree cleanup handled by defer statement
	}

	// Apply confidence filtering, sorting, and deduplication
	filteredResults := m.filterByConfidence(allResults)
	finalResults := m.sortAndDeduplicate(filteredResults)

	return finalResults, nil
}

// sortAndDeduplicate sorts results deterministically and removes duplicates
func (m *DefaultBroadcastMatcher) sortAndDeduplicate(results []*MatchResult) []*MatchResult {
	// First, sort for deterministic ordering
	sort.Slice(results, func(i, j int) bool {
		// Sort by file path first
		if results[i].Context.FilePath != results[j].Context.FilePath {
			return results[i].Context.FilePath < results[j].Context.FilePath
		}
		
		// Then by position (line, column)
		if results[i].Position.Row != results[j].Position.Row {
			return results[i].Position.Row < results[j].Position.Row
		}
		
		if results[i].Position.Column != results[j].Position.Column {
			return results[i].Position.Column < results[j].Position.Column
		}
		
		// Finally by content for complete determinism
		return results[i].Content < results[j].Content
	})
	
	// Then deduplicate if enabled
	if m.config.DeduplicateMatches {
		return m.deduplicateResults(results)
	}
	
	return results
}

// MatchBroadcast finds Laravel broadcast channel patterns.
func (m *DefaultBroadcastMatcher) MatchBroadcast(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*BroadcastMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	broadcastMatches := make([]*BroadcastMatch, 0, len(results))
	for _, result := range results {
		if broadcastMatch, ok := result.Data.(*BroadcastMatch); ok {
			broadcastMatches = append(broadcastMatches, broadcastMatch)
		}
	}

	return broadcastMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultBroadcastMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultBroadcastMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultBroadcastMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	return nil
}

// processBroadcastMatch processes individual broadcast channel matches.
func (m *DefaultBroadcastMatcher) processBroadcastMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var methodName string
	var channelName string
	var position parser.Point

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "method":
			methodNode := capture.Node
			methodName = string(methodNode.Content(tree.Source))
			position = parser.Point{Row: int(methodNode.StartPoint().Row), Column: int(methodNode.StartPoint().Column)}
		case "channel_name":
			channelNode := capture.Node
			channelName = m.extractStringLiteral(string(channelNode.Content(tree.Source)))
			// If position wasn't set by method, use channel name position
			if position.Row == 0 && position.Column == 0 {
				position = parser.Point{Row: int(channelNode.StartPoint().Row), Column: int(channelNode.StartPoint().Column)}
			}
		case "function":
			// For routes/channels.php function calls without Broadcast:: prefix
			functionNode := capture.Node
			methodName = string(functionNode.Content(tree.Source))
			position = parser.Point{Row: int(functionNode.StartPoint().Row), Column: int(functionNode.StartPoint().Column)}
		case "facade":
			// For Broadcast facade calls
			if methodName == "" {
				// This is a facade call, we'll get the method from another capture
				position = parser.Point{Row: int(capture.Node.StartPoint().Row), Column: int(capture.Node.StartPoint().Column)}
			}
		}
	}

	// Skip if we don't have essential information
	if channelName == "" && !m.isChannelParameterQuery(queryDef.Name) {
		return nil
	}

	// For parameter detection queries, extract from the match
	if m.isChannelParameterQuery(queryDef.Name) {
		for _, capture := range match.Captures {
			captureName := query.CaptureNameForId(capture.Index)
			if captureName == "channel_name" {
				channelNode := capture.Node
				channelName = m.extractStringLiteral(string(channelNode.Content(tree.Source)))
				position = parser.Point{Row: int(channelNode.StartPoint().Row), Column: int(channelNode.StartPoint().Column)}
				break
			}
		}
	}

	// Create broadcast match
	broadcastMatch := m.buildBroadcastMatch(methodName, channelName, queryDef, filePath)

	// Analyze payload literals if callback is present
	broadcastMatch.PayloadLiteral = m.detectPayloadLiterals(tree, position)

	return &MatchResult{
		Type:       PatternTypeBroadcast,
		Position:   position,
		Content:    m.buildDisplayContent(methodName, channelName),
		Confidence: queryDef.Confidence,
		Data:       broadcastMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: m.isExplicitBroadcastUsage(queryDef.Name),
		},
	}
}

// buildBroadcastMatch creates a BroadcastMatch based on the detected method and channel name.
func (m *DefaultBroadcastMatcher) buildBroadcastMatch(methodName, channelName string, queryDef QueryDefinition, filePath string) *BroadcastMatch {
	broadcastMatch := &BroadcastMatch{
		Channel: channelName,
		Method:  methodName,
		Pattern: queryDef.Name,
		File:    filePath,
	}

	// Determine visibility based on method name
	switch methodName {
	case "channel":
		broadcastMatch.Visibility = "public"
	case "private":
		broadcastMatch.Visibility = "private"
	case "presence":
		broadcastMatch.Visibility = "presence"
	default:
		// Infer from pattern or default to public
		if strings.Contains(queryDef.Name, "private") {
			broadcastMatch.Visibility = "private"
		} else if strings.Contains(queryDef.Name, "presence") {
			broadcastMatch.Visibility = "presence"
		} else {
			broadcastMatch.Visibility = "public"
		}
	}

	// Extract parameters from channel name
	broadcastMatch.Params = m.extractChannelParameters(channelName)

	return broadcastMatch
}

// extractChannelParameters extracts parameter names from channel patterns like 'orders.{id}'.
func (m *DefaultBroadcastMatcher) extractChannelParameters(channelName string) []string {
	if channelName == "" {
		return []string{}
	}

	// Regex to match Laravel route-style parameters {param}
	paramRegex := regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	matches := paramRegex.FindAllStringSubmatch(channelName, -1)

	params := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			params = append(params, match[1])
		}
	}

	// Sort for deterministic output
	sort.Strings(params)
	return params
}

// extractStringLiteral extracts string content from quotes.
func (m *DefaultBroadcastMatcher) extractStringLiteral(str string) string {
	str = strings.TrimSpace(str)

	// Remove single or double quotes
	if len(str) >= 2 {
		if (str[0] == '"' && str[len(str)-1] == '"') ||
			(str[0] == '\'' && str[len(str)-1] == '\'') {
			return str[1 : len(str)-1]
		}
	}

	return str
}

// detectPayloadLiterals analyzes the callback to detect literal payload values.
func (m *DefaultBroadcastMatcher) detectPayloadLiterals(tree *parser.SyntaxTree, position parser.Point) bool {
	// This is a simplified implementation
	// In a full implementation, you might analyze the callback closure for literal return values
	sourceLines := strings.Split(string(tree.Source), "\n")

	// Check surrounding lines for literal returns
	for lineOffset := 0; lineOffset <= 3; lineOffset++ {
		lineIndex := position.Row + lineOffset
		if lineIndex >= 0 && lineIndex < len(sourceLines) {
			line := strings.ToLower(sourceLines[lineIndex])

			// Look for literal values in returns
			if strings.Contains(line, "return true") ||
				strings.Contains(line, "return false") ||
				strings.Contains(line, "return []") ||
				strings.Contains(line, "return [") ||
				(strings.Contains(line, "return") && (strings.Contains(line, "'") || strings.Contains(line, "\""))) {
				return true
			}
		}
	}

	return false
}

// buildDisplayContent creates a human-readable string representation of the broadcast channel.
func (m *DefaultBroadcastMatcher) buildDisplayContent(methodName, channelName string) string {
	if methodName == "" {
		return fmt.Sprintf("channel('%s')", channelName)
	}

	return fmt.Sprintf("Broadcast::%s('%s')", methodName, channelName)
}

// isChannelParameterQuery checks if this is a query specifically for parameter detection.
func (m *DefaultBroadcastMatcher) isChannelParameterQuery(queryName string) bool {
	return queryName == "channel_parameter_pattern"
}

// isExplicitBroadcastUsage determines if broadcast usage is explicit.
func (m *DefaultBroadcastMatcher) isExplicitBroadcastUsage(patternName string) bool {
	switch patternName {
	case "broadcast_channel_public", "broadcast_private_channel", "broadcast_presence_channel":
		return true
	case "broadcast_channel_with_namespace", "broadcast_facade_call":
		return true
	case "broadcast_in_routes_file":
		return true
	default:
		return false
	}
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultBroadcastMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
func (m *DefaultBroadcastMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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

// deduplicateResults removes duplicate matches by position and channel name.
func (m *DefaultBroadcastMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]*MatchResult)

	for _, result := range results {
		if broadcastMatch, ok := result.Data.(*BroadcastMatch); ok {
			// Create unique key based on position and channel details
			key := fmt.Sprintf("%s:%d:%d:%s:%s", broadcastMatch.Channel, result.Position.Row, result.Position.Column, broadcastMatch.Method, broadcastMatch.Visibility)

			if existing, exists := seen[key]; exists {
				// Keep the match with higher confidence
				if result.Confidence > existing.Confidence {
					seen[key] = result
				}
			} else {
				seen[key] = result
			}
		}
	}

	// Convert back to slice with deterministic ordering
	deduplicated := make([]*MatchResult, 0, len(seen))

	// Sort keys for deterministic output
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		deduplicated = append(deduplicated, seen[key])
	}

	return deduplicated
}

// GetSupportedBroadcastPatterns returns commonly used Laravel broadcast channel patterns.
func GetSupportedBroadcastPatterns() []string {
	return []string{
		"Broadcast::channel('orders.{id}', function ($user, $id) { return $user->id === Order::find($id)->user_id; })",
		"Broadcast::private('chat.{roomId}', function ($user, $roomId) { return $user->canAccessRoom($roomId); })",
		"Broadcast::presence('room.{id}', function ($user, $id) { return ['id' => $user->id, 'name' => $user->name]; })",
		"channel('public.notifications', function () { return true; })",
		"private('user.{id}', function ($user, $id) { return (int) $user->id === (int) $id; })",
		"presence('chat', function ($user) { return ['id' => $user->id]; })",
	}
}

// GetBroadcastMethodConventions returns Laravel broadcast method conventions.
func GetBroadcastMethodConventions() map[string]string {
	return map[string]string{
		"channel":  "Defines a public broadcast channel accessible to all users",
		"private":  "Defines a private broadcast channel with authorization callback",
		"presence": "Defines a presence channel that tracks connected users",
	}
}

// ValidateBroadcastChannelCall validates a broadcast channel call against Laravel conventions.
func ValidateBroadcastChannelCall(methodName string, channelName string, hasCallback bool) bool {
	switch methodName {
	case "channel":
		// Public channels can optionally have a callback
		return channelName != ""
	case "private", "presence":
		// Private and presence channels must have a callback
		return channelName != "" && hasCallback
	default:
		return false
	}
}

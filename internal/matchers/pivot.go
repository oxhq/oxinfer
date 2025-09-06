// Package matchers provides Laravel pivot relationship detection for many-to-many relationships.
package matchers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultPivotMatcher implements PivotMatcher interface.
type DefaultPivotMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewPivotMatcher creates a new Laravel pivot relationship matcher.
func NewPivotMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultPivotMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultPivotMatcher{
		config:           config,
		queryDefs:        PivotUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize pivot matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for pivot detection.
func (m *DefaultPivotMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile pivot queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultPivotMatcher) GetType() PatternType {
	return PatternTypePivot
}

// Match finds all Laravel pivot patterns in the syntax tree.
func (m *DefaultPivotMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("pivot matcher not initialized")
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

			// Process pivot matches
			result := m.processPivotMatch(match, query, queryDef, tree, filePath)
			if result != nil {
				allResults = append(allResults, result)

				// Respect match limits
				if len(allResults) >= m.config.MaxMatchesPerFile {
					cursor.Close()
					return m.deduplicateResults(allResults), nil
				}
			}
		}
		cursor.Close()
		// Tree cleanup handled by defer statement
	}

	// Apply confidence filtering and deduplication
	filteredResults := m.filterByConfidence(allResults)
	finalResults := m.deduplicateResults(filteredResults)

	// Ensure deterministic order by source position (row, then column)
	if len(finalResults) > 1 {
		sort.SliceStable(finalResults, func(i, j int) bool {
			if finalResults[i].Position.Row != finalResults[j].Position.Row {
				return finalResults[i].Position.Row < finalResults[j].Position.Row
			}
			if finalResults[i].Position.Column != finalResults[j].Position.Column {
				return finalResults[i].Position.Column < finalResults[j].Position.Column
			}
			// Tiebreaker: method name if available
			im, iok := finalResults[i].Data.(*PivotMatch)
			jm, jok := finalResults[j].Data.(*PivotMatch)
			if iok && jok && im.Method != jm.Method {
				return im.Method < jm.Method
			}
			return false
		})
	}

	return finalResults, nil
}

// MatchPivots finds Laravel pivot relationship patterns.
func (m *DefaultPivotMatcher) MatchPivots(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*PivotMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	pivotMatches := make([]*PivotMatch, 0, len(results))
	for _, result := range results {
		if pivotMatch, ok := result.Data.(*PivotMatch); ok {
			pivotMatches = append(pivotMatches, pivotMatch)
		}
	}

	return pivotMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultPivotMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultPivotMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultPivotMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	return nil
}

// processPivotMatch processes individual pivot usage matches.
func (m *DefaultPivotMatcher) processPivotMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var methodName string
	var argsNode *sitter.Node
	var aliasName string
	var position parser.Point

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "method", "pivot_method", "second_method":
			methodNode := capture.Node
			methodName = string(methodNode.Content(tree.Source))
			position = parser.Point{Row: int(methodNode.StartPoint().Row), Column: int(methodNode.StartPoint().Column)}
		case "args":
			argsNode = capture.Node
		case "alias":
			aliasNode := capture.Node
			aliasName = m.cleanStringLiteral(string(aliasNode.Content(tree.Source)))
		}
	}

	// Extract alias from arguments if method is 'as' and we have arguments
	if methodName == "as" && argsNode != nil && aliasName == "" {
		aliasName = m.extractAliasFromArgs(argsNode, tree)
	}

	// Validate we found a pivot method
	if methodName == "" {
		return nil
	}

	// Skip non-pivot methods unless they're part of a chain
	if !m.isPivotMethod(methodName) && queryDef.Name != "chained_pivot_methods" {
		return nil
	}

	// T5: Validate that withPivot is used in correct context (many-to-many relationships)
	if methodName == "withPivot" {
		// Check if it's in a belongsToMany context
		if !m.isInManyToManyContext(tree, position) {
			return nil // Skip if not in correct relationship context
		}
		
		// Extract pivot fields
		if argsNode != nil {
			pivotFields := m.extractPivotFields(argsNode, tree)
			if len(pivotFields) == 0 {
				return nil // withPivot without fields is not useful
			}
			
			// Create pivot match with fields
			relationName := m.inferRelationshipName(tree, position)
			pivotMatch := &PivotMatch{
				Relation:   relationName,
				Fields:     pivotFields,
				Timestamps: false,
				Alias:      "",
				Pattern:    queryDef.Name,
				Method:     methodName,
			}

			return &MatchResult{
				Type:       PatternTypePivot,
				Position:   position,
				Content:    fmt.Sprintf("->%s%s", methodName, m.getMethodArgsDisplay(methodName, pivotFields, "")),
				Confidence: queryDef.Confidence,
				Data:       pivotMatch,
				Context: &MatchContext{
					FilePath: filePath,
					Explicit: true,
				},
			}
		}
		return nil
	}

	// Handle withTimestamps
	if methodName == "withTimestamps" {
		if !m.isInManyToManyContext(tree, position) {
			return nil
		}
		
		relationName := m.inferRelationshipName(tree, position)
		pivotMatch := &PivotMatch{
			Relation:   relationName,
			Fields:     []string{},
			Timestamps: true,
			Alias:      "",
			Pattern:    queryDef.Name,
			Method:     methodName,
		}

		return &MatchResult{
			Type:       PatternTypePivot,
			Position:   position,
			Content:    fmt.Sprintf("->%s()", methodName),
			Confidence: queryDef.Confidence,
			Data:       pivotMatch,
			Context: &MatchContext{
				FilePath: filePath,
				Explicit: true,
			},
		}
	}

	// Handle as() for pivot accessor
	if methodName == "as" && aliasName != "" {
		if !m.isInManyToManyContext(tree, position) {
			return nil
		}
		
		relationName := m.inferRelationshipName(tree, position)
		pivotMatch := &PivotMatch{
			Relation:   relationName,
			Fields:     []string{},
			Timestamps: false,
			Alias:      aliasName,
			Pattern:    queryDef.Name,
			Method:     methodName,
		}

		return &MatchResult{
			Type:       PatternTypePivot,
			Position:   position,
			Content:    fmt.Sprintf("->%s('%s')", methodName, aliasName),
			Confidence: queryDef.Confidence,
			Data:       pivotMatch,
			Context: &MatchContext{
				FilePath: filePath,
				Explicit: true,
			},
		}
	}

	// Default case - should not reach here for valid pivot methods
	return nil
}

// extractAliasFromArgs extracts alias name from 'as' method arguments.
func (m *DefaultPivotMatcher) extractAliasFromArgs(argsNode *sitter.Node, tree *parser.SyntaxTree) string {
	// Walk through arguments to find the first string literal (alias name)
	for i := uint32(0); i < argsNode.ChildCount(); i++ {
		child := argsNode.Child(int(i))
		if child == nil {
			continue
		}

		// Handle argument nodes
		if child.Type() == "argument" {
			argChild := child.Child(0)
			if argChild != nil && argChild.Type() == "string" {
				alias := m.cleanStringLiteral(string(argChild.Content(tree.Source)))
				if alias != "" {
					return alias
				}
			}
		}
		// Handle direct string literals
		if child.Type() == "string" {
			alias := m.cleanStringLiteral(string(child.Content(tree.Source)))
			if alias != "" {
				return alias
			}
		}
	}

	return ""
}

// extractPivotFields extracts field names from withPivot method arguments.
// T5: Improved extraction with better handling of array syntax and multiple arguments
func (m *DefaultPivotMatcher) extractPivotFields(argsNode *sitter.Node, tree *parser.SyntaxTree) []string {
	var fields []string
	seenFields := make(map[string]bool) // T5: Prevent duplicates

	// Walk through arguments to find string literals
	for i := uint32(0); i < argsNode.ChildCount(); i++ {
		child := argsNode.Child(int(i))
		if child == nil {
			continue
		}

		// Handle argument nodes
		if child.Type() == "argument" {
			// Check if it's an array argument
			if arrayArg := m.extractArrayArgument(child, tree); len(arrayArg) > 0 {
				for _, field := range arrayArg {
					if !seenFields[field] {
						fields = append(fields, field)
						seenFields[field] = true
					}
				}
			} else {
				// Single string argument
				fieldName := m.extractStringFromArgument(child, tree)
				if fieldName != "" && !seenFields[fieldName] {
					fields = append(fields, fieldName)
					seenFields[fieldName] = true
				}
			}
		}
		// Handle direct string literals (backup case)
		if child.Type() == "string" || child.Type() == "encapsed_string" {
			fieldName := m.extractStringContent(child, tree)
			if fieldName != "" && !seenFields[fieldName] {
				fields = append(fields, fieldName)
				seenFields[fieldName] = true
			}
		}
		// T5: Handle array syntax like ['field1', 'field2']
		if child.Type() == "array_creation_expression" {
			arrayFields := m.extractArrayFields(child, tree)
			for _, field := range arrayFields {
				if !seenFields[field] {
					fields = append(fields, field)
					seenFields[field] = true
				}
			}
		}
	}

	// T5: Sort fields for deterministic output
	sort.Strings(fields)
	return fields
}

// extractStringFromArgument extracts string content from an argument node.
func (m *DefaultPivotMatcher) extractStringFromArgument(argNode *sitter.Node, tree *parser.SyntaxTree) string {
	// Look through the argument node's children for string content
	for j := uint32(0); j < argNode.ChildCount(); j++ {
		child := argNode.Child(int(j))
		if child == nil {
			continue
		}

		// Handle both string types
		if child.Type() == "string" || child.Type() == "encapsed_string" {
			return m.extractStringContent(child, tree)
		}
	}
	return ""
}

// extractStringContent extracts the actual string value from string or encapsed_string nodes.
func (m *DefaultPivotMatcher) extractStringContent(stringNode *sitter.Node, tree *parser.SyntaxTree) string {
	// For both string and encapsed_string, look for string_content child
	for i := uint32(0); i < stringNode.ChildCount(); i++ {
		child := stringNode.Child(int(i))
		if child != nil && child.Type() == "string_content" {
			return string(child.Content(tree.Source))
		}
	}

	// Fallback: clean the entire string content
	return m.cleanStringLiteral(string(stringNode.Content(tree.Source)))
}

// cleanStringLiteral removes quotes from string literals.
func (m *DefaultPivotMatcher) cleanStringLiteral(str string) string {
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

// isPivotMethod checks if a method name is a pivot-related method.
func (m *DefaultPivotMatcher) isPivotMethod(methodName string) bool {
	switch methodName {
	case "withPivot", "withTimestamps", "as":
		return true
	default:
		return false
	}
}

// inferRelationshipName attempts to determine the relationship method name from context.
func (m *DefaultPivotMatcher) inferRelationshipName(tree *parser.SyntaxTree, position parser.Point) string {
	// This is a simplified implementation - in practice, you might want to
	// walk up the AST to find the parent method declaration or relationship call

	// Look for common patterns like "belongsToMany" in the surrounding context
	sourceLines := strings.Split(string(tree.Source), "\n")
	if position.Row < len(sourceLines) {
		currentLine := sourceLines[position.Row]

		// Check for belongsToMany in the current line
		if strings.Contains(currentLine, "belongsToMany") {
			// Try to extract the model name from belongsToMany call
			if parts := strings.Split(currentLine, "belongsToMany"); len(parts) > 1 {
				afterBtm := strings.TrimSpace(parts[1])
				if strings.HasPrefix(afterBtm, "(") {
					// Extract first argument which is usually the model class
					parenContent := strings.TrimPrefix(afterBtm, "(")
					if commaIndex := strings.Index(parenContent, ","); commaIndex > 0 {
						modelRef := strings.TrimSpace(parenContent[:commaIndex])
						modelRef = m.cleanStringLiteral(modelRef)
						if strings.Contains(modelRef, "::class") {
							return strings.Replace(modelRef, "::class", "", 1)
						}
						return modelRef
					} else if closeIndex := strings.Index(parenContent, ")"); closeIndex > 0 {
						modelRef := strings.TrimSpace(parenContent[:closeIndex])
						modelRef = m.cleanStringLiteral(modelRef)
						if strings.Contains(modelRef, "::class") {
							return strings.Replace(modelRef, "::class", "", 1)
						}
						return modelRef
					}
				}
			}
		}
	}

	// Check surrounding lines for context
	for lineOffset := -2; lineOffset <= 2; lineOffset++ {
		lineIndex := position.Row + lineOffset
		if lineIndex >= 0 && lineIndex < len(sourceLines) {
			line := sourceLines[lineIndex]
			if strings.Contains(line, "belongsToMany") {
				return "belongsToMany"
			}
		}
	}

	return "unknown"
}

// getMethodArgsDisplay creates a display string for method arguments.
func (m *DefaultPivotMatcher) getMethodArgsDisplay(methodName string, fields []string, alias string) string {
	switch methodName {
	case "withPivot":
		if len(fields) > 0 {
			return fmt.Sprintf("('%s')", strings.Join(fields, "', '"))
		}
		return "()"
	case "withTimestamps":
		return "()"
	case "as":
		if alias != "" {
			return fmt.Sprintf("('%s')", alias)
		}
		return "()"
	default:
		return "()"
	}
}

// isExplicitPivotUsage determines if pivot usage is explicit.
func (m *DefaultPivotMatcher) isExplicitPivotUsage(patternName string) bool {
	switch patternName {
	case "with_pivot_method", "with_timestamps_method", "pivot_accessor_alias":
		return true
	case "belongs_to_many_with_pivot":
		return true
	case "chained_pivot_methods":
		return true
	default:
		return false
	}
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultPivotMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
func (m *DefaultPivotMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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

// deduplicateResults removes duplicate matches by position and method.
func (m *DefaultPivotMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]*MatchResult)

	for _, result := range results {
		if pivotMatch, ok := result.Data.(*PivotMatch); ok {
			// Create unique key based on position and method
			key := fmt.Sprintf("%s:%d:%d:%s", pivotMatch.Relation, result.Position.Row, result.Position.Column, pivotMatch.Method)

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

	deduplicated := make([]*MatchResult, 0, len(seen))
	for _, result := range seen {
		deduplicated = append(deduplicated, result)
	}

	return deduplicated
}

// GetSupportedPivotPatterns returns commonly used Laravel pivot relationship patterns.
func GetSupportedPivotPatterns() []string {
	return []string{
		"->withPivot('field1', 'field2')",
		"->withTimestamps()",
		"->as('pivot_alias')",
		"belongsToMany(User::class)->withPivot('role', 'permissions')",
		"belongsToMany(Tag::class)->withTimestamps()->as('tagging')",
	}
}

// GetPivotMethodConventions returns Laravel pivot method conventions.
func GetPivotMethodConventions() map[string]string {
	return map[string]string{
		"withPivot":      "Specifies additional columns on pivot table",
		"withTimestamps": "Adds created_at and updated_at timestamps to pivot",
		"as":             "Sets custom accessor name for pivot table data",
		"belongsToMany":  "Defines many-to-many relationship with pivot table",
	}
}

// ValidatePivotMethodCall validates a pivot method call against Laravel conventions.
func ValidatePivotMethodCall(methodName string, args []string) bool {
	switch methodName {
	case "withPivot":
		// Must have at least one field argument
		return len(args) > 0
	case "withTimestamps":
		// Should not have arguments
		return len(args) == 0
	case "as":
		// Must have exactly one string argument
		return len(args) == 1
	default:
		return false
	}
}

// extractArrayArgument extracts fields from an array passed as argument
// T5: Handle ['field1', 'field2'] syntax in withPivot
func (m *DefaultPivotMatcher) extractArrayArgument(argNode *sitter.Node, tree *parser.SyntaxTree) []string {
	var fields []string
	
	// Look for array_creation_expression in the argument
	for j := uint32(0); j < argNode.ChildCount(); j++ {
		child := argNode.Child(int(j))
		if child == nil {
			continue
		}
		
		if child.Type() == "array_creation_expression" {
			return m.extractArrayFields(child, tree)
		}
	}
	
	return fields
}

// extractArrayFields extracts string fields from an array creation expression
// T5: Parse ['field1', 'field2', 'field3'] syntax
func (m *DefaultPivotMatcher) extractArrayFields(arrayNode *sitter.Node, tree *parser.SyntaxTree) []string {
	var fields []string
	
	// Walk through array elements
	for i := uint32(0); i < arrayNode.ChildCount(); i++ {
		child := arrayNode.Child(int(i))
		if child == nil {
			continue
		}
		
		// Handle array_element_initializer
		if child.Type() == "array_element_initializer" {
			// Look for string value in the initializer
			for j := uint32(0); j < child.ChildCount(); j++ {
				elemChild := child.Child(int(j))
				if elemChild != nil && (elemChild.Type() == "string" || elemChild.Type() == "encapsed_string") {
					field := m.extractStringContent(elemChild, tree)
					if field != "" {
						fields = append(fields, field)
					}
				}
			}
		}
		// Direct string in array
		if child.Type() == "string" || child.Type() == "encapsed_string" {
			field := m.extractStringContent(child, tree)
			if field != "" {
				fields = append(fields, field)
			}
		}
	}
	
	return fields
}
// isInManyToManyContext checks if the pivot method is used in a belongsToMany relationship
// T5: Validate that pivot methods are only detected in correct context
func (m *DefaultPivotMatcher) isInManyToManyContext(tree *parser.SyntaxTree, position parser.Point) bool {
	sourceLines := strings.Split(string(tree.Source), "\n")
	
	// Check current line and surrounding lines for belongsToMany
	for lineOffset := -5; lineOffset <= 2; lineOffset++ {
		lineIndex := position.Row + lineOffset
		if lineIndex >= 0 && lineIndex < len(sourceLines) {
			line := sourceLines[lineIndex]
			
			// Look for belongsToMany relationship
			if strings.Contains(line, "belongsToMany") {
				return true
			}
			
			// Also check for morphToMany and morphedByMany (polymorphic many-to-many)
			if strings.Contains(line, "morphToMany") || strings.Contains(line, "morphedByMany") {
				return true
			}
		}
	}
	
	return false
}
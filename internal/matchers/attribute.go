// Package matchers provides Laravel attribute accessor detection for Laravel 9+ attributes.
package matchers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultAttributeMatcher implements AttributeMatcher interface.
type DefaultAttributeMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewAttributeMatcher creates a new Laravel attribute accessor matcher.
func NewAttributeMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultAttributeMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultAttributeMatcher{
		config:           config,
		queryDefs:        AttributeUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize attribute matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for attribute detection.
func (m *DefaultAttributeMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile attribute queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultAttributeMatcher) GetType() PatternType {
	return PatternTypeAttribute
}

// Match finds all Laravel attribute patterns in the syntax tree.
func (m *DefaultAttributeMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("attribute matcher not initialized")
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

			// Process attribute matches
			result := m.processAttributeMatch(match, query, queryDef, tree, filePath)
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

	// Sort results by position for deterministic output
	return m.sortResultsByPosition(finalResults), nil
}

// MatchAttributes finds Laravel attribute accessor patterns.
func (m *DefaultAttributeMatcher) MatchAttributes(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*AttributeMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	attributeMatches := make([]*AttributeMatch, 0, len(results))
	for _, result := range results {
		if attributeMatch, ok := result.Data.(*AttributeMatch); ok {
			attributeMatches = append(attributeMatches, attributeMatch)
		}
	}

	return attributeMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultAttributeMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultAttributeMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultAttributeMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	return nil
}

// processAttributeMatch processes individual attribute usage matches.
func (m *DefaultAttributeMatcher) processAttributeMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var methodName string
	var returnType string
	var position parser.Point
	var argsNode *sitter.Node
	var castArray *sitter.Node

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "method_name":
			methodNode := capture.Node
			methodName = string(methodNode.Content(tree.Source))
			position = parser.Point{Row: int(methodNode.StartPoint().Row), Column: int(methodNode.StartPoint().Column)}
		case "return_type":
			returnTypeNode := capture.Node
			returnType = string(returnTypeNode.Content(tree.Source))
		case "args":
			argsNode = capture.Node
		case "cast_array":
			castArray = capture.Node
		case "class_name":
			// Capture class names for validation but not needed for processing
		case "method", "static_method", "chain_method":
			// Capture method names in chains but not needed for main processing
		}
	}

	// Process different types of attribute patterns
	switch queryDef.Name {
	case "modern_attribute_method":
		return m.processModernAttributeMethod(methodName, returnType, position, queryDef, filePath)
	case "attribute_make_call":
		return m.processAttributeMakeCall(methodName, argsNode, position, queryDef, tree, filePath)
	case "legacy_get_attribute":
		return m.processLegacyGetAttribute(methodName, position, queryDef, filePath)
	case "legacy_set_attribute":
		return m.processLegacySetAttribute(methodName, position, queryDef, filePath)
	case "attribute_with_get":
		return m.processAttributeWithGet(argsNode, position, queryDef, tree, filePath)
	case "attribute_with_set":
		return m.processAttributeWithSet(argsNode, position, queryDef, tree, filePath)
	case "attribute_with_cast":
		return m.processAttributeWithCast(castArray, position, queryDef, tree, filePath)
	default:
		return nil
	}
}

// processModernAttributeMethod processes modern attribute methods with return type Attribute.
func (m *DefaultAttributeMatcher) processModernAttributeMethod(
	methodName, returnType string,
	position parser.Point,
	queryDef QueryDefinition,
	filePath string,
) *MatchResult {
	if methodName == "" || returnType != "Attribute" {
		return nil
	}

	// Extract attribute name from method name
	attributeName := m.extractAttributeNameFromMethod(methodName)
	if attributeName == "" {
		return nil
	}

	attributeMatch := &AttributeMatch{
		Name:     attributeName,
		Type:     returnType,
		Accessor: true, // Modern attributes can be both
		Mutator:  true, // Modern attributes can be both
		IsModern: true,
		Pattern:  queryDef.Name,
		Method:   methodName,
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("public function %s(): %s", methodName, returnType),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			MethodName: methodName,
			FilePath:   filePath,
			Explicit:   true,
		},
	}
}

// processAttributeMakeCall processes Attribute::make() calls.
func (m *DefaultAttributeMatcher) processAttributeMakeCall(
	methodName string,
	argsNode *sitter.Node,
	position parser.Point,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	// Extract arguments from Attribute::make() call
	castType := m.extractCastTypeFromArgs(argsNode, tree)

	attributeMatch := &AttributeMatch{
		Name:     "unknown", // Will be inferred from context if possible
		CastType: castType,
		Accessor: true,
		Mutator:  true, // Modern attributes support both by default
		IsModern: true,
		Pattern:  queryDef.Name,
		Method:   "make",
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("Attribute::make(%s)", m.getArgsDisplay(argsNode, tree)),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: true,
		},
	}
}

// processLegacyGetAttribute processes legacy get{Name}Attribute methods.
func (m *DefaultAttributeMatcher) processLegacyGetAttribute(
	methodName string,
	position parser.Point,
	queryDef QueryDefinition,
	filePath string,
) *MatchResult {
	attributeName := m.extractAttributeNameFromLegacyMethod(methodName, "get", "Attribute")
	if attributeName == "" {
		return nil
	}

	attributeMatch := &AttributeMatch{
		Name:     attributeName,
		Accessor: true,
		Mutator:  false,
		IsModern: false,
		Pattern:  queryDef.Name,
		Method:   methodName,
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("public function %s($value)", methodName),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			MethodName: methodName,
			FilePath:   filePath,
			Explicit:   true,
		},
	}
}

// processLegacySetAttribute processes legacy set{Name}Attribute methods.
func (m *DefaultAttributeMatcher) processLegacySetAttribute(
	methodName string,
	position parser.Point,
	queryDef QueryDefinition,
	filePath string,
) *MatchResult {
	attributeName := m.extractAttributeNameFromLegacyMethod(methodName, "set", "Attribute")
	if attributeName == "" {
		return nil
	}

	attributeMatch := &AttributeMatch{
		Name:     attributeName,
		Accessor: false,
		Mutator:  true,
		IsModern: false,
		Pattern:  queryDef.Name,
		Method:   methodName,
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("public function %s($value)", methodName),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			MethodName: methodName,
			FilePath:   filePath,
			Explicit:   true,
		},
	}
}

// processAttributeWithGet processes Attribute::make()->get() chains.
func (m *DefaultAttributeMatcher) processAttributeWithGet(
	argsNode *sitter.Node,
	position parser.Point,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	attributeMatch := &AttributeMatch{
		Name:     "unknown",
		Accessor: true,
		Mutator:  false, // Only getter specified
		IsModern: true,
		Pattern:  queryDef.Name,
		Method:   "make",
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("Attribute::make()->get(%s)", m.getArgsDisplay(argsNode, tree)),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: true,
		},
	}
}

// processAttributeWithSet processes Attribute::make()->set() chains.
func (m *DefaultAttributeMatcher) processAttributeWithSet(
	argsNode *sitter.Node,
	position parser.Point,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	attributeMatch := &AttributeMatch{
		Name:     "unknown",
		Accessor: false, // Only setter specified
		Mutator:  true,
		IsModern: true,
		Pattern:  queryDef.Name,
		Method:   "make",
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("Attribute::make()->set(%s)", m.getArgsDisplay(argsNode, tree)),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: true,
		},
	}
}

// processAttributeWithCast processes casted attributes from $casts property.
func (m *DefaultAttributeMatcher) processAttributeWithCast(
	castArray *sitter.Node,
	position parser.Point,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	// Extract cast mappings from array
	castMappings := m.extractCastMappings(castArray, tree)
	if len(castMappings) == 0 {
		return nil
	}

	// For now, create a single result representing all casts
	// In a more sophisticated implementation, we might create one result per cast
	attributeMatch := &AttributeMatch{
		Name:     fmt.Sprintf("casts[%d]", len(castMappings)),
		Accessor: true,
		Mutator:  true,
		IsModern: false, // Casts are not modern attributes
		Pattern:  queryDef.Name,
		Method:   "$casts",
	}

	return &MatchResult{
		Type:       PatternTypeAttribute,
		Position:   position,
		Content:    fmt.Sprintf("protected $casts = [%d attributes]", len(castMappings)),
		Confidence: queryDef.Confidence,
		Data:       attributeMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: true,
		},
	}
}

// extractAttributeNameFromMethod extracts attribute name from modern method names.
func (m *DefaultAttributeMatcher) extractAttributeNameFromMethod(methodName string) string {
	// Modern attribute methods can have any name that makes sense
	// The attribute name is typically the snake_case version of the method name
	return m.camelToSnake(methodName)
}

// extractAttributeNameFromLegacyMethod extracts attribute name from legacy method names.
func (m *DefaultAttributeMatcher) extractAttributeNameFromLegacyMethod(methodName, prefix, suffix string) string {
	// Remove prefix and suffix, convert to snake_case
	if !strings.HasPrefix(methodName, prefix) || !strings.HasSuffix(methodName, suffix) {
		return ""
	}

	attributePart := methodName[len(prefix) : len(methodName)-len(suffix)]
	return m.camelToSnake(attributePart)
}

// extractCastTypeFromArgs extracts cast type from Attribute::make() arguments.
func (m *DefaultAttributeMatcher) extractCastTypeFromArgs(argsNode *sitter.Node, tree *parser.SyntaxTree) string {
	if argsNode == nil {
		return ""
	}

	// Look for cast specifications in the arguments
	// This is a simplified implementation - full implementation would parse closure arguments
	return "mixed"
}

// extractCastMappings extracts attribute cast mappings from $casts array.
func (m *DefaultAttributeMatcher) extractCastMappings(castArray *sitter.Node, tree *parser.SyntaxTree) map[string]string {
	mappings := make(map[string]string)
	if castArray == nil {
		return mappings
	}

	// Walk array elements to find key-value pairs
	for i := uint32(0); i < castArray.ChildCount(); i++ {
		child := castArray.Child(int(i))
		if child == nil || child.Type() != "array_element_initializer" {
			continue
		}

		// Extract key and value
		key := ""
		value := ""

		for j := uint32(0); j < child.ChildCount(); j++ {
			grandchild := child.Child(int(j))
			if grandchild == nil {
				continue
			}

			switch grandchild.Type() {
			case "string":
				if key == "" {
					key = m.cleanStringLiteral(string(grandchild.Content(tree.Source)))
				} else if value == "" {
					value = m.cleanStringLiteral(string(grandchild.Content(tree.Source)))
				}
			}
		}

		if key != "" && value != "" {
			mappings[key] = value
		}
	}

	return mappings
}

// camelToSnake converts camelCase to snake_case.
func (m *DefaultAttributeMatcher) camelToSnake(s string) string {
	// Use regex to insert underscores before uppercase letters
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	snake := re.ReplaceAllString(s, "${1}_${2}")
	return strings.ToLower(snake)
}

// cleanStringLiteral removes quotes from string literals.
func (m *DefaultAttributeMatcher) cleanStringLiteral(str string) string {
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

// getArgsDisplay creates a display string for method arguments.
func (m *DefaultAttributeMatcher) getArgsDisplay(argsNode *sitter.Node, tree *parser.SyntaxTree) string {
	if argsNode == nil {
		return ""
	}

	// Extract and format arguments
	content := strings.TrimSpace(string(argsNode.Content(tree.Source)))
	// Remove outer parentheses if present
	if strings.HasPrefix(content, "(") && strings.HasSuffix(content, ")") {
		content = content[1 : len(content)-1]
	}

	return content
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultAttributeMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
func (m *DefaultAttributeMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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
func (m *DefaultAttributeMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]*MatchResult)

	for _, result := range results {
		if attributeMatch, ok := result.Data.(*AttributeMatch); ok {
			// Create unique key based on position (row/column for method location)
			// This will group matches from the same method together
			posKey := fmt.Sprintf("%d:%d", result.Position.Row, result.Position.Column)

			if existing, exists := seen[posKey]; exists {
				// Prefer modern_attribute_method matches over attribute_make_call matches
				existingMatch := existing.Data.(*AttributeMatch)
				if attributeMatch.Pattern == "modern_attribute_method" && existingMatch.Pattern != "modern_attribute_method" {
					seen[posKey] = result // Replace with modern method match
				} else if attributeMatch.Pattern != "modern_attribute_method" && existingMatch.Pattern == "modern_attribute_method" {
					// Keep existing modern method match, skip this one
					continue
				} else if result.Confidence > existing.Confidence {
					// If same pattern type, keep higher confidence
					seen[posKey] = result
				}
			} else {
				seen[posKey] = result
			}
		}
	}

	deduplicated := make([]*MatchResult, 0, len(seen))
	for _, result := range seen {
		deduplicated = append(deduplicated, result)
	}

	return deduplicated
}

// sortResultsByPosition sorts match results by their source position for deterministic output.
func (m *DefaultAttributeMatcher) sortResultsByPosition(results []*MatchResult) []*MatchResult {
	// Sort by row first, then by column
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Position.Row > results[j].Position.Row ||
				(results[i].Position.Row == results[j].Position.Row && results[i].Position.Column > results[j].Position.Column) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results
}

// GetSupportedAttributePatterns returns commonly used Laravel attribute accessor patterns.
func GetSupportedAttributePatterns() []string {
	return []string{
		"public function fullName(): Attribute",
		"return Attribute::make(get: fn ($value) => strtoupper($value))",
		"return Attribute::make()->get(fn ($value) => strtoupper($value))",
		"public function getFirstNameAttribute($value)",
		"public function setFirstNameAttribute($value)",
		"protected $casts = ['created_at' => 'datetime']",
	}
}

// GetAttributeMethodConventions returns Laravel attribute method conventions.
func GetAttributeMethodConventions() map[string]string {
	return map[string]string{
		"modern_accessor": "public function {name}(): Attribute with return Attribute::make()",
		"legacy_accessor": "public function get{Name}Attribute($value) method pattern",
		"legacy_mutator":  "public function set{Name}Attribute($value) method pattern",
		"casts":           "protected $casts property for automatic type casting",
	}
}

// ValidateAttributeMethodCall validates an attribute method call against Laravel conventions.
func ValidateAttributeMethodCall(methodName string, isModern bool) bool {
	if isModern {
		// Modern attributes can have any reasonable method name
		return methodName != "" && !strings.Contains(methodName, "Attribute")
	}

	// Legacy attributes must follow get{Name}Attribute or set{Name}Attribute pattern
	legacyGetPattern := regexp.MustCompile(`^get[A-Z][a-zA-Z0-9]*Attribute$`)
	legacySetPattern := regexp.MustCompile(`^set[A-Z][a-zA-Z0-9]*Attribute$`)

	return legacyGetPattern.MatchString(methodName) || legacySetPattern.MatchString(methodName)
}

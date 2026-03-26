// Package matchers provides Laravel polymorphic relationship detection for complex object relationships.
package matchers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultPolymorphicMatcher implements PolymorphicMatcher interface.
type DefaultPolymorphicMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
	maxDepth         int
	morphMap         map[string]string // Global morph map cache
}

// NewPolymorphicMatcher creates a new Laravel polymorphic relationship matcher.
func NewPolymorphicMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultPolymorphicMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultPolymorphicMatcher{
		config:           config,
		queryDefs:        PolymorphicUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
		maxDepth:         config.MaxRelationshipDepth,
		morphMap:         make(map[string]string),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize polymorphic matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for polymorphic detection.
func (m *DefaultPolymorphicMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile polymorphic queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultPolymorphicMatcher) GetType() PatternType {
	return PatternTypePolymorphic
}

// SetMaxDepth configures the maximum relationship traversal depth.
func (m *DefaultPolymorphicMatcher) SetMaxDepth(maxDepth int) {
	m.maxDepth = maxDepth
}

// GetMaxDepth returns the current maximum traversal depth.
func (m *DefaultPolymorphicMatcher) GetMaxDepth() int {
	return m.maxDepth
}

// Match finds all Laravel polymorphic patterns in the syntax tree.
func (m *DefaultPolymorphicMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("polymorphic matcher not initialized")
	}

	if tree == nil || tree.Root == nil {
		return nil, fmt.Errorf("invalid syntax tree provided")
	}

	var allResults []*MatchResult

	// First, extract morph map to understand global type mappings
	m.extractMorphMap(tree)

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

			// Process polymorphic matches
			result := m.processPolymorphicMatch(match, query, queryDef, tree, filePath)
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

	return finalResults, nil
}

// MatchPolymorphic finds Laravel polymorphic relationship patterns.
func (m *DefaultPolymorphicMatcher) MatchPolymorphic(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*PolymorphicMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	polymorphicMatches := make([]*PolymorphicMatch, 0, len(results))
	for _, result := range results {
		if polymorphicMatch, ok := result.Data.(*PolymorphicMatch); ok {
			polymorphicMatches = append(polymorphicMatches, polymorphicMatch)
		}
	}

	return polymorphicMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultPolymorphicMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultPolymorphicMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultPolymorphicMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	m.morphMap = nil
	return nil
}

// extractMorphMap extracts global polymorphic type mappings from Relation::morphMap() calls.
func (m *DefaultPolymorphicMatcher) extractMorphMap(tree *parser.SyntaxTree) {
	// Clear existing morph map
	m.morphMap = make(map[string]string)

	// Look for Relation::morphMap() calls throughout the tree
	sourceLines := strings.Split(string(tree.Source), "\n")
	for _, line := range sourceLines {
		if strings.Contains(line, "Relation::morphMap") || strings.Contains(line, "morphMap") {
			m.parseMorphMapLine(line)
		}
	}
}

// parseMorphMapLine extracts type mappings from a single line containing morphMap.
func (m *DefaultPolymorphicMatcher) parseMorphMapLine(line string) {
	// Simple regex-like parsing for common patterns
	// This is a simplified implementation - in practice, you might want more robust parsing

	// Look for array-like patterns: 'key' => 'Value::class'
	parts := strings.Split(line, "=>")
	for i := 0; i < len(parts)-1; i++ {
		keyPart := strings.TrimSpace(parts[i])
		valuePart := strings.TrimSpace(parts[i+1])

		// Extract key (remove quotes and array syntax)
		key := m.extractStringLiteral(keyPart)
		if key == "" {
			// Try to get the last quoted string in the key part
			keyParts := strings.Split(keyPart, "'")
			if len(keyParts) >= 2 {
				key = keyParts[len(keyParts)-2]
			}
		}

		// Extract value (remove quotes and ::class)
		value := m.extractStringLiteral(valuePart)
		if value == "" {
			// Try to get class name from ::class pattern
			if strings.Contains(valuePart, "::class") {
				classParts := strings.Split(valuePart, "::class")
				if len(classParts) > 0 {
					className := strings.TrimSpace(classParts[0])
					className = strings.Trim(className, "'\"")
					value = className
				}
			}
		}

		if key != "" && value != "" {
			m.morphMap[key] = value
		}
	}
}

// extractStringLiteral extracts string content from quotes.
func (m *DefaultPolymorphicMatcher) extractStringLiteral(str string) string {
	str = strings.TrimSpace(str)

	// Remove single or double quotes
	if len(str) >= 2 {
		if (str[0] == '"' && str[len(str)-1] == '"') ||
			(str[0] == '\'' && str[len(str)-1] == '\'') {
			return str[1 : len(str)-1]
		}
	}

	return ""
}

// processPolymorphicMatch processes individual polymorphic usage matches.
func (m *DefaultPolymorphicMatcher) processPolymorphicMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var methodName string
	var position parser.Point
	var modelArg string
	var nameArg string
	var typeArg string
	var idArg string

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "method":
			methodNode := capture.Node
			methodName = string(methodNode.Content(tree.Source))
			position = parser.Point{Row: int(methodNode.StartPoint().Row), Column: int(methodNode.StartPoint().Column)}
		case "model_arg":
			modelNode := capture.Node
			modelArg = m.cleanArgument(string(modelNode.Content(tree.Source)))
		case "name_arg":
			nameNode := capture.Node
			nameArg = m.extractStringLiteral(string(nameNode.Content(tree.Source)))
		case "type_arg":
			typeNode := capture.Node
			typeArg = m.extractStringLiteral(string(typeNode.Content(tree.Source)))
		case "id_arg":
			idNode := capture.Node
			idArg = m.extractStringLiteral(string(idNode.Content(tree.Source)))
		}
	}

	// Validate we found a polymorphic method
	if methodName == "" {
		return nil
	}

	// Skip non-polymorphic methods
	if !m.isPolymorphicMethod(methodName) {
		return nil
	}

	// Determine relationship context from enclosing method via AST
	enclosingMethod := m.getEnclosingMethodName(match.Captures[0].Node, tree)
	relationName := enclosingMethod
	if relationName == "" {
		return nil
	}

	// Extract parent model FQCN from enclosing class via AST and gate on Eloquent inheritance
	classFQCN := m.getEnclosingClassFQCN(match.Captures[0].Node, tree)
	if classFQCN == "" {
		return nil
	}
	if !m.classExtendsEloquentModel(match.Captures[0].Node, tree) {
		return nil
	}
	parentModel := classFQCN

	// Create polymorphic match based on method type
	polymorphicMatch := m.buildPolymorphicMatch(methodName, modelArg, nameArg, typeArg, idArg, relationName, queryDef, parentModel)

	// Apply depth truncation logic
	depthTruncated := false
	currentDepth := m.calculateRelationshipDepth(tree, position)
	if currentDepth > m.maxDepth {
		depthTruncated = true
		polymorphicMatch.MaxDepth = m.maxDepth
	}
	polymorphicMatch.DepthTruncated = depthTruncated

	// Build discriminator information
	if discriminator := m.buildDiscriminator(methodName, nameArg, typeArg, modelArg); discriminator != nil {
		polymorphicMatch.Discriminator = discriminator
	}

	// Add context as Model::relationName
	polymorphicMatch.Method = parentModel + "::" + relationName

	return &MatchResult{
		Type:       PatternTypePolymorphic,
		Position:   position,
		Content:    m.buildDisplayContent(methodName, modelArg, nameArg, typeArg, idArg),
		Confidence: queryDef.Confidence,
		Data:       polymorphicMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: m.isExplicitPolymorphicUsage(queryDef.Name),
		},
	}
}

// buildPolymorphicMatch creates a PolymorphicMatch based on the detected method and arguments.
func (m *DefaultPolymorphicMatcher) buildPolymorphicMatch(methodName, modelArg, nameArg, typeArg, idArg, relationName string, queryDef QueryDefinition, parentModel string) *PolymorphicMatch {
	polymorphicMatch := &PolymorphicMatch{
		Relation: relationName,
		Type:     methodName,
		Pattern:  queryDef.Name,
		Method:   methodName,
		Context:  fmt.Sprintf("%s::%s", parentModel, relationName), // Format: "App\Models\Post::imageable"
	}

	switch methodName {
	case "morphTo":
		// morphTo relationships - belongs to polymorphic
		if nameArg != "" {
			polymorphicMatch.Relation = nameArg
		}
		if typeArg != "" {
			polymorphicMatch.MorphType = typeArg
		}
		if idArg != "" {
			polymorphicMatch.MorphId = idArg
		}
		// Infer default column names if not specified (use relationName)
		if polymorphicMatch.MorphType == "" {
			base := polymorphicMatch.Relation
			if base == "" {
				base = relationName
			}
			if base != "" {
				polymorphicMatch.MorphType = base + "_type"
			}
		}
		if polymorphicMatch.MorphId == "" {
			base := polymorphicMatch.Relation
			if base == "" {
				base = relationName
			}
			if base != "" {
				polymorphicMatch.MorphId = base + "_id"
			}
		}
	case "morphOne", "morphMany":
		// morphOne/morphMany relationships - has polymorphic
		polymorphicMatch.Model = m.cleanModelReference(modelArg)
		if nameArg != "" {
			polymorphicMatch.Relation = nameArg
		} else {
			polymorphicMatch.Relation = relationName
		}
		// For morphOne/morphMany, infer the inverse morph type/id columns from relation name
		baseName := strings.ToLower(polymorphicMatch.Relation)
		if baseName == "" {
			baseName = relationName
		}
		polymorphicMatch.MorphType = baseName + "_type"
		polymorphicMatch.MorphId = baseName + "_id"
	case "morphByMany", "morphToMany":
		// Many-to-many polymorphic relationships
		polymorphicMatch.Model = m.cleanModelReference(modelArg)
		if nameArg != "" {
			polymorphicMatch.Relation = nameArg
		}
	}

	return polymorphicMatch
}

// buildDiscriminator creates discriminator mapping information.
func (m *DefaultPolymorphicMatcher) buildDiscriminator(methodName, nameArg, typeArg, modelArg string) *PolymorphicDiscriminator {
	discriminator := &PolymorphicDiscriminator{
		Mapping: make(map[string]string),
	}

	// Set property name based on method type
	switch methodName {
	case "morphTo":
		if typeArg != "" {
			discriminator.PropertyName = typeArg
		} else if nameArg != "" {
			discriminator.PropertyName = nameArg + "_type"
		} else {
			discriminator.PropertyName = "morphable_type"
		}
	case "morphOne", "morphMany", "morphByMany", "morphToMany":
		baseName := strings.ToLower(nameArg)
		if baseName == "" {
			baseName = "morphable"
		}
		discriminator.PropertyName = baseName + "_type"
	}

	// Add global morph map mappings
	if len(m.morphMap) > 0 {
		for key, value := range m.morphMap {
			discriminator.Mapping[key] = value
		}
		discriminator.Source = "morphMap"
		discriminator.IsExplicit = true
	} else {
		// Infer mapping from model argument
		if modelArg != "" {
			cleanModel := m.cleanModelReference(modelArg)
			if cleanModel != "" {
				// Create a default mapping using the model class name
				discriminator.Mapping[strings.ToLower(cleanModel)] = cleanModel
				discriminator.Source = "inferred"
				discriminator.IsExplicit = false
			}
		}
	}

	// Only return discriminator if it has useful information
	if discriminator.PropertyName == "" && len(discriminator.Mapping) == 0 {
		return nil
	}

	return discriminator
}

// cleanModelReference cleans up model class references (removes ::class, quotes, etc.).
func (m *DefaultPolymorphicMatcher) cleanModelReference(modelRef string) string {
	modelRef = strings.TrimSpace(modelRef)

	// Remove ::class suffix
	modelRef = strings.TrimSuffix(modelRef, "::class")

	// Remove quotes
	modelRef = m.extractStringLiteral(modelRef)
	if modelRef == "" {
		// If extraction failed, try simple trim
		modelRef = strings.Trim(strings.TrimSpace(modelRef), "'\"")
	}

	// Extract just the class name if it's a full namespace
	parts := strings.Split(modelRef, "\\")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}

	return modelRef
}

// cleanArgument cleans up argument strings by removing extra whitespace and common tokens.
func (m *DefaultPolymorphicMatcher) cleanArgument(arg string) string {
	arg = strings.TrimSpace(arg)

	// Remove common argument wrappers
	if strings.HasPrefix(arg, "argument(") && strings.HasSuffix(arg, ")") {
		arg = arg[9 : len(arg)-1] // Remove "argument(" and ")"
		arg = strings.TrimSpace(arg)
	}

	return arg
}

// isPolymorphicMethod checks if a method name is a polymorphic relationship method.
func (m *DefaultPolymorphicMatcher) isPolymorphicMethod(methodName string) bool {
	switch methodName {
	case "morphTo", "morphOne", "morphMany", "morphByMany", "morphToMany":
		return true
	default:
		return false
	}
}

// inferRelationshipName attempts to determine the relationship method name from context.
func (m *DefaultPolymorphicMatcher) inferRelationshipName(tree *parser.SyntaxTree, position parser.Point, methodName string) string {
	// Look for method declaration context
	sourceLines := strings.Split(string(tree.Source), "\n")

	// Check current and surrounding lines for method declaration
	for lineOffset := -3; lineOffset <= 1; lineOffset++ {
		lineIndex := position.Row + lineOffset
		if lineIndex >= 0 && lineIndex < len(sourceLines) {
			line := sourceLines[lineIndex]

			// Look for method declaration patterns
			if strings.Contains(line, "function") && strings.Contains(line, "(") {
				// Extract method name
				if methodMatch := m.extractMethodName(line); methodMatch != "" {
					return methodMatch
				}
			}
		}
	}

	// Fallback: use method name or generate default
	switch methodName {
	case "morphTo":
		return "morphable" // Default Laravel convention
	case "morphOne", "morphMany":
		return "morphable"
	case "morphByMany", "morphToMany":
		return "morphables"
	default:
		return "polymorphic"
	}
}

// extractMethodName extracts the method name from a function declaration line.
func (m *DefaultPolymorphicMatcher) extractMethodName(line string) string {
	// Look for "function methodName(" pattern
	if funcIndex := strings.Index(line, "function"); funcIndex >= 0 {
		afterFunc := line[funcIndex+8:] // Skip "function"
		afterFunc = strings.TrimSpace(afterFunc)

		// Find the opening parenthesis
		if parenIndex := strings.Index(afterFunc, "("); parenIndex > 0 {
			methodName := strings.TrimSpace(afterFunc[:parenIndex])
			if methodName != "" && m.isValidMethodName(methodName) {
				return methodName
			}
		}
	}

	return ""
}

// isValidMethodName checks if a string is a valid PHP method name.
func (m *DefaultPolymorphicMatcher) isValidMethodName(name string) bool {
	if len(name) == 0 {
		return false
	}

	// Basic validation: starts with letter or underscore, contains only alphanumeric and underscore
	first := name[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}

	for _, char := range name[1:] {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_') {
			return false
		}
	}

	return true
}

// calculateRelationshipDepth estimates the depth of relationship traversal.
func (m *DefaultPolymorphicMatcher) calculateRelationshipDepth(tree *parser.SyntaxTree, position parser.Point) int {
	// This is a simplified depth calculation
	// In a more sophisticated implementation, you might traverse the AST to find nested relationships

	sourceLines := strings.Split(string(tree.Source), "\n")
	depth := 1 // Base depth for the current relationship

	// Look for nested relationship calls in surrounding context
	for lineOffset := -2; lineOffset <= 2; lineOffset++ {
		lineIndex := position.Row + lineOffset
		if lineIndex >= 0 && lineIndex < len(sourceLines) {
			line := sourceLines[lineIndex]

			// Count relationship method calls as depth indicators
			relationshipMethods := []string{"belongsTo", "hasOne", "hasMany", "belongsToMany", "morphTo", "morphOne", "morphMany"}
			for _, method := range relationshipMethods {
				if strings.Contains(line, method+"(") {
					depth++
				}
			}
		}
	}

	return depth
}

// buildDisplayContent creates a human-readable string representation of the polymorphic relationship.
func (m *DefaultPolymorphicMatcher) buildDisplayContent(methodName, modelArg, nameArg, typeArg, idArg string) string {
	var parts []string
	parts = append(parts, "->"+methodName+"(")

	var args []string
	if modelArg != "" {
		args = append(args, m.cleanModelReference(modelArg)+"::class")
	}
	if nameArg != "" {
		args = append(args, "'"+nameArg+"'")
	}
	if typeArg != "" {
		args = append(args, "'"+typeArg+"'")
	}
	if idArg != "" {
		args = append(args, "'"+idArg+"'")
	}

	if len(args) > 0 {
		parts = append(parts, strings.Join(args, ", "))
	}

	parts = append(parts, ")")
	return strings.Join(parts, "")
}

// isExplicitPolymorphicUsage determines if polymorphic usage is explicit.
func (m *DefaultPolymorphicMatcher) isExplicitPolymorphicUsage(patternName string) bool {
	switch patternName {
	case "morph_to_relationship", "morph_one_relationship", "morph_many_relationship":
		return true
	case "relation_morph_map":
		return true
	case "morph_to_with_name", "morph_to_with_type_and_id":
		return true
	case "morph_one_with_name", "morph_many_with_name":
		return true
	case "morph_by_many_relationship", "morph_to_many_relationship":
		return true
	case "polymorphic_in_return_statement":
		return true
	default:
		return false
	}
}

// extractParentModel extracts the parent model class name from the file path and source context.
func (m *DefaultPolymorphicMatcher) extractParentModel(tree *parser.SyntaxTree, filePath string, position parser.Point) string {
	// First, try to extract from namespace and class declaration in the source
	if modelFromSource := m.extractModelFromSource(tree); modelFromSource != "" {
		return modelFromSource
	}

	// Fallback: infer from file path (Laravel convention: app/Models/ModelName.php)
	if modelFromPath := m.extractModelFromPath(filePath); modelFromPath != "" {
		return modelFromPath
	}

	// Last resort: use "UnknownModel"
	return "UnknownModel"
}

// getEnclosingMethodName walks up from a node to find the containing method name
func (m *DefaultPolymorphicMatcher) getEnclosingMethodName(node *sitter.Node, tree *parser.SyntaxTree) string {
	current := node
	for current != nil {
		if current.Type() == "method_declaration" {
			return m.getMethodNameFromNode(current, tree.Source)
		}
		current = current.Parent()
	}
	return ""
}

// getEnclosingClassFQCN resolves the FQCN of the containing class
func (m *DefaultPolymorphicMatcher) getEnclosingClassFQCN(node *sitter.Node, tree *parser.SyntaxTree) string {
	current := node
	for current != nil {
		if current.Type() == "class_declaration" {
			className := m.getClassNameFromNode(current, tree.Source)
			ns := m.getNamespaceFromTree(tree.Source)
			if ns != "" {
				return ns + "\\" + className
			}
			return className
		}
		current = current.Parent()
	}
	return ""
}

// classExtendsEloquentModel checks class_declaration base_clause for Eloquent Model
func (m *DefaultPolymorphicMatcher) classExtendsEloquentModel(node *sitter.Node, tree *parser.SyntaxTree) bool {
	current := node
	for current != nil {
		if current.Type() == "class_declaration" {
			// scan children for base_clause
			for i := uint32(0); i < current.ChildCount(); i++ {
				ch := current.Child(int(i))
				if ch != nil && ch.Type() == "base_clause" {
					txt := strings.ReplaceAll(string(ch.Content(tree.Source)), " ", "")
					if strings.Contains(txt, "Illuminate\\Database\\Eloquent\\Model") ||
						strings.HasSuffix(txt, "\\Model") || strings.HasSuffix(txt, "Model") ||
						txt == "Model" || strings.HasSuffix(txt, ":Model") {
						return true
					}
				}
			}
			return false
		}
		current = current.Parent()
	}
	return false
}

// extractModelFromSource extracts the full model class name from namespace and class declaration.
func (m *DefaultPolymorphicMatcher) extractModelFromSource(tree *parser.SyntaxTree) string {
	sourceLines := strings.Split(string(tree.Source), "\n")

	var namespace string
	var className string

	for _, line := range sourceLines {
		line = strings.TrimSpace(line)

		// Extract namespace
		if strings.HasPrefix(line, "namespace ") && strings.HasSuffix(line, ";") {
			namespace = strings.TrimSpace(line[10 : len(line)-1]) // Remove "namespace " and ";"
		}

		// Extract class name
		if strings.Contains(line, "class ") && strings.Contains(line, "extends Model") {
			// Look for "class ClassName extends Model"
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "class" && i+1 < len(parts) {
					className = parts[i+1]
					break
				}
			}
			if className != "" {
				break // Found class, stop looking
			}
		}
	}

	// Combine namespace and class name
	if namespace != "" && className != "" {
		return fmt.Sprintf("%s\\%s", namespace, className)
	} else if className != "" {
		return className
	}

	return ""
}

// extractModelFromPath extracts model name from Laravel convention file path.
func (m *DefaultPolymorphicMatcher) extractModelFromPath(filePath string) string {
	// Laravel convention: app/Models/ModelName.php or App/Models/ModelName.php
	if strings.Contains(filePath, "/Models/") {
		parts := strings.Split(filePath, "/Models/")
		if len(parts) >= 2 {
			modelFile := parts[len(parts)-1] // Get the part after last "/Models/"
			// Remove .php extension
			if strings.HasSuffix(modelFile, ".php") {
				modelName := strings.TrimSuffix(modelFile, ".php")
				// Assume App\Models namespace by default
				return fmt.Sprintf("App\\Models\\%s", modelName)
			}
		}
	}

	return ""
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultPolymorphicMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
func (m *DefaultPolymorphicMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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
func (m *DefaultPolymorphicMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]*MatchResult)

	for _, result := range results {
		if polymorphicMatch, ok := result.Data.(*PolymorphicMatch); ok {
			// Create unique key based on position and relationship details
			key := fmt.Sprintf("%s:%d:%d:%s:%s", polymorphicMatch.Relation, result.Position.Row, result.Position.Column, polymorphicMatch.Method, polymorphicMatch.Type)

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

// extractControllerMethodContext walks up the AST to find the controller class and method
// containing this polymorphic pattern match
func (m *DefaultPolymorphicMatcher) extractControllerMethodContext(node *sitter.Node, tree *parser.SyntaxTree, filePath string) string {
	current := node

	// Walk up the AST to find the method declaration
	for current != nil {
		if current.Type() == "method_declaration" {
			// Found method, get its name
			methodName := m.getMethodNameFromNode(current, tree.Source)

			// Continue walking up to find the class
			classNode := current
			for classNode != nil {
				if classNode.Type() == "class_declaration" {
					className := m.getClassNameFromNode(classNode, tree.Source)
					namespace := m.getNamespaceFromTree(tree.Source)

					// Build FQCN
					var fqcn string
					if namespace != "" {
						fqcn = namespace + "\\" + className
					} else {
						fqcn = className
					}

					return fqcn + "::" + methodName
				}
				classNode = classNode.Parent()
			}
		}
		current = current.Parent()
	}

	// If we can't find method context, return empty (will be marked unresolvable)
	return ""
}

// getMethodNameFromNode extracts method name from method_declaration node
func (m *DefaultPolymorphicMatcher) getMethodNameFromNode(methodNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getClassNameFromNode extracts class name from class_declaration node
func (m *DefaultPolymorphicMatcher) getClassNameFromNode(classNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getNamespaceFromTree extracts namespace from the file by parsing the source
func (m *DefaultPolymorphicMatcher) getNamespaceFromTree(source []byte) string {
	sourceStr := string(source)

	// Look for namespace declaration
	lines := strings.Split(sourceStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "namespace ") {
			// Extract namespace
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				namespace := parts[1]
				// Remove semicolon if present
				namespace = strings.TrimSuffix(namespace, ";")
				return namespace
			}
		}
	}

	return ""
}

// GetSupportedPolymorphicPatterns returns commonly used Laravel polymorphic relationship patterns.
func GetSupportedPolymorphicPatterns() []string {
	return []string{
		"$this->morphTo()",
		"$this->morphOne(Comment::class, 'commentable')",
		"$this->morphMany(Tag::class, 'taggable')",
		"$this->morphTo('imageable', 'imageable_type', 'imageable_id')",
		"Relation::morphMap(['post' => Post::class, 'video' => Video::class])",
		"$this->morphByMany(Tag::class, 'taggable')",
		"$this->morphToMany(Video::class, 'videoable')",
	}
}

// GetPolymorphicMethodConventions returns Laravel polymorphic method conventions.
func GetPolymorphicMethodConventions() map[string]string {
	return map[string]string{
		"morphTo":     "Defines polymorphic belongs-to relationship (child side)",
		"morphOne":    "Defines polymorphic one-to-one relationship (parent side)",
		"morphMany":   "Defines polymorphic one-to-many relationship (parent side)",
		"morphByMany": "Defines polymorphic many-to-many relationship (inverse)",
		"morphToMany": "Defines polymorphic many-to-many relationship",
		"morphMap":    "Defines global polymorphic type mappings",
	}
}

// ValidatePolymorphicMethodCall validates a polymorphic method call against Laravel conventions.
func ValidatePolymorphicMethodCall(methodName string, args []string) bool {
	switch methodName {
	case "morphTo":
		// Can have 0, 1, or 3 arguments (name, type, id)
		return len(args) == 0 || len(args) == 1 || len(args) == 3
	case "morphOne", "morphMany":
		// Must have at least 1 argument (model class), optionally name and type/id columns
		return len(args) >= 1 && len(args) <= 4
	case "morphByMany", "morphToMany":
		// Must have at least 1 argument (model class) and relationship name
		return len(args) >= 2
	default:
		return false
	}
}

// Package matchers provides Laravel Resource detection for API responses.
package matchers

import (
	"context"
	"fmt"
	"strings"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultResourceMatcher implements ResourceMatcher interface.
type DefaultResourceMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
}

// NewResourceMatcher creates a new Laravel Resource matcher.
func NewResourceMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultResourceMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}

	if config == nil {
		config = DefaultMatcherConfig()
	}

	matcher := &DefaultResourceMatcher{
		config:           config,
		queryDefs:        ResourceUsageQueries,
		compiler:         NewQueryCompiler(language),
		confidenceLevels: DefaultConfidenceLevels(),
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize resource matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for resource detection.
func (m *DefaultResourceMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile resource queries: %w", err)
	}

	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultResourceMatcher) GetType() PatternType {
	return PatternTypeResource
}

// Match finds all Laravel Resource patterns in the syntax tree.
func (m *DefaultResourceMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("resource matcher not initialized")
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

			// Process resource matches
			result := m.processResourceMatch(match, query, queryDef, tree, filePath)
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

// MatchResources finds Laravel Resource usage patterns.
func (m *DefaultResourceMatcher) MatchResources(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*ResourceMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	resourceMatches := make([]*ResourceMatch, 0, len(results))
	for _, result := range results {
		if resourceMatch, ok := result.Data.(*ResourceMatch); ok {
			resourceMatches = append(resourceMatches, resourceMatch)
		}
	}

	return resourceMatches, nil
}

// GetQueries returns the compiled tree-sitter queries.
func (m *DefaultResourceMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultResourceMatcher) IsInitialized() bool {
	return m.initialized && len(m.queries) > 0
}

// Close releases resources held by the matcher.
func (m *DefaultResourceMatcher) Close() error {
	if m.compiler != nil {
		m.compiler.Close()
	}

	m.initialized = false
	m.queries = nil
	return nil
}

// processResourceMatch processes individual resource usage matches.
func (m *DefaultResourceMatcher) processResourceMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var className string
	var methodName string
	var position parser.Point
	var classNode *sitter.Node

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "class":
			classNode = capture.Node
			className = string(classNode.Content(tree.Source))
			position = parser.Point{Row: int(classNode.StartPoint().Row), Column: int(classNode.StartPoint().Column)}
		case "method":
			methodNode := capture.Node
			methodName = string(methodNode.Content(tree.Source))
		}
	}

	// Validate we found a resource class
	if className == "" {
		return nil
	}

	resolvedClassName := m.resolveClassName(className, tree)

	// Clean class name
	className = m.cleanResourceClassName(className)

	// Determine if class looks like a Resource or is an alias to a Resource
	looksResource := m.isResourceClass(className, resolvedClassName)
	if !looksResource {
		// Try alias resolution based on use statements
		aliasMap := parseUseAliases(string(tree.Source))
		if fqcn, ok := aliasMap[className]; ok && m.isResourceClass(className, fqcn) {
			looksResource = true
			resolvedClassName = fqcn
		}
	}
	if !looksResource {
		return nil
	}

	patternName := m.classifyPattern(classNode, methodName, queryDef.Name)

	// Determine if this is a collection or single resource
	isCollection := m.determineCollectionType(patternName, methodName)

	// Extract controller context from the AST
	controllerMethod := m.extractControllerMethodContext(match.Captures[0].Node, tree, filePath)

	// Create resource match
	resourceMatch := &ResourceMatch{
		Class:      className,
		FQCN:       strings.TrimPrefix(resolvedClassName, `\`),
		Collection: isCollection,
		Pattern:    patternName,
		Method:     controllerMethod, // Use controller context instead of resource method
	}

	return &MatchResult{
		Type:       PatternTypeResource,
		Position:   position,
		Content:    fmt.Sprintf("%s%s", className, m.getMethodSuffix(methodName)),
		Confidence: m.matchConfidence(patternName),
		Data:       resourceMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: m.isExplicitResourceUsage(patternName),
		},
	}
}

// cleanResourceClassName cleans and normalizes resource class names.
func (m *DefaultResourceMatcher) cleanResourceClassName(className string) string {
	// Remove leading/trailing whitespace
	className = strings.TrimSpace(className)

	// Remove namespace separators if they exist
	parts := strings.Split(className, "\\")
	if len(parts) > 0 {
		className = parts[len(parts)-1]
	}

	return className
}

// isResourceClass validates that a class name appears to be a Laravel Resource.
func (m *DefaultResourceMatcher) isResourceClass(className, resolvedClassName string) bool {
	// Must end with "Resource"
	if strings.HasSuffix(className, "Resource") || strings.HasSuffix(resolvedClassName, "Resource") {
		goto validate
	}

	// Explicit ResourceCollection classes are also response resources when they
	// live under a Laravel resources namespace.
	if (strings.HasSuffix(className, "Collection") || strings.HasSuffix(resolvedClassName, "Collection")) &&
		strings.Contains(resolvedClassName, `\Resources\`) {
		goto validate
	}

	return false

validate:

	// Must be at least "XResource" (minimum length check)
	shortResolved := resolvedClassName
	if idx := strings.LastIndex(shortResolved, `\`); idx != -1 {
		shortResolved = shortResolved[idx+1:]
	}
	if len(className) < 9 && len(shortResolved) < 9 {
		return false
	}

	// Must start with uppercase letter (PSR-4 compliance)
	if len(className) > 0 && !isUpperCase(className[0]) {
		return false
	}

	return true
}

// determineCollectionType determines if resource usage is for collections or single items.
func (m *DefaultResourceMatcher) determineCollectionType(patternName, methodName string) bool {
	switch patternName {
	case "resource_collection_static", "return_resource_collection":
		return true // ::collection() calls are definitely collections
	case "resource_make_static":
		return false // ::make() calls are typically single resources
	case "new_resource_instantiation", "return_new_resource", "variable_resource_assignment":
		return false // Direct instantiation is typically single resources
	default:
		return false // Default to single resource
	}
}

func (m *DefaultResourceMatcher) classifyPattern(classNode *sitter.Node, methodName, fallback string) string {
	inReturn := false
	inAssignment := false

	for current := classNode; current != nil; current = current.Parent() {
		switch current.Type() {
		case "return_statement":
			inReturn = true
		case "assignment_expression":
			inAssignment = true
		}
	}

	switch methodName {
	case "collection":
		if inReturn {
			return "return_resource_collection"
		}
		return "resource_collection_static"
	case "make":
		return "resource_make_static"
	}

	if inAssignment {
		return "variable_resource_assignment"
	}
	if inReturn {
		return "return_new_resource"
	}
	if fallback != "" {
		return fallback
	}
	return "new_resource_instantiation"
}

// isExplicitResourceUsage determines if resource usage is explicit.
func (m *DefaultResourceMatcher) isExplicitResourceUsage(patternName string) bool {
	switch patternName {
	case "return_new_resource", "return_resource_collection":
		return true // Return statements are explicit
	case "resource_collection_static", "resource_make_static":
		return true // Static method calls are explicit
	case "new_resource_instantiation":
		return true // Direct instantiation is explicit
	case "variable_resource_assignment":
		return false // Variable assignment is less explicit
	default:
		return false
	}
}

// getMethodSuffix returns a suffix for display based on method used.
func (m *DefaultResourceMatcher) getMethodSuffix(methodName string) string {
	switch methodName {
	case "collection":
		return "::collection()"
	case "make":
		return "::make()"
	default:
		return ""
	}
}

// resolveClassName attempts to resolve the full class name using import statements.
func (m *DefaultResourceMatcher) resolveClassName(className string, tree *parser.SyntaxTree) string {
	aliasMap := parseUseAliases(string(tree.Source))
	if fqcn, ok := aliasMap[className]; ok {
		return fqcn
	}
	return className
}

func (m *DefaultResourceMatcher) matchConfidence(patternName string) float64 {
	switch patternName {
	case "resource_collection_static", "return_resource_collection":
		return 1.0
	case "new_resource_instantiation", "return_new_resource":
		return 0.98
	case "resource_make_static", "variable_resource_assignment":
		return 0.95
	default:
		return 0.95
	}
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
func (m *DefaultResourceMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
func (m *DefaultResourceMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
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
func (m *DefaultResourceMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if !m.config.DeduplicateMatches {
		return results
	}

	selected := make(map[string]*MatchResult)

	rank := func(rm *ResourceMatch) int {
		switch rm.Pattern {
		case "resource_make_static":
			return 100
		case "return_resource_collection":
			return 95
		case "resource_collection_static":
			return 90
		case "return_new_resource":
			return 80
		case "new_resource_instantiation":
			return 70
		case "variable_resource_assignment":
			return 60
		default:
			return 10
		}
	}

	for _, result := range results {
		if resourceMatch, ok := result.Data.(*ResourceMatch); ok {
			key := fmt.Sprintf("%s:%d:%d", resourceMatch.Class, result.Position.Row, result.Position.Column)
			if prev, exists := selected[key]; exists {
				prevRM := prev.Data.(*ResourceMatch)
				r1 := rank(resourceMatch)
				r2 := rank(prevRM)
				if r1 > r2 || (r1 == r2 && result.Confidence > prev.Confidence) {
					selected[key] = result
				}
			} else {
				selected[key] = result
			}
		}
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(selected))
	for key := range selected {
		keys = append(keys, key)
	}

	// Sort keys alphabetically for consistent ordering
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	deduplicated := make([]*MatchResult, 0, len(selected))
	for _, key := range keys {
		deduplicated = append(deduplicated, selected[key])
	}
	return deduplicated
}

// parseUseAliases extracts alias → FQCN mappings from use statements
func parseUseAliases(src string) map[string]string {
	m := make(map[string]string)
	lines := strings.Split(src, "\n")
	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		if !strings.HasPrefix(s, "use ") || !strings.HasSuffix(s, ";") {
			continue
		}
		s = strings.TrimSuffix(strings.TrimPrefix(s, "use "), ";")
		parts := strings.Split(s, " as ")
		if len(parts) == 1 {
			fqcn := strings.TrimSpace(parts[0])
			alias := fqcn
			if idx := strings.LastIndex(alias, "\\"); idx != -1 {
				alias = alias[idx+1:]
			}
			if alias != "" {
				m[alias] = fqcn
			}
		} else {
			fqcn := strings.TrimSpace(parts[0])
			alias := strings.TrimSpace(parts[1])
			if alias != "" {
				m[alias] = fqcn
			}
		}
	}
	return m
}

// isUpperCase checks if a byte represents an uppercase letter.
func isUpperCase(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

// GetSupportedResourcePatterns returns commonly used Laravel Resource patterns.
func GetSupportedResourcePatterns() []string {
	return []string{
		"new UserResource($user)",
		"UserResource::collection($users)",
		"UserResource::make($user)",
		"return new UserResource($user)",
		"return UserResource::collection($users)",
	}
}

// GetResourceNamingConventions returns Laravel Resource naming conventions.
func GetResourceNamingConventions() map[string]string {
	return map[string]string{
		"suffix":     "Resource",
		"namespace":  "App\\Http\\Resources",
		"collection": "::collection()",
		"single":     "new or ::make()",
	}
}

// ValidateResourceClassName validates a resource class name against Laravel conventions.
func ValidateResourceClassName(className string) bool {
	// Must end with "Resource"
	if !strings.HasSuffix(className, "Resource") {
		return false
	}

	// Must be properly capitalized
	if len(className) > 0 && !isUpperCase(className[0]) {
		return false
	}

	// Must have reasonable length
	if len(className) < 9 || len(className) > 100 {
		return false
	}

	return true
}

// extractControllerMethodContext walks up the AST to find the controller class and method
// containing this resource pattern match
func (m *DefaultResourceMatcher) extractControllerMethodContext(node *sitter.Node, tree *parser.SyntaxTree, filePath string) string {
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
func (m *DefaultResourceMatcher) getMethodNameFromNode(methodNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getClassNameFromNode extracts class name from class_declaration node
func (m *DefaultResourceMatcher) getClassNameFromNode(classNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getNamespaceFromTree extracts namespace from the file by parsing the source
func (m *DefaultResourceMatcher) getNamespaceFromTree(source []byte) string {
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

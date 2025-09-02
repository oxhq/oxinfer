// Package matchers provides Laravel query scope detection for local and global scopes.
package matchers

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultScopeMatcher implements ScopeMatcher interface.
type DefaultScopeMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
	
	// Patterns for scope name extraction
	scopeMethodPattern *regexp.Regexp
	whereMethodPattern *regexp.Regexp
}

// NewScopeMatcher creates a new Laravel query scope matcher.
func NewScopeMatcher(language *sitter.Language, config *MatcherConfig) (*DefaultScopeMatcher, error) {
	if language == nil {
		return nil, fmt.Errorf("language cannot be nil")
	}
	
	if config == nil {
		config = DefaultMatcherConfig()
	}

	// Compile regex patterns for scope name extraction
	scopeMethodPattern, err := regexp.Compile(`^scope([A-Z][a-zA-Z0-9]*)$`)
	if err != nil {
		return nil, fmt.Errorf("failed to compile scope method pattern: %w", err)
	}
	
	whereMethodPattern, err := regexp.Compile(`^where([A-Z][a-zA-Z0-9]*)$`)
	if err != nil {
		return nil, fmt.Errorf("failed to compile where method pattern: %w", err)
	}

	matcher := &DefaultScopeMatcher{
		config:             config,
		queryDefs:          ScopeUsageQueries,
		compiler:           NewQueryCompiler(language),
		confidenceLevels:   DefaultConfidenceLevels(),
		scopeMethodPattern: scopeMethodPattern,
		whereMethodPattern: whereMethodPattern,
	}

	// Compile all queries
	if err := matcher.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize scope matcher: %w", err)
	}

	return matcher, nil
}

// initialize compiles all tree-sitter queries for scope detection.
func (m *DefaultScopeMatcher) initialize() error {
	queries, err := m.compiler.CompileQueries(m.queryDefs)
	if err != nil {
		return fmt.Errorf("failed to compile scope queries: %w", err)
	}
	
	m.queries = queries
	m.initialized = true
	return nil
}

// GetType returns the pattern type this matcher detects.
func (m *DefaultScopeMatcher) GetType() PatternType {
	return PatternTypeScope
}

// Match finds all Laravel query scope patterns in the syntax tree.
func (m *DefaultScopeMatcher) Match(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*MatchResult, error) {
	if !m.initialized {
		return nil, fmt.Errorf("scope matcher not initialized")
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

			// Process scope matches
			result := m.processScopeMatch(match, query, queryDef, tree, filePath)
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

// MatchScopes finds Laravel query scope patterns with detailed extraction.
func (m *DefaultScopeMatcher) MatchScopes(ctx context.Context, tree *parser.SyntaxTree, filePath string) ([]*ScopeMatch, error) {
	results, err := m.Match(ctx, tree, filePath)
	if err != nil {
		return nil, err
	}

	scopes := make([]*ScopeMatch, 0, len(results))
	for _, result := range results {
		if scopeMatch, ok := result.Data.(*ScopeMatch); ok {
			scopes = append(scopes, scopeMatch)
		}
	}

	return scopes, nil
}

// processMatch processes a single tree-sitter match and extracts scope information.
func (m *DefaultScopeMatcher) processMatch(match *sitter.QueryMatch, queryDef QueryDefinition, tree *parser.SyntaxTree, filePath string) (*MatchResult, error) {
	if len(match.Captures) == 0 {
		return nil, fmt.Errorf("no captures in match")
	}

	var scopeMatch *ScopeMatch
	var confidence float64
	var position parser.Point
	var content string

	// Extract basic position and content from first capture
	firstCapture := match.Captures[0]
	position = parser.Point{
		Row:    int(firstCapture.Node.StartPoint().Row),
		Column: int(firstCapture.Node.StartPoint().Column),
	}
	content = firstCapture.Node.Content(tree.Source)

	// Process based on query type
	switch queryDef.Name {
	case "local_scope_definition":
		scopeMatch = m.processLocalScopeDefinition(match, tree)
		confidence = queryDef.Confidence
	case "scope_method_call_on_query":
		scopeMatch = m.processScopeMethodCall(match, tree, "query")
		confidence = queryDef.Confidence
	case "scope_method_call_on_model":
		scopeMatch = m.processScopeMethodCall(match, tree, "model")
		confidence = queryDef.Confidence
	case "scope_without_prefix_on_query":
		scopeMatch = m.processScopeWithoutPrefix(match, tree, "query")
		confidence = queryDef.Confidence
	case "scope_without_prefix_on_model_query":
		scopeMatch = m.processScopeWithoutPrefix(match, tree, "model_query")
		confidence = queryDef.Confidence
	case "global_scope_class_definition":
		scopeMatch = m.processGlobalScopeClass(match, tree)
		confidence = queryDef.Confidence
	case "global_scope_apply_method":
		scopeMatch = m.processGlobalScopeApply(match, tree)
		confidence = queryDef.Confidence
	case "scope_registration_in_boot":
		scopeMatch = m.processScopeRegistration(match, tree)
		confidence = queryDef.Confidence
	case "has_many_with_scope":
		scopeMatch = m.processRelationshipScope(match, tree)
		confidence = queryDef.Confidence
	case "whereable_scope_pattern":
		scopeMatch = m.processWhereableScope(match, tree)
		confidence = queryDef.Confidence
	default:
		return nil, fmt.Errorf("unknown query type: %s", queryDef.Name)
	}

	if scopeMatch == nil {
		return nil, fmt.Errorf("failed to process scope match")
	}

	// Apply confidence threshold filtering
	if confidence < m.config.MinConfidenceThreshold {
		return nil, nil // Filter out low confidence matches
	}

	// Extract class context if available
	className := m.extractClassName(tree, filePath)
	
	return &MatchResult{
		Type:       PatternTypeScope,
		Position:   position,
		Content:    content,
		Confidence: confidence,
		Data:       scopeMatch,
		Context: &MatchContext{
			ClassName: className,
			FilePath:  filePath,
			Explicit:  confidence >= m.confidenceLevels.High,
		},
	}, nil
}

// processLocalScopeDefinition processes scope method definitions in models.
func (m *DefaultScopeMatcher) processLocalScopeDefinition(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	methodName, ok := captures["method_name"]
	if !ok {
		return nil
	}
	
	methodNameStr := methodName.Content(tree.Source)
	scopeName := m.extractScopeName(methodNameStr)
	
	if scopeName == "" {
		return nil
	}

	// Extract parameters if available
	var args []interface{}
	if bodyCapture, ok := captures["body"]; ok {
		args = m.extractScopeArguments(bodyCapture, tree)
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       m.extractClassName(tree, ""),
		Args:     args,
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "definition",
		Method:   methodNameStr,
		Context:  "model",
	}
}

// processScopeMethodCall processes direct scope method calls.
func (m *DefaultScopeMatcher) processScopeMethodCall(match *sitter.QueryMatch, tree *parser.SyntaxTree, context string) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	var methodName, modelClass string
	var args []interface{}
	
	if scopeMethod, ok := captures["scope_method"]; ok {
		methodName = scopeMethod.Content(tree.Source)
	}
	
	if modelCapture, ok := captures["model_class"]; ok {
		modelClass = modelCapture.Content(tree.Source)
	}
	
	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	scopeName := m.extractScopeName(methodName)
	if scopeName == "" {
		return nil
	}

	onClass := modelClass
	if onClass == "" {
		onClass = m.extractClassName(tree, "")
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       onClass,
		Args:     args,
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "usage",
		Method:   methodName,
		Context:  context,
	}
}

// processScopeWithoutPrefix processes scope calls without 'scope' prefix.
func (m *DefaultScopeMatcher) processScopeWithoutPrefix(match *sitter.QueryMatch, tree *parser.SyntaxTree, context string) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	var scopeName, modelClass string
	var args []interface{}
	
	if scopeCapture, ok := captures["scope_name"]; ok {
		scopeName = scopeCapture.Content(tree.Source)
	}
	
	if modelCapture, ok := captures["model_class"]; ok {
		modelClass = modelCapture.Content(tree.Source)
	} else if varCapture, ok := captures["model_var"]; ok {
		// Try to infer model class from variable usage context
		modelClass = m.inferModelFromVariable(varCapture, tree)
	}
	
	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	onClass := modelClass
	if onClass == "" {
		onClass = m.extractClassName(tree, "")
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       onClass,
		Args:     args,
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "usage",
		Method:   scopeName,
		Context:  context,
	}
}

// processGlobalScopeClass processes global scope class definitions.
func (m *DefaultScopeMatcher) processGlobalScopeClass(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	className, ok := captures["class_name"]
	if !ok {
		return nil
	}
	
	classNameStr := className.Content(tree.Source)
	scopeName := strings.TrimSuffix(classNameStr, "Scope")
	scopeName = strings.ToLower(scopeName)

	return &ScopeMatch{
		Name:     scopeName,
		On:       classNameStr,
		Args:     []interface{}{},
		IsGlobal: true,
		IsLocal:  false,
		Pattern:  "global_definition",
		Method:   "apply",
		Context:  "global_scope",
	}
}

// processGlobalScopeApply processes apply methods in global scopes.
func (m *DefaultScopeMatcher) processGlobalScopeApply(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	var args []interface{}
	if bodyCapture, ok := captures["body"]; ok {
		args = m.extractScopeArguments(bodyCapture, tree)
	}

	className := m.extractClassName(tree, "")
	scopeName := strings.TrimSuffix(className, "Scope")
	scopeName = strings.ToLower(scopeName)

	return &ScopeMatch{
		Name:     scopeName,
		On:       className,
		Args:     args,
		IsGlobal: true,
		IsLocal:  false,
		Pattern:  "global_application",
		Method:   "apply",
		Context:  "global_scope",
	}
}

// processScopeRegistration processes scope registration in boot methods.
func (m *DefaultScopeMatcher) processScopeRegistration(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	var args []interface{}
	if argsCapture, ok := captures["scope_arg"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	// Try to determine scope name from arguments
	scopeName := "unknown"
	if len(args) > 0 {
		if strArg, ok := args[0].(string); ok {
			scopeName = strings.ToLower(strArg)
		}
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       m.extractClassName(tree, ""),
		Args:     args,
		IsGlobal: true,
		IsLocal:  false,
		Pattern:  "registration",
		Method:   "addGlobalScope",
		Context:  "boot",
	}
}

// processRelationshipScope processes scope usage in relationships.
func (m *DefaultScopeMatcher) processRelationshipScope(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	var scopeMethod, relationMethod string
	var args []interface{}
	
	if scopeCapture, ok := captures["scope_method"]; ok {
		scopeMethod = scopeCapture.Content(tree.Source)
	}
	
	if relationCapture, ok := captures["relation_method"]; ok {
		relationMethod = relationCapture.Content(tree.Source)
	}
	
	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	scopeName := m.extractScopeName(scopeMethod)
	if scopeName == "" {
		scopeName = scopeMethod
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       m.extractClassName(tree, ""),
		Args:     args,
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "relationship",
		Method:   scopeMethod,
		Context:  relationMethod,
	}
}

// processWhereableScope processes dynamic whereable scopes.
func (m *DefaultScopeMatcher) processWhereableScope(match *sitter.QueryMatch, tree *parser.SyntaxTree) *ScopeMatch {
	captures := m.mapCaptures(match)
	
	methodName, ok := captures["method_name"]
	if !ok {
		return nil
	}
	
	methodNameStr := methodName.Content(tree.Source)
	
	// Extract scope name from whereXxx pattern
	matches := m.whereMethodPattern.FindStringSubmatch(methodNameStr)
	if len(matches) < 2 {
		return nil
	}
	
	scopeName := strings.ToLower(matches[1])
	
	var args []interface{}
	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	return &ScopeMatch{
		Name:     scopeName,
		On:       m.extractClassName(tree, ""),
		Args:     args,
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "whereable",
		Method:   methodNameStr,
		Context:  "dynamic",
	}
}

// Helper methods

// extractScopeName extracts the scope name from a method name.
func (m *DefaultScopeMatcher) extractScopeName(methodName string) string {
	matches := m.scopeMethodPattern.FindStringSubmatch(methodName)
	if len(matches) >= 2 {
		return strings.ToLower(matches[1])
	}
	return ""
}

// extractScopeArguments extracts arguments from a scope call.
func (m *DefaultScopeMatcher) extractScopeArguments(node *sitter.Node, tree *parser.SyntaxTree) []interface{} {
	var args []interface{}
	
	// This is a simplified implementation - in practice you'd want to 
	// walk the argument nodes and extract literal values
	content := node.Content(tree.Source)
	if strings.TrimSpace(content) != "()" {
		// For now, just indicate that arguments are present
		args = append(args, content)
	}
	
	return args
}

// extractClassName extracts the class name from the syntax tree.
func (m *DefaultScopeMatcher) extractClassName(tree *parser.SyntaxTree, filePath string) string {
	if tree == nil || tree.Root == nil {
		return "UnknownClass"
	}

	// Try to find class declaration using simple tree traversal
	className := m.findClassNameInTree(tree)
	if className != "" {
		return className
	}

	// If no classes found in tree, try to extract from file path as fallback
	if filePath != "" {
		return m.inferClassNameFromFilePath(filePath)
	}

	return "UnknownClass"
}

// findClassNameInTree performs a simple traversal to find class declarations.
func (m *DefaultScopeMatcher) findClassNameInTree(tree *parser.SyntaxTree) string {
	if tree == nil || tree.Source == nil {
		return ""
	}

	// Use simple regex to find class declarations in source code
	// This is a basic implementation that works for most PHP class declarations
	classPattern, err := regexp.Compile(`class\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:extends|implements|\{)`)
	if err != nil {
		return ""
	}

	matches := classPattern.FindSubmatch(tree.Source)
	if len(matches) >= 2 {
		return string(matches[1])
	}

	return ""
}

// inferClassNameFromFilePath attempts to infer a class name from the file path.
func (m *DefaultScopeMatcher) inferClassNameFromFilePath(filePath string) string {
	if filePath == "" {
		return "UnknownClass"
	}

	// Extract filename without extension
	fileName := filepath.Base(filePath)
	if dotIndex := strings.LastIndex(fileName, "."); dotIndex != -1 {
		fileName = fileName[:dotIndex]
	}

	// Convert to PascalCase (common PHP class naming convention)
	if fileName != "" {
		return strings.Title(fileName)
	}

	return "UnknownClass"
}

// inferModelFromVariable tries to infer model class from variable context.
func (m *DefaultScopeMatcher) inferModelFromVariable(node *sitter.Node, tree *parser.SyntaxTree) string {
	if node == nil || tree == nil {
		return ""
	}

	// Get the variable name
	varName := string(node.Content(tree.Source))
	if varName == "" {
		return ""
	}

	// Try to trace back to variable assignment or type declaration
	// This is a simplified implementation - in practice you'd want to
	// walk the AST to find variable assignments or type hints
	
	// Look for common Laravel model variable naming patterns
	if strings.HasPrefix(varName, "$") {
		varName = strings.TrimPrefix(varName, "$")
	}

	// Convert from camelCase/snake_case to PascalCase
	modelName := m.variableNameToClassName(varName)
	if modelName != "" {
		return modelName
	}

	return ""
}

// variableNameToClassName converts a variable name to a likely class name.
func (m *DefaultScopeMatcher) variableNameToClassName(varName string) string {
	if varName == "" {
		return ""
	}

	// Convert snake_case to PascalCase
	if strings.Contains(varName, "_") {
		parts := strings.Split(varName, "_")
		for i, part := range parts {
			parts[i] = strings.Title(part)
		}
		return strings.Join(parts, "")
	}

	// Convert camelCase to PascalCase
	if varName != "" {
		return strings.Title(varName)
	}

	return ""
}

// mapCaptures converts captures array to a map for easier access.
func (m *DefaultScopeMatcher) mapCaptures(match *sitter.QueryMatch) map[string]*sitter.Node {
	captures := make(map[string]*sitter.Node)
	
	for _, capture := range match.Captures {
		// This requires the query to define capture names
		// We'll need to use the capture index to match with query definition
		captures[fmt.Sprintf("capture_%d", capture.Index)] = capture.Node
	}
	
	return captures
}

// convertToSitterNode converts SyntaxTree back to tree-sitter node and tree for querying.
// This re-parses the content to get proper tree-sitter structures.
func (m *DefaultScopeMatcher) convertToSitterNode(tree *parser.SyntaxTree) (*sitter.Node, *sitter.Tree, error) {
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
		return nil, nil, fmt.Errorf("re-parsing returned nil root node")
	}

	return rootNode, sitterTree, nil
}

// processScopeMatch processes individual scope matches.
func (m *DefaultScopeMatcher) processScopeMatch(
	match *sitter.QueryMatch,
	query *sitter.Query,
	queryDef QueryDefinition,
	tree *parser.SyntaxTree,
	filePath string,
) *MatchResult {
	var scopeName string
	var position parser.Point

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)

		switch captureName {
		case "scope_name":
			scopeNode := capture.Node
			scopeName = string(scopeNode.Content(tree.Source))
			position = parser.Point{Row: int(scopeNode.StartPoint().Row), Column: int(scopeNode.StartPoint().Column)}
		case "method_name":
			if scopeName == "" {
				methodNode := capture.Node
				methodName := string(methodNode.Content(tree.Source))
				// Extract scope name from method name (remove "scope" prefix)
				if strings.HasPrefix(strings.ToLower(methodName), "scope") {
					scopeName = strings.TrimPrefix(methodName, "scope")
					scopeName = strings.ToLower(scopeName[:1]) + scopeName[1:]
				}
				position = parser.Point{Row: int(methodNode.StartPoint().Row), Column: int(methodNode.StartPoint().Column)}
			}
		}
	}

	// Skip if we don't have essential information
	if scopeName == "" {
		return nil
	}

	// Create scope match
	scopeMatch := &ScopeMatch{
		Name:     scopeName,
		On:       "Model",
		Args:     []interface{}{},
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  queryDef.Name,
		Method:   "scope" + strings.Title(scopeName),
		Context:  "model_method",
	}

	return &MatchResult{
		Type:       PatternTypeScope,
		Position:   position,
		Content:    m.buildDisplayContent(scopeName),
		Confidence: queryDef.Confidence,
		Data:       scopeMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: m.isExplicitScopeUsage(queryDef.Name),
		},
	}
}

// buildDisplayContent creates a human-readable string representation of the scope.
func (m *DefaultScopeMatcher) buildDisplayContent(scopeName string) string {
	return fmt.Sprintf("scope%s()", strings.Title(scopeName))
}

// isExplicitScopeUsage determines if scope usage is explicit.
func (m *DefaultScopeMatcher) isExplicitScopeUsage(patternName string) bool {
	switch patternName {
	case "model_scope_method", "query_scope_call":
		return true
	default:
		return false
	}
}

// filterByConfidence removes matches below the minimum confidence threshold.
func (m *DefaultScopeMatcher) filterByConfidence(results []*MatchResult) []*MatchResult {
	if m.config == nil || m.config.MinConfidenceThreshold <= 0 {
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

// deduplicateResults removes duplicate matches by position and scope name.
func (m *DefaultScopeMatcher) deduplicateResults(results []*MatchResult) []*MatchResult {
	if m.config == nil || !m.config.DeduplicateMatches {
		return results
	}

	seen := make(map[string]*MatchResult)

	for _, result := range results {
		if scopeMatch, ok := result.Data.(*ScopeMatch); ok {
			// Create unique key based on position and scope details
			key := fmt.Sprintf("%s:%d:%d:%s", scopeMatch.Name, result.Position.Row, result.Position.Column, scopeMatch.Method)

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
	// Simple sort without external dependencies
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, key := range keys {
		deduplicated = append(deduplicated, seen[key])
	}

	return deduplicated
}

// GetQueries returns the tree-sitter queries used by this matcher.
func (m *DefaultScopeMatcher) GetQueries() []*sitter.Query {
	return m.queries
}

// IsInitialized returns true if the matcher is ready for use.
func (m *DefaultScopeMatcher) IsInitialized() bool {
	return m.initialized
}

// Close releases any resources held by the matcher.
func (m *DefaultScopeMatcher) Close() error {
	if m.compiler != nil {
		return m.compiler.Close()
	}
	return nil
}
// Package matchers provides Laravel query scope detection for local and global scopes.
package matchers

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// ScopeRegistry tracks custom scope methods declared in model classes for Laravel compliance.
type ScopeRegistry struct {
	// modelScopes maps model FQCN to set of declared custom scope names
	modelScopes map[string]map[string]struct{}
	// builderMethods contains Laravel Query Builder methods that should be excluded
	builderMethods map[string]struct{}
}

// NewScopeRegistry creates a new scope registry with Laravel-compliant builder exclusions.
func NewScopeRegistry() *ScopeRegistry {
	builderMethods := map[string]struct{}{
		"where": {}, "wherein": {}, "wherenull": {}, "wherebetween": {},
		"wherenotin": {}, "wherenotnull": {}, "wherenotbetween": {},
		"wherehas": {}, "wheredoesnthave": {}, "whereexists": {},
		"wherecolumn": {}, "whereraw": {}, "wheredate": {},
		"wheremonth": {}, "whereday": {}, "whereyear": {}, "wheretime": {},
		"orwhere": {}, "orwherein": {}, "orwherenull": {},
		"orwherebetween": {}, "orwherenotin": {}, "orwherenotnull": {},
		"get": {}, "first": {}, "firstorfail": {}, "firstorcreate": {},
		"firstornew": {}, "find": {}, "findorfail": {}, "findornew": {},
		"findmany": {}, "all": {}, "pluck": {}, "value": {},
		"chunk": {}, "chunkbyid": {}, "cursor": {}, "lazy": {},
		"lazybyid": {}, "sole": {},
		"count": {}, "sum": {}, "avg": {}, "average": {},
		"min": {}, "max": {}, "exists": {}, "doesntexist": {},
		"orderby": {}, "orderbydesc": {}, "orderbyraw": {},
		"latest": {}, "oldest": {}, "inrandomorder": {},
		"groupby": {}, "groupbyraw": {}, "having": {}, "havingraw": {},
		"limit": {}, "take": {}, "skip": {}, "offset": {}, "forpage": {},
		"join": {}, "leftjoin": {}, "rightjoin": {}, "crossjoin": {},
		"joinsub": {}, "leftjoinsub": {}, "rightjoinsub": {},
		"select": {}, "selectraw": {}, "selectsub": {},
		"addselect": {}, "distinct": {},
		"with": {}, "withcount": {}, "withsum": {}, "withavg": {},
		"withmin": {}, "withmax": {}, "withexists": {},
		"load": {}, "loadcount": {}, "loadsum": {},
		"without": {}, "withonly": {},
		"update": {}, "updateorcreate": {}, "increment": {},
		"decrement": {}, "delete": {}, "forcedelete": {},
		"restore": {}, "truncate": {},
		"lockforupdate": {}, "sharedlock": {},
		"union": {}, "unionall": {},
		"tosql": {}, "torawsql": {}, "dd": {}, "dump": {},
		"paginate": {}, "simplepaginate": {}, "cursorpaginate": {},
		"getbindings": {}, "tobase": {}, "explain": {},
		"when": {}, "unless": {}, "tap": {}, "pipe": {},
		"clone": {}, "copy": {},
		"each": {}, "map": {}, "filter": {}, "reject": {},
		"reduce": {}, "every": {}, "some": {}, "contains": {},
		"toarray": {}, "tojson": {}, "toresponse": {},
		"resolve": {}, "only": {}, "except": {},
		"middleware": {}, "withoutmiddleware": {},
		"subdays": {}, "adddays": {}, "subhours": {}, "addhours": {},
		"todatestring": {}, "todatetimestring": {},
	}

	return &ScopeRegistry{
		modelScopes:    make(map[string]map[string]struct{}),
		builderMethods: builderMethods,
	}
}

// RegisterScope adds a custom scope for a model FQCN.
func (sr *ScopeRegistry) RegisterScope(modelFQCN, scopeName string) {
	sr.AddModelScope(modelFQCN, scopeName)
}

// AddModelScope adds a custom scope for a model FQCN (lowercase normalized).
func (sr *ScopeRegistry) AddModelScope(modelFQCN, scopeName string) {
	modelKey := strings.ToLower(modelFQCN)
	scopeKey := strings.ToLower(scopeName)
	
	if sr.modelScopes[modelKey] == nil {
		sr.modelScopes[modelKey] = make(map[string]struct{})
	}
	sr.modelScopes[modelKey][scopeKey] = struct{}{}
}

// IsCustomScope returns true if the method is a registered custom scope for the model.
func (sr *ScopeRegistry) IsCustomScope(modelFQCN, methodName string) bool {
	return sr.HasScope(modelFQCN, methodName)
}

// HasScope returns true if the method is a registered custom scope for the model.
func (sr *ScopeRegistry) HasScope(modelFQCN, scopeName string) bool {
	modelKey := strings.ToLower(modelFQCN)
	scopeKey := strings.ToLower(scopeName)
	
	if scopes, exists := sr.modelScopes[modelKey]; exists {
		_, ok := scopes[scopeKey]
		return ok
	}
	return false
}

// IsBuilderMethod returns true if the method is a Laravel Query Builder method (should be excluded).
func (sr *ScopeRegistry) IsBuilderMethod(methodName string) bool {
	_, ok := sr.builderMethods[strings.ToLower(methodName)]
	return ok
}

// ExistsInAnyModel returns true if the scope exists in any model.
func (sr *ScopeRegistry) ExistsInAnyModel(scopeName string) bool {
	scopeKey := strings.ToLower(scopeName)
	for _, scopes := range sr.modelScopes {
		if _, ok := scopes[scopeKey]; ok {
			return true
		}
	}
	return false
}

// DefaultScopeMatcher implements ScopeMatcher interface.
type DefaultScopeMatcher struct {
	config           *MatcherConfig
	queries          []*sitter.Query
	queryDefs        []QueryDefinition
	compiler         *QueryCompiler
	initialized      bool
	confidenceLevels *ConfidenceLevel
	scopeRegistry    *ScopeRegistry

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
		scopeRegistry:      NewScopeRegistry(),
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

// SetScopeRegistry sets the scope registry for the matcher.
func (m *DefaultScopeMatcher) SetScopeRegistry(registry *ScopeRegistry) {
	if registry != nil {
		m.scopeRegistry = registry
	}
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

			// Process scope matches using comprehensive implementation
			result, err := m.processMatch(match, queryDef, tree, filePath)
			if err != nil {
				continue // Log error but continue processing
			}
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

	// Extract controller context for proper association
	controllerContext := m.extractControllerMethodContext(firstCapture.Node, tree, filePath)
	if controllerContext != "" {
		scopeMatch.Context = controllerContext
	}

	// Extract class context if available
	className, _ := m.extractClassName(tree, filePath)

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

	// Get model FQCN - skip if we can't resolve it properly (Laravel requirement)
	modelFQCN, ok := m.extractClassName(tree, "")
	if !ok || modelFQCN == "" {
		return nil // Skip emitting if FQCN can't be resolved
	}

	// Register this scope in the registry for Laravel compliance
	m.scopeRegistry.RegisterScope(modelFQCN, scopeName)

	// Extract parameters if available
	var args []any
	if bodyCapture, ok := captures["body"]; ok {
		args = m.extractScopeArguments(bodyCapture, tree)
	}


	return &ScopeMatch{
		Name:     scopeName,
		On:       modelFQCN,
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
    var args []any

	if scopeMethod, ok := captures["scope_method"]; ok {
		methodName = scopeMethod.Content(tree.Source)
	}

	if modelCapture, ok := captures["model_class"]; ok {
		modelClass = modelCapture.Content(tree.Source)
	}

	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	// Laravel compliance: Exclude builder methods
	if m.scopeRegistry.IsBuilderMethod(methodName) {
		return nil
	}

	scopeName := m.extractScopeName(methodName)
	if scopeName == "" {
		return nil
	}

    // Strict model resolution: only emit scopes when we can definitively resolve to a model FQCN
    var resolvedModelFQCN string

    // Build namespace and alias map from file AST
    ns, aliases := m.buildAliasMap(tree)

    // First, try the captured model class
    if modelClass != "" {
        // Clean and resolve model class (remove quotes, ::class suffix, etc.)
        cleanModel := m.cleanModelReference(modelClass)
        resolved := m.resolveFQCN(cleanModel, ns, aliases)
        if m.isValidModelFQCN(resolved) {
            resolvedModelFQCN = resolved
        }
    }
	
	// If no valid model resolved, skip this scope emission
	// Never emit scopes with controller names or unresolved models
	if resolvedModelFQCN == "" {
		return nil
	}
	
	// Only accept if it's a registered custom scope for this specific model
	if !m.scopeRegistry.HasScope(resolvedModelFQCN, scopeName) {
		return nil
	}


	return &ScopeMatch{
		Name:     scopeName,
		On:       resolvedModelFQCN, // Use resolved model FQCN, never controller names
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
    var args []any

	if scopeCapture, ok := captures["scope_name"]; ok {
		scopeName = scopeCapture.Content(tree.Source)
	}

	if m.scopeRegistry.IsBuilderMethod(scopeName) {
		return nil
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

    // Strict model resolution: only emit scopes when we can definitively resolve to a model FQCN
    var resolvedModelFQCN string

    // Build namespace and alias map from file AST
    ns, aliases := m.buildAliasMap(tree)

    // First, try the captured model class
    if modelClass != "" {
        // Clean and resolve model class
        cleanModel := m.cleanModelReference(modelClass)
        resolved := m.resolveFQCN(cleanModel, ns, aliases)
        if m.isValidModelFQCN(resolved) {
            resolvedModelFQCN = resolved
        }
    }
	
	// If no valid model resolved, skip this scope emission
	if resolvedModelFQCN == "" {
		return nil
	}
	
	// Only accept if it's a registered custom scope for this specific model
	if !m.scopeRegistry.HasScope(resolvedModelFQCN, scopeName) {
		return nil
	}


	return &ScopeMatch{
		Name:     scopeName,
		On:       resolvedModelFQCN, // Use resolved model FQCN, never controller names
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
		Args:     []any{},
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

	var args []any
	if bodyCapture, ok := captures["body"]; ok {
		args = m.extractScopeArguments(bodyCapture, tree)
	}

	className, ok := m.extractClassName(tree, "")
	if !ok {
		return nil
	}
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

	var args []any
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

	onClass, _ := m.extractClassName(tree, "")

	return &ScopeMatch{
		Name:     scopeName,
		On:       onClass,
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
	var args []any

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

	onClass, _ := m.extractClassName(tree, "")

	return &ScopeMatch{
		Name:     scopeName,
		On:       onClass,
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

	var args []any
	if argsCapture, ok := captures["args"]; ok {
		args = m.extractScopeArguments(argsCapture, tree)
	}

	onClass, _ := m.extractClassName(tree, "")

	return &ScopeMatch{
		Name:     scopeName,
		On:       onClass,
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
func (m *DefaultScopeMatcher) extractScopeArguments(node *sitter.Node, tree *parser.SyntaxTree) []any {
	var args []any

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
func (m *DefaultScopeMatcher) extractClassName(tree *parser.SyntaxTree, filePath string) (string, bool) {
	if tree == nil || tree.Root == nil {
		return "", false
	}

	// Try to find class declaration using simple tree traversal
	className := m.findClassNameInTree(tree)
	if className != "" {
		return className, true
	}

	// If no classes found in tree, try to extract from file path as fallback
	if filePath != "" {
		inferredName := m.inferClassNameFromFilePath(filePath)
		if inferredName != "" && inferredName != "UnknownClass" {
			return inferredName, true
		}
	}

	return "", false
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
		return ""
	}

	// Extract filename without extension
	fileName := filepath.Base(filePath)
	if dotIndex := strings.LastIndex(fileName, "."); dotIndex != -1 {
		fileName = fileName[:dotIndex]
	}

	// Convert to PascalCase (common PHP class naming convention)
	if fileName != "" {
		return strings.ToUpper(fileName[:1]) + fileName[1:]
	}

	return ""
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
	varName = strings.TrimPrefix(varName, "$")

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
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
		return strings.Join(parts, "")
	}

	// Convert camelCase to PascalCase
	if varName != "" {
		return strings.ToUpper(varName[:1]) + varName[1:]
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

// buildAliasMap extracts file namespace and use aliases via AST
func (m *DefaultScopeMatcher) buildAliasMap(tree *parser.SyntaxTree) (string, map[string]string) {
    ns := ""
    aliases := make(map[string]string)
    root, sitterTree, err := m.convertToSitterNode(tree)
    if err != nil || root == nil {
        return ns, aliases
    }
    defer sitterTree.Close()

    var walk func(n *sitter.Node)
    walk = func(n *sitter.Node) {
        if n == nil {
            return
        }
        switch n.Type() {
        case "namespace_definition":
            // find namespace_name child
            for i := uint32(0); i < n.ChildCount(); i++ {
                ch := n.Child(int(i))
                if ch != nil && ch.Type() == "namespace_name" {
                    ns = string(ch.Content(tree.Source))
                    break
                }
            }
        case "namespace_use_declaration":
            // iterate clauses
            for i := uint32(0); i < n.ChildCount(); i++ {
                cl := n.Child(int(i))
                if cl == nil {
                    continue
                }
                if cl.Type() == "namespace_use_clause" || cl.Type() == "use_declaration" {
                    // get qualified_name and optional alias
                    var qname, alias string
                    for j := uint32(0); j < cl.ChildCount(); j++ {
                        c := cl.Child(int(j))
                        if c == nil {
                            continue
                        }
                        t := c.Type()
                        if t == "qualified_name" || t == "name" {
                            qname = string(c.Content(tree.Source))
                        }
                        if t == "as" {
                            // alias token, next name is alias
                            if j+1 < cl.ChildCount() {
                                al := cl.Child(int(j + 1))
                                if al != nil {
                                    alias = string(al.Content(tree.Source))
                                }
                            }
                        }
                        if t == "alias" { // some grammars
                            alias = string(c.Content(tree.Source))
                        }
                    }
                    if qname != "" {
                        if alias == "" {
                            // default alias = last segment
                            parts := strings.Split(qname, "\\")
                            alias = parts[len(parts)-1]
                        }
                        aliases[alias] = qname
                    }
                }
            }
        }
        for i := uint32(0); i < n.ChildCount(); i++ {
            walk(n.Child(int(i)))
        }
    }
    walk(root)
    return ns, aliases
}

// resolveFQCN resolves a class reference to FQCN using alias map and namespace
func (m *DefaultScopeMatcher) resolveFQCN(ref, ns string, aliases map[string]string) string {
    if ref == "" {
        return ""
    }
    // Already fully qualified
    if strings.Contains(ref, "\\") {
        // if ref starts with leading backslash, trim
        return strings.TrimPrefix(ref, "\\")
    }
    // Resolve via alias
    if fq, ok := aliases[ref]; ok {
        return fq
    }
    // Fallback to file namespace
    if ns != "" {
        return ns + "\\" + ref
    }
    return ref
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

// extractControllerMethodContext walks up the AST to find the controller class and method
// containing this scope pattern match
func (m *DefaultScopeMatcher) extractControllerMethodContext(node *sitter.Node, tree *parser.SyntaxTree, filePath string) string {
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
func (m *DefaultScopeMatcher) getMethodNameFromNode(methodNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getClassNameFromNode extracts class name from class_declaration node  
func (m *DefaultScopeMatcher) getClassNameFromNode(classNode *sitter.Node, source []byte) string {
	for i := uint32(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(int(i))
		if child.Type() == "name" {
			return string(child.Content(source))
		}
	}
	return ""
}

// getNamespaceFromTree extracts namespace from the file by parsing the source
func (m *DefaultScopeMatcher) getNamespaceFromTree(source []byte) string {
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

// cleanModelReference cleans up model class references (removes ::class, quotes, etc.)
func (m *DefaultScopeMatcher) cleanModelReference(modelRef string) string {
	modelRef = strings.TrimSpace(modelRef)
	
	// Remove ::class suffix
	modelRef = strings.TrimSuffix(modelRef, "::class")
	
	// Remove quotes
	if len(modelRef) >= 2 {
		if (modelRef[0] == '"' && modelRef[len(modelRef)-1] == '"') ||
			(modelRef[0] == '\'' && modelRef[len(modelRef)-1] == '\'') {
			modelRef = modelRef[1 : len(modelRef)-1]
		}
	}
	
	return modelRef
}

// isValidModelFQCN checks if the FQCN looks like a valid Eloquent model
func (m *DefaultScopeMatcher) isValidModelFQCN(fqcn string) bool {
	if fqcn == "" {
		return false
	}
	
	// Must contain namespace separators (backslashes)
	if !strings.Contains(fqcn, "\\") {
		return false
	}
	
	// Should be in App\Models namespace (Laravel convention)
	// or contain "Model" in the path, but let's be permissive for now
	if strings.HasPrefix(fqcn, "App\\Models\\") {
		return true
	}
	
	// Allow other model namespaces but exclude obvious non-models
	excludePatterns := []string{
		"App\\Http\\Controllers\\",
		"App\\Http\\Requests\\", 
		"App\\Http\\Responses\\",
		"App\\Services\\",
		"App\\Actions\\",
		"App\\Console\\",
		"App\\Repositories\\",
		"App\\Observers\\",
		"App\\ValueObjects\\",
	}
	
	for _, pattern := range excludePatterns {
		if strings.HasPrefix(fqcn, pattern) {
			return false
		}
	}
	
	return true
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

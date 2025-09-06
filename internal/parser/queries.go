// Package parser provides AST query system for extracting PHP constructs from syntax trees.
// Implements tree-sitter query patterns for comprehensive PHP language construct detection
// with error resilience, performance optimization, and deterministic output ordering.
package parser

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// DefaultQueryEngine implements the QueryEngine interface using tree-sitter queries.
// Provides high-level extraction of PHP language constructs with compiled query caching.
type DefaultQueryEngine struct {
	language *sitter.Language         // PHP language grammar
	queries  map[string]*sitter.Query // Compiled query cache
	mu       sync.RWMutex             // Thread safety for query cache
}

// Tree-sitter query patterns for PHP constructs
const (
	// Namespace declarations
	namespaceQuery = `
		(namespace_definition) @namespace.definition
	`

	// Class declarations with inheritance and modifiers
	classQuery = `
		(class_declaration) @class.definition
	`

	// Method declarations with full signatures
	methodQuery = `
		(method_declaration) @method.definition
	`

	// Trait declarations and usage
	traitQuery = `
		(trait_declaration) @trait.definition
	`

	// Function declarations
	functionQuery = `
		(function_definition) @function.definition
	`

	// Interface declarations
	interfaceQuery = `
		(interface_declaration) @interface.definition
	`

	// Use statements and imports
	useQuery = `
		(namespace_use_declaration) @use.statement
	`

	// Properties and constants
	propertyQuery = `
		(property_declaration) @property.definition
	`
)

// NewQueryEngine creates a new query engine with the PHP language grammar.
func NewQueryEngine(language *sitter.Language) (*DefaultQueryEngine, error) {
	if language == nil {
		return nil, NewInternalError("query_engine", "language grammar is nil", nil)
	}

	engine := &DefaultQueryEngine{
		language: language,
		queries:  make(map[string]*sitter.Query),
	}

	// Pre-compile common queries for better performance
	if err := engine.compileQueries(); err != nil {
		return nil, fmt.Errorf("failed to compile queries: %w", err)
	}

	return engine, nil
}

// compileQueries pre-compiles all query patterns for efficient reuse.
func (q *DefaultQueryEngine) compileQueries() error {
	queries := map[string]string{
		"namespace": namespaceQuery,
		"class":     classQuery,
		"method":    methodQuery,
		"trait":     traitQuery,
		"function":  functionQuery,
		"interface": interfaceQuery,
		"use":       useQuery,
		"property":  propertyQuery,
	}

	for name, pattern := range queries {
		query, err := sitter.NewQuery([]byte(pattern), q.language)
		if err != nil {
			return NewInternalError("query_compiler",
				fmt.Sprintf("failed to compile %s query", name), err)
		}
		q.queries[name] = query
	}

	return nil
}

// getQuery retrieves a compiled query by name with thread safety.
func (q *DefaultQueryEngine) getQuery(name string) (*sitter.Query, error) {
	q.mu.RLock()
	query, exists := q.queries[name]
	q.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("query %s not found", name)
	}
	if query == nil {
		return nil, fmt.Errorf("query %s is nil", name)
	}

	return query, nil
}

// ExtractNamespaces finds all namespace declarations in the syntax tree.
func (q *DefaultQueryEngine) ExtractNamespaces(tree *SyntaxTree) ([]PHPNamespace, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("namespace")
	if err != nil {
		return nil, err
	}

	// Convert our SyntaxNode to tree-sitter node for query execution
	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var namespaces []PHPNamespace

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		namespace, err := q.extractNamespaceFromCaptures(match.Captures, tree.Source)
		if err != nil {
			// Log error but continue processing other namespaces
			continue
		}
		namespaces = append(namespaces, namespace)
	}

	// Sort namespaces deterministically by name
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i].Name < namespaces[j].Name
	})

	return namespaces, nil
}

// ExtractClasses finds all class definitions in the syntax tree.
func (q *DefaultQueryEngine) ExtractClasses(tree *SyntaxTree) ([]PHPClass, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("class")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var classes []PHPClass

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		class, err := q.extractClassFromCaptures(match.Captures, tree.Source)
		if err != nil {
			// Continue processing other classes on error
			continue
		}
		classes = append(classes, class)
	}

	// Extract methods for each class
	for i := range classes {
		methods, err := q.extractClassMethods(&classes[i], tree)
		if err == nil {
			classes[i].Methods = methods
		}
	}

	// Sort classes deterministically by fully qualified name
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].FullyQualifiedName < classes[j].FullyQualifiedName
	})

	return classes, nil
}

// ExtractMethods finds all method definitions within classes.
func (q *DefaultQueryEngine) ExtractMethods(tree *SyntaxTree) ([]PHPMethod, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("method")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var methods []PHPMethod

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		method, err := q.extractMethodFromCaptures(match.Captures, tree.Source)
		if err != nil {
			continue
		}
		methods = append(methods, method)
	}

	// Sort methods deterministically by class name then method name
	sort.Slice(methods, func(i, j int) bool {
		if methods[i].ClassName == methods[j].ClassName {
			return methods[i].Name < methods[j].Name
		}
		return methods[i].ClassName < methods[j].ClassName
	})

	return methods, nil
}

// ExtractTraits finds all trait definitions and usage.
func (q *DefaultQueryEngine) ExtractTraits(tree *SyntaxTree) ([]PHPTrait, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("trait")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var traits []PHPTrait

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		trait, err := q.extractTraitFromCaptures(match.Captures, tree.Source)
		if err != nil {
			continue
		}
		traits = append(traits, trait)
	}

	// Sort traits deterministically by fully qualified name
	sort.Slice(traits, func(i, j int) bool {
		return traits[i].FullyQualifiedName < traits[j].FullyQualifiedName
	})

	return traits, nil
}

// ExtractFunctions finds all function definitions (global and closures).
func (q *DefaultQueryEngine) ExtractFunctions(tree *SyntaxTree) ([]PHPFunction, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("function")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var functions []PHPFunction

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		function, err := q.extractFunctionFromCaptures(match.Captures, tree.Source)
		if err != nil {
			continue
		}
		functions = append(functions, function)
	}

	// Sort functions deterministically by namespace then name
	sort.Slice(functions, func(i, j int) bool {
		if functions[i].Namespace == functions[j].Namespace {
			return functions[i].Name < functions[j].Name
		}
		return functions[i].Namespace < functions[j].Namespace
	})

	return functions, nil
}

// ExtractInterfaces finds all interface definitions.
func (q *DefaultQueryEngine) ExtractInterfaces(tree *SyntaxTree) ([]PHPInterface, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("interface")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var interfaces []PHPInterface

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		iface, err := q.extractInterfaceFromCaptures(match.Captures, tree.Source)
		if err != nil {
			continue
		}
		interfaces = append(interfaces, iface)
	}

	// Sort interfaces deterministically by fully qualified name
	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].FullyQualifiedName < interfaces[j].FullyQualifiedName
	})

	return interfaces, nil
}

// ExtractUseStatements finds all use/import statements in the syntax tree.
func (q *DefaultQueryEngine) ExtractUseStatements(tree *SyntaxTree) ([]PHPUseStatement, error) {
	if tree == nil || tree.Root == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	query, err := q.getQuery("use")
	if err != nil {
		return nil, err
	}

	rootNode, err := q.convertSyntaxNodeToSitterNode(tree)
	if err != nil {
		return nil, err
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, rootNode)
	var useStatements []PHPUseStatement

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		useStmt, err := q.extractUseStatementFromCaptures(match.Captures, tree.Source)
		if err != nil {
			continue
		}
		useStatements = append(useStatements, useStmt)
	}

	// Sort use statements deterministically by fully qualified name
	sort.Slice(useStatements, func(i, j int) bool {
		return useStatements[i].FullyQualifiedName < useStatements[j].FullyQualifiedName
	})

	return useStatements, nil
}

// Helper methods for extracting constructs from query captures

// convertSyntaxNodeToSitterNode converts our SyntaxNode back to tree-sitter node.
// This is a temporary approach until we refactor to work directly with tree-sitter nodes.
func (q *DefaultQueryEngine) convertSyntaxNodeToSitterNode(tree *SyntaxTree) (*sitter.Node, error) {
	// Since we need the original tree-sitter node for queries, we'll need to re-parse
	// This is not optimal but necessary given the current structure
	parser := sitter.NewParser()
	if parser == nil {
		return nil, NewInternalError("parser", "failed to create temporary parser", nil)
	}

	parser.SetLanguage(q.language)

	sitterTree, err := parser.ParseCtx(context.Background(), nil, tree.Source)
	if err != nil {
		return nil, WrapTreeSitterError("temporary parse", err)
	}
	if sitterTree == nil {
		return nil, NewParserError("temporary parsing failed", ErrParsingFailed)
	}

	rootNode := sitterTree.RootNode()
	if rootNode == nil {
		sitterTree.Close()
		return nil, NewParserError("temporary tree has no root", ErrParsingFailed)
	}

	// Note: Caller is responsible for closing the tree
	return rootNode, nil
}

// extractNamespaceFromCaptures extracts namespace information from query captures.
func (q *DefaultQueryEngine) extractNamespaceFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPNamespace, error) {
	var namespace PHPNamespace

	for _, capture := range captures {
		if capture.Node.Type() != "namespace_definition" {
			continue
		}

		// Find namespace name from child nodes
		nameNode := q.findChildByType(capture.Node, "namespace_name")
		if nameNode != nil {
			namespace.Name = strings.TrimSpace(string(nameNode.Content(source)))
		}

		namespace.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if namespace.Name == "" {
		return namespace, fmt.Errorf("namespace name not found in captures")
	}

	return namespace, nil
}

// extractUseStatementFromCaptures extracts use statement information from query captures.
func (q *DefaultQueryEngine) extractUseStatementFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPUseStatement, error) {
	var useStmt PHPUseStatement

	for _, capture := range captures {
		if capture.Node.Type() != "namespace_use_declaration" {
			continue
		}

		// Find use clause from child nodes
		useClause := q.findChildByType(capture.Node, "namespace_use_clause")
		if useClause == nil {
			continue
		}

		// Extract the qualified name (fully qualified name)
		qualifiedName := q.findChildByType(useClause, "qualified_name")
		if qualifiedName == nil {
			continue
		}
		
		fullyQualifiedName := strings.TrimSpace(string(qualifiedName.Content(source)))
		// Remove leading backslash if present
		fullyQualifiedName = strings.TrimPrefix(fullyQualifiedName, "\\")
		
		useStmt.FullyQualifiedName = fullyQualifiedName
		useStmt.Type = "class" // Default type

		// Check for alias
		aliasClause := q.findChildByType(useClause, "namespace_aliasing_clause")
		if aliasClause != nil {
			aliasIdentifier := q.findChildByType(aliasClause, "name")
			if aliasIdentifier != nil {
				useStmt.Alias = strings.TrimSpace(string(aliasIdentifier.Content(source)))
			}
		}

		// If no alias, use the last part of the namespace as the implicit alias
		if useStmt.Alias == "" {
			parts := strings.Split(fullyQualifiedName, "\\")
			if len(parts) > 0 {
				useStmt.Alias = parts[len(parts)-1]
			}
		}

		useStmt.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
		
		break // Only process first valid use statement per capture
	}

	if useStmt.FullyQualifiedName == "" {
		return useStmt, fmt.Errorf("use statement name not found in captures")
	}

	return useStmt, nil
}

// findChildByType finds the first child node with the specified type.
func (q *DefaultQueryEngine) findChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

// findChildrenByType finds all child nodes with the specified type.
func (q *DefaultQueryEngine) findChildrenByType(node *sitter.Node, nodeType string) []*sitter.Node {
	var children []*sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			children = append(children, child)
		}
	}
	return children
}

// getNodeText safely extracts text from a node.
func (q *DefaultQueryEngine) getNodeText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(string(node.Content(source)))
}

// hasKeyword checks if a node contains a specific keyword as a child.
func (q *DefaultQueryEngine) hasKeyword(node *sitter.Node, source []byte, keyword string) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && q.getNodeText(child, source) == keyword {
			return true
		}
	}
	return false
}

// extractClassFromCaptures extracts class information from query captures.
func (q *DefaultQueryEngine) extractClassFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPClass, error) {
	var class PHPClass

	for _, capture := range captures {
		if capture.Node.Type() != "class_declaration" {
			continue
		}

		// Extract class name
		nameNode := q.findChildByType(capture.Node, "name")
		if nameNode != nil {
			class.Name = q.getNodeText(nameNode, source)
		}

		// Check for abstract/final modifiers
		class.IsAbstract = q.hasKeyword(capture.Node, source, "abstract")
		class.IsFinal = q.hasKeyword(capture.Node, source, "final")

		// Extract extends clause
		baseClause := q.findChildByType(capture.Node, "base_clause")
		if baseClause != nil {
			qualifiedName := q.findChildByType(baseClause, "qualified_name")
			if qualifiedName == nil {
				qualifiedName = q.findChildByType(baseClause, "name")
			}
			if qualifiedName != nil {
				class.Extends = q.getNodeText(qualifiedName, source)
			}
		}

		// Extract implements clause
		interfaceClause := q.findChildByType(capture.Node, "class_interface_clause")
		if interfaceClause != nil {
			interfaceNames := q.findChildrenByType(interfaceClause, "qualified_name")
			for _, interfaceName := range interfaceNames {
				class.Implements = append(class.Implements, q.getNodeText(interfaceName, source))
			}
		}

		class.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if class.Name == "" {
		return class, fmt.Errorf("class name not found in captures")
	}

	// Set default visibility
	class.Visibility = "public"

	// Sort implements for deterministic output
	sort.Strings(class.Implements)

	// Build fully qualified name (will be updated with namespace context later)
	class.FullyQualifiedName = class.Name

	return class, nil
}

// extractMethodFromCaptures extracts method information from query captures.
func (q *DefaultQueryEngine) extractMethodFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPMethod, error) {
	var method PHPMethod

	for _, capture := range captures {
		if capture.Node.Type() != "method_declaration" {
			continue
		}

		// Extract method name
		nameNode := q.findChildByType(capture.Node, "name")
		if nameNode != nil {
			method.Name = q.getNodeText(nameNode, source)
		}

		// Extract visibility modifier
		visibilityNode := q.findChildByType(capture.Node, "visibility_modifier")
		if visibilityNode != nil {
			method.Visibility = q.getNodeText(visibilityNode, source)
		} else {
			method.Visibility = "public" // Default visibility
		}

		// Check for modifiers
		method.IsStatic = q.hasKeyword(capture.Node, source, "static")
		method.IsAbstract = q.hasKeyword(capture.Node, source, "abstract")
		method.IsFinal = q.hasKeyword(capture.Node, source, "final")

		// Extract return type - look for : type_name pattern
		for i := 0; i < int(capture.Node.ChildCount()); i++ {
			child := capture.Node.Child(i)
			if child != nil && child.Type() == ":" {
				// The next child should be the return type
				if i+1 < int(capture.Node.ChildCount()) {
					returnTypeChild := capture.Node.Child(i + 1)
					if returnTypeChild != nil {
						// Extract text from return type node, could be name, qualified_name, etc.
						method.ReturnType = q.getNodeText(returnTypeChild, source)
					}
				}
				break
			}
		}

		method.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if method.Name == "" {
		return method, fmt.Errorf("method name not found in captures")
	}

	return method, nil
}

// extractClassMethods finds methods within a specific class.
func (q *DefaultQueryEngine) extractClassMethods(class *PHPClass, tree *SyntaxTree) ([]PHPMethod, error) {
	// This would normally traverse the class body to find methods
	// For now, we'll return an empty slice and let the full method extraction handle it
	return []PHPMethod{}, nil
}

// extractTraitFromCaptures extracts trait information from query captures.
func (q *DefaultQueryEngine) extractTraitFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPTrait, error) {
	var trait PHPTrait

	for _, capture := range captures {
		if capture.Node.Type() != "trait_declaration" {
			continue
		}

		// Extract trait name
		nameNode := q.findChildByType(capture.Node, "name")
		if nameNode != nil {
			trait.Name = q.getNodeText(nameNode, source)
			trait.FullyQualifiedName = trait.Name
		}

		trait.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if trait.Name == "" {
		return trait, fmt.Errorf("trait name not found in captures")
	}

	return trait, nil
}

// extractFunctionFromCaptures extracts function information from query captures.
func (q *DefaultQueryEngine) extractFunctionFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPFunction, error) {
	var function PHPFunction

	for _, capture := range captures {
		if capture.Node.Type() != "function_definition" {
			continue
		}

		// Extract function name
		nameNode := q.findChildByType(capture.Node, "name")
		if nameNode != nil {
			function.Name = q.getNodeText(nameNode, source)
		}

		// Extract return type
		returnTypeNode := q.findChildByType(capture.Node, "return_type")
		if returnTypeNode != nil {
			function.ReturnType = q.getNodeText(returnTypeNode, source)
		}

		function.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if function.Name == "" {
		return function, fmt.Errorf("function name not found in captures")
	}

	return function, nil
}

// extractInterfaceFromCaptures extracts interface information from query captures.
func (q *DefaultQueryEngine) extractInterfaceFromCaptures(captures []sitter.QueryCapture, source []byte) (PHPInterface, error) {
	var iface PHPInterface

	for _, capture := range captures {
		if capture.Node.Type() != "interface_declaration" {
			continue
		}

		// Extract interface name
		nameNode := q.findChildByType(capture.Node, "name")
		if nameNode != nil {
			iface.Name = q.getNodeText(nameNode, source)
			iface.FullyQualifiedName = iface.Name
		}

		// Extract extends clause - find base_clause node
		baseClause := q.findChildByType(capture.Node, "base_clause")
		if baseClause != nil {
			// base_clause contains "extends" keyword followed by names
			for i := 0; i < int(baseClause.ChildCount()); i++ {
				child := baseClause.Child(i)
				if child != nil && (child.Type() == "name" || child.Type() == "qualified_name") {
					iface.Extends = append(iface.Extends, q.getNodeText(child, source))
				}
			}
		}

		iface.Position = SourcePosition{
			StartLine:   int(capture.Node.StartPoint().Row) + 1,
			StartColumn: int(capture.Node.StartPoint().Column) + 1,
			EndLine:     int(capture.Node.EndPoint().Row) + 1,
			EndColumn:   int(capture.Node.EndPoint().Column) + 1,
			StartByte:   int(capture.Node.StartByte()),
			EndByte:     int(capture.Node.EndByte()),
		}
	}

	if iface.Name == "" {
		return iface, fmt.Errorf("interface name not found in captures")
	}

	// Sort extends for deterministic output
	sort.Strings(iface.Extends)

	return iface, nil
}

// Close releases query engine resources.
func (q *DefaultQueryEngine) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Close all compiled queries
	for name, query := range q.queries {
		if query != nil {
			query.Close()
		}
		delete(q.queries, name)
	}

	q.language = nil
	return nil
}

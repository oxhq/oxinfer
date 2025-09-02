// Package matchers provides Laravel Resource detection for API responses.
package matchers

import (
	"context"
	"fmt"
	"strings"

	"github.com/garaekz/oxinfer/internal/parser"
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

	// Extract captures
	for _, capture := range match.Captures {
		captureName := query.CaptureNameForId(capture.Index)
		
		switch captureName {
		case "class":
			classNode := capture.Node
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

    // Clean class name
    className = m.cleanResourceClassName(className)

    // Determine if class looks like a Resource or is an alias to a Resource
    looksResource := m.isResourceClass(className)
    if !looksResource {
        // Try alias resolution based on use statements
        aliasMap := parseUseAliases(string(tree.Source))
        if fqcn, ok := aliasMap[className]; ok && strings.HasSuffix(fqcn, "Resource") {
            looksResource = true
        }
    }
    if !looksResource {
        return nil
    }

	// Determine if this is a collection or single resource
	isCollection := m.determineCollectionType(queryDef.Name, methodName)

    // Create resource match
    resourceMatch := &ResourceMatch{
        Class:      className,
        Collection: isCollection,
        Pattern:    queryDef.Name,
        Method:     methodName,
    }

    // Import resolution disabled for class display; keep short class name

	return &MatchResult{
		Type:       PatternTypeResource,
		Position:   position,
		Content:    fmt.Sprintf("%s%s", className, m.getMethodSuffix(methodName)),
		Confidence: queryDef.Confidence,
		Data:       resourceMatch,
		Context: &MatchContext{
			FilePath: filePath,
			Explicit: m.isExplicitResourceUsage(queryDef.Name),
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
func (m *DefaultResourceMatcher) isResourceClass(className string) bool {
	// Must end with "Resource"
	if !strings.HasSuffix(className, "Resource") {
		return false
	}
	
	// Must be at least "XResource" (minimum length check)
	if len(className) < 9 { // "XResource" = 9 chars
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
        if rm.Method == "collection" {
            if rm.Pattern == "return_resource_collection" {
                return 100
            }
            if rm.Pattern == "resource_collection_static" {
                return 90
            }
        }
        if rm.Method == "make" {
            if rm.Pattern == "resource_make_static" {
                return 100
            }
        }
        switch rm.Pattern {
        case "return_new_resource":
            return 80
        case "new_resource_instantiation":
            return 70
        case "variable_resource_assignment":
            return 60
        case "resource_collection_static", "return_resource_collection":
            return 50
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

    deduplicated := make([]*MatchResult, 0, len(selected))
    for _, r := range selected {
        deduplicated = append(deduplicated, r)
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

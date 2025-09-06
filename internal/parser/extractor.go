// Package parser provides PHP construct extraction functionality.
// Implements comprehensive extraction and normalization of PHP language constructs
// from syntax trees with Laravel pattern detection and structured output.
package parser

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultPHPConstructExtractor implements PHPConstructExtractor interface.
// Combines QueryEngine results into structured representations for pattern analysis.
type DefaultPHPConstructExtractor struct {
	queryEngine QueryEngine   // Query engine for construct extraction
	config      *ParserConfig // Configuration for extraction behavior
}

// NewPHPConstructExtractor creates a new PHP construct extractor.
func NewPHPConstructExtractor(queryEngine QueryEngine, config *ParserConfig) *DefaultPHPConstructExtractor {
	if config == nil {
		config = DefaultParserConfig()
	}

	return &DefaultPHPConstructExtractor{
		queryEngine: queryEngine,
		config:      config,
	}
}

// ExtractAllConstructs performs comprehensive extraction of PHP constructs.
// Returns all classes, methods, functions, traits, and interfaces found.
func (e *DefaultPHPConstructExtractor) ExtractAllConstructs(tree *SyntaxTree) (*PHPFileStructure, error) {
	if tree == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	startTime := time.Now()
	structure := &PHPFileStructure{
		ParsedAt: startTime,
	}

	// Extract all construct types concurrently for better performance
	type extractResult struct {
		name string
		data any
		err  error
	}

	results := make(chan extractResult, 6)
	var wg sync.WaitGroup

	// Extract namespaces
	wg.Add(1)
	go func() {
		defer wg.Done()
		namespaces, err := e.queryEngine.ExtractNamespaces(tree)
		if err == nil && len(namespaces) > 0 {
			// Use first namespace as primary (PHP allows only one namespace per file)
			structure.Namespace = &namespaces[0]
		}
		results <- extractResult{"namespaces", namespaces, err}
	}()

	// Extract classes
	wg.Add(1)
	go func() {
		defer wg.Done()
		classes, err := e.queryEngine.ExtractClasses(tree)
		results <- extractResult{"classes", classes, err}
	}()

	// Extract interfaces
	wg.Add(1)
	go func() {
		defer wg.Done()
		interfaces, err := e.queryEngine.ExtractInterfaces(tree)
		results <- extractResult{"interfaces", interfaces, err}
	}()

	// Extract traits
	wg.Add(1)
	go func() {
		defer wg.Done()
		traits, err := e.queryEngine.ExtractTraits(tree)
		results <- extractResult{"traits", traits, err}
	}()

	// Extract functions
	wg.Add(1)
	go func() {
		defer wg.Done()
		functions, err := e.queryEngine.ExtractFunctions(tree)
		results <- extractResult{"functions", functions, err}
	}()

	// Extract use statements
	wg.Add(1)
	go func() {
		defer wg.Done()
		useStmts, err := e.extractUseStatements(tree)
		results <- extractResult{"use_statements", useStmts, err}
	}()

	// Wait for all extractions to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for result := range results {
		if result.err != nil {
			// Log error but continue processing other constructs
			continue
		}

		switch result.name {
		case "classes":
			if classes, ok := result.data.([]PHPClass); ok {
				structure.Classes = classes
			}
		case "interfaces":
			if interfaces, ok := result.data.([]PHPInterface); ok {
				structure.Interfaces = interfaces
			}
		case "traits":
			if traits, ok := result.data.([]PHPTrait); ok {
				structure.Traits = traits
			}
		case "functions":
			if functions, ok := result.data.([]PHPFunction); ok {
				structure.Functions = functions
			}
		case "use_statements":
			if useStmts, ok := result.data.([]PHPUseStatement); ok {
				structure.UseStatements = useStmts
			}
		}
	}

	// Post-process constructs with namespace context
	e.resolveFullyQualifiedNames(structure)

	structure.ParseDuration = time.Since(startTime)
	return structure, nil
}

// ExtractLaravelPatterns identifies Laravel-specific patterns in PHP code.
// Looks for controllers, models, requests, resources, and other Laravel constructs.
func (e *DefaultPHPConstructExtractor) ExtractLaravelPatterns(tree *SyntaxTree) (*LaravelPatterns, error) {
	if tree == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	if !e.config.EnableLaravelPatterns {
		return &LaravelPatterns{}, nil
	}

	// Get all constructs first
	structure, err := e.ExtractAllConstructs(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to extract constructs for Laravel analysis: %w", err)
	}

	patterns := &LaravelPatterns{}

	// Analyze classes for Laravel patterns
	for _, class := range structure.Classes {
		// Check for Laravel controllers
		if e.isLaravelController(class) {
			controller := e.extractLaravelController(class)
			patterns.Controllers = append(patterns.Controllers, controller)
		}

		// Check for Eloquent models
		if e.isLaravelModel(class) {
			model := e.extractLaravelModel(class)
			patterns.Models = append(patterns.Models, model)
		}

		// Check for Form Requests
		if e.isLaravelRequest(class) {
			request := e.extractLaravelRequest(class)
			patterns.Requests = append(patterns.Requests, request)
		}

		// Check for API Resources
		if e.isLaravelResource(class) {
			resource := e.extractLaravelResource(class)
			patterns.Resources = append(patterns.Resources, resource)
		}

		// Check for Middleware
		if e.isLaravelMiddleware(class) {
			middleware := e.extractLaravelMiddleware(class)
			patterns.Middlewares = append(patterns.Middlewares, middleware)
		}
	}

	// Sort all patterns for deterministic output
	e.sortLaravelPatterns(patterns)

	return patterns, nil
}

// ExtractDocBlocks parses PHPDoc comments and annotations.
// Returns structured documentation with type hints and parameter descriptions.
func (e *DefaultPHPConstructExtractor) ExtractDocBlocks(tree *SyntaxTree) ([]PHPDocBlock, error) {
	if tree == nil {
		return nil, NewParserError("syntax tree is nil", ErrInvalidPHPContent)
	}

	if !e.config.EnableDocBlocks {
		return []PHPDocBlock{}, nil
	}

	// For now, return empty slice - full docblock parsing would require more complex queries
	return []PHPDocBlock{}, nil
}

// ExtractUseStatements finds all use/import statements.
// Returns qualified class names and their aliases.
func (e *DefaultPHPConstructExtractor) ExtractUseStatements(tree *SyntaxTree) ([]PHPUseStatement, error) {
	return e.extractUseStatements(tree)
}

// extractUseStatements internal implementation for use statement extraction.
func (e *DefaultPHPConstructExtractor) extractUseStatements(tree *SyntaxTree) ([]PHPUseStatement, error) {
	// Use the query engine to extract use statements
	return e.queryEngine.ExtractUseStatements(tree)
}

// resolveFullyQualifiedNames updates construct names with namespace context.
func (e *DefaultPHPConstructExtractor) resolveFullyQualifiedNames(structure *PHPFileStructure) {
	namespace := ""
	if structure.Namespace != nil {
		namespace = structure.Namespace.Name
	}

	// Update classes
	for i := range structure.Classes {
		if namespace != "" && !strings.Contains(structure.Classes[i].FullyQualifiedName, "\\") {
			structure.Classes[i].FullyQualifiedName = namespace + "\\" + structure.Classes[i].Name
			structure.Classes[i].Namespace = namespace
		}
	}

	// Update interfaces
	for i := range structure.Interfaces {
		if namespace != "" && !strings.Contains(structure.Interfaces[i].FullyQualifiedName, "\\") {
			structure.Interfaces[i].FullyQualifiedName = namespace + "\\" + structure.Interfaces[i].Name
			structure.Interfaces[i].Namespace = namespace
		}
	}

	// Update traits
	for i := range structure.Traits {
		if namespace != "" && !strings.Contains(structure.Traits[i].FullyQualifiedName, "\\") {
			structure.Traits[i].FullyQualifiedName = namespace + "\\" + structure.Traits[i].Name
			structure.Traits[i].Namespace = namespace
		}
	}

	// Update functions
	for i := range structure.Functions {
		if namespace != "" && structure.Functions[i].Namespace == "" {
			structure.Functions[i].Namespace = namespace
		}
	}
}

// Laravel pattern detection helpers

// isLaravelController checks if a class is a Laravel controller.
func (e *DefaultPHPConstructExtractor) isLaravelController(class PHPClass) bool {
	// Check if class extends Controller or has Controller in name/namespace
	return strings.HasSuffix(class.Name, "Controller") ||
		strings.Contains(class.Extends, "Controller") ||
		strings.Contains(class.FullyQualifiedName, "\\Controllers\\")
}

// isLaravelModel checks if a class is an Eloquent model.
func (e *DefaultPHPConstructExtractor) isLaravelModel(class PHPClass) bool {
	// Check if class extends Model or is in Models namespace
	return strings.Contains(class.Extends, "Model") ||
		strings.Contains(class.FullyQualifiedName, "\\Models\\") ||
		strings.Contains(class.FullyQualifiedName, "App\\")
}

// isLaravelRequest checks if a class is a Form Request.
func (e *DefaultPHPConstructExtractor) isLaravelRequest(class PHPClass) bool {
	return strings.HasSuffix(class.Name, "Request") ||
		strings.Contains(class.Extends, "FormRequest") ||
		strings.Contains(class.FullyQualifiedName, "\\Requests\\")
}

// isLaravelResource checks if a class is an API Resource.
func (e *DefaultPHPConstructExtractor) isLaravelResource(class PHPClass) bool {
	return strings.HasSuffix(class.Name, "Resource") ||
		strings.Contains(class.Extends, "Resource") ||
		strings.Contains(class.FullyQualifiedName, "\\Resources\\")
}

// isLaravelMiddleware checks if a class is middleware.
func (e *DefaultPHPConstructExtractor) isLaravelMiddleware(class PHPClass) bool {
	return strings.HasSuffix(class.Name, "Middleware") ||
		strings.Contains(class.FullyQualifiedName, "\\Middleware\\") ||
		e.hasHandleMethod(class)
}

// hasHandleMethod checks if a class has a handle method (middleware pattern).
func (e *DefaultPHPConstructExtractor) hasHandleMethod(class PHPClass) bool {
	for _, method := range class.Methods {
		if method.Name == "handle" {
			return true
		}
	}
	return false
}

// Laravel construct extractors

// extractLaravelController extracts controller-specific information.
func (e *DefaultPHPConstructExtractor) extractLaravelController(class PHPClass) LaravelController {
	controller := LaravelController{
		Class: class,
	}

	// Extract public methods as actions
	for _, method := range class.Methods {
		if method.Visibility == "public" && method.Name != "__construct" {
			controller.Actions = append(controller.Actions, method)
		}
	}

	return controller
}

// extractLaravelModel extracts model-specific information.
func (e *DefaultPHPConstructExtractor) extractLaravelModel(class PHPClass) LaravelModel {
	model := LaravelModel{
		Class: class,
	}

	// Extract table name, fillable, hidden, etc. would require more complex analysis
	// For now, just return the basic structure
	return model
}

// extractLaravelRequest extracts request-specific information.
func (e *DefaultPHPConstructExtractor) extractLaravelRequest(class PHPClass) LaravelRequest {
	request := LaravelRequest{
		Class: class,
		Rules: make(map[string]string),
	}

	// Find authorize method
	for _, method := range class.Methods {
		if method.Name == "authorize" {
			request.AuthorizationMethod = &method
			break
		}
	}

	return request
}

// extractLaravelResource extracts resource-specific information.
func (e *DefaultPHPConstructExtractor) extractLaravelResource(class PHPClass) LaravelResource {
	resource := LaravelResource{
		Class: class,
	}

	// Find toArray method
	for _, method := range class.Methods {
		if method.Name == "toArray" {
			resource.ToArrayMethod = &method
			break
		}
	}

	return resource
}

// extractLaravelMiddleware extracts middleware-specific information.
func (e *DefaultPHPConstructExtractor) extractLaravelMiddleware(class PHPClass) LaravelMiddleware {
	middleware := LaravelMiddleware{
		Class: class,
	}

	// Find handle method
	for _, method := range class.Methods {
		if method.Name == "handle" {
			middleware.HandleMethod = &method
			break
		}
	}

	return middleware
}

// sortLaravelPatterns sorts all Laravel patterns for deterministic output.
func (e *DefaultPHPConstructExtractor) sortLaravelPatterns(patterns *LaravelPatterns) {
	// Sort controllers by class name
	sort.Slice(patterns.Controllers, func(i, j int) bool {
		return patterns.Controllers[i].Class.FullyQualifiedName < patterns.Controllers[j].Class.FullyQualifiedName
	})

	// Sort models by class name
	sort.Slice(patterns.Models, func(i, j int) bool {
		return patterns.Models[i].Class.FullyQualifiedName < patterns.Models[j].Class.FullyQualifiedName
	})

	// Sort requests by class name
	sort.Slice(patterns.Requests, func(i, j int) bool {
		return patterns.Requests[i].Class.FullyQualifiedName < patterns.Requests[j].Class.FullyQualifiedName
	})

	// Sort resources by class name
	sort.Slice(patterns.Resources, func(i, j int) bool {
		return patterns.Resources[i].Class.FullyQualifiedName < patterns.Resources[j].Class.FullyQualifiedName
	})

	// Sort middlewares by class name
	sort.Slice(patterns.Middlewares, func(i, j int) bool {
		return patterns.Middlewares[i].Class.FullyQualifiedName < patterns.Middlewares[j].Class.FullyQualifiedName
	})

	// Sort routes by URI then method
	sort.Slice(patterns.Routes, func(i, j int) bool {
		if patterns.Routes[i].URI == patterns.Routes[j].URI {
			return patterns.Routes[i].Method < patterns.Routes[j].Method
		}
		return patterns.Routes[i].URI < patterns.Routes[j].URI
	})
}

// Close releases extractor resources.
func (e *DefaultPHPConstructExtractor) Close() error {
	// No resources to clean up in the extractor itself
	// QueryEngine cleanup is handled separately
	return nil
}

// Package parser provides comprehensive tests for the AST query system.
// Tests tree-sitter query patterns, PHP construct extraction, Laravel pattern detection,
// error handling, and performance characteristics.
package parser

import (
	"testing"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

// Test data for PHP constructs
const (
	// Simple PHP class for basic testing
	simpleClassPHP = `<?php
namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Http\JsonResponse;

class ApiController extends Controller
{
    public function __construct()
    {
        $this->middleware('auth');
    }
    
    public function index(Request $request): JsonResponse
    {
        return response()->json(['data' => 'test']);
    }
    
    protected static function helper(): array
    {
        return ['status' => 'ok'];
    }
}
`

	// PHP trait for trait extraction testing
	traitPHP = `<?php
namespace App\Traits;

trait HasPermissions 
{
    public function can(string $permission): bool
    {
        return true;
    }
    
    protected function hasRole(string $role): bool
    {
        return false;
    }
}
`

	// PHP interface for interface extraction testing
	interfacePHP = `<?php
namespace App\Contracts;

interface PaymentGateway extends Gateway
{
    public function charge(float $amount): bool;
    public function refund(string $transactionId): bool;
}
`

	// Complex PHP file with multiple constructs
	complexPHP = `<?php
namespace App\Services;

use App\Models\User;
use App\Events\UserCreated;

interface UserServiceInterface
{
    public function create(array $data): User;
}

abstract class BaseService
{
    protected $model;
    
    abstract public function validate(array $data): bool;
}

class UserService extends BaseService implements UserServiceInterface
{
    use HasLogging;
    
    private $eventDispatcher;
    
    public function __construct(EventDispatcher $dispatcher)
    {
        $this->eventDispatcher = $dispatcher;
    }
    
    public function create(array $data): User
    {
        if (!$this->validate($data)) {
            throw new ValidationException('Invalid data');
        }
        
        $user = User::create($data);
        $this->eventDispatcher->dispatch(new UserCreated($user));
        
        return $user;
    }
    
    public function validate(array $data): bool
    {
        return !empty($data['email']);
    }
}

function globalFunction(string $param): void
{
    echo $param;
}
`

	// Malformed PHP for error handling tests
	malformedQueriesPHP = `<?php
class BrokenClass {
    public function missingBrace()
    // Missing opening brace
        return true;
    }
    
    private function incomplete(
    // Missing parameters closing
    {
        return false;
    }
}
`
)

// TestNewQueryEngine tests query engine creation and initialization.
func TestNewQueryEngine(t *testing.T) {
	tests := []struct {
		name        string
		language    *sitter.Language
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid PHP language",
			language:    php.GetLanguage(),
			expectError: false,
		},
		{
			name:        "nil language",
			language:    nil,
			expectError: true,
			errorMsg:    "language grammar is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewQueryEngine(tt.language)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if engine == nil {
				t.Error("expected non-nil engine")
				return
			}

			// Verify queries were compiled
			if len(engine.queries) == 0 {
				t.Error("expected compiled queries, got none")
			}

			// Test closing
			if err := engine.Close(); err != nil {
				t.Errorf("unexpected error closing engine: %v", err)
			}
		})
	}
}

// TestExtractNamespaces tests namespace extraction functionality.
func TestExtractNamespaces(t *testing.T) {
	engine, tree := setupTestEngine(t, simpleClassPHP)
	defer engine.Close()

	namespaces, err := engine.ExtractNamespaces(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting namespaces: %v", err)
	}

	if len(namespaces) != 1 {
		t.Errorf("expected 1 namespace, got %d", len(namespaces))
	}

	if len(namespaces) > 0 {
		ns := namespaces[0]
		if ns.Name != "App\\Http\\Controllers" {
			t.Errorf("expected namespace 'App\\Http\\Controllers', got %q", ns.Name)
		}

		if ns.Position.StartLine <= 0 {
			t.Error("expected valid start line position")
		}
	}
}

// TestExtractClasses tests class extraction functionality.
func TestExtractClasses(t *testing.T) {
	engine, tree := setupTestEngine(t, simpleClassPHP)
	defer engine.Close()

	classes, err := engine.ExtractClasses(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting classes: %v", err)
	}

	if len(classes) != 1 {
		t.Errorf("expected 1 class, got %d", len(classes))
	}

	if len(classes) > 0 {
		class := classes[0]
		if class.Name != "ApiController" {
			t.Errorf("expected class name 'ApiController', got %q", class.Name)
		}

		if class.Extends != "Controller" {
			t.Errorf("expected extends 'Controller', got %q", class.Extends)
		}

		if class.Visibility != "public" {
			t.Errorf("expected visibility 'public', got %q", class.Visibility)
		}

		if class.Position.StartLine <= 0 {
			t.Error("expected valid start line position")
		}
	}
}

// TestExtractMethods tests method extraction functionality.
func TestExtractMethods(t *testing.T) {
	engine, tree := setupTestEngine(t, simpleClassPHP)
	defer engine.Close()

	methods, err := engine.ExtractMethods(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting methods: %v", err)
	}

	if len(methods) < 2 {
		t.Errorf("expected at least 2 methods, got %d", len(methods))
	}

	// Find specific methods
	var constructorFound, indexFound, helperFound bool
	for _, method := range methods {
		switch method.Name {
		case "__construct":
			constructorFound = true
			if method.Visibility != "public" {
				t.Errorf("expected constructor visibility 'public', got %q", method.Visibility)
			}
		case "index":
			indexFound = true
			if method.Visibility != "public" {
				t.Errorf("expected index visibility 'public', got %q", method.Visibility)
			}
			if method.ReturnType != "JsonResponse" {
				t.Errorf("expected index return type 'JsonResponse', got %q", method.ReturnType)
			}
		case "helper":
			helperFound = true
			if method.Visibility != "protected" {
				t.Errorf("expected helper visibility 'protected', got %q", method.Visibility)
			}
			if !method.IsStatic {
				t.Error("expected helper to be static")
			}
		}
	}

	if !constructorFound {
		t.Error("expected to find __construct method")
	}
	if !indexFound {
		t.Error("expected to find index method")
	}
	if !helperFound {
		t.Error("expected to find helper method")
	}
}

// TestExtractTraits tests trait extraction functionality.
func TestExtractTraits(t *testing.T) {
	engine, tree := setupTestEngine(t, traitPHP)
	defer engine.Close()

	traits, err := engine.ExtractTraits(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting traits: %v", err)
	}

	if len(traits) != 1 {
		t.Errorf("expected 1 trait, got %d", len(traits))
	}

	if len(traits) > 0 {
		trait := traits[0]
		if trait.Name != "HasPermissions" {
			t.Errorf("expected trait name 'HasPermissions', got %q", trait.Name)
		}

		if trait.Position.StartLine <= 0 {
			t.Error("expected valid start line position")
		}
	}
}

// TestExtractFunctions tests function extraction functionality.
func TestExtractFunctions(t *testing.T) {
	engine, tree := setupTestEngine(t, complexPHP)
	defer engine.Close()

	functions, err := engine.ExtractFunctions(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting functions: %v", err)
	}

	if len(functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(functions))
	}

	if len(functions) > 0 {
		function := functions[0]
		if function.Name != "globalFunction" {
			t.Errorf("expected function name 'globalFunction', got %q", function.Name)
		}

		if function.Position.StartLine <= 0 {
			t.Error("expected valid start line position")
		}
	}
}

// TestExtractInterfaces tests interface extraction functionality.
func TestExtractInterfaces(t *testing.T) {
	engine, tree := setupTestEngine(t, interfacePHP)
	defer engine.Close()

	interfaces, err := engine.ExtractInterfaces(tree)
	if err != nil {
		t.Fatalf("unexpected error extracting interfaces: %v", err)
	}

	if len(interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(interfaces))
	}

	if len(interfaces) > 0 {
		iface := interfaces[0]
		if iface.Name != "PaymentGateway" {
			t.Errorf("expected interface name 'PaymentGateway', got %q", iface.Name)
		}

		if len(iface.Extends) != 1 || iface.Extends[0] != "Gateway" {
			t.Errorf("expected extends ['Gateway'], got %v", iface.Extends)
		}

		if iface.Position.StartLine <= 0 {
			t.Error("expected valid start line position")
		}
	}
}

// TestComplexFileExtraction tests extraction from a file with multiple construct types.
func TestComplexFileExtraction(t *testing.T) {
	engine, tree := setupTestEngine(t, complexPHP)
	defer engine.Close()

	// Test namespace extraction
	namespaces, err := engine.ExtractNamespaces(tree)
	if err != nil {
		t.Errorf("unexpected error extracting namespaces: %v", err)
	}
	if len(namespaces) != 1 || namespaces[0].Name != "App\\Services" {
		t.Errorf("expected namespace 'App\\Services', got %v", namespaces)
	}

	// Test class extraction
	classes, err := engine.ExtractClasses(tree)
	if err != nil {
		t.Errorf("unexpected error extracting classes: %v", err)
	}
	if len(classes) < 2 {
		t.Errorf("expected at least 2 classes, got %d", len(classes))
	}

	// Verify abstract and concrete classes
	var baseServiceFound, userServiceFound bool
	for _, class := range classes {
		switch class.Name {
		case "BaseService":
			baseServiceFound = true
			if !class.IsAbstract {
				t.Error("expected BaseService to be abstract")
			}
		case "UserService":
			userServiceFound = true
			if class.Extends != "BaseService" {
				t.Errorf("expected UserService extends BaseService, got %q", class.Extends)
			}
		}
	}

	if !baseServiceFound {
		t.Error("expected to find BaseService class")
	}
	if !userServiceFound {
		t.Error("expected to find UserService class")
	}

	// Test interface extraction
	interfaces, err := engine.ExtractInterfaces(tree)
	if err != nil {
		t.Errorf("unexpected error extracting interfaces: %v", err)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "UserServiceInterface" {
		t.Errorf("expected interface 'UserServiceInterface', got %v", interfaces)
	}

	// Test function extraction
	functions, err := engine.ExtractFunctions(tree)
	if err != nil {
		t.Errorf("unexpected error extracting functions: %v", err)
	}
	if len(functions) != 1 || functions[0].Name != "globalFunction" {
		t.Errorf("expected function 'globalFunction', got %v", functions)
	}
}

// TestQueryErrorHandling tests query engine behavior with malformed PHP code.
func TestQueryErrorHandling(t *testing.T) {
	engine, tree := setupTestEngine(t, malformedQueriesPHP)
	defer engine.Close()

	// Should not panic and should return partial results
	classes, err := engine.ExtractClasses(tree)
	if err != nil {
		t.Errorf("unexpected error with malformed PHP: %v", err)
	}

	// May find some constructs despite syntax errors
	if len(classes) > 1 {
		t.Logf("found %d classes in malformed PHP", len(classes))
	}

	// Test with nil tree
	_, err = engine.ExtractClasses(nil)
	if err == nil {
		t.Error("expected error with nil tree")
	}
}

// TestDeterministicOutput tests that extraction results are deterministic.
func TestDeterministicOutput(t *testing.T) {
	engine, tree := setupTestEngine(t, complexPHP)
	defer engine.Close()

	// Run extraction multiple times
	runs := 5
	var allClassResults [][]PHPClass
	var allMethodResults [][]PHPMethod

	for i := 0; i < runs; i++ {
		classes, err := engine.ExtractClasses(tree)
		if err != nil {
			t.Fatalf("unexpected error in run %d: %v", i, err)
		}
		allClassResults = append(allClassResults, classes)

		methods, err := engine.ExtractMethods(tree)
		if err != nil {
			t.Fatalf("unexpected error in run %d: %v", i, err)
		}
		allMethodResults = append(allMethodResults, methods)
	}

	// Verify all runs produce identical results
	for i := 1; i < runs; i++ {
		if !equalClassSlices(allClassResults[0], allClassResults[i]) {
			t.Errorf("class results differ between run 0 and run %d", i)
		}
		if !equalMethodSlices(allMethodResults[0], allMethodResults[i]) {
			t.Errorf("method results differ between run 0 and run %d", i)
		}
	}
}

// TestPerformance tests query performance with reasonable time limits.
func TestPerformance(t *testing.T) {
	engine, tree := setupTestEngine(t, complexPHP)
	defer engine.Close()

	// Test individual extraction performance
	tests := []struct {
		name    string
		fn      func() error
		maxTime time.Duration
	}{
		{
			name: "namespace extraction",
			fn: func() error {
				_, err := engine.ExtractNamespaces(tree)
				return err
			},
			maxTime: 10 * time.Millisecond,
		},
		{
			name: "class extraction",
			fn: func() error {
				_, err := engine.ExtractClasses(tree)
				return err
			},
			maxTime: 20 * time.Millisecond,
		},
		{
			name: "method extraction",
			fn: func() error {
				_, err := engine.ExtractMethods(tree)
				return err
			},
			maxTime: 20 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			if err := tt.fn(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			elapsed := time.Since(start)

			if elapsed > tt.maxTime {
				t.Errorf("extraction took %v, expected less than %v", elapsed, tt.maxTime)
			}
		})
	}
}

// TestQueryConcurrency tests thread-safe access to the query engine.
func TestQueryConcurrency(t *testing.T) {
	engine, tree := setupTestEngine(t, complexPHP)
	defer engine.Close()

	// Run concurrent extractions
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err1 := engine.ExtractClasses(tree)
			if err1 != nil {
				results <- err1
				return
			}

			_, err2 := engine.ExtractMethods(tree)
			if err2 != nil {
				results <- err2
				return
			}

			results <- nil
		}()
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent extraction failed: %v", err)
		}
	}
}

// Helper functions for testing

// setupTestEngine creates a query engine and parses test PHP content.
func setupTestEngine(t *testing.T, phpContent string) (*DefaultQueryEngine, *SyntaxTree) {
	t.Helper()

	language := php.GetLanguage()
	if language == nil {
		t.Fatal("failed to get PHP language")
	}

	engine, err := NewQueryEngine(language)
	if err != nil {
		t.Fatalf("failed to create query engine: %v", err)
	}

	parser, err := NewPHPParser(nil)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	tree, err := parser.ParseContent([]byte(phpContent))
	if err != nil {
		t.Fatalf("failed to parse PHP content: %v", err)
	}

	return engine, tree
}

// containsString checks if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(len(substr) == 0 ||
			(len(s) > 0 &&
				(s == substr ||
					containsString(s[1:], substr) ||
					(len(s) >= len(substr) && s[:len(substr)] == substr))))
}

// equalClassSlices compares two class slices for equality.
func equalClassSlices(a, b []PHPClass) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].FullyQualifiedName != b[i].FullyQualifiedName ||
			a[i].Extends != b[i].Extends ||
			a[i].Visibility != b[i].Visibility ||
			a[i].IsAbstract != b[i].IsAbstract ||
			a[i].IsFinal != b[i].IsFinal {
			return false
		}
	}

	return true
}

// equalMethodSlices compares two method slices for equality.
func equalMethodSlices(a, b []PHPMethod) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].ClassName != b[i].ClassName ||
			a[i].Visibility != b[i].Visibility ||
			a[i].IsStatic != b[i].IsStatic ||
			a[i].IsAbstract != b[i].IsAbstract ||
			a[i].IsFinal != b[i].IsFinal ||
			a[i].ReturnType != b[i].ReturnType {
			return false
		}
	}

	return true
}

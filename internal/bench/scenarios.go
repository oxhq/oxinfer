// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// It defines realistic test scenarios to validate MVP performance targets.
package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BenchmarkScenario defines a performance testing scenario with expected constraints.
type BenchmarkScenario struct {
	Name             string        `json:"name"`
	Description      string        `json:"description"`
	FileCount        int           `json:"fileCount"`
	RouteCount       int           `json:"routeCount"`
	ControllerCount  int           `json:"controllerCount"`
	ModelCount       int           `json:"modelCount"`
	MaxDuration      time.Duration `json:"maxDurationMs"`
	MaxMemoryMB      int64         `json:"maxMemoryMB"`
	ExpectedPatterns int           `json:"expectedPatterns"`
	CacheEnabled     bool          `json:"cacheEnabled"`
	ScenarioType     ScenarioType  `json:"type"`
	ProjectStructure ProjectLayout `json:"projectStructure"`
}

// ScenarioType categorizes benchmark scenarios by complexity and use case.
type ScenarioType string

const (
	ScenarioSmall      ScenarioType = "small"      // <100 files, basic Laravel
	ScenarioMedium     ScenarioType = "medium"     // 200-600 files, typical project
	ScenarioLarge      ScenarioType = "large"      // 600-1500 files, enterprise
	ScenarioEnterprise ScenarioType = "enterprise" // >1500 files, complex patterns
	ScenarioCold       ScenarioType = "cold"       // No cache, first run
	ScenarioWarm       ScenarioType = "warm"       // With cache, incremental
)

// ProjectLayout describes the structure of files in a test scenario.
type ProjectLayout struct {
	Controllers      []ControllerInfo `json:"controllers"`
	Models           []ModelInfo      `json:"models"`
	Routes           []RouteInfo      `json:"routes"`
	Migrations       []string         `json:"migrations"`
	Middleware       []string         `json:"middleware"`
	ServiceProviders []string         `json:"serviceProviders"`
	VendorPackages   []string         `json:"vendorPackages"`
}

// ControllerInfo describes a controller for benchmark generation.
type ControllerInfo struct {
	Name       string   `json:"name"`
	Namespace  string   `json:"namespace"`
	Methods    []string `json:"methods"`
	Middleware []string `json:"middleware"`
	Resources  bool     `json:"resources"`
}

// ModelInfo describes a model for benchmark generation.
type ModelInfo struct {
	Name          string   `json:"name"`
	Namespace     string   `json:"namespace"`
	Relationships []string `json:"relationships"`
	Scopes        []string `json:"scopes"`
	Attributes    []string `json:"attributes"`
	Pivots        []string `json:"pivots"`
	Polymorphic   bool     `json:"polymorphic"`
}

// RouteInfo describes a route definition for benchmark generation.
type RouteInfo struct {
	Method     string   `json:"method"`
	URI        string   `json:"uri"`
	Controller string   `json:"controller"`
	Action     string   `json:"action"`
	Middleware []string `json:"middleware"`
	Parameters []string `json:"parameters"`
}

// ScenarioGenerator creates test scenarios for benchmarking.
type ScenarioGenerator interface {
	// GenerateScenario creates test files for a benchmark scenario
	GenerateScenario(ctx context.Context, scenario *BenchmarkScenario, baseDir string) error

	// CleanupScenario removes generated test files
	CleanupScenario(ctx context.Context, scenario *BenchmarkScenario, baseDir string) error

	// ValidateScenario checks if scenario constraints are realistic
	ValidateScenario(scenario *BenchmarkScenario) error
}

// MVPBenchmarkScenarios returns the standard scenarios for validating MVP performance targets.
func MVPBenchmarkScenarios() []*BenchmarkScenario {
	return []*BenchmarkScenario{
		{
			Name:             "Small Project - Cold Run",
			Description:      "Small Laravel project without cache",
			FileCount:        50,
			RouteCount:       25,
			ControllerCount:  8,
			ModelCount:       5,
			MaxDuration:      3 * time.Second,
			MaxMemoryMB:      256,
			ExpectedPatterns: 45,
			CacheEnabled:     false,
			ScenarioType:     ScenarioCold,
			ProjectStructure: generateSmallProjectLayout(),
		},
		{
			Name:             "Medium Project - Cold Run",
			Description:      "Typical Laravel project (200-600 files) without cache",
			FileCount:        400,
			RouteCount:       150,
			ControllerCount:  35,
			ModelCount:       25,
			MaxDuration:      10 * time.Second, // MVP target
			MaxMemoryMB:      512,
			ExpectedPatterns: 280,
			CacheEnabled:     false,
			ScenarioType:     ScenarioCold,
			ProjectStructure: generateMediumProjectLayout(),
		},
		{
			Name:             "Medium Project - Warm Run",
			Description:      "Typical Laravel project with warm cache",
			FileCount:        400,
			RouteCount:       150,
			ControllerCount:  35,
			ModelCount:       25,
			MaxDuration:      2 * time.Second, // MVP target
			MaxMemoryMB:      384,
			ExpectedPatterns: 280,
			CacheEnabled:     true,
			ScenarioType:     ScenarioWarm,
			ProjectStructure: generateMediumProjectLayout(),
		},
		{
			Name:             "Large Project - Cold Run",
			Description:      "Enterprise Laravel project (600-1500 files)",
			FileCount:        800,
			RouteCount:       300,
			ControllerCount:  65,
			ModelCount:       45,
			MaxDuration:      25 * time.Second,
			MaxMemoryMB:      1024,
			ExpectedPatterns: 520,
			CacheEnabled:     false,
			ScenarioType:     ScenarioLarge,
			ProjectStructure: generateLargeProjectLayout(),
		},
		{
			Name:             "Performance Regression Test",
			Description:      "Standardized test for detecting performance regressions",
			FileCount:        300,
			RouteCount:       120,
			ControllerCount:  25,
			ModelCount:       18,
			MaxDuration:      7 * time.Second,
			MaxMemoryMB:      512,
			ExpectedPatterns: 210,
			CacheEnabled:     false,
			ScenarioType:     ScenarioMedium,
			ProjectStructure: generateRegressionTestLayout(),
		},
	}
}

// generateSmallProjectLayout creates a realistic small Laravel project structure.
func generateSmallProjectLayout() ProjectLayout {
	return ProjectLayout{
		Controllers: []ControllerInfo{
			{Name: "HomeController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show"}, Resources: false},
			{Name: "UserController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show", "store", "update", "destroy"}, Resources: true},
			{Name: "PostController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show", "store", "update"}, Resources: true},
			{Name: "AuthController", Namespace: "App\\Http\\Controllers\\Auth", Methods: []string{"login", "logout", "register"}, Resources: false},
		},
		Models: []ModelInfo{
			{Name: "User", Namespace: "App\\Models", Relationships: []string{"hasMany:posts"}, Scopes: []string{"active"}, Attributes: []string{"name", "email"}},
			{Name: "Post", Namespace: "App\\Models", Relationships: []string{"belongsTo:user"}, Scopes: []string{"published"}, Attributes: []string{"title", "content"}},
			{Name: "Category", Namespace: "App\\Models", Relationships: []string{"hasMany:posts"}, Scopes: []string{}, Attributes: []string{"name", "slug"}},
		},
		Routes: []RouteInfo{
			{Method: "GET", URI: "/", Controller: "HomeController", Action: "index"},
			{Method: "GET", URI: "/users", Controller: "UserController", Action: "index"},
			{Method: "GET", URI: "/users/{user}", Controller: "UserController", Action: "show", Parameters: []string{"user"}},
			{Method: "POST", URI: "/users", Controller: "UserController", Action: "store"},
		},
		Migrations:       []string{"create_users_table", "create_posts_table", "create_categories_table"},
		Middleware:       []string{"auth", "throttle", "web"},
		ServiceProviders: []string{"AppServiceProvider", "RouteServiceProvider"},
		VendorPackages:   []string{},
	}
}

// generateMediumProjectLayout creates a realistic medium-sized Laravel project structure.
func generateMediumProjectLayout() ProjectLayout {
	return ProjectLayout{
		Controllers: []ControllerInfo{
			{Name: "HomeController", Namespace: "App\\Http\\Controllers", Methods: []string{"index"}, Resources: false},
			{Name: "UserController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show", "store", "update", "destroy"}, Resources: true},
			{Name: "PostController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show", "store", "update", "destroy"}, Resources: true},
			{Name: "CommentController", Namespace: "App\\Http\\Controllers", Methods: []string{"store", "update", "destroy"}, Resources: false},
			{Name: "CategoryController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show"}, Resources: false},
			{Name: "TagController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "show"}, Resources: false},
			{Name: "AuthController", Namespace: "App\\Http\\Controllers\\Auth", Methods: []string{"login", "logout", "register"}, Resources: false},
			{Name: "AdminController", Namespace: "App\\Http\\Controllers\\Admin", Methods: []string{"dashboard", "users", "posts"}, Resources: false},
			{Name: "APIController", Namespace: "App\\Http\\Controllers\\API", Methods: []string{"index", "show", "store", "update", "destroy"}, Resources: true},
			{Name: "NotificationController", Namespace: "App\\Http\\Controllers", Methods: []string{"index", "mark_read"}, Resources: false},
		},
		Models: []ModelInfo{
			{Name: "User", Namespace: "App\\Models", Relationships: []string{"hasMany:posts", "hasMany:comments"}, Scopes: []string{"active", "verified"}, Attributes: []string{"name", "email", "avatar"}},
			{Name: "Post", Namespace: "App\\Models", Relationships: []string{"belongsTo:user", "hasMany:comments", "belongsToMany:tags"}, Scopes: []string{"published", "featured"}, Attributes: []string{"title", "content", "slug"}},
			{Name: "Comment", Namespace: "App\\Models", Relationships: []string{"belongsTo:user", "belongsTo:post"}, Scopes: []string{"approved"}, Attributes: []string{"content"}},
			{Name: "Category", Namespace: "App\\Models", Relationships: []string{"hasMany:posts"}, Scopes: []string{}, Attributes: []string{"name", "slug", "description"}},
			{Name: "Tag", Namespace: "App\\Models", Relationships: []string{"belongsToMany:posts"}, Scopes: []string{}, Attributes: []string{"name", "slug"}},
			{Name: "Notification", Namespace: "App\\Models", Relationships: []string{"belongsTo:user", "morphTo:notifiable"}, Scopes: []string{"unread"}, Attributes: []string{"type", "data"}, Polymorphic: true},
		},
		Routes: []RouteInfo{
			{Method: "GET", URI: "/", Controller: "HomeController", Action: "index"},
			{Method: "GET", URI: "/users", Controller: "UserController", Action: "index"},
			{Method: "GET", URI: "/users/{user}", Controller: "UserController", Action: "show", Parameters: []string{"user"}},
			{Method: "POST", URI: "/users", Controller: "UserController", Action: "store"},
			{Method: "GET", URI: "/posts", Controller: "PostController", Action: "index"},
			{Method: "GET", URI: "/posts/{post}", Controller: "PostController", Action: "show", Parameters: []string{"post"}},
			{Method: "POST", URI: "/posts", Controller: "PostController", Action: "store"},
			{Method: "GET", URI: "/categories", Controller: "CategoryController", Action: "index"},
			{Method: "GET", URI: "/api/users", Controller: "APIController", Action: "index"},
		},
		Migrations: []string{
			"create_users_table", "create_posts_table", "create_comments_table",
			"create_categories_table", "create_tags_table", "create_post_tag_table",
			"create_notifications_table",
		},
		Middleware: []string{"auth", "throttle", "web", "api", "admin", "verified"},
		ServiceProviders: []string{
			"AppServiceProvider", "RouteServiceProvider", "EventServiceProvider",
			"BroadcastServiceProvider", "AuthServiceProvider",
		},
		VendorPackages: []string{"laravel/sanctum", "spatie/laravel-permission"},
	}
}

// generateLargeProjectLayout creates a realistic large Laravel project structure.
func generateLargeProjectLayout() ProjectLayout {
	layout := generateMediumProjectLayout()

	// Expand controllers
	layout.Controllers = append(layout.Controllers,
		ControllerInfo{Name: "ProductController", Namespace: "App\\Http\\Controllers\\Shop", Methods: []string{"index", "show", "store", "update", "destroy"}, Resources: true},
		ControllerInfo{Name: "OrderController", Namespace: "App\\Http\\Controllers\\Shop", Methods: []string{"index", "show", "store", "update"}, Resources: true},
		ControllerInfo{Name: "PaymentController", Namespace: "App\\Http\\Controllers\\Shop", Methods: []string{"process", "webhook"}, Resources: false},
		ControllerInfo{Name: "InventoryController", Namespace: "App\\Http\\Controllers\\Admin", Methods: []string{"index", "update", "report"}, Resources: false},
		ControllerInfo{Name: "ReportController", Namespace: "App\\Http\\Controllers\\Admin", Methods: []string{"sales", "users", "export"}, Resources: false},
		ControllerInfo{Name: "WebhookController", Namespace: "App\\Http\\Controllers\\API", Methods: []string{"stripe", "paypal", "mailgun"}, Resources: false},
	)

	// Expand models
	layout.Models = append(layout.Models,
		ModelInfo{Name: "Product", Namespace: "App\\Models", Relationships: []string{"hasMany:orderItems", "belongsToMany:categories"}, Scopes: []string{"available", "featured"}, Attributes: []string{"name", "price", "stock"}},
		ModelInfo{Name: "Order", Namespace: "App\\Models", Relationships: []string{"belongsTo:user", "hasMany:orderItems"}, Scopes: []string{"completed", "pending"}, Attributes: []string{"total", "status"}},
		ModelInfo{Name: "OrderItem", Namespace: "App\\Models", Relationships: []string{"belongsTo:order", "belongsTo:product"}, Scopes: []string{}, Attributes: []string{"quantity", "price"}},
		ModelInfo{Name: "Payment", Namespace: "App\\Models", Relationships: []string{"belongsTo:order"}, Scopes: []string{"successful"}, Attributes: []string{"amount", "method", "status"}},
		ModelInfo{Name: "Address", Namespace: "App\\Models", Relationships: []string{"morphTo:addressable"}, Scopes: []string{}, Attributes: []string{"street", "city", "country"}, Polymorphic: true},
	)

	// Add more routes
	layout.Routes = append(layout.Routes,
		RouteInfo{Method: "GET", URI: "/shop/products", Controller: "ProductController", Action: "index"},
		RouteInfo{Method: "GET", URI: "/shop/products/{product}", Controller: "ProductController", Action: "show", Parameters: []string{"product"}},
		RouteInfo{Method: "POST", URI: "/shop/orders", Controller: "OrderController", Action: "store"},
		RouteInfo{Method: "POST", URI: "/payment/process", Controller: "PaymentController", Action: "process"},
		RouteInfo{Method: "POST", URI: "/webhooks/stripe", Controller: "WebhookController", Action: "stripe"},
	)

	return layout
}

// generateRegressionTestLayout creates a standardized layout for regression testing.
func generateRegressionTestLayout() ProjectLayout {
	// Use medium layout but with predictable structure for consistent benchmarking
	layout := generateMediumProjectLayout()

	// Standardize for consistent results
	layout.VendorPackages = []string{"laravel/sanctum"} // Fixed vendor dependencies

	return layout
}

// GetScenarioByName retrieves a benchmark scenario by name.
func GetScenarioByName(name string) (*BenchmarkScenario, error) {
	scenarios := MVPBenchmarkScenarios()

	for _, scenario := range scenarios {
		if scenario.Name == name {
			return scenario, nil
		}
	}

	return nil, fmt.Errorf("benchmark scenario not found: %s", name)
}

// ListScenarioNames returns a list of all available benchmark scenario names.
func ListScenarioNames() []string {
	scenarios := MVPBenchmarkScenarios()
	names := make([]string, len(scenarios))

	for i, scenario := range scenarios {
		names[i] = scenario.Name
	}

	return names
}

// ValidateScenario checks if a scenario has realistic constraints and complete structure.
func ValidateScenario(scenario *BenchmarkScenario) error {
	if scenario == nil {
		return fmt.Errorf("scenario cannot be nil")
	}

	if strings.TrimSpace(scenario.Name) == "" {
		return fmt.Errorf("scenario name cannot be empty")
	}

	if scenario.FileCount <= 0 {
		return fmt.Errorf("file count must be positive, got %d", scenario.FileCount)
	}

	if scenario.MaxDuration <= 0 {
		return fmt.Errorf("max duration must be positive, got %v", scenario.MaxDuration)
	}

	if scenario.MaxMemoryMB <= 0 {
		return fmt.Errorf("max memory must be positive, got %d MB", scenario.MaxMemoryMB)
	}

	// Validate project structure consistency
	layout := scenario.ProjectStructure

	if len(layout.Controllers) == 0 {
		return fmt.Errorf("scenario must have at least one controller")
	}

	if len(layout.Models) == 0 {
		return fmt.Errorf("scenario must have at least one model")
	}

	// Check that controller/model counts roughly match the layout
	if scenario.ControllerCount > 0 && len(layout.Controllers) > scenario.ControllerCount*2 {
		return fmt.Errorf("controller count mismatch: expected ~%d, layout has %d", scenario.ControllerCount, len(layout.Controllers))
	}

	if scenario.ModelCount > 0 && len(layout.Models) > scenario.ModelCount*2 {
		return fmt.Errorf("model count mismatch: expected ~%d, layout has %d", scenario.ModelCount, len(layout.Models))
	}

	return nil
}

// EstimateScenarioComplexity returns a complexity score for a scenario (0.0 to 1.0).
func EstimateScenarioComplexity(scenario *BenchmarkScenario) float64 {
	if scenario == nil {
		return 0.0
	}

	// Weight different complexity factors
	fileComplexity := float64(scenario.FileCount) / 1000.0           // 1000 files = 1.0
	routeComplexity := float64(scenario.RouteCount) / 500.0          // 500 routes = 1.0
	patternComplexity := float64(scenario.ExpectedPatterns) / 1000.0 // 1000 patterns = 1.0

	// Average with slight weight toward file count
	complexity := (fileComplexity*0.4 + routeComplexity*0.3 + patternComplexity*0.3)

	// Cap at 1.0
	if complexity > 1.0 {
		complexity = 1.0
	}

	return complexity
}

// GetScenariosByType returns all scenarios of a given type.
func GetScenariosByType(scenarioType ScenarioType) []*BenchmarkScenario {
	scenarios := MVPBenchmarkScenarios()
	var filtered []*BenchmarkScenario

	for _, scenario := range scenarios {
		if scenario.ScenarioType == scenarioType {
			filtered = append(filtered, scenario)
		}
	}

	return filtered
}

// GenerateTestPath creates a standardized test directory path for a scenario.
func GenerateTestPath(baseDir, scenarioName string) string {
	// Convert scenario name to filesystem-safe name
	safeName := strings.ReplaceAll(scenarioName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "-", "_")
	safeName = strings.ToLower(safeName)

	return filepath.Join(baseDir, "bench_scenarios", safeName)
}

// DefaultScenarioGenerator implements ScenarioGenerator interface.
type DefaultScenarioGenerator struct{}

// GenerateScenario creates test files for a benchmark scenario.
func (g *DefaultScenarioGenerator) GenerateScenario(ctx context.Context, scenario *BenchmarkScenario, baseDir string) error {
	// Create Laravel project structure
	dirs := []string{
		"app/Http/Controllers",
		"app/Models",
		"routes",
		"database/migrations",
		"app/Http/Middleware",
		"app/Providers",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(baseDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", fullPath, err)
		}
	}

	// Generate controllers
	for _, controller := range scenario.ProjectStructure.Controllers {
		if err := g.generateControllerFile(baseDir, controller); err != nil {
			return fmt.Errorf("failed to generate controller %s: %w", controller.Name, err)
		}
	}

	// Generate models
	for _, model := range scenario.ProjectStructure.Models {
		if err := g.generateModelFile(baseDir, model); err != nil {
			return fmt.Errorf("failed to generate model %s: %w", model.Name, err)
		}
	}

	// Generate routes
	if len(scenario.ProjectStructure.Routes) > 0 {
		if err := g.generateRouteFile(baseDir, scenario.ProjectStructure.Routes); err != nil {
			return fmt.Errorf("failed to generate route file: %w", err)
		}
	}

	// Generate composer.json
	if err := g.generateComposerFile(baseDir); err != nil {
		return fmt.Errorf("failed to generate composer.json: %w", err)
	}

	return nil
}

// CleanupScenario removes generated test files.
func (g *DefaultScenarioGenerator) CleanupScenario(ctx context.Context, scenario *BenchmarkScenario, baseDir string) error {
	return os.RemoveAll(baseDir)
}

// ValidateScenario checks if scenario constraints are realistic.
func (g *DefaultScenarioGenerator) ValidateScenario(scenario *BenchmarkScenario) error {
	if scenario.FileCount < 1 {
		return fmt.Errorf("file count must be positive")
	}
	if scenario.MaxDuration <= 0 {
		return fmt.Errorf("max duration must be positive")
	}
	if scenario.MaxMemoryMB <= 0 {
		return fmt.Errorf("max memory must be positive")
	}
	return nil
}

// Helper methods for file generation

func (g *DefaultScenarioGenerator) generateControllerFile(baseDir string, controller ControllerInfo) error {
	controllerContent := fmt.Sprintf(`<?php

namespace %s;

use Illuminate\Http\Request;
use Illuminate\Http\Response;

class %s extends Controller
{
`, controller.Namespace, controller.Name)

	for _, method := range controller.Methods {
		controllerContent += fmt.Sprintf(`
    public function %s(Request $request): Response
    {
        return response()->json(['message' => '%s method'], 200);
    }
`, method, method)
	}

	controllerContent += "}\n"

	fileName := filepath.Join(baseDir, "app/Http/Controllers", controller.Name+".php")
	return os.WriteFile(fileName, []byte(controllerContent), 0644)
}

func (g *DefaultScenarioGenerator) generateModelFile(baseDir string, model ModelInfo) error {
	modelContent := fmt.Sprintf(`<?php

namespace %s;

use Illuminate\Database\Eloquent\Model;

class %s extends Model
{
    protected $fillable = [
`, model.Namespace, model.Name)

	for i, attr := range model.Attributes {
		comma := ","
		if i == len(model.Attributes)-1 {
			comma = ""
		}
		modelContent += fmt.Sprintf("        '%s'%s\n", attr, comma)
	}

	modelContent += "    ];\n"

	// Add relationships
	for _, rel := range model.Relationships {
		parts := strings.Split(rel, ":")
		if len(parts) == 2 {
			relType := parts[0]
			relModel := parts[1]
			modelContent += fmt.Sprintf(`
    public function %s()
    {
        return $this->%s(%s::class);
    }
`, strings.ToLower(relModel), relType, strings.ToUpper(relModel[:1])+relModel[1:])
		}
	}

	// Add scopes
	for _, scope := range model.Scopes {
		modelContent += fmt.Sprintf(`
    public function scope%s($query)
    {
        return $query->where('status', '%s');
    }
`, strings.ToUpper(scope[:1])+scope[1:], scope)
	}

	modelContent += "}\n"

	fileName := filepath.Join(baseDir, "app/Models", model.Name+".php")
	return os.WriteFile(fileName, []byte(modelContent), 0644)
}

func (g *DefaultScenarioGenerator) generateRouteFile(baseDir string, routes []RouteInfo) error {
	routeContent := `<?php

use Illuminate\Support\Facades\Route;

`

	for _, route := range routes {
		routeContent += fmt.Sprintf("Route::%s('%s', '%s@%s');\n",
			strings.ToLower(route.Method), route.URI, route.Controller, route.Action)
	}

	fileName := filepath.Join(baseDir, "routes", "web.php")
	if err := os.MkdirAll(filepath.Dir(fileName), 0755); err != nil {
		return fmt.Errorf("failed to create routes directory: %w", err)
	}
	return os.WriteFile(fileName, []byte(routeContent), 0644)
}

func (g *DefaultScenarioGenerator) generateComposerFile(baseDir string) error {
	composerContent := `{
    "name": "oxinfer/benchmark-test",
    "type": "project",
    "require": {
        "php": "^8.1",
        "laravel/framework": "^10.0"
    },
    "autoload": {
        "psr-4": {
            "App\\": "app/",
            "Database\\Factories\\": "database/factories/",
            "Database\\Seeders\\": "database/seeders/"
        }
    },
    "minimum-stability": "stable",
    "prefer-stable": true
}`

	fileName := filepath.Join(baseDir, "composer.json")
	return os.WriteFile(fileName, []byte(composerContent), 0644)
}

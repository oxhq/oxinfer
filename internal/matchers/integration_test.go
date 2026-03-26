// Package matchers provides integration tests for composite matcher and processor integration.
//go:build goexperiment.jsonv2

package matchers

import (
	"context"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/parser"
	"github.com/smacker/go-tree-sitter/php"
)

func TestCompositePatternMatcher_Integration(t *testing.T) {
	// Initialize PHP language for tree-sitter
	language := php.GetLanguage()
	if language == nil {
		t.Fatal("Failed to get PHP language")
	}

	// Create composite matcher with all available matchers
	config := DefaultMatcherConfig()
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create composite matcher: %v", err)
	}

	// Add all matchers
	httpMatcher, err := NewHTTPStatusMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create HTTP status matcher: %v", err)
	}

	requestMatcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create request usage matcher: %v", err)
	}

	resourceMatcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create resource matcher: %v", err)
	}

	// Register matchers
	if err := composite.AddMatcher(httpMatcher); err != nil {
		t.Fatalf("Failed to add HTTP status matcher: %v", err)
	}
	if err := composite.AddMatcher(requestMatcher); err != nil {
		t.Fatalf("Failed to add request usage matcher: %v", err)
	}
	if err := composite.AddMatcher(resourceMatcher); err != nil {
		t.Fatalf("Failed to add resource matcher: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;

class UserController extends Controller
{
    /**
     * Display a listing of users.
     */
    public function index(Request $request)
    {
        $users = User::query();
        
        // Request usage: query filtering
        if ($request->has('search')) {
            $search = $request->input('search');
            $users->where('name', 'like', "%{$search}%");
        }
        
        $users = $users->get();
        
        // Resource usage: collection
        return UserResource::collection($users);
    }

    /**
     * Store a newly created user.
     */
    public function store(Request $request)
    {
        // Request usage: validation and data extraction
        $validated = $request->validate([
            'name' => 'required|string|max:255',
            'email' => 'required|email|unique:users',
            'avatar' => 'nullable|file|mimes:jpg,png|max:2048'
        ]);
        
        $userData = $request->only(['name', 'email']);
        
        // File upload handling
        if ($request->hasFile('avatar')) {
            $avatar = $request->file('avatar');
            $userData['avatar_path'] = $avatar->store('avatars');
        }
        
        $user = User::create($userData);
        
        // HTTP status: explicit 201 Created
        // Resource usage: single resource
        return new UserResource($user);
    }

    /**
     * Display the specified user.
     */
    public function show(Request $request, $id)
    {
        $user = User::findOrFail($id);
        
        // Resource usage: single resource
        return UserResource::make($user);
    }

    /**
     * Update the specified user.
     */
    public function update(Request $request, $id)
    {
        $user = User::findOrFail($id);
        
        // Request usage: JSON data processing
        $jsonData = $request->json();
        $updateData = $request->only(['name', 'email']);
        
        $user->update($updateData);
        
        // HTTP status: explicit 200 OK
        // Resource usage: single resource
        return response()->json(new UserResource($user), 200);
    }

    /**
     * Remove the specified user.
     */
    public function destroy($id)
    {
        $user = User::findOrFail($id);
        $user->delete();
        
        // HTTP status: explicit 204 No Content
        return response(null, 204);
    }

    /**
     * Handle user avatar upload.
     */
    public function uploadAvatar(Request $request, $id)
    {
        $user = User::findOrFail($id);
        
        // Request usage: file upload
        if (!$request->hasFile('avatar')) {
            // HTTP status: abort with 400
            abort(400, 'No avatar file provided');
        }
        
        $avatar = $request->file('avatar');
        $path = $avatar->store('avatars');
        
        $user->update(['avatar_path' => $path]);
        
        // HTTP status: explicit 201 Created
        // Resource usage: single resource
        return response()->json(new UserResource($user), 201);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		t.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	// Run composite matcher
	ctx := context.Background()
	filePath := "UserController.php"
	patterns, err := composite.MatchAll(ctx, syntaxTree, filePath)
	if err != nil {
		t.Fatalf("Composite matcher failed: %v", err)
	}

	// Verify we got results from all matcher types
	if patterns == nil {
		t.Fatal("Expected LaravelPatterns result, got nil")
	}

	// Verify HTTP status patterns were detected
	if len(patterns.HTTPStatus) == 0 {
		t.Error("Expected HTTP status patterns to be detected")
	} else {
		t.Logf("Detected %d HTTP status patterns", len(patterns.HTTPStatus))
		for i, status := range patterns.HTTPStatus {
			t.Logf("HTTP Status %d: %d (explicit: %t, pattern: %s)", i, status.Status, status.Explicit, status.Pattern)
		}
	}

	// Verify request usage patterns were detected
	if len(patterns.RequestUsage) == 0 {
		t.Error("Expected request usage patterns to be detected")
	} else {
		t.Logf("Detected %d request usage patterns", len(patterns.RequestUsage))
		for i, req := range patterns.RequestUsage {
			t.Logf("Request Usage %d: content types %v, methods %v", i, req.ContentTypes, req.Methods)
		}
	}

	// Verify resource patterns were detected
	if len(patterns.Resources) == 0 {
		t.Error("Expected resource patterns to be detected")
	} else {
		t.Logf("Detected %d resource patterns", len(patterns.Resources))
		for i, res := range patterns.Resources {
			t.Logf("Resource %d: %s (collection: %t, pattern: %s)", i, res.Class, res.Collection, res.Pattern)
		}
	}

	// Verify processing metadata
	if patterns.ProcessedAt == 0 {
		t.Error("Expected ProcessedAt timestamp to be set")
	}

	if patterns.ProcessingMs == 0 {
		t.Error("Expected ProcessingMs to be set")
	}

	if patterns.FilePath != filePath {
		t.Errorf("Expected FilePath %s, got %s", filePath, patterns.FilePath)
	}

	t.Logf("Integration test completed successfully:")
	t.Logf("- File: %s", patterns.FilePath)
	t.Logf("- Class: %s", patterns.ClassName)
	t.Logf("- Processing time: %dms", patterns.ProcessingMs)
	t.Logf("- HTTP Status patterns: %d", len(patterns.HTTPStatus))
	t.Logf("- Request Usage patterns: %d", len(patterns.RequestUsage))
	t.Logf("- Resource patterns: %d", len(patterns.Resources))
}

func TestPatternMatchingProcessor_Integration(t *testing.T) {
	language := php.GetLanguage()
	if language == nil {
		t.Fatal("Failed to get PHP language")
	}

	// Create processor with all matchers
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\ProductResource;
use Illuminate\Http\Request;

class ProductController extends Controller
{
    public function store(Request $request)
    {
        // Complex request pattern with multiple content types
        $productData = $request->validate([
            'name' => 'required|string',
            'price' => 'required|numeric',
            'image' => 'nullable|file'
        ]);
        
        $data = $request->only(['name', 'price', 'description']);
        
        if ($request->hasFile('image')) {
            $image = $request->file('image');
            $data['image_path'] = $image->store('products');
        }
        
        // JSON data handling
        $metadata = $request->json('metadata', []);
        $data['metadata'] = $metadata;
        
        $product = Product::create($data);
        
        // Return with explicit status and resource
        return response()->json(new ProductResource($product), 201);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		t.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	// Process file
	ctx := context.Background()
	filePath := "ProductController.php"
	patterns, err := processor.ProcessFile(ctx, syntaxTree, filePath)
	if err != nil {
		t.Fatalf("Processor failed: %v", err)
	}

	// Convert to emitter format
	controller, err := processor.ConvertToEmitterFormat(patterns)
	if err != nil {
		t.Fatalf("Failed to convert to emitter format: %v", err)
	}

	// Verify emitter.Controller structure
	if controller == nil {
		t.Fatal("Expected emitter.Controller, got nil")
	}

	// FQCN and Method inference are placeholders in this stage; ensure non-empty defaults
	if controller.FQCN == "" {
		t.Error("Expected non-empty controller FQCN")
	}
	if controller.Method == "" {
		t.Error("Expected non-empty controller method")
	}

	// Verify HTTP status was converted
	if controller.HTTP == nil {
		t.Error("Expected HTTP status information to be converted")
	} else {
		if controller.HTTP.Status == nil || *controller.HTTP.Status != 201 {
			t.Errorf("Expected HTTP status 201, got %v", controller.HTTP.Status)
		}
		if controller.HTTP.Explicit == nil || !*controller.HTTP.Explicit {
			t.Error("Expected HTTP status to be explicit")
		}
	}

	// Verify request usage was converted
	if controller.Request == nil {
		t.Error("Expected request information to be converted")
	} else {
		expectedContentTypes := []string{"application/json", "multipart/form-data", "application/x-www-form-urlencoded"}
		if !containsAllStrings(controller.Request.ContentTypes, expectedContentTypes) {
			t.Errorf("Expected content types %v, got %v", expectedContentTypes, controller.Request.ContentTypes)
		}

		if len(controller.Request.Body) == 0 {
			t.Error("Expected request body parameters to be detected")
		}

		if len(controller.Request.Files) == 0 {
			t.Error("Expected request file parameters to be detected")
		}
	}

	// Verify resources were converted
	if len(controller.Resources) == 0 {
		t.Error("Expected resource information to be converted")
	} else {
		resource := controller.Resources[0]
		if resource.Class != "ProductResource" {
			t.Errorf("Expected resource class 'ProductResource', got %s", resource.Class)
		}
		if resource.Collection {
			t.Error("Expected single resource, not collection")
		}
	}

	// Get processing stats
	stats := processor.GetStats()
	if stats.FilesProcessed == 0 {
		t.Error("Expected FilesProcessed to be > 0")
	}
	if stats.PatternsDetected == 0 {
		t.Error("Expected PatternsDetected to be > 0")
	}
	if stats.TotalMatches == 0 {
		t.Error("Expected TotalMatches to be > 0")
	}

	t.Logf("Processor integration test completed successfully:")
	t.Logf("- Files processed: %d", stats.FilesProcessed)
	t.Logf("- Patterns detected: %d", stats.PatternsDetected)
	t.Logf("- Total matches: %d", stats.TotalMatches)
	t.Logf("- Average confidence: %.2f", stats.AverageConfidence)
	t.Logf("- Processing time: %dms", stats.ProcessingTimeMs)
}

func TestCompositePatternMatcher_MatcherManagement(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create composite matcher: %v", err)
	}

	// Test adding matchers (language already declared above)
	httpMatcher, err := NewHTTPStatusMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create HTTP status matcher: %v", err)
	}

	if err := composite.AddMatcher(httpMatcher); err != nil {
		t.Fatalf("Failed to add HTTP status matcher: %v", err)
	}

	// Verify matcher was added
	matchers := composite.GetMatchers()
	if len(matchers) != 1 {
		t.Errorf("Expected 1 matcher, got %d", len(matchers))
	}

	if _, exists := matchers[PatternTypeHTTPStatus]; !exists {
		t.Error("Expected HTTP status matcher to be registered")
	}

	// Test removing matcher
	if err := composite.RemoveMatcher(PatternTypeHTTPStatus); err != nil {
		t.Fatalf("Failed to remove HTTP status matcher: %v", err)
	}

	// Verify matcher was removed
	matchers = composite.GetMatchers()
	if len(matchers) != 0 {
		t.Errorf("Expected 0 matchers, got %d", len(matchers))
	}

	// Test duplicate matcher addition
	if err := composite.AddMatcher(httpMatcher); err != nil {
		t.Fatalf("Failed to add HTTP status matcher: %v", err)
	}

	// Adding same type again should replace
	httpMatcher2, err := NewHTTPStatusMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create second HTTP status matcher: %v", err)
	}

	if err := composite.AddMatcher(httpMatcher2); err != nil {
		t.Fatalf("Failed to replace HTTP status matcher: %v", err)
	}

	matchers = composite.GetMatchers()
	if len(matchers) != 1 {
		t.Errorf("Expected 1 matcher after replacement, got %d", len(matchers))
	}
}

func TestPatternMatchingProcessor_ErrorHandling(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with nil syntax tree
	ctx := context.Background()
	_, err = processor.ProcessFile(ctx, nil, "test.php")
	if err == nil {
		t.Error("Expected error when processing nil syntax tree")
	}

	// Test with malformed PHP content
	malformedContent := `<?php this is not valid PHP syntax {{{{`
	tree, err := parsePHPContent(malformedContent, language)
	if err != nil {
		t.Fatalf("Failed to parse malformed PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(malformedContent),
	}

	// Should handle malformed content gracefully
	patterns, err := processor.ProcessFile(ctx, syntaxTree, "malformed.php")
	if err != nil {
		t.Errorf("Processor should handle malformed content gracefully: %v", err)
	}

	if patterns == nil {
		t.Error("Expected patterns result even for malformed content")
	}
}

// Benchmark tests for integration performance
func BenchmarkCompositePatternMatcher_MatchAll(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		b.Fatalf("Failed to create composite matcher: %v", err)
	}

	// Add all matchers
	httpMatcher, _ := NewHTTPStatusMatcher(language, config)
	requestMatcher, _ := NewRequestUsageMatcher(language, config)
	resourceMatcher, _ := NewResourceMatcher(language, config)

	composite.AddMatcher(httpMatcher)
	composite.AddMatcher(requestMatcher)
	composite.AddMatcher(resourceMatcher)

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class BenchmarkController extends Controller {
    public function store(Request $request) {
        $data = $request->validate(['name' => 'required']);
        $file = $request->file('upload');
        $user = User::create($data);
        return response()->json(new UserResource($user), 201);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		b.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_, err := composite.MatchAll(ctx, syntaxTree, "benchmark.php")
		if err != nil {
			b.Fatalf("Composite matcher failed: %v", err)
		}
	}
}

func BenchmarkPatternMatchingProcessor_ProcessFile(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class ProcessorBenchmark extends Controller {
    public function complexMethod(Request $request) {
        $validated = $request->validate(['name' => 'required', 'email' => 'required']);
        $json = $request->json();
        $file = $request->file('avatar');
        $filtered = $request->only(['name', 'email']);
        
        if (!$validated) abort(422, 'Validation failed');
        
        $user = User::create($validated);
        return response()->json(UserResource::make($user), 201);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		b.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		patterns, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
		if err != nil {
			b.Fatalf("Processor failed: %v", err)
		}

		// Also benchmark conversion to emitter format
		_, err = processor.ConvertToEmitterFormat(patterns)
		if err != nil {
			b.Fatalf("Failed to convert to emitter format: %v", err)
		}
	}
}

func TestIntegration_RealWorldController(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Real-world-like controller with complex patterns
	phpContent := `<?php

namespace App\Http\Controllers\API\V1;

use App\Http\Controllers\Controller;
use App\Http\Resources\UserResource;
use App\Http\Resources\UserCollection;
use App\Http\Requests\StoreUserRequest;
use App\Http\Requests\UpdateUserRequest;
use App\Models\User;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\Support\Facades\Storage;

class UserController extends Controller
{
    /**
     * Display a paginated listing of users.
     *
     * @param  \Illuminate\Http\Request  $request
     * @return \Illuminate\Http\Response
     */
    public function index(Request $request)
    {
        $query = User::query();

        // Search functionality
        if ($request->has('search')) {
            $search = $request->input('search');
            $query->where(function ($q) use ($search) {
                $q->where('name', 'like', "%{$search}%")
                  ->orWhere('email', 'like', "%{$search}%");
            });
        }

        // Filtering
        if ($request->has('role')) {
            $query->whereHas('roles', function ($q) use ($request) {
                $q->where('name', $request->input('role'));
            });
        }

        // Sorting
        $sortBy = $request->input('sort_by', 'created_at');
        $sortOrder = $request->input('sort_order', 'desc');
        $query->orderBy($sortBy, $sortOrder);

        $users = $query->paginate($request->input('per_page', 15));

        return new UserCollection($users);
    }

    /**
     * Store a newly created user in storage.
     *
     * @param  \App\Http\Requests\StoreUserRequest  $request
     * @return \Illuminate\Http\Response
     */
    public function store(StoreUserRequest $request)
    {
        // Get validated data
        $validated = $request->validated();
        
        // Handle avatar upload
        if ($request->hasFile('avatar')) {
            $avatar = $request->file('avatar');
            $path = $avatar->store('avatars', 'public');
            $validated['avatar_path'] = $path;
        }

        // Handle JSON metadata
        if ($request->has('metadata')) {
            $metadata = $request->json('metadata');
            $validated['metadata'] = $metadata;
        }

        $user = User::create($validated);

        return response()->json(new UserResource($user), 201);
    }

    /**
     * Display the specified user.
     *
     * @param  \App\Models\User  $user
     * @return \Illuminate\Http\Response
     */
    public function show(User $user)
    {
        return UserResource::make($user);
    }

    /**
     * Update the specified user in storage.
     *
     * @param  \App\Http\Requests\UpdateUserRequest  $request
     * @param  \App\Models\User  $user
     * @return \Illuminate\Http\Response
     */
    public function update(UpdateUserRequest $request, User $user)
    {
        $validated = $request->validated();

        // Handle avatar update
        if ($request->hasFile('avatar')) {
            // Delete old avatar
            if ($user->avatar_path) {
                Storage::disk('public')->delete($user->avatar_path);
            }

            $avatar = $request->file('avatar');
            $path = $avatar->store('avatars', 'public');
            $validated['avatar_path'] = $path;
        }

        // Update user
        $user->update($validated);
        $user->refresh();

        return response()->json(new UserResource($user));
    }

    /**
     * Remove the specified user from storage.
     *
     * @param  \App\Models\User  $user
     * @return \Illuminate\Http\Response
     */
    public function destroy(User $user)
    {
        // Delete avatar file if exists
        if ($user->avatar_path) {
            Storage::disk('public')->delete($user->avatar_path);
        }

        $user->delete();

        return response(null, 204);
    }

    /**
     * Bulk operations endpoint.
     *
     * @param  \Illuminate\Http\Request  $request
     * @return \Illuminate\Http\Response
     */
    public function bulk(Request $request)
    {
        $action = $request->input('action');
        $userIds = $request->input('user_ids', []);

        if (empty($userIds)) {
            abort(400, 'No user IDs provided');
        }

        $users = User::whereIn('id', $userIds)->get();

        if ($users->count() !== count($userIds)) {
            abort(404, 'Some users not found');
        }

        switch ($action) {
            case 'delete':
                foreach ($users as $user) {
                    if ($user->avatar_path) {
                        Storage::disk('public')->delete($user->avatar_path);
                    }
                    $user->delete();
                }
                return response(null, 204);

            case 'activate':
                User::whereIn('id', $userIds)->update(['active' => true]);
                return response()->json(['message' => 'Users activated'], 200);

            case 'deactivate':
                User::whereIn('id', $userIds)->update(['active' => false]);
                return response()->json(['message' => 'Users deactivated'], 200);

            default:
                abort(400, 'Invalid action');
        }
    }

    /**
     * Export users to CSV.
     *
     * @param  \Illuminate\Http\Request  $request
     * @return \Illuminate\Http\Response
     */
    public function export(Request $request)
    {
        $format = $request->input('format', 'csv');
        
        if (!in_array($format, ['csv', 'json', 'excel'])) {
            abort(400, 'Invalid export format');
        }

        // This would normally generate and return a file
        return response()->json([
            'message' => 'Export initiated',
            'format' => $format,
            'estimated_time' => '2-3 minutes'
        ], 202);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		t.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	// Process the complex controller
	ctx := context.Background()
	filePath := "UserController.php"
	start := time.Now()
	patterns, err := processor.ProcessFile(ctx, syntaxTree, filePath)
	processingTime := time.Since(start)

	if err != nil {
		t.Fatalf("Processor failed on real-world controller: %v", err)
	}

	// Verify comprehensive pattern detection
	if patterns == nil {
		t.Fatal("Expected patterns result")
	}

	// Log detailed results
	t.Logf("Real-world controller analysis completed in %v:", processingTime)
	t.Logf("- Class: %s", patterns.ClassName)
	t.Logf("- HTTP Status patterns: %d", len(patterns.HTTPStatus))
	t.Logf("- Request Usage patterns: %d", len(patterns.RequestUsage))
	t.Logf("- Resource patterns: %d", len(patterns.Resources))

	// Verify we detected multiple patterns of each type
	if len(patterns.HTTPStatus) < 3 {
		t.Errorf("Expected at least 3 HTTP status patterns, got %d", len(patterns.HTTPStatus))
	}

	if len(patterns.RequestUsage) < 1 {
		t.Errorf("Expected at least 1 request usage pattern, got %d", len(patterns.RequestUsage))
	}

	if len(patterns.Resources) < 3 {
		t.Errorf("Expected at least 3 resource patterns, got %d", len(patterns.Resources))
	}

	// Test conversion to emitter format for complex controller
	controller, err := processor.ConvertToEmitterFormat(patterns)
	if err != nil {
		t.Fatalf("Failed to convert complex controller to emitter format: %v", err)
	}

	// Verify emitter format handling of complex patterns
	if controller == nil {
		t.Fatal("Expected emitter.Controller result")
	}

	controllerJSON, _ := json.Marshal(controller, json.Deterministic(true))
	t.Logf("Converted controller format:\n%s", string(controllerJSON))

	// Get final stats
	stats := processor.GetStats()
	t.Logf("Final processing statistics:")
	t.Logf("- Files processed: %d", stats.FilesProcessed)
	t.Logf("- Patterns detected: %d", stats.PatternsDetected)
	t.Logf("- Total matches: %d", stats.TotalMatches)
	t.Logf("- Average confidence: %.2f", stats.AverageConfidence)
	t.Logf("- Processing time: %dms", stats.ProcessingTimeMs)
}

func TestIntegration_PolymorphicRelationships(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Model with polymorphic relationships
	phpContent := `<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\MorphTo;
use Illuminate\Database\Eloquent\Relations\MorphOne;
use Illuminate\Database\Eloquent\Relations\MorphMany;

class Comment extends Model
{
    /**
     * Polymorphic relationship to commentable entities.
     */
    public function commentable(): MorphTo
    {
        return $this->morphTo();
    }
    
    /**
     * Polymorphic relationship to user avatar.
     */
    public function avatar(): MorphOne
    {
        return $this->morphOne(Image::class, 'imageable');
    }
    
    /**
     * Polymorphic relationship to tags.
     */
    public function tags(): MorphMany
    {
        return $this->morphMany(Tag::class, 'taggable');
    }
    
    /**
     * Custom morphTo with explicit columns.
     */
    public function owner(): MorphTo
    {
        return $this->morphTo('owner', 'owner_type', 'owner_id');
    }
}

class Image extends Model
{
    /**
     * Get the owning imageable model.
     */
    public function imageable(): MorphTo
    {
        return $this->morphTo();
    }
}

class Tag extends Model
{
    /**
     * Get the owning taggable model.
     */
    public function taggable(): MorphTo
    {
        return $this->morphTo();
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		t.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	// Process the polymorphic models
	ctx := context.Background()
	filePath := "Comment.php"
	patterns, err := processor.ProcessFile(ctx, syntaxTree, filePath)
	if err != nil {
		t.Fatalf("Processor failed on polymorphic model: %v", err)
	}

	// Verify polymorphic patterns were detected
	if patterns == nil {
		t.Fatal("Expected patterns result")
	}

	// Check that polymorphic patterns were detected
	if len(patterns.Polymorphics) == 0 {
		t.Error("Expected polymorphic patterns to be detected")
	} else {
		t.Logf("Detected %d polymorphic patterns", len(patterns.Polymorphics))
		for i, poly := range patterns.Polymorphics {
			t.Logf("Polymorphic %d: %s (%s)", i, poly.Relation, poly.Type)
		}
	}

	// Convert to emitter format
	model, err := processor.ConvertToModelFormat(patterns)
	if err != nil {
		t.Fatalf("Failed to convert to model format: %v", err)
	}

	// Note: Polymorphic relationships are now handled at the top-level delta, not on individual models
	if model != nil {
		t.Logf("Model converted successfully: %s", model.FQCN)
	}

	t.Logf("Polymorphic integration test completed successfully:")
	t.Logf("- File: %s", patterns.FilePath)
	t.Logf("- Polymorphic patterns: %d", len(patterns.Polymorphics))
	t.Logf("- Processing time: %dms", patterns.ProcessingMs)
}

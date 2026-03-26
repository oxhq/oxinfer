//go:build goexperiment.jsonv2

package contracts

import (
	"path/filepath"
	"testing"

	"github.com/oxhq/oxinfer/internal/cli"
	"github.com/oxhq/oxinfer/internal/emitter"
)

func TestLoadAnalysisRequestNormalizesInvokableController(t *testing.T) {
	fixtureRoot := contractFixtureRoot(t)
	requestJSON := []byte(`{
		"contractVersion": "oxcribe.oxinfer.v2",
		"requestId": "req-1",
		"runtimeFingerprint": "fp-1",
		"manifest": {
			"project": {
				"root": "` + fixtureRoot + `",
				"composer": "composer.json"
			},
			"scan": {
				"targets": ["app", "routes"],
				"globs": ["**/*.php"]
			}
		},
		"runtime": {
			"app": {
				"basePath": "` + fixtureRoot + `",
				"laravelVersion": "10.0.0",
				"phpVersion": "8.3.0",
				"appEnv": "testing"
			},
			"packages": [
				{
					"name": "spatie/laravel-data",
					"version": "^4.0"
				}
			],
			"routes": [
				{
					"routeId": "users.invoke",
					"methods": ["GET"],
					"uri": "users/invoke",
					"domain": null,
					"name": "users.invoke",
					"prefix": "api",
					"middleware": ["api"],
					"where": {},
					"defaults": {},
					"bindings": [],
					"action": {
						"kind": "invokable_controller",
						"fqcn": "App\\Http\\Controllers\\InvokeUserController"
					}
				}
			]
		}
	}`)

	request, err := LoadAnalysisRequest(requestJSON)
	if err != nil {
		t.Fatalf("LoadAnalysisRequest() error = %v", err)
	}

	action := request.Runtime.Routes[0].Action
	if action.Method == nil || *action.Method != "__invoke" {
		t.Fatalf("invokable action method = %v, want __invoke", action.Method)
	}
}

func TestLoadAnalysisRequestRejectsSchemaViolationsAsInputErrors(t *testing.T) {
	fixtureRoot := contractFixtureRoot(t)
	requestJSON := []byte(`{
		"contractVersion": "oxcribe.oxinfer.v2",
		"requestId": "req-2",
		"runtimeFingerprint": "fp-2",
		"manifest": {
			"project": {
				"root": "` + fixtureRoot + `",
				"composer": "composer.json"
			},
			"scan": {
				"targets": ["app", "routes"],
				"globs": ["**/*.php"]
			}
		},
		"runtime": {
			"app": {
				"basePath": "` + fixtureRoot + `",
				"laravelVersion": "10.0.0",
				"phpVersion": "8.3.0",
				"appEnv": "testing"
			},
			"routes": []
		},
		"extra": true
	}`)

	_, err := LoadAnalysisRequest(requestJSON)
	if err == nil {
		t.Fatal("LoadAnalysisRequest() error = nil, want validation error")
	}

	cliErr, ok := err.(*cli.CLIError)
	if !ok {
		t.Fatalf("error type = %T, want *cli.CLIError", err)
	}
	if cliErr.ExitCode != cli.ExitInputError {
		t.Fatalf("exit code = %v, want %v", cliErr.ExitCode, cli.ExitInputError)
	}
}

func TestBuildAnalysisResponseFiltersControllersAndBuildsMatches(t *testing.T) {
	indexMethod := "index"
	invokableMethod := "__invoke"
	request := &AnalysisRequest{
		ContractVersion:    ContractVersion,
		RequestID:          "req-3",
		RuntimeFingerprint: "fp-3",
		Runtime: RuntimeSnapshot{
			App: RuntimeApp{
				BasePath:       "/tmp/app",
				LaravelVersion: "12.0.0",
				PHPVersion:     "8.3.0",
				AppEnv:         "testing",
			},
			Routes: []RuntimeRoute{
				{
					RouteID:    "users.index",
					Methods:    []string{"GET"},
					URI:        "users",
					Domain:     nil,
					Name:       stringPtr("users.index"),
					Prefix:     stringPtr("api"),
					Middleware: []string{"api"},
					Where:      map[string]any{},
					Defaults:   map[string]any{},
					Bindings:   []RouteBinding{},
					Action: RouteAction{
						Kind:   ActionKindControllerMethod,
						FQCN:   stringPtr(`App\Http\Controllers\UserController`),
						Method: &indexMethod,
					},
				},
				{
					RouteID:    "users.invoke",
					Methods:    []string{"GET"},
					URI:        "users/invoke",
					Domain:     nil,
					Name:       stringPtr("users.invoke"),
					Prefix:     stringPtr("api"),
					Middleware: []string{"api"},
					Where:      map[string]any{},
					Defaults:   map[string]any{},
					Bindings:   []RouteBinding{},
					Action: RouteAction{
						Kind:   ActionKindInvokableController,
						FQCN:   stringPtr(`App\Http\Controllers\InvokeUserController`),
						Method: &invokableMethod,
					},
				},
				{
					RouteID:    "auth.user",
					Methods:    []string{"GET"},
					URI:        "user",
					Domain:     nil,
					Name:       nil,
					Prefix:     stringPtr("api"),
					Middleware: []string{"api", "auth:sanctum"},
					Where:      map[string]any{},
					Defaults:   map[string]any{},
					Bindings:   []RouteBinding{},
					Action: RouteAction{
						Kind: ActionKindClosure,
					},
				},
				{
					RouteID:    "users.missing",
					Methods:    []string{"GET"},
					URI:        "users/missing",
					Domain:     nil,
					Name:       stringPtr("users.missing"),
					Prefix:     stringPtr("api"),
					Middleware: []string{"api"},
					Where:      map[string]any{},
					Defaults:   map[string]any{},
					Bindings:   []RouteBinding{},
					Action: RouteAction{
						Kind:   ActionKindControllerMethod,
						FQCN:   stringPtr(`App\Http\Controllers\UserController`),
						Method: stringPtr("missing"),
					},
				},
			},
		},
	}

	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 5,
				Skipped:     0,
				DurationMs:  0,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\UserController`,
				Method: "index",
				Responses: []emitter.Response{
					{
						Kind:        "json_object",
						Status:      intPtr(200),
						Explicit:    boolPtr(false),
						ContentType: "application/json",
						BodySchema: &emitter.ResourceSchemaNode{
							Type: "object",
							Properties: map[string]emitter.ResourceSchemaNode{
								"data": {Ref: `App\Http\Resources\UserResource`},
							},
							Required: []string{"data"},
						},
						Source: "direct_return",
						Via:    "return",
					},
					{
						Kind:     "redirect",
						Status:   intPtr(302),
						Explicit: boolPtr(true),
						Headers: map[string]string{
							"Location": "/login",
						},
						Redirect: &emitter.RedirectInfo{
							TargetKind: "url",
							Target:     stringPtr("/login"),
						},
						Source: "redirect()",
						Via:    "redirect()",
					},
				},
			},
			{FQCN: `App\Http\Controllers\InvokeUserController`, Method: "__invoke"},
			{FQCN: `App\Http\Controllers\UserController`, Method: "destroy"},
		},
		Models: []emitter.Model{{FQCN: `App\Models\User`}},
		Resources: []emitter.ResourceDef{
			{
				FQCN:  `App\Http\Resources\UserResource`,
				Class: "UserResource",
				Schema: emitter.ResourceSchemaNode{
					Type: "object",
					Properties: map[string]emitter.ResourceSchemaNode{
						"id": {Type: "integer"},
					},
					Required: []string{"id"},
				},
			},
		},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	response := BuildAnalysisResponse(request, delta, "0.1.0")
	if response.Status != ResponseStatusPartial {
		t.Fatalf("response status = %q, want %q", response.Status, ResponseStatusPartial)
	}

	if len(response.RouteMatches) != 4 {
		t.Fatalf("route match count = %d, want 4", len(response.RouteMatches))
	}
	if response.RouteMatches[0].MatchStatus != MatchStatusMatched {
		t.Fatalf("first match status = %q, want %q", response.RouteMatches[0].MatchStatus, MatchStatusMatched)
	}
	if response.RouteMatches[1].MatchStatus != MatchStatusMatched {
		t.Fatalf("second match status = %q, want %q", response.RouteMatches[1].MatchStatus, MatchStatusMatched)
	}
	if response.RouteMatches[2].MatchStatus != MatchStatusRuntimeOnly {
		t.Fatalf("closure match status = %q, want %q", response.RouteMatches[2].MatchStatus, MatchStatusRuntimeOnly)
	}
	if response.RouteMatches[3].MatchStatus != MatchStatusMissingStatic {
		t.Fatalf("missing match status = %q, want %q", response.RouteMatches[3].MatchStatus, MatchStatusMissingStatic)
	}

	if len(response.Delta.Controllers) != 2 {
		t.Fatalf("filtered controller count = %d, want 2", len(response.Delta.Controllers))
	}
	if response.Delta.Controllers[0].Method != "index" || response.Delta.Controllers[1].Method != "__invoke" {
		t.Fatalf("filtered controllers = %#v, want matched actions only", response.Delta.Controllers)
	}
	if len(response.Delta.Resources) != 1 || response.Delta.Resources[0].FQCN != `App\Http\Resources\UserResource` {
		t.Fatalf("filtered resources = %#v, want UserResource preserved via response schema", response.Delta.Resources)
	}
	if len(response.Delta.Controllers[0].Responses) != 2 {
		t.Fatalf("filtered responses = %#v, want json + redirect responses preserved", response.Delta.Controllers[0].Responses)
	}

	if response.Meta.DiagnosticCounts.Info != 1 || response.Meta.DiagnosticCounts.Warn != 1 || response.Meta.DiagnosticCounts.Error != 0 {
		t.Fatalf("diagnostic counts = %#v, want info=1 warn=1 error=0", response.Meta.DiagnosticCounts)
	}

	if err := ValidateAnalysisResponse(response); err != nil {
		t.Fatalf("ValidateAnalysisResponse() error = %v", err)
	}
}

func contractFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "test", "fixtures", "integration", "minimal-laravel"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	return root
}

func intPtr(v int) *int {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

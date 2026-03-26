//go:build goexperiment.jsonv2

package response

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
)

func TestEnrichDeltaBuildsResourceSchemas(t *testing.T) {
	projectRoot := t.TempDir()

	writeTestFile(t, filepath.Join(projectRoot, "composer.json"), `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Resources/CategoryResource.php"), `<?php

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\JsonResource;

class CategoryResource extends JsonResource
{
    public function toArray(Request $request): array
    {
        return [
            'id' => $this->id,
            'name' => $this->name,
            'slug' => $this->slug,
        ];
    }
}
`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Resources/TagResource.php"), `<?php

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\JsonResource;

class TagResource extends JsonResource
{
    public function toArray(Request $request): array
    {
        return [
            'id' => $this->id,
            'name' => $this->name,
        ];
    }
}
`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Resources/ProductResource.php"), `<?php

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\JsonResource;

class ProductResource extends JsonResource
{
    public function toArray(Request $request): array
    {
        return [
            'id' => $this->id,
            'name' => $this->name,
            'price' => $this->price,
            'is_active' => $this->is_active,
            'created_at' => $this->created_at,
            'category' => new CategoryResource($this->whenLoaded('category')),
            'tags' => TagResource::collection($this->whenLoaded('tags')),
            'reviews_count' => $this->when(
                $this->relationLoaded('reviews'),
                fn () => $this->reviews->count()
            ),
            'average_rating' => $this->when(
                $this->relationLoaded('reviews'),
                fn () => $this->reviews->avg('rating')
            ),
        ];
    }
}
`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Resources/ProductCollection.php"), `<?php

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\ResourceCollection;

class ProductCollection extends ResourceCollection
{
    public function toArray(Request $request): array
    {
        return [
            'data' => ProductResource::collection($this->collection),
            'meta' => [
                'total' => $this->resource->total(),
                'per_page' => $this->resource->perPage(),
                'current_page' => $this->resource->currentPage(),
                'last_page' => $this->resource->lastPage(),
            ],
            'links' => [
                'first' => $this->resource->url(1),
                'last' => $this->resource->url($this->resource->lastPage()),
                'prev' => $this->resource->previousPageUrl(),
                'next' => $this->resource->nextPageUrl(),
            ],
        ];
    }
}
`)

	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 1,
				Skipped:     0,
				DurationMs:  0,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\ProductController",
				Method: "index",
				Resources: []emitter.Resource{
					{Class: "ProductCollection", FQCN: "App\\Http\\Resources\\ProductCollection"},
				},
			},
			{
				FQCN:   "App\\Http\\Controllers\\ProductController",
				Method: "show",
				Resources: []emitter.Resource{
					{Class: "ProductResource", FQCN: "App\\Http\\Resources\\ProductResource"},
				},
			},
		},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	manifestData := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectRoot,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
	}

	if err := EnrichDelta(context.Background(), manifestData, delta); err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	if len(delta.Resources) != 4 {
		t.Fatalf("len(delta.Resources) = %d, want 4", len(delta.Resources))
	}

	resourceIndex := make(map[string]emitter.ResourceDef, len(delta.Resources))
	for _, resource := range delta.Resources {
		resourceIndex[resource.FQCN] = resource
	}

	product := resourceIndex["App\\Http\\Resources\\ProductResource"]
	if product.FQCN == "" {
		t.Fatalf("missing ProductResource definition: %#v", delta.Resources)
	}
	if product.Schema.Type != "object" {
		t.Fatalf("ProductResource schema type = %q, want object", product.Schema.Type)
	}
	assertRequiredKeys(t, product.Schema.Required, "created_at", "id", "is_active", "name", "price")
	if product.Schema.Properties["id"].Type != "integer" {
		t.Fatalf("ProductResource.id type = %q, want integer", product.Schema.Properties["id"].Type)
	}
	if product.Schema.Properties["price"].Type != "number" {
		t.Fatalf("ProductResource.price type = %q, want number", product.Schema.Properties["price"].Type)
	}
	if product.Schema.Properties["is_active"].Type != "boolean" {
		t.Fatalf("ProductResource.is_active type = %q, want boolean", product.Schema.Properties["is_active"].Type)
	}
	if product.Schema.Properties["created_at"].Format != "date-time" {
		t.Fatalf("ProductResource.created_at format = %q, want date-time", product.Schema.Properties["created_at"].Format)
	}
	if product.Schema.Properties["category"].Ref != "App\\Http\\Resources\\CategoryResource" {
		t.Fatalf("ProductResource.category ref = %q, want CategoryResource", product.Schema.Properties["category"].Ref)
	}
	if product.Schema.Properties["category"].Nullable == nil || !*product.Schema.Properties["category"].Nullable {
		t.Fatalf("ProductResource.category nullable = %#v, want true", product.Schema.Properties["category"].Nullable)
	}
	if product.Schema.Properties["tags"].Type != "array" || product.Schema.Properties["tags"].Items == nil || product.Schema.Properties["tags"].Items.Ref != "App\\Http\\Resources\\TagResource" {
		t.Fatalf("ProductResource.tags = %#v, want array of TagResource", product.Schema.Properties["tags"])
	}
	if contains(product.Schema.Required, "tags") {
		t.Fatalf("ProductResource.tags should not be required: %#v", product.Schema.Required)
	}
	if contains(product.Schema.Required, "reviews_count") {
		t.Fatalf("ProductResource.reviews_count should not be required: %#v", product.Schema.Required)
	}

	collection := resourceIndex["App\\Http\\Resources\\ProductCollection"]
	if collection.Schema.Properties["data"].Type != "array" || collection.Schema.Properties["data"].Items == nil || collection.Schema.Properties["data"].Items.Ref != "App\\Http\\Resources\\ProductResource" {
		t.Fatalf("ProductCollection.data = %#v, want array of ProductResource", collection.Schema.Properties["data"])
	}
	if collection.Schema.Properties["meta"].Properties["total"].Type != "integer" {
		t.Fatalf("ProductCollection.meta.total type = %q, want integer", collection.Schema.Properties["meta"].Properties["total"].Type)
	}
	if collection.Schema.Properties["links"].Properties["prev"].Format != "uri" {
		t.Fatalf("ProductCollection.links.prev format = %q, want uri", collection.Schema.Properties["links"].Properties["prev"].Format)
	}
	if collection.Schema.Properties["links"].Properties["prev"].Nullable == nil || !*collection.Schema.Properties["links"].Properties["prev"].Nullable {
		t.Fatalf("ProductCollection.links.prev nullable = %#v, want true", collection.Schema.Properties["links"].Properties["prev"].Nullable)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func assertRequiredKeys(t *testing.T, required []string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if !contains(required, key) {
			t.Fatalf("required keys %#v missing %q", required, key)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestEnrichDeltaBuildsControllerResponses(t *testing.T) {
	projectRoot := t.TempDir()

	writeTestFile(t, filepath.Join(projectRoot, "composer.json"), `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Resources/ReportResource.php"), `<?php

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\JsonResource;

class ReportResource extends JsonResource
{
    public function toArray(Request $request): array
    {
        return [
            'id' => 11,
            'title' => 'Quarterly Report',
        ];
    }
}
`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Controllers/ApiController.php"), `<?php

namespace App\Http\Controllers;

use App\Http\Resources\ReportResource;
use Illuminate\Auth\Access\AuthorizationException;
use Illuminate\Database\Eloquent\ModelNotFoundException;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Response;
use Inertia\Inertia;
use Illuminate\Validation\ValidationException;

class ApiController
{
    public function show(): array
    {
        return [
            'id' => 42,
            'name' => 'Widget',
            'meta' => [
                'is_active' => true,
                'count' => 7,
            ],
        ];
    }

    public function local(): array
    {
        $payload = [
            'id' => 7,
            'name' => 'Local',
        ];

        return $payload;
    }

    public function store(): JsonResponse
    {
        return response()->json([
            'id' => 100,
            'message' => 'created',
        ], 201);
    }

    public function alias(): JsonResponse
    {
        $payload = [
            'id' => 101,
            'message' => 'accepted',
        ];
        $status = 201;

        return response($payload, $status);
    }

    public function queued(): JsonResponse
    {
        $payload = [
            'id' => 102,
            'message' => 'queued',
        ];
        $response = response()->json($payload)->setStatusCode(202);

        return $response;
    }

    public function list(): JsonResponse
    {
        return response([
            [
                'id' => 1,
                'name' => 'A',
            ],
            [
                'id' => 2,
                'name' => 'B',
            ],
        ], 202);
    }

    public function destroy(): Response
    {
        return response()->noContent();
    }

    public function redirectToReport(): Response
    {
        return redirect('/reports', 301, ['X-Trace' => 'redirect']);
    }

    public function routeRedirect(): Response
    {
        return redirect()->route('dashboard', [], 307, ['X-Trace' => 'route']);
    }

    public function downloadCsv(): Response
    {
        return response()->download('/tmp/report.csv', 'report.csv', ['Content-Type' => 'text/csv'])->setStatusCode(206);
    }

    public function archive(): Response
    {
        return response()->streamDownload(function () {
            echo 'archive';
        }, 'archive.zip', ['Content-Type' => 'application/zip']);
    }

    public function preview(): Response
    {
        return response()->file('/tmp/report.pdf', ['Content-Type' => 'application/pdf']);
    }

    public function streamFeed(): Response
    {
        return response()->stream(function () {
            echo 'feed';
        }, 207, ['Content-Type' => 'text/plain']);
    }

    public function streamJsonFeed(): Response
    {
        return response()->streamJson(['items' => [1, 2]], 208, ['X-Stream' => 'json']);
    }

    public function dashboard(): Response
    {
        $props = [
            'filters' => [
                'team' => 'api',
            ],
            'stats' => [
                'count' => 3,
            ],
        ];

        return Inertia::render('Dashboard/Index', $props);
    }

    public function move(): Response
    {
        return Inertia::location('/teams/current');
    }

    public function errors(): Response
    {
        $statusCode = 400;

        if (request()->boolean('missing')) {
            abort($statusCode, 'Missing');
        }

        if (request()->boolean('forbidden')) {
            throw new AuthorizationException('Denied');
        }

        if (request()->boolean('gone')) {
            throw new ModelNotFoundException();
        }

        if (request()->boolean('invalid')) {
            throw ValidationException::withMessages([
                'email' => ['Invalid email address'],
                'name' => ['Required'],
            ]);
        }

        return response()->json(['ok' => true]);
    }

    public function additionalResource(): Response
    {
        return ReportResource::make(['id' => 11])->additional([
            'meta' => [
                'version' => 2,
            ],
            'links' => [
                'self' => 'https://example.test/reports/11',
            ],
        ]);
    }

    public function resourceCollection(): JsonResponse
    {
        return response()->json(ReportResource::collection([
            ['id' => 1, 'title' => 'A'],
        ]));
    }
}
`)

	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 1,
				Skipped:     0,
				DurationMs:  0,
			},
		},
		Controllers: []emitter.Controller{
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "show"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "local"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "store"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "alias"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "queued"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "list"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "destroy"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "redirectToReport"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "routeRedirect"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "downloadCsv"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "archive"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "preview"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "streamFeed"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "streamJsonFeed"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "dashboard"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "move"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "errors"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "additionalResource"},
			{FQCN: "App\\Http\\Controllers\\ApiController", Method: "resourceCollection"},
		},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	manifestData := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectRoot,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
	}

	if err := EnrichDelta(context.Background(), manifestData, delta); err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	if len(delta.Controllers) != 19 {
		t.Fatalf("len(delta.Controllers) = %d, want 19", len(delta.Controllers))
	}

	controllerIndex := make(map[string]emitter.Controller, len(delta.Controllers))
	for _, controller := range delta.Controllers {
		controllerIndex[controller.Method] = controller
	}

	show := controllerIndex["show"]
	if len(show.Responses) != 1 {
		t.Fatalf("show responses = %d, want 1", len(show.Responses))
	}
	if show.Responses[0].Kind != "json_object" {
		t.Fatalf("show response kind = %q, want json_object", show.Responses[0].Kind)
	}
	if show.Responses[0].Status == nil || *show.Responses[0].Status != 200 {
		t.Fatalf("show response status = %#v, want 200", show.Responses[0].Status)
	}
	if show.Responses[0].Explicit == nil || *show.Responses[0].Explicit {
		t.Fatalf("show response explicit = %#v, want false", show.Responses[0].Explicit)
	}
	if show.Responses[0].ContentType != "application/json" {
		t.Fatalf("show response contentType = %q, want application/json", show.Responses[0].ContentType)
	}
	if show.Responses[0].BodySchema == nil || show.Responses[0].BodySchema.Type != "object" {
		t.Fatalf("show response bodySchema = %#v, want object", show.Responses[0].BodySchema)
	}
	if show.Responses[0].BodySchema.Properties["meta"].Properties["count"].Type != "integer" {
		t.Fatalf("show response meta.count type = %q, want integer", show.Responses[0].BodySchema.Properties["meta"].Properties["count"].Type)
	}

	local := controllerIndex["local"]
	if len(local.Responses) != 1 {
		t.Fatalf("local responses = %d, want 1", len(local.Responses))
	}
	if local.Responses[0].Kind != "json_object" {
		t.Fatalf("local response kind = %q, want json_object", local.Responses[0].Kind)
	}
	if local.Responses[0].BodySchema == nil || local.Responses[0].BodySchema.Properties["id"].Type != "integer" {
		t.Fatalf("local response bodySchema = %#v, want object with id integer", local.Responses[0].BodySchema)
	}

	store := controllerIndex["store"]
	if len(store.Responses) != 1 {
		t.Fatalf("store responses = %d, want 1", len(store.Responses))
	}
	if store.Responses[0].Kind != "json_object" {
		t.Fatalf("store response kind = %q, want json_object", store.Responses[0].Kind)
	}
	if store.Responses[0].Status == nil || *store.Responses[0].Status != 201 {
		t.Fatalf("store response status = %#v, want 201", store.Responses[0].Status)
	}
	if store.Responses[0].Explicit == nil || !*store.Responses[0].Explicit {
		t.Fatalf("store response explicit = %#v, want true", store.Responses[0].Explicit)
	}
	if store.Responses[0].BodySchema == nil || store.Responses[0].BodySchema.Properties["message"].Type != "string" {
		t.Fatalf("store response bodySchema = %#v, want object with message string", store.Responses[0].BodySchema)
	}

	alias := controllerIndex["alias"]
	if len(alias.Responses) != 1 {
		t.Fatalf("alias responses = %d, want 1", len(alias.Responses))
	}
	if alias.Responses[0].Status == nil || *alias.Responses[0].Status != 201 {
		t.Fatalf("alias response status = %#v, want 201", alias.Responses[0].Status)
	}
	if alias.Responses[0].Explicit == nil || !*alias.Responses[0].Explicit {
		t.Fatalf("alias response explicit = %#v, want true", alias.Responses[0].Explicit)
	}
	if alias.Responses[0].BodySchema == nil || alias.Responses[0].BodySchema.Properties["message"].Type != "string" {
		t.Fatalf("alias response bodySchema = %#v, want object with message string", alias.Responses[0].BodySchema)
	}

	queued := controllerIndex["queued"]
	if len(queued.Responses) != 1 {
		t.Fatalf("queued responses = %d, want 1", len(queued.Responses))
	}
	if queued.Responses[0].Status == nil || *queued.Responses[0].Status != 202 {
		t.Fatalf("queued response status = %#v, want 202", queued.Responses[0].Status)
	}
	if queued.Responses[0].Explicit == nil || !*queued.Responses[0].Explicit {
		t.Fatalf("queued response explicit = %#v, want true", queued.Responses[0].Explicit)
	}
	if queued.Responses[0].BodySchema == nil || queued.Responses[0].BodySchema.Properties["id"].Type != "integer" {
		t.Fatalf("queued response bodySchema = %#v, want object with id integer", queued.Responses[0].BodySchema)
	}

	list := controllerIndex["list"]
	if len(list.Responses) != 1 {
		t.Fatalf("list responses = %d, want 1", len(list.Responses))
	}
	if list.Responses[0].Kind != "json_array" {
		t.Fatalf("list response kind = %q, want json_array", list.Responses[0].Kind)
	}
	if list.Responses[0].Status == nil || *list.Responses[0].Status != 202 {
		t.Fatalf("list response status = %#v, want 202", list.Responses[0].Status)
	}
	if list.Responses[0].BodySchema == nil || list.Responses[0].BodySchema.Type != "array" {
		t.Fatalf("list response bodySchema = %#v, want array", list.Responses[0].BodySchema)
	}

	destroy := controllerIndex["destroy"]
	if len(destroy.Responses) != 1 {
		t.Fatalf("destroy responses = %d, want 1", len(destroy.Responses))
	}
	if destroy.Responses[0].Kind != "no_content" {
		t.Fatalf("destroy response kind = %q, want no_content", destroy.Responses[0].Kind)
	}
	if destroy.Responses[0].Status == nil || *destroy.Responses[0].Status != 204 {
		t.Fatalf("destroy response status = %#v, want 204", destroy.Responses[0].Status)
	}
	if destroy.Responses[0].Explicit == nil || !*destroy.Responses[0].Explicit {
		t.Fatalf("destroy response explicit = %#v, want true", destroy.Responses[0].Explicit)
	}
	if destroy.Responses[0].BodySchema != nil {
		t.Fatalf("destroy response bodySchema = %#v, want nil", destroy.Responses[0].BodySchema)
	}

	redirectToReport := controllerIndex["redirectToReport"]
	if len(redirectToReport.Responses) != 1 {
		t.Fatalf("redirectToReport responses = %d, want 1", len(redirectToReport.Responses))
	}
	if redirectToReport.Responses[0].Kind != "redirect" {
		t.Fatalf("redirectToReport response kind = %q, want redirect", redirectToReport.Responses[0].Kind)
	}
	if redirectToReport.Responses[0].Status == nil || *redirectToReport.Responses[0].Status != 301 {
		t.Fatalf("redirectToReport response status = %#v, want 301", redirectToReport.Responses[0].Status)
	}
	if redirectToReport.Responses[0].Headers["Location"] != "/reports" {
		t.Fatalf("redirectToReport Location = %q, want /reports", redirectToReport.Responses[0].Headers["Location"])
	}

	routeRedirect := controllerIndex["routeRedirect"]
	if len(routeRedirect.Responses) != 1 {
		t.Fatalf("routeRedirect responses = %d, want 1", len(routeRedirect.Responses))
	}
	if routeRedirect.Responses[0].Headers["Location"] != "route:dashboard" {
		t.Fatalf("routeRedirect Location = %q, want route:dashboard", routeRedirect.Responses[0].Headers["Location"])
	}
	if routeRedirect.Responses[0].Status == nil || *routeRedirect.Responses[0].Status != 307 {
		t.Fatalf("routeRedirect response status = %#v, want 307", routeRedirect.Responses[0].Status)
	}

	downloadCsv := controllerIndex["downloadCsv"]
	if len(downloadCsv.Responses) != 1 {
		t.Fatalf("downloadCsv responses = %d, want 1", len(downloadCsv.Responses))
	}
	if downloadCsv.Responses[0].Kind != "download" {
		t.Fatalf("downloadCsv response kind = %q, want download", downloadCsv.Responses[0].Kind)
	}
	if downloadCsv.Responses[0].Status == nil || *downloadCsv.Responses[0].Status != 206 {
		t.Fatalf("downloadCsv response status = %#v, want 206", downloadCsv.Responses[0].Status)
	}
	if downloadCsv.Responses[0].ContentType != "text/csv" {
		t.Fatalf("downloadCsv contentType = %q, want text/csv", downloadCsv.Responses[0].ContentType)
	}
	if !strings.Contains(downloadCsv.Responses[0].Headers["Content-Disposition"], `report.csv`) {
		t.Fatalf("downloadCsv Content-Disposition = %q, want filename", downloadCsv.Responses[0].Headers["Content-Disposition"])
	}

	archive := controllerIndex["archive"]
	if len(archive.Responses) != 1 {
		t.Fatalf("archive responses = %d, want 1", len(archive.Responses))
	}
	if archive.Responses[0].Kind != "download" {
		t.Fatalf("archive response kind = %q, want download", archive.Responses[0].Kind)
	}
	if archive.Responses[0].ContentType != "application/zip" {
		t.Fatalf("archive contentType = %q, want application/zip", archive.Responses[0].ContentType)
	}

	preview := controllerIndex["preview"]
	if len(preview.Responses) != 1 {
		t.Fatalf("preview responses = %d, want 1", len(preview.Responses))
	}
	if preview.Responses[0].ContentType != "application/pdf" {
		t.Fatalf("preview contentType = %q, want application/pdf", preview.Responses[0].ContentType)
	}

	streamFeed := controllerIndex["streamFeed"]
	if len(streamFeed.Responses) != 1 {
		t.Fatalf("streamFeed responses = %d, want 1", len(streamFeed.Responses))
	}
	if streamFeed.Responses[0].Kind != "stream" {
		t.Fatalf("streamFeed response kind = %q, want stream", streamFeed.Responses[0].Kind)
	}
	if streamFeed.Responses[0].Status == nil || *streamFeed.Responses[0].Status != 207 {
		t.Fatalf("streamFeed response status = %#v, want 207", streamFeed.Responses[0].Status)
	}
	if streamFeed.Responses[0].ContentType != "text/plain" {
		t.Fatalf("streamFeed contentType = %q, want text/plain", streamFeed.Responses[0].ContentType)
	}
	if streamFeed.Responses[0].BodySchema != nil {
		t.Fatalf("streamFeed response bodySchema = %#v, want nil", streamFeed.Responses[0].BodySchema)
	}

	streamJSONFeed := controllerIndex["streamJsonFeed"]
	if len(streamJSONFeed.Responses) != 1 {
		t.Fatalf("streamJsonFeed responses = %d, want 1", len(streamJSONFeed.Responses))
	}
	if streamJSONFeed.Responses[0].Kind != "stream" {
		t.Fatalf("streamJsonFeed response kind = %q, want stream", streamJSONFeed.Responses[0].Kind)
	}
	if streamJSONFeed.Responses[0].Status == nil || *streamJSONFeed.Responses[0].Status != 208 {
		t.Fatalf("streamJsonFeed response status = %#v, want 208", streamJSONFeed.Responses[0].Status)
	}
	if streamJSONFeed.Responses[0].ContentType != "application/json" {
		t.Fatalf("streamJsonFeed contentType = %q, want application/json", streamJSONFeed.Responses[0].ContentType)
	}
	if streamJSONFeed.Responses[0].Headers["X-Stream"] != "json" {
		t.Fatalf("streamJsonFeed X-Stream = %q, want json", streamJSONFeed.Responses[0].Headers["X-Stream"])
	}
	if streamJSONFeed.Responses[0].BodySchema == nil || streamJSONFeed.Responses[0].BodySchema.Properties["items"].Type != "array" {
		t.Fatalf("streamJsonFeed response bodySchema = %#v, want object with items array", streamJSONFeed.Responses[0].BodySchema)
	}

	dashboard := controllerIndex["dashboard"]
	if len(dashboard.Responses) != 1 {
		t.Fatalf("dashboard responses = %d, want 1", len(dashboard.Responses))
	}
	if dashboard.Responses[0].Kind != "inertia" {
		t.Fatalf("dashboard response kind = %q, want inertia", dashboard.Responses[0].Kind)
	}
	if dashboard.Responses[0].ContentType != "text/html" {
		t.Fatalf("dashboard contentType = %q, want text/html", dashboard.Responses[0].ContentType)
	}
	if dashboard.Responses[0].Inertia == nil || dashboard.Responses[0].Inertia.Component != "Dashboard/Index" {
		t.Fatalf("dashboard inertia info = %#v, want component Dashboard/Index", dashboard.Responses[0].Inertia)
	}
	if dashboard.Responses[0].Inertia.PropsSchema == nil || dashboard.Responses[0].Inertia.PropsSchema.Properties["stats"].Properties["count"].Type != "integer" {
		t.Fatalf("dashboard inertia props = %#v, want integer stats.count", dashboard.Responses[0].Inertia.PropsSchema)
	}

	move := controllerIndex["move"]
	if len(move.Responses) != 1 {
		t.Fatalf("move responses = %d, want 1", len(move.Responses))
	}
	if move.Responses[0].Kind != "redirect" {
		t.Fatalf("move response kind = %q, want redirect", move.Responses[0].Kind)
	}
	if move.Responses[0].Status == nil || *move.Responses[0].Status != 409 {
		t.Fatalf("move response status = %#v, want 409", move.Responses[0].Status)
	}
	if move.Responses[0].Redirect == nil || move.Responses[0].Redirect.TargetKind != "inertia_location" {
		t.Fatalf("move redirect info = %#v, want inertia_location", move.Responses[0].Redirect)
	}
	if move.Responses[0].Headers["X-Inertia-Location"] != "/teams/current" {
		t.Fatalf("move X-Inertia-Location = %q, want /teams/current", move.Responses[0].Headers["X-Inertia-Location"])
	}

	errors := controllerIndex["errors"]
	if len(errors.Responses) != 5 {
		t.Fatalf("errors responses = %d, want 5", len(errors.Responses))
	}
	assertResponseStatus(t, errors.Responses, 200, "json_object")
	assertResponseStatus(t, errors.Responses, 400, "json_object")
	assertResponseStatus(t, errors.Responses, 403, "json_object")
	assertResponseStatus(t, errors.Responses, 404, "json_object")
	validation := assertResponseStatus(t, errors.Responses, 422, "json_object")
	if validation.BodySchema == nil || validation.BodySchema.Properties["errors"].Properties["email"].Items == nil || validation.BodySchema.Properties["errors"].Properties["email"].Items.Type != "string" {
		t.Fatalf("validation response schema = %#v, want errors.email as array of strings", validation.BodySchema)
	}
	if validation.Source != "ValidationException::withMessages" {
		t.Fatalf("validation response source = %q, want ValidationException::withMessages", validation.Source)
	}

	additional := controllerIndex["additionalResource"]
	if len(additional.Responses) != 1 {
		t.Fatalf("additionalResource responses = %d, want 1", len(additional.Responses))
	}
	if additional.Responses[0].Kind != "json_object" {
		t.Fatalf("additionalResource response kind = %q, want json_object", additional.Responses[0].Kind)
	}
	if additional.Responses[0].BodySchema == nil || additional.Responses[0].BodySchema.Properties["meta"].Properties["version"].Type != "integer" {
		t.Fatalf("additionalResource bodySchema = %#v, want merged meta.version integer", additional.Responses[0].BodySchema)
	}
	if additional.Responses[0].BodySchema.Properties["links"].Properties["self"].Format != "uri" {
		t.Fatalf("additionalResource links.self format = %q, want uri", additional.Responses[0].BodySchema.Properties["links"].Properties["self"].Format)
	}

	collectionResource := controllerIndex["resourceCollection"]
	if len(collectionResource.Responses) != 1 {
		t.Fatalf("resourceCollection responses = %d, want 1", len(collectionResource.Responses))
	}
	if collectionResource.Responses[0].BodySchema == nil || collectionResource.Responses[0].BodySchema.Properties["data"].Type != "array" {
		t.Fatalf("resourceCollection bodySchema = %#v, want data array envelope", collectionResource.Responses[0].BodySchema)
	}
	if collectionResource.Responses[0].BodySchema.Properties["data"].Items == nil || collectionResource.Responses[0].BodySchema.Properties["data"].Items.Ref != "App\\Http\\Resources\\ReportResource" {
		t.Fatalf("resourceCollection data items = %#v, want ReportResource ref", collectionResource.Responses[0].BodySchema.Properties["data"].Items)
	}
}

func assertResponseStatus(t *testing.T, responses []emitter.Response, status int, kind string) emitter.Response {
	t.Helper()
	for _, response := range responses {
		if response.Status != nil && *response.Status == status && response.Kind == kind {
			return response
		}
	}

	t.Fatalf("missing %s response with status %d in %#v", kind, status, responses)
	return emitter.Response{}
}

func TestEnrichDeltaBuildsAuthorizationHintsAndImplicitResponses(t *testing.T) {
	projectRoot := t.TempDir()

	writeTestFile(t, filepath.Join(projectRoot, "composer.json"), `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Requests/ShowPostRequest.php"), `<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;

class ShowPostRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }
}
`)

	writeTestFile(t, filepath.Join(projectRoot, "app/Http/Controllers/PostController.php"), `<?php

namespace App\Http\Controllers;

use App\Http\Requests\ShowPostRequest;
use App\Models\Post;
use Illuminate\Database\Eloquent\ModelNotFoundException;
use Illuminate\Http\JsonResponse;
use Illuminate\Support\Facades\Gate;

class PostController
{
    public function __construct()
    {
        $this->authorizeResource(Post::class, 'post', ['only' => ['show', 'update']]);
    }

    public function index(): JsonResponse
    {
        return response()->json(['ok' => true]);
    }

    public function show(ShowPostRequest $request, Post $post): JsonResponse
    {
        $this->authorize('view', $post);
        Gate::authorize('publish', Post::class);
        Gate::allows('preview', $post);

        abort_if($request->boolean('archived'), 410);
        throw_if($request->boolean('missing'), new ModelNotFoundException());
        Post::query()->findOrFail(1);

        return response()->json(['id' => 1]);
    }

    public function preview(Post $post): JsonResponse
    {
        Gate::allows('preview', $post);

        return response()->json(['preview' => true]);
    }
}
`)

	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 1,
				Skipped:     0,
				DurationMs:  0,
			},
		},
		Controllers: []emitter.Controller{
			{FQCN: "App\\Http\\Controllers\\PostController", Method: "index"},
			{FQCN: "App\\Http\\Controllers\\PostController", Method: "show"},
			{FQCN: "App\\Http\\Controllers\\PostController", Method: "preview"},
		},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	manifestData := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectRoot,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
	}

	if err := EnrichDelta(context.Background(), manifestData, delta); err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	controllerIndex := make(map[string]emitter.Controller, len(delta.Controllers))
	for _, controller := range delta.Controllers {
		controllerIndex[controller.Method] = controller
	}

	index := controllerIndex["index"]
	if len(index.Authorization) != 0 {
		t.Fatalf("index authorization = %#v, want none", index.Authorization)
	}

	show := controllerIndex["show"]
	if len(show.Authorization) < 4 {
		t.Fatalf("show authorization = %#v, want multiple hints", show.Authorization)
	}

	resourceHint := assertAuthorizationHint(t, show.Authorization, "$this->authorizeResource", "authorize_resource")
	if resourceHint.Ability == nil || *resourceHint.Ability != "view" {
		t.Fatalf("authorizeResource ability = %#v, want view", resourceHint.Ability)
	}
	if resourceHint.Parameter == nil || *resourceHint.Parameter != "post" {
		t.Fatalf("authorizeResource parameter = %#v, want post", resourceHint.Parameter)
	}

	directHint := assertAuthorizationHint(t, show.Authorization, "$this->authorize", "authorize")
	if directHint.Ability == nil || *directHint.Ability != "view" {
		t.Fatalf("$this->authorize ability = %#v, want view", directHint.Ability)
	}
	if directHint.Target == nil || *directHint.Target != "App\\Models\\Post" {
		t.Fatalf("$this->authorize target = %#v, want App\\\\Models\\\\Post", directHint.Target)
	}

	gateHint := assertAuthorizationHint(t, show.Authorization, "Gate::authorize", "authorize")
	if gateHint.TargetKind == nil || *gateHint.TargetKind != "class" {
		t.Fatalf("Gate::authorize targetKind = %#v, want class", gateHint.TargetKind)
	}

	formRequestHint := assertAuthorizationHint(t, show.Authorization, "FormRequest::authorize", "form_request_authorize")
	if formRequestHint.Target == nil || *formRequestHint.Target != "App\\Http\\Requests\\ShowPostRequest" {
		t.Fatalf("FormRequest::authorize target = %#v, want ShowPostRequest", formRequestHint.Target)
	}

	allowsHint := assertAuthorizationHint(t, show.Authorization, "Gate::allows", "allows")
	if allowsHint.EnforcesFailureResponse {
		t.Fatalf("Gate::allows should not enforce failure response: %#v", allowsHint)
	}

	assertResponseStatus(t, show.Responses, 200, "json_object")
	assertResponseStatus(t, show.Responses, 403, "json_object")
	assertResponseStatus(t, show.Responses, 404, "json_object")
	assertResponseStatus(t, show.Responses, 410, "json_object")
	assertResponseStatus(t, show.Responses, 422, "json_object")

	preview := controllerIndex["preview"]
	if len(preview.Authorization) != 1 {
		t.Fatalf("preview authorization = %#v, want one Gate::allows hint", preview.Authorization)
	}
	if preview.Authorization[0].Source != "Gate::allows" {
		t.Fatalf("preview authorization source = %q, want Gate::allows", preview.Authorization[0].Source)
	}
	if len(preview.Responses) != 1 {
		t.Fatalf("preview responses = %#v, want one response", preview.Responses)
	}
	if preview.Responses[0].Status == nil || *preview.Responses[0].Status != 200 {
		t.Fatalf("preview response status = %#v, want 200", preview.Responses[0].Status)
	}
}

func assertAuthorizationHint(t *testing.T, hints []emitter.AuthorizationHint, source, kind string) emitter.AuthorizationHint {
	t.Helper()
	for _, hint := range hints {
		if hint.Source == source && hint.Kind == kind {
			return hint
		}
	}

	t.Fatalf("missing authorization hint %s/%s in %#v", source, kind, hints)
	return emitter.AuthorizationHint{}
}

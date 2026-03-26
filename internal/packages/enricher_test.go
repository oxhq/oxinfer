//go:build goexperiment.jsonv2

package packages

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
)

func TestEnrichDeltaAddsQueryBuilderShape(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/UserController.php": `<?php
namespace App\Http\Controllers;

use App\Models\User;
use Spatie\QueryBuilder\AllowedFilter;
use Spatie\QueryBuilder\QueryBuilder;

class UserController
{
    public function index()
    {
        QueryBuilder::for(User::class)
            ->allowedFilters(['status', AllowedFilter::trashed()])
            ->allowedIncludes(['team'])
            ->allowedSorts(['name'])
            ->allowedFields(['users.id', 'users.email']);
    }
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\UserController`,
				Method: "index",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelQueryBuilder},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want inferred query shape")
	}
	assertHasOrderedObjectKey(t, request.Query, "filter")
	assertHasOrderedObjectKey(t, request.Query["filter"], "status")
	assertHasOrderedObjectKey(t, request.Query["filter"], "trashed")
	assertHasOrderedObjectKey(t, request.Query, "fields")
	assertHasOrderedObjectKey(t, request.Query["fields"], "users")
	assertHasOrderedObjectKey(t, request.Query, "include")
	assertHasOrderedObjectKey(t, request.Query, "sort")
}

func TestEnrichDeltaAddsLaravelDataBodyShape(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/UserController.php": `<?php
namespace App\Http\Controllers;

use App\Data\UserData;

class UserController
{
    public function store(UserData $payload)
    {
    }
}`,
		"app/Data/UserData.php": `<?php
namespace App\Data;

use App\Data\AddressData;
use Spatie\LaravelData\Data;

class UserData extends Data
{
    public function __construct(
        public string $name,
        public AddressData $address,
    ) {
    }
}`,
		"app/Data/AddressData.php": `<?php
namespace App\Data;

use Spatie\LaravelData\Data;

class AddressData extends Data
{
    public function __construct(
        public string $city,
    ) {
    }
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\UserController`,
				Method: "store",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelData},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want inferred body shape")
	}
	assertHasOrderedObjectKey(t, request.Body, "name")
	assertHasOrderedObjectKey(t, request.Body, "address")
	assertHasOrderedObjectKey(t, request.Body["address"], "city")
	if len(request.ContentTypes) != 1 || request.ContentTypes[0] != "application/json" {
		t.Fatalf("content types = %#v, want [application/json]", request.ContentTypes)
	}

	addressField := expectRequestField(t, request.Fields, "body", "address")
	if addressField.Kind != "object" || addressField.Type != `App\Data\AddressData` {
		t.Fatalf("address field = %#v, want object App\\Data\\AddressData", addressField)
	}
	if addressField.Required == nil || !*addressField.Required {
		t.Fatalf("address field required = %#v, want true", addressField.Required)
	}

	cityField := expectRequestField(t, request.Fields, "body", "address.city")
	if cityField.Kind != "scalar" || cityField.ScalarType != "string" {
		t.Fatalf("address.city field = %#v, want scalar string", cityField)
	}
}

func TestEnrichDeltaMergesInheritedLaravelDataShape(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/PageController.php": `<?php
namespace App\Http\Controllers;

use App\Data\StorePageData;

class PageController
{
    public function store(StorePageData $payload)
    {
    }
}`,
		"app/Data/BaseContentData.php": `<?php
namespace App\Data;

use Spatie\LaravelData\Data;

class BaseContentData extends Data
{
    public function __construct(
        public string $title,
    ) {
    }
}`,
		"app/Data/SeoData.php": `<?php
namespace App\Data;

use Spatie\LaravelData\Data;

class SeoData extends Data
{
    public string $slug;
}`,
		"app/Data/StorePageData.php": `<?php
namespace App\Data;

class StorePageData extends BaseContentData
{
    public function __construct(
        string $title,
        public ?SeoData $seo,
    ) {
        parent::__construct($title);
    }
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\PageController`,
				Method: "store",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelData},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want inherited data shape")
	}
	assertHasOrderedObjectKey(t, request.Body, "title")
	assertHasOrderedObjectKey(t, request.Body, "seo")
	assertHasOrderedObjectKey(t, request.Body["seo"], "slug")
}

func TestEnrichDeltaInfersLaravelDataCollectionsFromDocblocks(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/ArticleController.php": `<?php
namespace App\Http\Controllers;

use App\Data\ArticleData;

class ArticleController
{
    public function store(ArticleData $payload)
    {
    }
}`,
		"app/Data/SeoData.php": `<?php
namespace App\Data;

use Spatie\LaravelData\Data;

class SeoData extends Data
{
    public function __construct(
        public string $slug,
    ) {
    }
}`,
		"app/Data/ArticleData.php": `<?php
namespace App\Data;

use Spatie\LaravelData\Data;

class ArticleData extends Data
{
    /**
     * @var array<SeoData>
     */
    public array $history = [];
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\ArticleController`,
				Method: "store",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelData},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want docblock collection shape")
	}
	assertHasOrderedObjectKey(t, request.Body, "history")
	assertHasOrderedObjectKey(t, request.Body["history"], "_item")
	assertHasOrderedObjectKey(t, request.Body["history"]["_item"], "slug")

	historyField := expectRequestField(t, request.Fields, "body", "history")
	if historyField.Collection == nil || !*historyField.Collection || historyField.ItemType != `App\Data\SeoData` {
		t.Fatalf("history field = %#v, want collection of App\\Data\\SeoData", historyField)
	}
	if nested := expectRequestField(t, request.Fields, "body", "history[].slug"); nested.ScalarType != "string" {
		t.Fatalf("history[].slug field = %#v, want scalar string", nested)
	}
}

func TestEnrichDeltaAddsMediaLibraryMultipartFiles(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/MediaController.php": `<?php
namespace App\Http\Controllers;

class MediaController
{
    public function store()
    {
        $user->addMediaFromRequest('avatar')->toMediaCollection('avatars');
        $post->addMultipleMediaFromRequest(['gallery', 'cover'])->toMediaCollection('images');
    }
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\MediaController`,
				Method: "store",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelMediaLibrary},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want inferred multipart files")
	}
	assertHasOrderedObjectKey(t, request.Files, "avatar")
	assertHasOrderedObjectKey(t, request.Files, "cover")
	assertHasOrderedObjectKey(t, request.Files, "gallery")
	if len(request.ContentTypes) != 1 || request.ContentTypes[0] != "multipart/form-data" {
		t.Fatalf("content types = %#v, want [multipart/form-data]", request.ContentTypes)
	}
}

func TestEnrichDeltaResolvesQueryBuilderHelpersAndConstants(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/SearchController.php": `<?php
namespace App\Http\Controllers;

use Spatie\QueryBuilder\AllowedFilter;
use Spatie\QueryBuilder\QueryBuilder;

class SearchController
{
    private const SORTS = ['-published_at', 'title'];

    public function index()
    {
        QueryBuilder::for('users')
            ->allowedFilters(self::filters())
            ->allowedIncludes(self::includes())
            ->allowedSorts(self::SORTS)
            ->allowedFields(self::fields());
    }

    private static function filters(): array
    {
        return [
            AllowedFilter::exact('state', 'posts.status'),
            AllowedFilter::callback('tagged', static function ($query) {
                return $query->where('status', 'published');
            }),
            AllowedFilter::trashed(),
        ];
    }

    private static function includes(): array
    {
        return ['author.profile', 'comments.user'];
    }

    private static function fields(): array
    {
        return self::FIELD_SET;
    }

    private const FIELD_SET = ['posts.id', 'posts.title'];
}`,
	})

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\SearchController`,
				Method: "index",
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelQueryBuilder},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	request := delta.Controllers[0].Request
	if request == nil {
		t.Fatal("controller request = nil, want helper-driven query shape")
	}
	assertHasOrderedObjectKey(t, request.Query["filter"], "state")
	assertHasOrderedObjectKey(t, request.Query["filter"], "tagged")
	assertHasOrderedObjectKey(t, request.Query["filter"], "trashed")
	assertHasOrderedObjectKey(t, request.Query["include"], "author")
	assertHasOrderedObjectKey(t, request.Query["include"]["author"], "profile")
	assertHasOrderedObjectKey(t, request.Query["include"], "comments")
	assertHasOrderedObjectKey(t, request.Query["include"]["comments"], "user")
	assertHasOrderedObjectKey(t, request.Query["sort"], "published_at")
	assertHasOrderedObjectKey(t, request.Query["sort"], "title")
	assertHasOrderedObjectKey(t, request.Query["fields"], "posts")
	assertHasOrderedObjectKey(t, request.Query["fields"]["posts"], "id")
	assertHasOrderedObjectKey(t, request.Query["fields"]["posts"], "title")

	includeField := expectRequestField(t, request.Fields, "query", "include")
	if got := includeField.AllowedValues; len(got) != 2 || got[0] != "author.profile" || got[1] != "comments.user" {
		t.Fatalf("include allowedValues = %#v, want [author.profile comments.user]", got)
	}
	sortField := expectRequestField(t, request.Fields, "query", "sort")
	if got := sortField.AllowedValues; len(got) != 2 || got[0] != "published_at" || got[1] != "title" {
		t.Fatalf("sort allowedValues = %#v, want [published_at title]", got)
	}
	postsField := expectRequestField(t, request.Fields, "query", "fields.posts")
	if got := postsField.AllowedValues; len(got) != 2 || got[0] != "id" || got[1] != "title" {
		t.Fatalf("fields.posts allowedValues = %#v, want [id title]", got)
	}
}

func TestEnrichDeltaAddsTranslatableModelAttributes(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Models/Post.php": `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Spatie\Translatable\HasTranslations;

class Post extends Model
{
    use HasTranslations;

    protected array $translatable = ['name', 'summary'];
}`,
	})

	delta := &emitter.Delta{
		Models: []emitter.Model{
			{
				FQCN: `App\Models\Post`,
				Attributes: []emitter.Attribute{
					{Name: "slug", Via: "Attribute::make"},
				},
			},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelTranslatable},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	attributes := delta.Models[0].Attributes
	expectAttribute := func(name, via string) {
		t.Helper()
		for _, attribute := range attributes {
			if attribute.Name == name && attribute.Via == via {
				return
			}
		}
		t.Fatalf("attributes = %#v, want {%q %q}", attributes, name, via)
	}

	expectAttribute("name", "spatie/laravel-translatable")
	expectAttribute("summary", "spatie/laravel-translatable")
	expectAttribute("slug", "Attribute::make")
}

func TestEnrichDeltaAddsTranslatableModelsMissingFromDelta(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Models/Page.php": `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Spatie\Translatable\HasTranslations as SpatieHasTranslations;

class Page extends Model
{
    use SpatieHasTranslations;

    public static array $translatable = ['headline', 'excerpt'];
}`,
	})

	delta := &emitter.Delta{
		Models: []emitter.Model{},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelTranslatable},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	if len(delta.Models) != 1 {
		t.Fatalf("model count = %d, want 1", len(delta.Models))
	}
	if delta.Models[0].FQCN != `App\Models\Page` {
		t.Fatalf("model fqcn = %q, want App\\Models\\Page", delta.Models[0].FQCN)
	}

	attributes := delta.Models[0].Attributes
	expectAttribute := func(name string) {
		t.Helper()
		for _, attribute := range attributes {
			if attribute.Name == name && attribute.Via == "spatie/laravel-translatable" {
				return
			}
		}
		t.Fatalf("attributes = %#v, want translatable field %q", attributes, name)
	}

	expectAttribute("excerpt")
	expectAttribute("headline")
}

func TestEnrichDeltaUsesRealSpatieFixturePaths(t *testing.T) {
	projectRoot := spatieFixtureRoot(t)

	delta := &emitter.Delta{
		Controllers: []emitter.Controller{
			{
				FQCN:   `App\Http\Controllers\SearchController`,
				Method: "index",
			},
			{
				FQCN:   `App\Http\Controllers\AdvancedSearchController`,
				Method: "index",
			},
			{
				FQCN:   `App\Http\Controllers\PublishController`,
				Method: "store",
			},
			{
				FQCN:   `App\Http\Controllers\AdvancedPublishController`,
				Method: "__invoke",
			},
			{
				FQCN:   `App\Http\Controllers\MediaController`,
				Method: "store",
			},
			{
				FQCN:   `App\Http\Controllers\MediaController`,
				Method: "gallery",
			},
			{
				FQCN:   `App\Http\Controllers\MediaAttachmentsController`,
				Method: "store",
			},
		},
		Models: []emitter.Model{
			{FQCN: `App\Models\Post`},
			{FQCN: `App\Models\Page`},
			{FQCN: `App\Models\Series`},
		},
	}

	err := EnrichDelta(context.Background(), createPackageManifest(projectRoot), []DetectedPackage{
		{Name: PackageSpatieLaravelData},
		{Name: PackageSpatieLaravelQueryBuilder},
		{Name: PackageSpatieLaravelMediaLibrary},
		{Name: PackageSpatieLaravelTranslatable},
	}, delta)
	if err != nil {
		t.Fatalf("EnrichDelta() error = %v", err)
	}

	search := delta.Controllers[0].Request
	if search == nil {
		t.Fatal("search controller request = nil, want query shape")
	}
	assertHasOrderedObjectKey(t, search.Query, "include")
	assertHasOrderedObjectKey(t, search.Query["include"], "author")
	assertHasOrderedObjectKey(t, search.Query["include"]["author"], "profile")
	assertHasOrderedObjectKey(t, search.Query["include"], "comments")
	assertHasOrderedObjectKey(t, search.Query["include"]["comments"], "user")
	assertHasOrderedObjectKey(t, search.Query["fields"], "posts")
	assertHasOrderedObjectKey(t, search.Query["fields"]["posts"], "title")
	assertHasOrderedObjectKey(t, search.Query["fields"]["posts"], "summary")
	assertHasOrderedObjectKey(t, search.Query["fields"], "authors")
	assertHasOrderedObjectKey(t, search.Query["fields"]["authors"], "name")
	assertHasOrderedObjectKey(t, search.Query["sort"], "published_at")
	assertHasOrderedObjectKey(t, search.Query["sort"], "title")
	assertHasOrderedObjectKey(t, search.Query["sort"], "status")

	advancedSearch := delta.Controllers[1].Request
	if advancedSearch == nil {
		t.Fatal("advanced search controller request = nil, want query shape")
	}
	assertHasOrderedObjectKey(t, advancedSearch.Query["filter"], "published_after")
	assertHasOrderedObjectKey(t, advancedSearch.Query["filter"], "state")
	assertHasOrderedObjectKey(t, advancedSearch.Query["filter"], "tagged")
	assertHasOrderedObjectKey(t, advancedSearch.Query["fields"], "media")
	assertHasOrderedObjectKey(t, advancedSearch.Query["fields"]["media"], "name")
	assertHasOrderedObjectKey(t, advancedSearch.Query["include"]["tags"], "media")
	assertHasOrderedObjectKey(t, advancedSearch.Query["sort"], "updated_at")

	publish := delta.Controllers[2].Request
	if publish == nil {
		t.Fatal("publish controller request = nil, want body shape")
	}
	assertHasOrderedObjectKey(t, publish.Body, "seo")
	assertHasOrderedObjectKey(t, publish.Body["seo"], "slug")
	assertHasOrderedObjectKey(t, publish.Body, "reviewer")
	assertHasOrderedObjectKey(t, publish.Body["reviewer"], "approval")
	assertHasOrderedObjectKey(t, publish.Body["reviewer"]["approval"], "slug")
	assertHasOrderedObjectKey(t, publish.Body, "notes")

	advancedPublish := delta.Controllers[3].Request
	if advancedPublish == nil {
		t.Fatal("advanced publish controller request = nil, want inherited collection body shape")
	}
	assertHasOrderedObjectKey(t, advancedPublish.Body, "featured")
	assertHasOrderedObjectKey(t, advancedPublish.Body, "approvalHistory")
	assertHasOrderedObjectKey(t, advancedPublish.Body["approvalHistory"], "_item")
	assertHasOrderedObjectKey(t, advancedPublish.Body["approvalHistory"]["_item"], "name")
	assertHasOrderedObjectKey(t, advancedPublish.Body["approvalHistory"]["_item"], "approval")
	assertHasOrderedObjectKey(t, advancedPublish.Body["approvalHistory"]["_item"]["approval"], "slug")
	assertHasOrderedObjectKey(t, advancedPublish.Body, "preview")
	assertHasOrderedObjectKey(t, advancedPublish.Body["preview"], "slug")
	assertHasOrderedObjectKey(t, advancedPublish.Body, "reviewers")
	assertHasOrderedObjectKey(t, advancedPublish.Body["reviewers"], "_item")
	assertHasOrderedObjectKey(t, advancedPublish.Body["reviewers"]["_item"], "name")
	assertHasOrderedObjectKey(t, advancedPublish.Body["reviewers"]["_item"], "approval")
	assertHasOrderedObjectKey(t, advancedPublish.Body["reviewers"]["_item"]["approval"], "slug")
	assertHasOrderedObjectKey(t, advancedPublish.Body, "teaser")
	assertHasOrderedObjectKey(t, advancedPublish.Body["teaser"], "slug")

	media := delta.Controllers[4].Request
	if media == nil {
		t.Fatal("media controller request = nil, want multipart files")
	}
	assertHasOrderedObjectKey(t, media.Files, "avatar")
	assertHasOrderedObjectKey(t, media.Files, "gallery")
	assertHasOrderedObjectKey(t, media.Files, "cover")
	if len(media.ContentTypes) != 1 || media.ContentTypes[0] != "multipart/form-data" {
		t.Fatalf("media content types = %#v, want [multipart/form-data]", media.ContentTypes)
	}

	gallery := delta.Controllers[5].Request
	if gallery == nil {
		t.Fatal("gallery controller request = nil, want multipart files")
	}
	assertHasOrderedObjectKey(t, gallery.Files, "hero_image")
	assertHasOrderedObjectKey(t, gallery.Files, "attachments")
	if len(gallery.ContentTypes) != 1 || gallery.ContentTypes[0] != "multipart/form-data" {
		t.Fatalf("gallery content types = %#v, want [multipart/form-data]", gallery.ContentTypes)
	}

	attachments := delta.Controllers[6].Request
	if attachments == nil {
		t.Fatal("attachments controller request = nil, want request-file inference")
	}
	assertHasOrderedObjectKey(t, attachments.Files, "thumbnail")
	assertHasOrderedObjectKey(t, attachments.Files, "preview_pdf")
	assertHasOrderedObjectKey(t, attachments.Files, "attachments")
	assertHasOrderedObjectKey(t, attachments.Files, "gallery_images")
	assertHasOrderedObjectKey(t, attachments.Files["gallery_images"], "_item")
	if len(attachments.ContentTypes) != 1 || attachments.ContentTypes[0] != "multipart/form-data" {
		t.Fatalf("attachments content types = %#v, want [multipart/form-data]", attachments.ContentTypes)
	}
	galleryImagesField := expectRequestField(t, attachments.Fields, "files", "gallery_images")
	if galleryImagesField.Collection == nil || !*galleryImagesField.Collection || galleryImagesField.ItemType != "file" {
		t.Fatalf("gallery_images field = %#v, want file collection", galleryImagesField)
	}

	previewField := expectRequestField(t, advancedPublish.Fields, "body", "preview")
	if previewField.Optional == nil || !*previewField.Optional || previewField.Type != `App\Data\SeoData` {
		t.Fatalf("preview field = %#v, want optional App\\Data\\SeoData", previewField)
	}
	teaserField := expectRequestField(t, advancedPublish.Fields, "body", "teaser")
	if teaserField.Optional == nil || !*teaserField.Optional || len(teaserField.Wrappers) == 0 {
		t.Fatalf("teaser field = %#v, want optional wrapper metadata", teaserField)
	}
	includeMetadata := expectRequestField(t, advancedSearch.Fields, "query", "include")
	if got := includeMetadata.AllowedValues; len(got) == 0 || got[0] != "author.profile" {
		t.Fatalf("advanced search include field = %#v, want allowedValues", includeMetadata)
	}

	wantModels := map[string][]struct {
		name string
		via  string
	}{
		`App\Models\Post`: {
			{name: "title", via: "spatie/laravel-translatable"},
			{name: "summary", via: "spatie/laravel-translatable"},
		},
		`App\Models\Page`: {
			{name: "title", via: "spatie/laravel-translatable"},
		},
		`App\Models\Series`: {
			{name: "description", via: "spatie/laravel-translatable"},
			{name: "subtitle", via: "spatie/laravel-translatable"},
			{name: "title", via: "spatie/laravel-translatable"},
		},
	}

	for _, model := range delta.Models {
		expectations, ok := wantModels[model.FQCN]
		if !ok {
			continue
		}

		for _, expectation := range expectations {
			expectModelAttribute(t, model.Attributes, expectation.name, expectation.via)
		}

		delete(wantModels, model.FQCN)
	}

	if len(wantModels) != 0 {
		t.Fatalf("missing expected models: %#v", wantModels)
	}
}

func createPackageManifest(projectRoot string) *manifest.Manifest {
	return &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectRoot,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
		},
	}
}

func createPackageProject(t *testing.T, files map[string]string) string {
	t.Helper()

	projectRoot := t.TempDir()
	for relPath, content := range files {
		absPath := filepath.Join(projectRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("os.MkdirAll(%s) error = %v", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", relPath, err)
		}
	}
	return projectRoot
}

func assertHasOrderedObjectKey(t *testing.T, object emitter.OrderedObject, key string) {
	t.Helper()
	if _, ok := object[key]; !ok {
		t.Fatalf("ordered object = %#v, want key %q", object, key)
	}
}

func expectRequestField(t *testing.T, fields []emitter.RequestField, location, path string) emitter.RequestField {
	t.Helper()
	for _, field := range fields {
		if field.Location == location && field.Path == path {
			return field
		}
	}
	t.Fatalf("request fields = %#v, want %s:%s", fields, location, path)
	return emitter.RequestField{}
}

func expectModelAttribute(t *testing.T, attributes []emitter.Attribute, name, via string) {
	t.Helper()
	for _, attribute := range attributes {
		if attribute.Name == name && attribute.Via == via {
			return
		}
	}
	t.Fatalf("attributes = %#v, want {%q %q}", attributes, name, via)
}

func spatieFixtureRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "garaekz", "oxcribe", "tests", "Fixtures", "SpatieLaravelApp"))
	if err != nil {
		t.Fatalf("filepath.Abs(spatie fixture) error = %v", err)
	}

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("spatie fixture root %q unavailable: %v", root, err)
	}

	return root
}

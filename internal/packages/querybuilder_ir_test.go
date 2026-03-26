//go:build goexperiment.jsonv2

package packages

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/psr4"
)

func TestQueryBuilderRequestIRCapturesVariantsAndNestedShapes(t *testing.T) {
	projectRoot := spatieFixtureRoot(t)
	runtime := newTestSourceRuntime(t, projectRoot)

	filePath := filepath.Join(projectRoot, "app/Http/Controllers/AdvancedSearchController.php")
	sourceBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", filePath, err)
	}

	source := string(sourceBytes)
	_, body, ok := extractMethodSignatureAndBody(source, "index")
	if !ok {
		t.Fatal("extractMethodSignatureAndBody() = false, want controller body")
	}

	spec := runtime.extractQueryBuilderRequestIR(context.Background(), "App\\Http\\Controllers\\AdvancedSearchController", source, body, runtime.fileMetadata(filePath, source))

	assertFilterSpec := func(name, variant, column string) {
		t.Helper()
		for _, filter := range spec.Filters {
			if filter.Name == name && filter.Variant == variant && filter.Column == column {
				return
			}
		}
		t.Fatalf("filters = %#v, want %q variant=%q column=%q", spec.Filters, name, variant, column)
	}

	assertPathSpec := func(specs []queryBuilderPathSpec, name string, descending bool, segments ...string) {
		t.Helper()
		for _, pathSpec := range specs {
			if pathSpec.Name == name && pathSpec.Descending == descending {
				if len(segments) == 0 || equalStringSlices(pathSpec.Segments, segments) {
					return
				}
			}
		}
		t.Fatalf("path specs = %#v, want name=%q descending=%v segments=%#v", specs, name, descending, segments)
	}

	assertFilterSpec("state", "exact", "posts.status")
	assertFilterSpec("ownedBy", "scope", "")
	assertFilterSpec("tagged", "callback", "")
	assertFilterSpec("published_after", "custom", "")
	assertFilterSpec("trashed", "trashed", "")

	assertPathSpec(spec.Includes, "author.profile", false, "author", "profile")
	assertPathSpec(spec.Includes, "comments.user", false, "comments", "user")
	assertPathSpec(spec.Includes, "tags.media", false, "tags", "media")
	assertPathSpec(spec.Sorts, "published_at", true, "published_at")
	assertPathSpec(spec.Sorts, "title", false, "title")
	assertPathSpec(spec.Sorts, "status", false, "status")
	assertPathSpec(spec.Sorts, "updated_at", false, "updated_at")
	assertPathSpec(spec.Fields, "posts.id", false, "posts", "id")
	assertPathSpec(spec.Fields, "posts.title", false, "posts", "title")
	assertPathSpec(spec.Fields, "posts.summary", false, "posts", "summary")
	assertPathSpec(spec.Fields, "posts.status", false, "posts", "status")
	assertPathSpec(spec.Fields, "authors.name", false, "authors", "name")
	assertPathSpec(spec.Fields, "authors.email", false, "authors", "email")
	assertPathSpec(spec.Fields, "media.name", false, "media", "name")
}

func TestQueryBuilderRequestIRResolvesAliasedSpatieClassNames(t *testing.T) {
	projectRoot := createPackageProject(t, map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": "app/"
    }
  }
}`,
		"app/Http/Controllers/AliasedSearchController.php": `<?php
namespace App\Http\Controllers;

use App\Models\User;
use Spatie\QueryBuilder\AllowedFilter as QBFilter;
use Spatie\QueryBuilder\AllowedInclude as QBInclude;
use Spatie\QueryBuilder\AllowedSort as QBSort;
use Spatie\QueryBuilder\QueryBuilder;

class AliasedSearchController
{
    public function index()
    {
        $filters = [
            QBFilter::exact('state', 'users.status'),
            QBFilter::scope('ownedBy'),
            QBFilter::trashed(),
        ];

        $includes = [
            QBInclude::relationship('author.profile'),
        ];

        $sorts = [
            QBSort::field('-published_at'),
        ];

        $fields = [
            'users.id',
            'users.email',
        ];

        QueryBuilder::for(User::class)
            ->allowedFilters($filters)
            ->allowedIncludes($includes)
            ->allowedSorts($sorts)
            ->allowedFields($fields);
    }
}`,
	})

	runtime := newTestSourceRuntime(t, projectRoot)
	filePath := filepath.Join(projectRoot, "app/Http/Controllers/AliasedSearchController.php")
	sourceBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", filePath, err)
	}

	source := string(sourceBytes)
	_, body, ok := extractMethodSignatureAndBody(source, "index")
	if !ok {
		t.Fatal("extractMethodSignatureAndBody() = false, want controller body")
	}

	spec := runtime.extractQueryBuilderRequestIR(context.Background(), "App\\Http\\Controllers\\AliasedSearchController", source, body, runtime.fileMetadata(filePath, source))

	assertFilterSpec := func(name, variant, column string) {
		t.Helper()
		for _, filter := range spec.Filters {
			if filter.Name == name && filter.Variant == variant && filter.Column == column {
				return
			}
		}
		t.Fatalf("filters = %#v, want %q variant=%q column=%q", spec.Filters, name, variant, column)
	}

	assertPathSpec := func(specs []queryBuilderPathSpec, name string, descending bool, segments ...string) {
		t.Helper()
		for _, pathSpec := range specs {
			if pathSpec.Name == name && pathSpec.Descending == descending && equalStringSlices(pathSpec.Segments, segments) {
				return
			}
		}
		t.Fatalf("path specs = %#v, want name=%q descending=%v segments=%#v", specs, name, descending, segments)
	}

	assertFilterSpec("state", "exact", "users.status")
	assertFilterSpec("ownedBy", "scope", "")
	assertFilterSpec("trashed", "trashed", "")
	assertPathSpec(spec.Includes, "author.profile", false, "author", "profile")
	assertPathSpec(spec.Sorts, "published_at", true, "published_at")
	assertPathSpec(spec.Fields, "users.id", false, "users", "id")
	assertPathSpec(spec.Fields, "users.email", false, "users", "email")
}

func newTestSourceRuntime(t *testing.T, projectRoot string) *sourceRuntime {
	t.Helper()

	runtime := &sourceRuntime{
		sourceCache:    map[string]string{},
		metadataCache:  map[string]*phpFileMetadata{},
		dataClassCache: map[string]bool{},
		dataShapeCache: map[string]emitter.OrderedObject{},
	}

	resolver, err := psr4.NewPSR4ResolverFromManifest(createPackageManifest(projectRoot))
	if err != nil {
		t.Fatalf("psr4.NewPSR4ResolverFromManifest() error = %v", err)
	}

	runtime.resolver = resolver
	return runtime
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

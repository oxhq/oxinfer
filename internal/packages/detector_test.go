//go:build goexperiment.jsonv2

package packages

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oxhq/oxinfer/internal/contracts"
	"github.com/oxhq/oxinfer/internal/manifest"
)

func TestDetectInstalledPackagesMergesRuntimeAndComposerSources(t *testing.T) {
	projectRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectRoot, "composer.json"), []byte(`{
  "require": {
    "spatie/laravel-permission": "^6.0"
  },
  "require-dev": {
    "spatie/laravel-translatable": "^6.0"
  }
}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile(composer.json) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectRoot, "composer.lock"), []byte(`{
  "packages": [
    {
      "name": "spatie/laravel-query-builder",
      "version": "6.3.0"
    }
  ],
  "packages-dev": []
}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile(composer.lock) error = %v", err)
	}

	runtimeVersion := "4.5.0"
	detected, err := DetectInstalledPackages(projectRoot, []contracts.RuntimePackage{
		{Name: PackageSpatieLaravelData, Version: &runtimeVersion},
		{Name: "laravel/framework"},
	})
	if err != nil {
		t.Fatalf("DetectInstalledPackages() error = %v", err)
	}

	gotNames := make([]string, 0, len(detected))
	gotByName := make(map[string]DetectedPackage, len(detected))
	for _, pkg := range detected {
		gotNames = append(gotNames, pkg.Name)
		gotByName[pkg.Name] = pkg
	}

	wantNames := []string{
		PackageSpatieLaravelData,
		PackageSpatieLaravelPermission,
		PackageSpatieLaravelQueryBuilder,
		PackageSpatieLaravelTranslatable,
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("detected package names = %#v, want %#v", gotNames, wantNames)
	}

	if gotByName[PackageSpatieLaravelData].Source != "runtime" || gotByName[PackageSpatieLaravelData].Version != runtimeVersion {
		t.Fatalf("runtime package = %#v, want source=runtime version=%q", gotByName[PackageSpatieLaravelData], runtimeVersion)
	}
	if gotByName[PackageSpatieLaravelQueryBuilder].Source != "composer.lock" || gotByName[PackageSpatieLaravelQueryBuilder].Version != "6.3.0" {
		t.Fatalf("composer.lock package = %#v, want source=composer.lock version=6.3.0", gotByName[PackageSpatieLaravelQueryBuilder])
	}
	if gotByName[PackageSpatieLaravelPermission].Source != "composer.json" || gotByName[PackageSpatieLaravelPermission].Version != "^6.0" {
		t.Fatalf("composer.json require package = %#v, want source=composer.json version=^6.0", gotByName[PackageSpatieLaravelPermission])
	}
}

func TestMergeVendorWhitelistAddsDetectedPackagesDeterministically(t *testing.T) {
	manifestData := &manifest.Manifest{
		Scan: manifest.ScanConfig{
			VendorWhitelist: []string{
				"laravel/framework",
				PackageSpatieLaravelData,
			},
		},
	}

	MergeVendorWhitelist(manifestData, []DetectedPackage{
		{Name: PackageSpatieLaravelQueryBuilder},
		{Name: PackageSpatieLaravelData},
	})

	want := []string{
		"laravel/framework",
		PackageSpatieLaravelData,
		PackageSpatieLaravelQueryBuilder,
	}
	if !reflect.DeepEqual(manifestData.Scan.VendorWhitelist, want) {
		t.Fatalf("vendor whitelist = %#v, want %#v", manifestData.Scan.VendorWhitelist, want)
	}
}

func TestDetectRuntimeActionPackagesUsesComposerLockPSR4Mappings(t *testing.T) {
	projectRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectRoot, "composer.lock"), []byte(`{
  "packages": [
    {
      "name": "acme/core",
      "version": "1.0.0",
      "autoload": {
        "psr-4": {
          "Acme\\": "src/"
        }
      }
    },
    {
      "name": "acme/laravel-auth",
      "version": "2.4.0",
      "autoload": {
        "psr-4": {
          "Acme\\Laravel\\": [
            "src/"
          ]
        }
      }
    }
  ],
  "packages-dev": []
}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile(composer.lock) error = %v", err)
	}

	routes := []contracts.RuntimeRoute{
		{
			RouteID: "auth.callback",
			Action: contracts.RouteAction{
				Kind: contracts.ActionKindInvokableController,
				FQCN: stringPtr("Acme\\Laravel\\Controllers\\CallbackController"),
				Method: stringPtr("__invoke"),
			},
		},
		{
			RouteID: "auth.profile",
			Action: contracts.RouteAction{
				Kind: contracts.ActionKindControllerMethod,
				FQCN: stringPtr("Acme\\Controllers\\ProfileController"),
				Method: stringPtr("show"),
			},
		},
	}

	detected, err := DetectRuntimeActionPackages(projectRoot, routes)
	if err != nil {
		t.Fatalf("DetectRuntimeActionPackages() error = %v", err)
	}

	want := []DetectedPackage{
		{Name: "acme/core", Version: "1.0.0", Source: "runtime.route_action"},
		{Name: "acme/laravel-auth", Version: "2.4.0", Source: "runtime.route_action"},
	}
	if !reflect.DeepEqual(detected, want) {
		t.Fatalf("detected runtime action packages = %#v, want %#v", detected, want)
	}
}

func TestEnsureVendorScanTargetsAddsVendorWhenWhitelistPresent(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, "vendor"), 0o755); err != nil {
		t.Fatalf("os.Mkdir(vendor) error = %v", err)
	}

	manifestData := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: projectRoot,
		},
		Scan: manifest.ScanConfig{
			Targets:         []string{"app", "routes"},
			VendorWhitelist: []string{"auth0/login"},
		},
	}

	EnsureVendorScanTargets(manifestData)

	want := []string{"app", "routes", "vendor"}
	if !reflect.DeepEqual(manifestData.Scan.Targets, want) {
		t.Fatalf("targets = %#v, want %#v", manifestData.Scan.Targets, want)
	}

	EnsureVendorScanTargets(manifestData)
	if !reflect.DeepEqual(manifestData.Scan.Targets, want) {
		t.Fatalf("targets after second call = %#v, want %#v", manifestData.Scan.Targets, want)
	}
}

func TestEnsureVendorScanTargetsSkipsMissingVendorDirectory(t *testing.T) {
	projectRoot := t.TempDir()
	manifestData := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: projectRoot,
		},
		Scan: manifest.ScanConfig{
			Targets:         []string{"app", "routes"},
			VendorWhitelist: []string{"auth0/login"},
		},
	}

	EnsureVendorScanTargets(manifestData)

	want := []string{"app", "routes"}
	if !reflect.DeepEqual(manifestData.Scan.Targets, want) {
		t.Fatalf("targets = %#v, want %#v", manifestData.Scan.Targets, want)
	}
}

func stringPtr(value string) *string {
	return &value
}

package manifest

import (
	"testing"
)

func TestManifest_StructFields(t *testing.T) {
	// Test that the Manifest struct has the expected fields and can be initialized
	manifest := &Manifest{
		Project: ProjectConfig{
			Root:     "/test/project",
			Composer: "composer.json",
		},
		Scan: ScanConfig{
			Targets:         []string{"app/", "routes/"},
			VendorWhitelist: []string{"laravel/framework"},
			Globs:           []string{"**/*.php"},
		},
		Limits: &LimitsConfig{
			MaxFiles:    intPtr(5000),
			MaxFileSize: intPtr(1048576),
			Timeout:     intPtr(300),
		},
		Cache: &CacheConfig{
			Enabled: true,
			Dir:     ".oxinfer/cache",
			TTL:     intPtr(86400),
		},
		Features: &FeatureConfig{
			Routes:      boolPtr(true),
			Controllers: boolPtr(true),
			Models:      boolPtr(false),
			Middleware:  boolPtr(true),
			Migrations:  boolPtr(true),
			Policies:    boolPtr(false),
			Events:      boolPtr(true),
			Jobs:        boolPtr(false),
		},
	}

	// Test Project fields
	if manifest.Project.Root != "/test/project" {
		t.Errorf("expected root '/test/project', got %q", manifest.Project.Root)
	}
	if manifest.Project.Composer != "composer.json" {
		t.Errorf("expected composer 'composer.json', got %q", manifest.Project.Composer)
	}

	// Test Scan fields
	if len(manifest.Scan.Targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(manifest.Scan.Targets))
	}
	if manifest.Scan.Targets[0] != "app/" {
		t.Errorf("expected first target 'app/', got %q", manifest.Scan.Targets[0])
	}
	if len(manifest.Scan.VendorWhitelist) != 1 {
		t.Errorf("expected 1 vendor whitelist item, got %d", len(manifest.Scan.VendorWhitelist))
	}
	if len(manifest.Scan.Globs) != 1 {
		t.Errorf("expected 1 glob pattern, got %d", len(manifest.Scan.Globs))
	}

	// Test Limits fields
	if manifest.Limits == nil {
		t.Fatal("expected Limits to be set")
	}
	if manifest.Limits.MaxFiles == nil || *manifest.Limits.MaxFiles != 5000 {
		t.Errorf("expected MaxFiles 5000, got %v", manifest.Limits.MaxFiles)
	}
	if manifest.Limits.MaxFileSize == nil || *manifest.Limits.MaxFileSize != 1048576 {
		t.Errorf("expected MaxFileSize 1048576, got %v", manifest.Limits.MaxFileSize)
	}
	if manifest.Limits.Timeout == nil || *manifest.Limits.Timeout != 300 {
		t.Errorf("expected Timeout 300, got %v", manifest.Limits.Timeout)
	}

	// Test Cache fields
	if manifest.Cache == nil {
		t.Fatal("expected Cache to be set")
	}
	if !manifest.Cache.Enabled {
		t.Error("expected Cache.Enabled to be true")
	}
	if manifest.Cache.Dir != ".oxinfer/cache" {
		t.Errorf("expected Cache.Dir '.oxinfer/cache', got %q", manifest.Cache.Dir)
	}
	if manifest.Cache.TTL == nil || *manifest.Cache.TTL != 86400 {
		t.Errorf("expected Cache.TTL 86400, got %v", manifest.Cache.TTL)
	}

	// Test Features fields
	if manifest.Features == nil {
		t.Fatal("expected Features to be set")
	}
	if manifest.Features.Routes == nil || !*manifest.Features.Routes {
		t.Error("expected Features.Routes to be true")
	}
	if manifest.Features.Models == nil || *manifest.Features.Models {
		t.Error("expected Features.Models to be false")
	}
	if manifest.Features.Controllers == nil || !*manifest.Features.Controllers {
		t.Error("expected Features.Controllers to be true")
	}
}

func TestProjectConfig(t *testing.T) {
	project := ProjectConfig{
		Root:     "/path/to/project",
		Composer: "custom-composer.json",
	}

	if project.Root != "/path/to/project" {
		t.Errorf("expected root '/path/to/project', got %q", project.Root)
	}
	if project.Composer != "custom-composer.json" {
		t.Errorf("expected composer 'custom-composer.json', got %q", project.Composer)
	}
}

func TestScanConfig(t *testing.T) {
	scan := ScanConfig{
		Targets:         []string{"app/", "src/", "lib/"},
		VendorWhitelist: []string{"laravel/framework", "symfony/console"},
		Globs:           []string{"**/*.php", "**/*.blade.php"},
	}

	if len(scan.Targets) != 3 {
		t.Errorf("expected 3 targets, got %d", len(scan.Targets))
	}
	if scan.Targets[2] != "lib/" {
		t.Errorf("expected third target 'lib/', got %q", scan.Targets[2])
	}

	if len(scan.VendorWhitelist) != 2 {
		t.Errorf("expected 2 vendor whitelist items, got %d", len(scan.VendorWhitelist))
	}
	if scan.VendorWhitelist[1] != "symfony/console" {
		t.Errorf("expected second vendor 'symfony/console', got %q", scan.VendorWhitelist[1])
	}

	if len(scan.Globs) != 2 {
		t.Errorf("expected 2 glob patterns, got %d", len(scan.Globs))
	}
	if scan.Globs[1] != "**/*.blade.php" {
		t.Errorf("expected second glob '**/*.blade.php', got %q", scan.Globs[1])
	}
}

func TestLimitsConfig(t *testing.T) {
	limits := &LimitsConfig{
		MaxFiles:    intPtr(1000),
		MaxFileSize: intPtr(2097152),
		Timeout:     intPtr(600),
	}

	if limits.MaxFiles == nil || *limits.MaxFiles != 1000 {
		t.Errorf("expected MaxFiles 1000, got %v", limits.MaxFiles)
	}
	if limits.MaxFileSize == nil || *limits.MaxFileSize != 2097152 {
		t.Errorf("expected MaxFileSize 2097152, got %v", limits.MaxFileSize)
	}
	if limits.Timeout == nil || *limits.Timeout != 600 {
		t.Errorf("expected Timeout 600, got %v", limits.Timeout)
	}
}

func TestLimitsConfig_NilValues(t *testing.T) {
	limits := &LimitsConfig{}

	if limits.MaxFiles != nil {
		t.Error("expected MaxFiles to be nil by default")
	}
	if limits.MaxFileSize != nil {
		t.Error("expected MaxFileSize to be nil by default")
	}
	if limits.Timeout != nil {
		t.Error("expected Timeout to be nil by default")
	}
}

func TestCacheConfig(t *testing.T) {
	cache := &CacheConfig{
		Enabled: false,
		Dir:     "/custom/cache",
		TTL:     intPtr(3600),
	}

	if cache.Enabled {
		t.Error("expected Enabled to be false")
	}
	if cache.Dir != "/custom/cache" {
		t.Errorf("expected Dir '/custom/cache', got %q", cache.Dir)
	}
	if cache.TTL == nil || *cache.TTL != 3600 {
		t.Errorf("expected TTL 3600, got %v", cache.TTL)
	}
}

func TestCacheConfig_DefaultValues(t *testing.T) {
	cache := &CacheConfig{
		Enabled: true,
	}

	if !cache.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cache.Dir != "" {
		t.Errorf("expected empty Dir by default, got %q", cache.Dir)
	}
	if cache.TTL != nil {
		t.Errorf("expected TTL to be nil by default, got %v", cache.TTL)
	}
}

func TestFeatureConfig(t *testing.T) {
	features := &FeatureConfig{
		Routes:      boolPtr(true),
		Controllers: boolPtr(false),
		Models:      boolPtr(true),
		Middleware:  boolPtr(false),
		Migrations:  boolPtr(true),
		Policies:    boolPtr(false),
		Events:      boolPtr(true),
		Jobs:        boolPtr(false),
	}

	if features.Routes == nil || !*features.Routes {
		t.Error("expected Routes to be true")
	}
	if features.Controllers == nil || *features.Controllers {
		t.Error("expected Controllers to be false")
	}
	if features.Models == nil || !*features.Models {
		t.Error("expected Models to be true")
	}
	if features.Middleware == nil || *features.Middleware {
		t.Error("expected Middleware to be false")
	}
	if features.Migrations == nil || !*features.Migrations {
		t.Error("expected Migrations to be true")
	}
	if features.Policies == nil || *features.Policies {
		t.Error("expected Policies to be false")
	}
	if features.Events == nil || !*features.Events {
		t.Error("expected Events to be true")
	}
	if features.Jobs == nil || *features.Jobs {
		t.Error("expected Jobs to be false")
	}
}

func TestFeatureConfig_NilValues(t *testing.T) {
	features := &FeatureConfig{}

	if features.Routes != nil {
		t.Error("expected Routes to be nil by default")
	}
	if features.Controllers != nil {
		t.Error("expected Controllers to be nil by default")
	}
	if features.Models != nil {
		t.Error("expected Models to be nil by default")
	}
	if features.Middleware != nil {
		t.Error("expected Middleware to be nil by default")
	}
	if features.Migrations != nil {
		t.Error("expected Migrations to be nil by default")
	}
	if features.Policies != nil {
		t.Error("expected Policies to be nil by default")
	}
	if features.Events != nil {
		t.Error("expected Events to be nil by default")
	}
	if features.Jobs != nil {
		t.Error("expected Jobs to be nil by default")
	}
}

func TestManifest_ZeroValue(t *testing.T) {
	var manifest Manifest

	// Zero value manifest should have empty/nil fields
	if manifest.Project.Root != "" {
		t.Error("zero value manifest should have empty root")
	}
	if manifest.Project.Composer != "" {
		t.Error("zero value manifest should have empty composer")
	}

	if len(manifest.Scan.Targets) != 0 {
		t.Error("zero value manifest should have empty targets")
	}
	if len(manifest.Scan.VendorWhitelist) != 0 {
		t.Error("zero value manifest should have empty vendor whitelist")
	}
	if len(manifest.Scan.Globs) != 0 {
		t.Error("zero value manifest should have empty globs")
	}

	if manifest.Limits != nil {
		t.Error("zero value manifest should have nil limits")
	}
	if manifest.Cache != nil {
		t.Error("zero value manifest should have nil cache")
	}
	if manifest.Features != nil {
		t.Error("zero value manifest should have nil features")
	}
}

func TestManifest_MinimalValid(t *testing.T) {
	// Test a minimal valid manifest (matching schema requirements)
	manifest := &Manifest{
		Project: ProjectConfig{
			Root:     "/test/project",
			Composer: "composer.json",
		},
		Scan: ScanConfig{
			Targets: []string{"app/"},
		},
	}

	if manifest.Project.Root != "/test/project" {
		t.Error("minimal manifest should preserve root")
	}
	if manifest.Project.Composer != "composer.json" {
		t.Error("minimal manifest should preserve composer")
	}
	if len(manifest.Scan.Targets) != 1 || manifest.Scan.Targets[0] != "app/" {
		t.Error("minimal manifest should preserve targets")
	}

	// Optional fields should be nil/empty
	if manifest.Limits != nil {
		t.Error("minimal manifest should have nil limits")
	}
	if manifest.Cache != nil {
		t.Error("minimal manifest should have nil cache")
	}
	if manifest.Features != nil {
		t.Error("minimal manifest should have nil features")
	}
}

func TestApplyDefaults_Function(t *testing.T) {
	// Test that the applyDefaults function (not method) works correctly
	manifest := &Manifest{
		Project: ProjectConfig{
			Root: "/test/project",
		},
		Scan: ScanConfig{
			Targets: []string{"app/"},
		},
	}

	// Call the internal applyDefaults function
	applyDefaults(manifest)

	// Check that defaults were applied
	if manifest.Project.Composer != "composer.json" {
		t.Errorf("expected default composer 'composer.json', got %q", manifest.Project.Composer)
	}
	if len(manifest.Scan.Globs) == 0 {
		t.Error("expected default globs to be applied")
	}
	if len(manifest.Scan.Globs) > 0 && manifest.Scan.Globs[0] != "**/*.php" {
		t.Errorf("expected default glob '**/*.php', got %q", manifest.Scan.Globs[0])
	}
}

func TestApplyDefaults_WithLimits(t *testing.T) {
	manifest := &Manifest{
		Project: ProjectConfig{
			Root:     "/test/project",
			Composer: "composer.json",
		},
		Scan: ScanConfig{
			Targets: []string{"app/"},
		},
		Limits: &LimitsConfig{}, // Empty limits config
	}

	applyDefaults(manifest)

	// Check that limits defaults were applied
	if manifest.Limits.MaxFiles == nil {
		t.Error("expected MaxFiles default to be applied")
	} else if *manifest.Limits.MaxFiles != 10000 {
		t.Errorf("expected default MaxFiles 10000, got %d", *manifest.Limits.MaxFiles)
	}

	if manifest.Limits.MaxFileSize == nil {
		t.Error("expected MaxFileSize default to be applied")
	} else if *manifest.Limits.MaxFileSize != 5242880 {
		t.Errorf("expected default MaxFileSize 5242880, got %d", *manifest.Limits.MaxFileSize)
	}

	if manifest.Limits.Timeout == nil {
		t.Error("expected Timeout default to be applied")
	} else if *manifest.Limits.Timeout != 300 {
		t.Errorf("expected default Timeout 300, got %d", *manifest.Limits.Timeout)
	}
}

func TestApplyDefaults_WithCache(t *testing.T) {
	manifest := &Manifest{
		Project: ProjectConfig{
			Root:     "/test/project",
			Composer: "composer.json",
		},
		Scan: ScanConfig{
			Targets: []string{"app/"},
		},
		Cache: &CacheConfig{
			Enabled: true,
		},
	}

	applyDefaults(manifest)

	// Check that cache defaults were applied
	if manifest.Cache.Dir != ".oxinfer/cache" {
		t.Errorf("expected default cache dir '.oxinfer/cache', got %q", manifest.Cache.Dir)
	}

	if manifest.Cache.TTL == nil {
		t.Error("expected TTL default to be applied")
	} else if *manifest.Cache.TTL != 86400 {
		t.Errorf("expected default TTL 86400, got %d", *manifest.Cache.TTL)
	}
}

func TestApplyDefaults_WithFeatures(t *testing.T) {
	manifest := &Manifest{
		Project: ProjectConfig{
			Root:     "/test/project",
			Composer: "composer.json",
		},
		Scan: ScanConfig{
			Targets: []string{"app/"},
		},
		Features: &FeatureConfig{}, // Empty features config
	}

	applyDefaults(manifest)

	// Check that all features defaults were applied (all should be true)
	if manifest.Features.Routes == nil || !*manifest.Features.Routes {
		t.Error("expected Routes default to be true")
	}
	if manifest.Features.Controllers == nil || !*manifest.Features.Controllers {
		t.Error("expected Controllers default to be true")
	}
	if manifest.Features.Models == nil || !*manifest.Features.Models {
		t.Error("expected Models default to be true")
	}
	if manifest.Features.Middleware == nil || !*manifest.Features.Middleware {
		t.Error("expected Middleware default to be true")
	}
	if manifest.Features.Migrations == nil || !*manifest.Features.Migrations {
		t.Error("expected Migrations default to be true")
	}
	if manifest.Features.Policies == nil || !*manifest.Features.Policies {
		t.Error("expected Policies default to be true")
	}
	if manifest.Features.Events == nil || !*manifest.Features.Events {
		t.Error("expected Events default to be true")
	}
	if manifest.Features.Jobs == nil || !*manifest.Features.Jobs {
		t.Error("expected Jobs default to be true")
	}
}

// Helper functions for creating pointers to basic types
func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
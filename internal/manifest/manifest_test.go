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
			MaxWorkers: intPtr(4),
			MaxFiles:   intPtr(5000),
			MaxDepth:   intPtr(3),
		},
		Cache: &CacheConfig{
			Enabled: boolPtr(true),
			Kind:    stringPtr("sha256+mtime"),
		},
		Features: &FeatureConfig{
			HTTPStatus:        boolPtr(true),
			RequestUsage:      boolPtr(true),
			ResourceUsage:     boolPtr(false),
			WithPivot:         boolPtr(true),
			AttributeMake:     boolPtr(true),
			ScopesUsed:        boolPtr(false),
			Polymorphic:       boolPtr(true),
			BroadcastChannels: boolPtr(false),
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
	if manifest.Limits.MaxWorkers == nil || *manifest.Limits.MaxWorkers != 4 {
		t.Errorf("expected MaxWorkers 4, got %v", manifest.Limits.MaxWorkers)
	}
	if manifest.Limits.MaxDepth == nil || *manifest.Limits.MaxDepth != 3 {
		t.Errorf("expected MaxDepth 3, got %v", manifest.Limits.MaxDepth)
	}

	// Test Cache fields
	if manifest.Cache == nil {
		t.Fatal("expected Cache to be set")
	}
	if manifest.Cache.Enabled == nil || !*manifest.Cache.Enabled {
		t.Error("expected Cache.Enabled to be true")
	}
	if manifest.Cache.Kind == nil || *manifest.Cache.Kind != "sha256+mtime" {
		t.Errorf("expected Cache.Kind 'sha256+mtime', got %v", manifest.Cache.Kind)
	}

	// Test Features fields
	if manifest.Features == nil {
		t.Fatal("expected Features to be set")
	}
	if manifest.Features.HTTPStatus == nil || !*manifest.Features.HTTPStatus {
		t.Error("expected Features.HTTPStatus to be true")
	}
	if manifest.Features.ResourceUsage == nil || *manifest.Features.ResourceUsage {
		t.Error("expected Features.ResourceUsage to be false")
	}
	if manifest.Features.RequestUsage == nil || !*manifest.Features.RequestUsage {
		t.Error("expected Features.RequestUsage to be true")
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
		MaxWorkers: intPtr(4),
		MaxFiles:   intPtr(1000),
		MaxDepth:   intPtr(5),
	}

	if limits.MaxWorkers == nil || *limits.MaxWorkers != 4 {
		t.Errorf("expected MaxWorkers 4, got %v", limits.MaxWorkers)
	}
	if limits.MaxFiles == nil || *limits.MaxFiles != 1000 {
		t.Errorf("expected MaxFiles 1000, got %v", limits.MaxFiles)
	}
	if limits.MaxDepth == nil || *limits.MaxDepth != 5 {
		t.Errorf("expected MaxDepth 5, got %v", limits.MaxDepth)
	}
}

func TestLimitsConfig_NilValues(t *testing.T) {
	limits := &LimitsConfig{}

	if limits.MaxWorkers != nil {
		t.Error("expected MaxWorkers to be nil by default")
	}
	if limits.MaxFiles != nil {
		t.Error("expected MaxFiles to be nil by default")
	}
	if limits.MaxDepth != nil {
		t.Error("expected MaxDepth to be nil by default")
	}
}

func TestCacheConfig(t *testing.T) {
	cache := &CacheConfig{
		Enabled: boolPtr(false),
		Kind:    stringPtr("mtime"),
	}

	if cache.Enabled == nil || *cache.Enabled {
		t.Error("expected Enabled to be false")
	}
	if cache.Kind == nil || *cache.Kind != "mtime" {
		t.Errorf("expected Kind 'mtime', got %v", cache.Kind)
	}
}

func TestCacheConfig_DefaultValues(t *testing.T) {
	cache := &CacheConfig{
		Enabled: boolPtr(true),
	}

	if cache.Enabled == nil || !*cache.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cache.Kind != nil {
		t.Errorf("expected Kind to be nil by default, got %v", cache.Kind)
	}
}

func TestFeatureConfig(t *testing.T) {
	features := &FeatureConfig{
		HTTPStatus:        boolPtr(true),
		RequestUsage:      boolPtr(false),
		ResourceUsage:     boolPtr(true),
		WithPivot:         boolPtr(false),
		AttributeMake:     boolPtr(true),
		ScopesUsed:        boolPtr(false),
		Polymorphic:       boolPtr(true),
		BroadcastChannels: boolPtr(false),
	}

	if features.HTTPStatus == nil || !*features.HTTPStatus {
		t.Error("expected HTTPStatus to be true")
	}
	if features.RequestUsage == nil || *features.RequestUsage {
		t.Error("expected RequestUsage to be false")
	}
	if features.ResourceUsage == nil || !*features.ResourceUsage {
		t.Error("expected ResourceUsage to be true")
	}
	if features.WithPivot == nil || *features.WithPivot {
		t.Error("expected WithPivot to be false")
	}
	if features.AttributeMake == nil || !*features.AttributeMake {
		t.Error("expected AttributeMake to be true")
	}
	if features.ScopesUsed == nil || *features.ScopesUsed {
		t.Error("expected ScopesUsed to be false")
	}
	if features.Polymorphic == nil || !*features.Polymorphic {
		t.Error("expected Polymorphic to be true")
	}
	if features.BroadcastChannels == nil || *features.BroadcastChannels {
		t.Error("expected BroadcastChannels to be false")
	}
}

func TestFeatureConfig_NilValues(t *testing.T) {
	features := &FeatureConfig{}

	if features.HTTPStatus != nil {
		t.Error("expected HTTPStatus to be nil by default")
	}
	if features.RequestUsage != nil {
		t.Error("expected RequestUsage to be nil by default")
	}
	if features.ResourceUsage != nil {
		t.Error("expected ResourceUsage to be nil by default")
	}
	if features.WithPivot != nil {
		t.Error("expected WithPivot to be nil by default")
	}
	if features.AttributeMake != nil {
		t.Error("expected AttributeMake to be nil by default")
	}
	if features.ScopesUsed != nil {
		t.Error("expected ScopesUsed to be nil by default")
	}
	if features.Polymorphic != nil {
		t.Error("expected Polymorphic to be nil by default")
	}
	if features.BroadcastChannels != nil {
		t.Error("expected BroadcastChannels to be nil by default")
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
	if len(manifest.Scan.Globs) > 0 && manifest.Scan.Globs[0] != "app/**/*.php" {
		t.Errorf("expected default glob 'app/**/*.php', got %q", manifest.Scan.Globs[0])
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
	if manifest.Limits.MaxWorkers == nil {
		t.Error("expected MaxWorkers default to be applied")
	} else if *manifest.Limits.MaxWorkers != 8 {
		t.Errorf("expected default MaxWorkers 8, got %d", *manifest.Limits.MaxWorkers)
	}

	if manifest.Limits.MaxFiles == nil {
		t.Error("expected MaxFiles default to be applied")
	} else if *manifest.Limits.MaxFiles != 500 {
		t.Errorf("expected default MaxFiles 500, got %d", *manifest.Limits.MaxFiles)
	}

	if manifest.Limits.MaxDepth == nil {
		t.Error("expected MaxDepth default to be applied")
	} else if *manifest.Limits.MaxDepth != 2 {
		t.Errorf("expected default MaxDepth 2, got %d", *manifest.Limits.MaxDepth)
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
			Enabled: boolPtr(true),
		},
	}

	applyDefaults(manifest)

	// Check that cache defaults were applied
	if manifest.Cache.Kind == nil {
		t.Error("expected Kind default to be applied")
	} else if *manifest.Cache.Kind != "sha256+mtime" {
		t.Errorf("expected default Kind 'sha256+mtime', got %s", *manifest.Cache.Kind)
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
	if manifest.Features.HTTPStatus == nil || !*manifest.Features.HTTPStatus {
		t.Error("expected HTTPStatus default to be true")
	}
	if manifest.Features.RequestUsage == nil || !*manifest.Features.RequestUsage {
		t.Error("expected RequestUsage default to be true")
	}
	if manifest.Features.ResourceUsage == nil || !*manifest.Features.ResourceUsage {
		t.Error("expected ResourceUsage default to be true")
	}
	if manifest.Features.WithPivot == nil || !*manifest.Features.WithPivot {
		t.Error("expected WithPivot default to be true")
	}
	if manifest.Features.AttributeMake == nil || !*manifest.Features.AttributeMake {
		t.Error("expected AttributeMake default to be true")
	}
	if manifest.Features.ScopesUsed == nil || !*manifest.Features.ScopesUsed {
		t.Error("expected ScopesUsed default to be true")
	}
	if manifest.Features.Polymorphic == nil || !*manifest.Features.Polymorphic {
		t.Error("expected Polymorphic default to be true")
	}
	if manifest.Features.BroadcastChannels == nil || !*manifest.Features.BroadcastChannels {
		t.Error("expected BroadcastChannels default to be true")
	}
}

// Helper functions for creating pointers to basic types
func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}
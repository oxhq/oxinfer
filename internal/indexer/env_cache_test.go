package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/manifest"
)

// Test that DefaultFileIndexer.LoadFromManifest respects OXINFER_CACHE_DIR
// and persists cache entries to the specified directory.
func TestDefaultFileIndexer_UsesEnvCacheDir(t *testing.T) {
	t.Parallel()

	// Project root with a minimal app structure
	projectDir := t.TempDir()
	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}

	// Create a simple PHP file to index
	phpFile := filepath.Join(appDir, "EnvCacheTest.php")
	if err := os.WriteFile(phpFile, []byte("<?php // test file"), 0o644); err != nil {
		t.Fatalf("write php file: %v", err)
	}

	// Target cache directory via env var
	envCacheRoot := t.TempDir()
	envCacheDir := filepath.Join(envCacheRoot, "cachedir")
	oldEnv := os.Getenv("OXINFER_CACHE_DIR")
	t.Cleanup(func() { _ = os.Setenv("OXINFER_CACHE_DIR", oldEnv) })
	if err := os.Setenv("OXINFER_CACHE_DIR", envCacheDir); err != nil {
		t.Fatalf("set env: %v", err)
	}

	// Build manifest (composer.json intentionally absent in tests)
	man := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectDir,
			Composer: "", // triggers fallback project key hashing in indexer
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
		Cache: &manifest.CacheConfig{ // enable cache explicitly
			Enabled: boolPtr(true),
			Kind:    stringPtr(CacheModeModTime),
		},
	}

	idx := NewDefaultFileIndexer()
	if err := idx.LoadFromManifest(man); err != nil {
		t.Fatalf("LoadFromManifest: %v", err)
	}

	// Run indexing to exercise caching
	res, err := idx.IndexFiles(context.Background(), IndexConfig{})
	if err != nil {
		t.Fatalf("IndexFiles: %v", err)
	}
	if res == nil || len(res.Files) == 0 {
		t.Fatalf("expected >=1 indexed file, got: %+v", res)
	}

	// Allow async persistence goroutine to flush
	time.Sleep(50 * time.Millisecond)

	// Verify that cache index file exists under env-provided cache dir
	indexPath := filepath.Join(envCacheDir, "index.json")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected cache index at %s (env precedence), got err: %v", indexPath, err)
	}

	// Verify files directory exists
	if _, err := os.Stat(filepath.Join(envCacheDir, "files")); err != nil {
		t.Fatalf("expected cache files dir under %s, err: %v", envCacheDir, err)
	}
}

// Helpers are provided in indexer_test.go (boolPtr, stringPtr)

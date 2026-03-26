package indexer

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIndexFiles_AppliesVendorWhitelistFromExplicitConfig(t *testing.T) {
	projectRoot := t.TempDir()

	dirs := []string{
		filepath.Join(projectRoot, "app", "Http", "Controllers"),
		filepath.Join(projectRoot, "vendor", "auth0", "login", "src", "Controllers"),
		filepath.Join(projectRoot, "vendor", "spatie", "laravel-data", "src"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("os.MkdirAll(%s) error = %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(projectRoot, "app", "Http", "Controllers", "UserController.php"): `<?php class UserController {}`,
		filepath.Join(projectRoot, "vendor", "auth0", "login", "src", "Controllers", "LoginController.php"): `<?php class LoginController {}`,
		filepath.Join(projectRoot, "vendor", "spatie", "laravel-data", "src", "Data.php"): `<?php class Data {}`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", path, err)
		}
	}

	indexer := NewDefaultFileIndexer()
	result, err := indexer.IndexFiles(context.Background(), IndexConfig{
		Targets:         []string{"app", "vendor"},
		Globs:           []string{"**/*.php"},
		BaseDir:         projectRoot,
		MaxWorkers:      1,
		MaxFiles:        100,
		CacheEnabled:    false,
		VendorWhitelist: []string{"auth0/login"},
	})
	if err != nil {
		t.Fatalf("IndexFiles() error = %v", err)
	}

	var got []string
	for _, file := range result.Files {
		got = append(got, file.Path)
	}

	want := []string{
		"app/Http/Controllers/UserController.php",
		"vendor/auth0/login/src/Controllers/LoginController.php",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed files = %#v, want %#v", got, want)
	}
}

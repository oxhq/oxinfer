package parser

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLaravelParser_InheritsControllerMethodsFromParent(t *testing.T) {
	projectRoot := t.TempDir()
	controllerDir := filepath.Join(projectRoot, "vendor", "auth0", "login", "src", "Controllers")
	if err := os.MkdirAll(controllerDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	files := map[string]string{
		filepath.Join(controllerDir, "LoginController.php"): `<?php
namespace Auth0\Laravel\Controllers;

final class LoginController extends LoginControllerAbstract
{
}
`,
		filepath.Join(controllerDir, "LoginControllerAbstract.php"): `<?php
namespace Auth0\Laravel\Controllers;

abstract class LoginControllerAbstract extends ControllerAbstract
{
    public function __invoke($request)
    {
        return redirect('/login');
    }
}
`,
		filepath.Join(controllerDir, "ControllerAbstract.php"): `<?php
namespace Auth0\Laravel\Controllers;

abstract class ControllerAbstract
{
}
`,
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", path, err)
		}
	}

	parser, err := NewLaravelParser(false)
	if err != nil {
		t.Fatalf("NewLaravelParser() error = %v", err)
	}

	if _, err := parser.ParseFile(context.Background(), filepath.Join(controllerDir, "LoginController.php")); err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	got := parser.GetControllers()["Auth0\\Laravel\\Controllers\\LoginController"]
	want := []string{"__invoke"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controller methods = %#v, want %#v", got, want)
	}
}

func TestLaravelParser_InheritsControllerMethodsRecursively(t *testing.T) {
	projectRoot := t.TempDir()
	controllerDir := filepath.Join(projectRoot, "app", "Http", "Controllers")
	if err := os.MkdirAll(controllerDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	files := map[string]string{
		filepath.Join(controllerDir, "ThinController.php"): `<?php
namespace App\Http\Controllers;

final class ThinController extends ThinControllerAbstract
{
}
`,
		filepath.Join(controllerDir, "ThinControllerAbstract.php"): `<?php
namespace App\Http\Controllers;

abstract class ThinControllerAbstract extends BaseController
{
}
`,
		filepath.Join(controllerDir, "BaseController.php"): `<?php
namespace App\Http\Controllers;

abstract class BaseController
{
    public function __invoke()
    {
        return response()->json(['ok' => true]);
    }
}
`,
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) error = %v", path, err)
		}
	}

	parser, err := NewLaravelParser(false)
	if err != nil {
		t.Fatalf("NewLaravelParser() error = %v", err)
	}

	if _, err := parser.ParseFile(context.Background(), filepath.Join(controllerDir, "ThinController.php")); err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	got := parser.GetControllers()["App\\Http\\Controllers\\ThinController"]
	want := []string{"__invoke"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controller methods = %#v, want %#v", got, want)
	}
}

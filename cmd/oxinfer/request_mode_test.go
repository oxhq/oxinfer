package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRequestModeGoldenPairs(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		goldenPath  string
	}{
		{
			name:        "matched invokable",
			requestPath: "../../test/fixtures/contracts/request-mode/matched-invokable.json",
			goldenPath:  "../../test/golden/request-mode/matched-invokable.json",
		},
		{
			name:        "missing static",
			requestPath: "../../test/fixtures/contracts/request-mode/missing-static.json",
			goldenPath:  "../../test/golden/request-mode/missing-static.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestBytes := readTestFile(t, tt.requestPath)
			expectedBytes := readTestFile(t, tt.goldenPath)

			requestFile := writeTempFile(t, "analysis-request-*.json", requestBytes)
			exitCode, stdout, stderr := runWithCapturedOutput(t, []string{"--request", requestFile})
			if exitCode != 0 {
				t.Fatalf("run() exitCode = %v, want 0\nstderr=%s", exitCode, stderr)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}

			got := strings.TrimSpace(stdout)
			want := strings.TrimSpace(string(expectedBytes))
			if got != want {
				t.Fatalf("request mode output mismatch\nwant=%s\ngot=%s", want, got)
			}
		})
	}
}

func TestRunRequestModeRejectsInvalidSchema(t *testing.T) {
	tempDir := t.TempDir()
	requestPath := filepath.Join(tempDir, "invalid-request.json")
	if err := os.WriteFile(requestPath, []byte(`{"contractVersion":"oxcribe.oxinfer.v2","requestId":"bad","runtimeFingerprint":"fp","manifest":{},"runtime":{"app":{"basePath":"/tmp","laravelVersion":"12","phpVersion":"8.3","appEnv":"testing"},"routes":[]},"extra":true}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	exitCode, stdout, stderr := runWithCapturedOutput(t, []string{"--request", requestPath})
	if exitCode != 1 {
		t.Fatalf("run() exitCode = %v, want 1", exitCode)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"exit_code":1`) {
		t.Fatalf("stderr = %q, want JSON input error", stderr)
	}
}

func runWithCapturedOutput(t *testing.T, args []string) (int, string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout) error = %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr) error = %v", err)
	}

	os.Stdout = wOut
	os.Stderr = wErr

	exitCode := run(args)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var stdoutBuf, stderrBuf bytes.Buffer
	_, _ = stdoutBuf.ReadFrom(rOut)
	_, _ = stderrBuf.ReadFrom(rErr)

	return int(exitCode), strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String())
}

func readTestFile(t *testing.T, relPath string) []byte {
	t.Helper()

	data, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", relPath, err)
	}
	return data
}

func writeTempFile(t *testing.T, pattern string, data []byte) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := file.Write(data); err != nil {
		t.Fatalf("file.Write() error = %v", err)
	}

	return file.Name()
}

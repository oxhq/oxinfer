package psr4

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Compile-time interface check
var _ PathResolver = (*pathResolver)(nil)

// pathResolver implements the PathResolver interface for filesystem operations.
// It handles path resolution, file existence checking, and security validation.
type pathResolver struct {
	// baseDir is cached to avoid repeated validation
	baseDir string
}

// NewPathResolver creates a new PathResolver implementation.
// The baseDir parameter should be the absolute path to the project root
// containing composer.json.
func NewPathResolver(baseDir string) (PathResolver, error) {
	// Normalize and validate base directory
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for base directory %s: %w", baseDir, err)
	}

	// Resolve symlinks for deterministic paths
	resolvedBaseDir, err := filepath.EvalSymlinks(absBaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve symlinks for base directory %s: %w", absBaseDir, err)
	}
	absBaseDir = resolvedBaseDir

	// Verify base directory exists
	if info, err := os.Stat(absBaseDir); err != nil {
		return nil, fmt.Errorf("base directory does not exist: %s", absBaseDir)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("base directory is not a directory: %s", absBaseDir)
	}

	return &pathResolver{
		baseDir: absBaseDir,
	}, nil
}

// ResolvePath finds the first existing file from the candidates list.
// It resolves relative paths against baseDir and returns the first file that exists.
// Returns ResolutionError if no candidates exist or if context is cancelled.
func (r *pathResolver) ResolvePath(ctx context.Context, candidates []string, baseDir string) (string, error) {
	// Use provided baseDir or fall back to configured baseDir
	resolveBaseDir := baseDir
	if resolveBaseDir == "" {
		resolveBaseDir = r.baseDir
	}

	// Normalize base directory
	absBaseDir, err := filepath.Abs(resolveBaseDir)
	if err != nil {
		return "", &ResolutionError{
			Candidates: candidates,
			BaseDir:    resolveBaseDir,
			Message:    fmt.Sprintf("failed to resolve absolute path for base directory %s", resolveBaseDir),
			Cause:      err,
		}
	}

	// Resolve symlinks for deterministic paths
	resolvedBaseDir, err := filepath.EvalSymlinks(absBaseDir)
	if err != nil {
		return "", &ResolutionError{
			Candidates: candidates,
			BaseDir:    absBaseDir,
			Message:    fmt.Sprintf("failed to resolve symlinks for base directory %s", absBaseDir),
			Cause:      err,
		}
	}
	absBaseDir = resolvedBaseDir

	if len(candidates) == 0 {
		return "", &ResolutionError{
			Candidates: candidates,
			BaseDir:    absBaseDir,
			Message:    "no file candidates provided for resolution",
			Cause:      fmt.Errorf("empty candidates list"),
		}
	}

	// Track all attempted paths for error reporting
	var attemptedPaths []string

	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			return "", &ResolutionError{
				Candidates: candidates,
				BaseDir:    absBaseDir,
				Message:    "path resolution cancelled",
				Cause:      ctx.Err(),
			}
		default:
		}

		// Resolve candidate path
		resolvedPath, err := r.resolveCandidatePath(candidate, absBaseDir)
		if err != nil {
			// Skip invalid paths but continue with other candidates
			attemptedPaths = append(attemptedPaths, fmt.Sprintf("%s (error: %v)", candidate, err))
			continue
		}

		attemptedPaths = append(attemptedPaths, resolvedPath)

		// Debug: print what we're checking
		// fmt.Printf("DEBUG: candidate=%s, resolved=%s, exists=%v\n", candidate, resolvedPath, r.FileExists(resolvedPath))

		// Check if file exists
		if r.FileExists(resolvedPath) {
			return resolvedPath, nil
		}
	}

	// No candidates found
	return "", &ResolutionError{
		Candidates: candidates,
		BaseDir:    absBaseDir,
		Message:    fmt.Sprintf("no existing files found among %d candidates", len(candidates)),
		Cause:      fmt.Errorf("attempted paths: %s", strings.Join(attemptedPaths, ", ")),
	}
}

// FileExists checks if a file exists at the given path.
// Returns true if the path exists and is a regular file, false otherwise.
// Handles both absolute and relative paths efficiently.
func (r *pathResolver) FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// resolveCandidatePath converts a candidate path to an absolute path
// and validates it for security (prevents path traversal attacks).
func (r *pathResolver) resolveCandidatePath(candidate, baseDir string) (string, error) {
	// Handle empty candidate
	if candidate == "" {
		return "", fmt.Errorf("empty candidate path")
	}

	// Additional security: reject paths with null bytes (potential attack)
	if strings.Contains(candidate, "\x00") {
		return "", fmt.Errorf("path traversal detected: invalid path contains null byte: %s", candidate)
	}

	// Pre-check for obvious path traversal patterns before normalization
	// Only check for patterns that are clearly malicious (paths starting with .. or multiple dots)
	if strings.HasPrefix(candidate, "../") || strings.HasPrefix(candidate, "..\\") {
		return "", fmt.Errorf("path traversal detected: path starts with parent directory reference: %s", candidate)
	}

	// Check for multiple dot patterns that could be obfuscated traversal attempts
	if strings.Contains(candidate, "....//") || strings.Contains(candidate, "....\\\\") {
		return "", fmt.Errorf("path traversal detected: suspicious multiple dot pattern in path: %s", candidate)
	}

	// Convert all path separators to the OS-specific separator for consistent handling
	// Replace both types of separators to handle cross-platform paths
	normalizedCandidate := strings.ReplaceAll(candidate, "\\", "/")
	normalizedCandidate = filepath.FromSlash(normalizedCandidate)

	var absolutePath string

	if filepath.IsAbs(normalizedCandidate) {
		// Already absolute path
		absolutePath = normalizedCandidate
	} else {
		// Resolve relative path against base directory
		absolutePath = filepath.Join(baseDir, normalizedCandidate)
	}

	// Clean the path to resolve . and .. components
	cleanPath := filepath.Clean(absolutePath)

	// Security check: ensure resolved path is within base directory
	// This prevents path traversal attacks like "../../../etc/passwd"
	relPath, err := filepath.Rel(baseDir, cleanPath)
	if err != nil {
		return "", fmt.Errorf("path traversal detected: failed to determine relative path: %w", err)
	}

	// Path traversal check - reject paths that escape the base directory
	if strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." {
		return "", fmt.Errorf("path traversal detected: %s resolves outside base directory %s", candidate, baseDir)
	}

	return cleanPath, nil
}


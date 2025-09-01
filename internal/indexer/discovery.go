package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileDiscovererImpl implements the FileDiscoverer interface
type FileDiscovererImpl struct{}

// NewFileDiscoverer creates a new FileDiscovererImpl instance
func NewFileDiscoverer() *FileDiscovererImpl {
	return &FileDiscovererImpl{}
}

// DiscoverFiles enumerates PHP files from target directories using glob patterns.
// Returns files in deterministic order (sorted by path) for consistent results.
func (d *FileDiscovererImpl) DiscoverFiles(ctx context.Context, targets []string, globs []string, baseDir string) ([]FileInfo, error) {
	// Validate inputs
	if err := d.ValidateTargets(targets, baseDir); err != nil {
		return nil, fmt.Errorf("target validation failed: %w", err)
	}

	// Collect all files from all target directories
	var allFiles []FileInfo
	seenFiles := make(map[string]bool) // Deduplicate files that might match multiple patterns

	for _, target := range targets {
		targetPath := filepath.Join(baseDir, target)

		// Apply each glob pattern to this target
		for _, glob := range globs {
			files, err := d.discoverFilesInTarget(ctx, targetPath, glob, baseDir)
			if err != nil {
				return nil, fmt.Errorf("discovery failed for target=%s glob=%s: %w", target, glob, err)
			}

			// Add files, avoiding duplicates
			for _, file := range files {
				if !seenFiles[file.Path] {
					allFiles = append(allFiles, file)
					seenFiles[file.Path] = true
				}
			}
		}
	}

	// Filter to PHP files only
	phpFiles := d.FilterPHPFiles(allFiles)

	// Sort deterministically by relative path
	sort.Slice(phpFiles, func(i, j int) bool {
		return phpFiles[i].Path < phpFiles[j].Path
	})

	return phpFiles, nil
}

// discoverFilesInTarget discovers files in a single target directory using a single glob pattern
func (d *FileDiscovererImpl) discoverFilesInTarget(ctx context.Context, targetPath, glob, baseDir string) ([]FileInfo, error) {
	var files []FileInfo

	// Convert glob pattern to full path pattern
	globPattern := filepath.Join(targetPath, glob)

	// Use filepath.Glob for basic globbing, then walk for ** patterns
	if strings.Contains(glob, "**") {
		// Handle recursive globbing manually
		err := filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err != nil {
				// Skip inaccessible files/directories instead of failing
				return nil
			}

			if info.IsDir() {
				return nil
			}

			// Check if file matches the glob pattern
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return nil // Skip files we can't make relative
			}

			// Apply glob pattern matching
			matched, err := d.matchesGlob(relPath, filepath.Join(filepath.Base(targetPath), glob))
			if err != nil {
				return nil // Skip files with glob matching errors
			}

			if matched {
				files = append(files, FileInfo{
					Path:        filepath.ToSlash(relPath), // Use forward slashes for consistency
					AbsPath:     path,
					ModTime:     info.ModTime(),
					Size:        info.Size(),
					IsDirectory: false,
				})
			}

			return nil
		})

		if err != nil && err != ctx.Err() {
			return nil, NewDiscoveryError("discoverFilesInTarget", targetPath, err)
		}
	} else {
		// Use standard glob matching
		matches, err := filepath.Glob(globPattern)
		if err != nil {
			return nil, NewDiscoveryError("discoverFilesInTarget", globPattern, ErrInvalidGlob).
				WithMetadata("glob", glob)
		}

		for _, match := range matches {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			info, err := os.Stat(match)
			if err != nil {
				continue // Skip files we can't stat
			}

			if !info.IsDir() {
				relPath, err := filepath.Rel(baseDir, match)
				if err != nil {
					continue // Skip files we can't make relative
				}

				files = append(files, FileInfo{
					Path:        filepath.ToSlash(relPath),
					AbsPath:     match,
					ModTime:     info.ModTime(),
					Size:        info.Size(),
					IsDirectory: false,
				})
			}
		}
	}

	return files, nil
}

// matchesGlob checks if a file path matches a glob pattern
func (d *FileDiscovererImpl) matchesGlob(filePath, pattern string) (bool, error) {
	// Handle ** patterns by converting to standard glob
	if strings.Contains(pattern, "**") {
		// Convert **/*.php to */*.php or *.php depending on depth
		patternParts := strings.Split(pattern, "/")

		// Simple implementation: if pattern has **, match if file ends with the suffix
		if len(patternParts) > 0 {
			lastPart := patternParts[len(patternParts)-1]
			return filepath.Match(lastPart, filepath.Base(filePath))
		}
	}

	return filepath.Match(pattern, filePath)
}

// ValidateTargets checks that all target directories exist and are accessible.
// Returns descriptive errors for missing or inaccessible directories.
func (d *FileDiscovererImpl) ValidateTargets(targets []string, baseDir string) error {
	if baseDir == "" {
		return NewDiscoveryError("ValidateTargets", "", ErrInvalidPath).
			WithMetadata("reason", "empty base directory")
	}

	// Check if baseDir exists and is a directory
	baseStat, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDiscoveryError("ValidateTargets", baseDir, ErrTargetNotFound)
		}
		return NewDiscoveryError("ValidateTargets", baseDir, ErrTargetNotReadable)
	}

	if !baseStat.IsDir() {
		return NewDiscoveryError("ValidateTargets", baseDir, ErrInvalidPath).
			WithMetadata("reason", "base directory is not a directory")
	}

	// Validate each target
	for _, target := range targets {
		if err := d.validateSingleTarget(target, baseDir); err != nil {
			return err
		}
	}

	return nil
}

// validateSingleTarget validates a single target directory
func (d *FileDiscovererImpl) validateSingleTarget(target, baseDir string) error {
	// Check for path traversal attempts
	if strings.Contains(target, "..") {
		return NewDiscoveryError("validateSingleTarget", target, ErrPathTraversal).
			WithMetadata("reason", "target contains path traversal sequence")
	}

	// Build full target path
	targetPath := filepath.Join(baseDir, target)

	// Ensure target path is within base directory (security check)
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return NewDiscoveryError("validateSingleTarget", baseDir, err)
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return NewDiscoveryError("validateSingleTarget", targetPath, err)
	}

	// Check that target is within base directory
	relPath, err := filepath.Rel(absBase, absTarget)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return NewDiscoveryError("validateSingleTarget", target, ErrPathTraversal).
			WithMetadata("base", absBase).
			WithMetadata("target", absTarget)
	}

	// Check if target exists
	targetStat, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDiscoveryError("validateSingleTarget", targetPath, ErrTargetNotFound)
		}
		if os.IsPermission(err) {
			return NewDiscoveryError("validateSingleTarget", targetPath, ErrPermissionDenied)
		}
		return NewDiscoveryError("validateSingleTarget", targetPath, ErrTargetNotReadable)
	}

	// Check if target is a directory
	if !targetStat.IsDir() {
		return NewDiscoveryError("validateSingleTarget", targetPath, ErrInvalidPath).
			WithMetadata("reason", "target is not a directory")
	}

	// Check if directory is readable
	file, err := os.Open(targetPath)
	if err != nil {
		if os.IsPermission(err) {
			return NewDiscoveryError("validateSingleTarget", targetPath, ErrPermissionDenied)
		}
		return NewDiscoveryError("validateSingleTarget", targetPath, ErrTargetNotReadable)
	}
	file.Close()

	return nil
}

// FilterPHPFiles filters a list of files to include only PHP files.
// Uses efficient extension checking and excludes non-PHP files.
func (d *FileDiscovererImpl) FilterPHPFiles(files []FileInfo) []FileInfo {
	phpFiles := make([]FileInfo, 0, len(files))

	for _, file := range files {
		if d.isPHPFile(file.Path) && !file.IsDirectory {
			phpFiles = append(phpFiles, file)
		}
	}

	return phpFiles
}

// isPHPFile checks if a file is a PHP file based on its extension
func (d *FileDiscovererImpl) isPHPFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".php"
}

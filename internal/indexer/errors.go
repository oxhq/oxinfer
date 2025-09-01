package indexer

import (
	"errors"
	"fmt"
	"strings"
)

// Discovery errors
var (
	ErrTargetNotFound    = errors.New("target directory not found")
	ErrTargetNotReadable = errors.New("target directory not readable")
	ErrInvalidPath       = errors.New("invalid file path")
	ErrPathTraversal     = errors.New("path traversal detected")
	ErrMaxDepthExceeded  = errors.New("maximum traversal depth exceeded")
	ErrInvalidGlob       = errors.New("invalid glob pattern")
	ErrPermissionDenied  = errors.New("permission denied")
)

// DiscoveryError wraps discovery-related errors with additional context
type DiscoveryError struct {
	Op       string                 // Operation that failed (e.g., "DiscoverFiles", "ValidateTargets")
	Path     string                 // File or directory path involved
	Err      error                  // Underlying error
	Metadata map[string]interface{} // Additional error context
}

func (e *DiscoveryError) Error() string {
	var parts []string

	if e.Op != "" {
		parts = append(parts, fmt.Sprintf("operation=%s", e.Op))
	}

	if e.Path != "" {
		parts = append(parts, fmt.Sprintf("path=%s", e.Path))
	}

	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}

	if len(e.Metadata) > 0 {
		for key, value := range e.Metadata {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
	}

	return strings.Join(parts, ", ")
}

func (e *DiscoveryError) Unwrap() error {
	return e.Err
}

// NewDiscoveryError creates a new DiscoveryError with the given operation, path, and underlying error
func NewDiscoveryError(op, path string, err error) *DiscoveryError {
	return &DiscoveryError{
		Op:       op,
		Path:     path,
		Err:      err,
		Metadata: make(map[string]interface{}),
	}
}

// WithMetadata adds metadata to the discovery error
func (e *DiscoveryError) WithMetadata(key string, value interface{}) *DiscoveryError {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

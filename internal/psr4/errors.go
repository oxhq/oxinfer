package psr4

import "fmt"

// PSR-4 specific error types for composer parsing and validation.

// ErrComposerNotFound indicates that composer.json file could not be found.
var ErrComposerNotFound = fmt.Errorf("composer.json file not found")

// ErrComposerMalformed indicates that composer.json contains invalid JSON.
var ErrComposerMalformed = fmt.Errorf("composer.json contains malformed JSON")

// ErrMissingPSR4 indicates that composer.json lacks PSR-4 autoload configuration.
var ErrMissingPSR4 = fmt.Errorf("composer.json missing psr-4 autoload configuration")

// ErrInvalidNamespace indicates that a PSR-4 namespace prefix is malformed.
var ErrInvalidNamespace = fmt.Errorf("invalid PSR-4 namespace format")

// ErrInvalidFQCN indicates that a fully qualified class name is malformed.
var ErrInvalidFQCN = fmt.Errorf("invalid fully qualified class name")

// ErrClassNotMappable indicates that a class cannot be mapped to any file path.
var ErrClassNotMappable = fmt.Errorf("class cannot be mapped to file path")

// ErrFileNotMappable indicates that a file path cannot be mapped to a class name.
var ErrFileNotMappable = fmt.Errorf("file path cannot be mapped to class name")

// NewComposerNotFoundError creates a ComposerError for file not found cases.
func NewComposerNotFoundError(path string) *ComposerError {
	return &ComposerError{
		Path:    path,
		Message: fmt.Sprintf("composer.json not found at path: %s", path),
		Cause:   ErrComposerNotFound,
	}
}

// NewComposerMalformedError creates a ComposerError for JSON parsing failures.
func NewComposerMalformedError(path string, cause error) *ComposerError {
	return &ComposerError{
		Path:    path,
		Message: fmt.Sprintf("composer.json malformed at path: %s", path),
		Cause:   fmt.Errorf("%w: %v", ErrComposerMalformed, cause),
	}
}

// NewMissingPSR4Error creates a ComposerError for missing PSR-4 configuration.
func NewMissingPSR4Error(path string) *ComposerError {
	return &ComposerError{
		Path:    path,
		Message: fmt.Sprintf("composer.json at %s missing psr-4 autoload configuration", path),
		Cause:   ErrMissingPSR4,
	}
}

// NewInvalidNamespaceError creates a ComposerError for malformed namespace prefixes.
func NewInvalidNamespaceError(namespace string, cause error) *ComposerError {
	return &ComposerError{
		Path:    "",
		Message: fmt.Sprintf("invalid PSR-4 namespace format: %s", namespace),
		Cause:   fmt.Errorf("%w: %v", ErrInvalidNamespace, cause),
	}
}

// NewMappingError creates a MappingError for class mapping failures.
func NewMappingError(fqcn string, message string, cause error) *MappingError {
	if cause == nil {
		cause = ErrInvalidFQCN
	}
	return &MappingError{
		FQCN:    fqcn,
		Message: message,
		Cause:   cause,
	}
}

// NewClassNotMappableError creates a MappingError for unmappable classes.
func NewClassNotMappableError(fqcn string) *MappingError {
	return &MappingError{
		FQCN:    fqcn,
		Message: fmt.Sprintf("class '%s' cannot be mapped to any file path", fqcn),
		Cause:   ErrClassNotMappable,
	}
}

// NewFileNotMappableError creates a MappingError for unmappable files.
func NewFileNotMappableError(filePath string) *MappingError {
	return &MappingError{
		FQCN:    "",
		Message: fmt.Sprintf("file path '%s' cannot be mapped to any class name", filePath),
		Cause:   ErrFileNotMappable,
	}
}
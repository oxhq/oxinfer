// Package parser provides specialized error types for PHP parsing operations.
// These errors provide structured context for tree-sitter integration failures,
// syntax errors, and parser lifecycle issues.
package parser

import (
	"errors"
	"fmt"
)

// Standard parser error types for common failure scenarios
var (
	ErrParserNotInitialized = errors.New("parser not initialized")
	ErrInvalidPHPContent    = errors.New("invalid PHP content")
	ErrParsingFailed        = errors.New("PHP parsing failed")
	ErrTreeSitterError      = errors.New("tree-sitter internal error")
	ErrLanguageNotSet       = errors.New("PHP language not set on parser")
	ErrParserClosed         = errors.New("parser has been closed")
	ErrContentTooLarge      = errors.New("PHP content exceeds size limit")
	ErrParseTimeout         = errors.New("parsing operation timed out")
)

// ParserError represents a structured parser error with context.
// Provides detailed information about parser failures including file path,
// error type, and underlying error chains for debugging.
type ParserError struct {
	Type       string // Error category (parser, syntax, internal)
	Message    string // Human-readable error description
	FilePath   string // Source file path where error occurred
	Underlying error  // Wrapped underlying error
}

// Error implements the error interface with contextual information.
// Returns formatted error message including file path when available.
func (e *ParserError) Error() string {
	if e.FilePath != "" {
		return fmt.Sprintf("parser error in %s: %s", e.FilePath, e.Message)
	}
	return fmt.Sprintf("parser error: %s", e.Message)
}

// Unwrap returns the underlying error for error chain inspection.
// Enables Go 1.13+ error unwrapping for proper error handling.
func (e *ParserError) Unwrap() error {
	return e.Underlying
}

// NewParserError creates a new parser error with message and underlying cause.
// Used for wrapping errors from tree-sitter operations and file I/O.
func NewParserError(message string, err error) *ParserError {
	return &ParserError{
		Type:       "parser",
		Message:    message,
		Underlying: err,
	}
}

// NewParserErrorWithFile creates a parser error with file context.
// Includes file path information for better error reporting and debugging.
func NewParserErrorWithFile(filePath, message string, err error) *ParserError {
	return &ParserError{
		Type:       "parser",
		Message:    message,
		FilePath:   filePath,
		Underlying: err,
	}
}

// SyntaxError represents PHP syntax errors detected during parsing.
// Contains position information and error details from tree-sitter.
type SyntaxError struct {
	Type       string // Always "syntax"
	Message    string // Syntax error description
	FilePath   string // Source file path
	Line       int    // Error line number (1-indexed)
	Column     int    // Error column number (1-indexed)
	StartByte  uint32 // Error start byte position
	EndByte    uint32 // Error end byte position
	Underlying error  // Original tree-sitter error
}

// Error implements the error interface with position information.
// Returns formatted syntax error with line and column details.
func (e *SyntaxError) Error() string {
	if e.FilePath != "" {
		return fmt.Sprintf("syntax error in %s at line %d, column %d: %s",
			e.FilePath, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("syntax error at line %d, column %d: %s",
		e.Line, e.Column, e.Message)
}

// Unwrap returns the underlying tree-sitter error.
func (e *SyntaxError) Unwrap() error {
	return e.Underlying
}

// NewSyntaxError creates a syntax error with position information.
// Used when tree-sitter detects malformed PHP syntax.
func NewSyntaxError(filePath, message string, line, column int, startByte, endByte uint32, err error) *SyntaxError {
	return &SyntaxError{
		Type:       "syntax",
		Message:    message,
		FilePath:   filePath,
		Line:       line,
		Column:     column,
		StartByte:  startByte,
		EndByte:    endByte,
		Underlying: err,
	}
}

// InternalError represents internal parser failures that prevent operation.
// Used for memory allocation failures, CGO errors, and system-level issues.
type InternalError struct {
	Type       string // Always "internal"
	Message    string // Internal error description
	Component  string // Component where error occurred (parser, tree, query)
	Underlying error  // Original system error
}

// Error implements the error interface for internal errors.
func (e *InternalError) Error() string {
	return fmt.Sprintf("internal parser error in %s: %s", e.Component, e.Message)
}

// Unwrap returns the underlying system error.
func (e *InternalError) Unwrap() error {
	return e.Underlying
}

// NewInternalError creates an internal error for system-level failures.
// Used when tree-sitter or CGO operations fail at a low level.
func NewInternalError(component, message string, err error) *InternalError {
	return &InternalError{
		Type:       "internal",
		Message:    message,
		Component:  component,
		Underlying: err,
	}
}

// TimeoutError represents parsing operations that exceed time limits.
// Contains duration information for performance analysis.
type TimeoutError struct {
	Type      string // Always "timeout"
	Message   string // Timeout description
	FilePath  string // File being parsed when timeout occurred
	Duration  int64  // Time spent before timeout (milliseconds)
	SizeBytes int64  // File size that caused timeout
}

// Error implements the error interface for timeout errors.
func (e *TimeoutError) Error() string {
	if e.FilePath != "" {
		return fmt.Sprintf("parsing timeout after %dms for %s (%d bytes): %s",
			e.Duration, e.FilePath, e.SizeBytes, e.Message)
	}
	return fmt.Sprintf("parsing timeout after %dms: %s", e.Duration, e.Message)
}

// NewTimeoutError creates a timeout error with performance context.
// Used when parsing operations exceed configured time limits.
func NewTimeoutError(filePath, message string, duration, sizeBytes int64) *TimeoutError {
	return &TimeoutError{
		Type:      "timeout",
		Message:   message,
		FilePath:  filePath,
		Duration:  duration,
		SizeBytes: sizeBytes,
	}
}

// IsRecoverableError determines if an error allows continued operation.
// Syntax errors and some parser errors are recoverable, internal errors are not.
func IsRecoverableError(err error) bool {
	if err == nil {
		return true
	}

	// Syntax errors are recoverable - partial parsing can continue
	var syntaxErr *SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}

	// Some parser errors are recoverable
	var parserErr *ParserError
	if errors.As(err, &parserErr) {
		// File not found, invalid content are recoverable
		return errors.Is(err, ErrInvalidPHPContent) ||
			errors.Is(err, ErrContentTooLarge)
	}

	// Internal errors and timeouts are not recoverable
	var internalErr *InternalError
	var timeoutErr *TimeoutError
	return !errors.As(err, &internalErr) && !errors.As(err, &timeoutErr)
}

// WrapTreeSitterError wraps tree-sitter C library errors with context.
// Converts low-level tree-sitter failures into structured Go errors.
func WrapTreeSitterError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return NewInternalError("tree-sitter",
		fmt.Sprintf("%s operation failed", operation), err)
}

// WrapFileError wraps file I/O errors with parser context.
// Converts file system errors into parser-specific error types.
func WrapFileError(filePath, operation string, err error) error {
	if err == nil {
		return nil
	}

	return NewParserErrorWithFile(filePath,
		fmt.Sprintf("file %s failed", operation), err)
}

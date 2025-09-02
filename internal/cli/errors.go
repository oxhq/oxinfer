package cli

import (
	"fmt"
)

// ExitCode represents the different exit codes the CLI can return
type ExitCode int

const (
	ExitOK         ExitCode = 0 // Success
	ExitValidation ExitCode = 1 // Input validation error
	ExitInternal   ExitCode = 2 // Internal processing error
	ExitSchema     ExitCode = 3 // Schema load/validation failure
	ExitLimit      ExitCode = 4 // Hard limit exceeded
	ExitOwnership  ExitCode = 5 // Ownership violation (reserved)
)

// Aliases for backward compatibility with tests
const (
	ExitInputError        = ExitValidation
	ExitInternalError     = ExitInternal
	ExitSchemaError       = ExitSchema
	ExitHardLimitExceeded = ExitLimit
)

// String returns the human-readable description of the exit code
func (e ExitCode) String() string {
	switch e {
	case ExitOK:
		return "success"
	case ExitValidation:
		return "input validation error"
	case ExitInternal:
		return "internal processing error"
	case ExitSchema:
		return "schema load/validation failure"
	case ExitLimit:
		return "hard limit exceeded"
	case ExitOwnership:
		return "ownership violation"
	default:
		return fmt.Sprintf("unknown exit code %d", int(e))
	}
}

// CLIError represents an error that occurred during CLI execution
// It includes both the error message and the appropriate exit code
type CLIError struct {
	Type     string                 `json:"type"`
	Message  string                 `json:"message"`
	Details  map[string]interface{} `json:"details,omitempty"`
	ExitCode ExitCode               `json:"exit_code"`
}

// Error implements the error interface
func (e *CLIError) Error() string {
	return e.Message
}

// NewInputError creates a CLI error for input validation failures
func NewInputError(message string) *CLIError {
	return &CLIError{
		Type:     "validation",
		Message:  message,
		ExitCode: ExitValidation,
	}
}

// WrapInputError wraps an error as an input validation failure
func WrapInputError(message string, err error) *CLIError {
	details := make(map[string]interface{})
	if err != nil {
		details["underlying_error"] = err.Error()
	}
	return &CLIError{
		Type:     "validation",
		Message:  message,
		Details:  details,
		ExitCode: ExitValidation,
	}
}

// NewInternalError creates a CLI error for internal processing failures
func NewInternalError(message string) *CLIError {
	return &CLIError{
		Type:     "internal",
		Message:  message,
		ExitCode: ExitInternal,
	}
}

// WrapInternalError wraps an error as an internal processing failure
func WrapInternalError(message string, err error) *CLIError {
	details := make(map[string]interface{})
	if err != nil {
		details["underlying_error"] = err.Error()
	}
	return &CLIError{
		Type:     "internal",
		Message:  message,
		Details:  details,
		ExitCode: ExitInternal,
	}
}

// NewSchemaError creates a CLI error for schema validation failures
func NewSchemaError(message string) *CLIError {
	return &CLIError{
		Type:     "schema",
		Message:  message,
		ExitCode: ExitSchema,
	}
}

// WrapSchemaError wraps an error as a schema validation failure
func WrapSchemaError(message string, err error) *CLIError {
	details := make(map[string]interface{})
	if err != nil {
		details["underlying_error"] = err.Error()
	}
	return &CLIError{
		Type:     "schema",
		Message:  message,
		Details:  details,
		ExitCode: ExitSchema,
	}
}

// NewLimitError creates a CLI error for hard limit violations
func NewLimitError(message string) *CLIError {
	return &CLIError{
		Type:     "limit",
		Message:  message,
		ExitCode: ExitLimit,
	}
}

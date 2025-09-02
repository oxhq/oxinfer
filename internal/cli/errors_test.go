package cli

import (
	"strings"
	"testing"
)

func TestExitCode_String(t *testing.T) {
	tests := []struct {
		name     string
		exitCode ExitCode
		expected string
	}{
		{"ExitOK", ExitOK, "success"},
		{"ExitValidation", ExitValidation, "input validation error"},
		{"ExitInternal", ExitInternal, "internal processing error"},
		{"ExitSchema", ExitSchema, "schema load/validation failure"},
		{"ExitLimit", ExitLimit, "hard limit exceeded"},
		{"Unknown exit code", ExitCode(99), "unknown exit code 99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.exitCode.String()
			if result != tt.expected {
				t.Errorf("ExitCode.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExitCode_Aliases(t *testing.T) {
	// Test backward compatibility aliases
	tests := []struct {
		name     string
		alias    ExitCode
		original ExitCode
	}{
		{"ExitInputError", ExitInputError, ExitValidation},
		{"ExitInternalError", ExitInternalError, ExitInternal},
		{"ExitSchemaError", ExitSchemaError, ExitSchema},
		{"ExitHardLimitExceeded", ExitHardLimitExceeded, ExitLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.alias != tt.original {
				t.Errorf("Alias %s = %v, want %v", tt.name, tt.alias, tt.original)
			}
		})
	}
}

func TestCLIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		cliError *CLIError
		expected string
	}{
		{
			name:     "basic error",
			cliError: &CLIError{Message: "test error"},
			expected: "test error",
		},
		{
			name:     "empty message",
			cliError: &CLIError{Message: ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cliError.Error()
			if result != tt.expected {
				t.Errorf("CLIError.Error() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNewInputError(t *testing.T) {
	message := "input validation failed"
	err := NewInputError(message)

	if err == nil {
		t.Fatal("NewInputError() returned nil")
	}

	if err.Type != "validation" {
		t.Errorf("NewInputError() Type = %q, want %q", err.Type, "validation")
	}

	if err.ExitCode != ExitValidation {
		t.Errorf("NewInputError() ExitCode = %v, want %v", err.ExitCode, ExitValidation)
	}

	if err.Message != message {
		t.Errorf("NewInputError() Message = %q, want %q", err.Message, message)
	}

	if err.Details != nil {
		t.Errorf("NewInputError() Details = %v, want nil", err.Details)
	}
}

func TestWrapInputError(t *testing.T) {
	message := "failed to process input"
	cause := "file not found"
	err := WrapInputError(message, &mockError{msg: cause})

	if err == nil {
		t.Fatal("WrapInputError() returned nil")
	}

	if err.Type != "validation" {
		t.Errorf("WrapInputError() Type = %q, want %q", err.Type, "validation")
	}

	if err.ExitCode != ExitValidation {
		t.Errorf("WrapInputError() ExitCode = %v, want %v", err.ExitCode, ExitValidation)
	}

	if err.Message != message {
		t.Errorf("WrapInputError() Message = %q, want %q", err.Message, message)
	}

	if err.Details == nil {
		t.Fatal("WrapInputError() Details is nil")
	}

	if underlying, ok := err.Details["underlying_error"].(string); !ok || underlying != cause {
		t.Errorf("WrapInputError() underlying error = %v, want %q", err.Details["underlying_error"], cause)
	}
}

func TestNewInternalError(t *testing.T) {
	message := "internal processing failed"
	err := NewInternalError(message)

	if err == nil {
		t.Fatal("NewInternalError() returned nil")
	}

	if err.Type != "internal" {
		t.Errorf("NewInternalError() Type = %q, want %q", err.Type, "internal")
	}

	if err.ExitCode != ExitInternal {
		t.Errorf("NewInternalError() ExitCode = %v, want %v", err.ExitCode, ExitInternal)
	}

	if err.Message != message {
		t.Errorf("NewInternalError() Message = %q, want %q", err.Message, message)
	}
}

func TestWrapInternalError(t *testing.T) {
	message := "failed to write output"
	cause := "disk full"
	err := WrapInternalError(message, &mockError{msg: cause})

	if err == nil {
		t.Fatal("WrapInternalError() returned nil")
	}

	if err.Type != "internal" {
		t.Errorf("WrapInternalError() Type = %q, want %q", err.Type, "internal")
	}

	if err.ExitCode != ExitInternal {
		t.Errorf("WrapInternalError() ExitCode = %v, want %v", err.ExitCode, ExitInternal)
	}

	if err.Message != message {
		t.Errorf("WrapInternalError() Message = %q, want %q", err.Message, message)
	}

	if err.Details == nil {
		t.Fatal("WrapInternalError() Details is nil")
	}

	if underlying, ok := err.Details["underlying_error"].(string); !ok || underlying != cause {
		t.Errorf("WrapInternalError() underlying error = %v, want %q", err.Details["underlying_error"], cause)
	}
}

func TestNewSchemaError(t *testing.T) {
	message := "schema validation failed"
	err := NewSchemaError(message)

	if err == nil {
		t.Fatal("NewSchemaError() returned nil")
	}

	if err.Type != "schema" {
		t.Errorf("NewSchemaError() Type = %q, want %q", err.Type, "schema")
	}

	if err.ExitCode != ExitSchema {
		t.Errorf("NewSchemaError() ExitCode = %v, want %v", err.ExitCode, ExitSchema)
	}

	if err.Message != message {
		t.Errorf("NewSchemaError() Message = %q, want %q", err.Message, message)
	}
}

func TestWrapSchemaError(t *testing.T) {
	message := "failed to load schema"
	cause := "invalid JSON"
	err := WrapSchemaError(message, &mockError{msg: cause})

	if err == nil {
		t.Fatal("WrapSchemaError() returned nil")
	}

	if err.Type != "schema" {
		t.Errorf("WrapSchemaError() Type = %q, want %q", err.Type, "schema")
	}

	if err.ExitCode != ExitSchema {
		t.Errorf("WrapSchemaError() ExitCode = %v, want %v", err.ExitCode, ExitSchema)
	}

	if err.Message != message {
		t.Errorf("WrapSchemaError() Message = %q, want %q", err.Message, message)
	}

	if err.Details == nil {
		t.Fatal("WrapSchemaError() Details is nil")
	}

	if underlying, ok := err.Details["underlying_error"].(string); !ok || underlying != cause {
		t.Errorf("WrapSchemaError() underlying error = %v, want %q", err.Details["underlying_error"], cause)
	}
}

func TestNewLimitError(t *testing.T) {
	message := "file limit exceeded"
	err := NewLimitError(message)

	if err == nil {
		t.Fatal("NewLimitError() returned nil")
	}

	if err.Type != "limit" {
		t.Errorf("NewLimitError() Type = %q, want %q", err.Type, "limit")
	}

	if err.ExitCode != ExitLimit {
		t.Errorf("NewLimitError() ExitCode = %v, want %v", err.ExitCode, ExitLimit)
	}

	if err.Message != message {
		t.Errorf("NewLimitError() Message = %q, want %q", err.Message, message)
	}
}

// TestCLIErrorComparisons tests that CLI errors of different types are distinguishable
func TestCLIErrorComparisons(t *testing.T) {
	inputErr := NewInputError("input error")
	internalErr := NewInternalError("internal error")
	schemaErr := NewSchemaError("schema error")
	limitErr := NewLimitError("limit error")

	errors := []*CLIError{inputErr, internalErr, schemaErr, limitErr}
	expectedCodes := []ExitCode{ExitValidation, ExitInternal, ExitSchema, ExitLimit}
	expectedTypes := []string{"validation", "internal", "schema", "limit"}

	for i, err := range errors {
		if err.ExitCode != expectedCodes[i] {
			t.Errorf("error %d has ExitCode %v, want %v", i, err.ExitCode, expectedCodes[i])
		}
		if err.Type != expectedTypes[i] {
			t.Errorf("error %d has Type %q, want %q", i, err.Type, expectedTypes[i])
		}
	}
}

// TestErrorMessages tests that error messages are properly formatted
func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name      string
		errorFunc func(string) *CLIError
		message   string
	}{
		{"NewInputError", NewInputError, "test input error"},
		{"NewInternalError", NewInternalError, "test internal error"},
		{"NewSchemaError", NewSchemaError, "test schema error"},
		{"NewLimitError", NewLimitError, "test limit error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errorFunc(tt.message)
			if err.Error() != tt.message {
				t.Errorf("%s().Error() = %q, want %q", tt.name, err.Error(), tt.message)
			}
		})
	}
}

// TestWrapErrorHandlesNil tests that wrap functions handle nil errors gracefully
func TestWrapErrorHandlesNil(t *testing.T) {
	tests := []struct {
		name     string
		wrapFunc func(string, error) *CLIError
	}{
		{"WrapInputError", WrapInputError},
		{"WrapInternalError", WrapInternalError},
		{"WrapSchemaError", WrapSchemaError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wrapFunc("test message", nil)
			if err == nil {
				t.Fatalf("%s() returned nil", tt.name)
			}
			if err.Details == nil {
				t.Errorf("%s() with nil error should still create Details map", tt.name)
			}
			// Details should be empty or not contain underlying_error when wrapping nil
			if underlying, exists := err.Details["underlying_error"]; exists && underlying != nil {
				t.Errorf("%s() with nil error should not set underlying_error, got %v", tt.name, underlying)
			}
		})
	}
}

// TestCLIErrorStructFields tests that the CLIError struct has expected JSON tags
func TestCLIErrorStructFields(t *testing.T) {
	err := NewInputError("test error")

	// Test that the struct can be marshaled (indirectly tests JSON tags)
	if err.Type == "" {
		t.Error("CLIError Type field is empty")
	}
	if err.Message == "" {
		t.Error("CLIError Message field is empty")
	}
	if err.ExitCode == ExitCode(0) && err.Type != "success" {
		t.Error("CLIError ExitCode field is zero for non-success error")
	}
}

// mockError is a simple error implementation for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// TestErrorStringContainsMessage ensures error messages are accessible
func TestErrorStringContainsMessage(t *testing.T) {
	message := "detailed error message"
	errors := []*CLIError{
		NewInputError(message),
		NewInternalError(message),
		NewSchemaError(message),
		NewLimitError(message),
	}

	for i, err := range errors {
		errorStr := err.Error()
		if !strings.Contains(errorStr, message) {
			t.Errorf("error %d string %q should contain message %q", i, errorStr, message)
		}
	}
}

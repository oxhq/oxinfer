// Package parser provides PHP source code analysis using tree-sitter.
// This file implements tree-sitter PHP language integration with proper error handling
// and resource management for consistent grammar loading across the project.
package parser

import (
	"fmt"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

var (
	// phpLanguage holds the cached PHP language instance to avoid repeated initialization
	phpLanguage *sitter.Language

	// languageInitMutex protects the language initialization process
	languageInitMutex sync.Once

	// languageInitError holds any error that occurred during language initialization
	languageInitError error
)

// GetPHPLanguage returns the tree-sitter PHP language grammar instance.
// This function is thread-safe and caches the language instance to avoid
// repeated initialization overhead. Used by query engines and pattern matchers
// that need direct access to the PHP grammar for tree-sitter queries.
//
// Returns nil if the PHP language grammar cannot be loaded, which indicates
// a critical system configuration issue or corrupted tree-sitter installation.
func GetPHPLanguage() *sitter.Language {
	languageInitMutex.Do(func() {
		phpLanguage, languageInitError = initializePHPLanguage()
	})

	if languageInitError != nil {
		return nil
	}

	return phpLanguage
}

// GetPHPLanguageWithError returns the tree-sitter PHP language grammar instance with error details.
// Similar to GetPHPLanguage but provides detailed error information for debugging
// language loading issues. Use this when you need to handle or log language loading failures.
func GetPHPLanguageWithError() (*sitter.Language, error) {
	languageInitMutex.Do(func() {
		phpLanguage, languageInitError = initializePHPLanguage()
	})

	return phpLanguage, languageInitError
}

// initializePHPLanguage performs the actual PHP language initialization.
// This function is called once through sync.Once to ensure thread-safe initialization.
// It loads the tree-sitter PHP grammar and validates that it's properly configured.
func initializePHPLanguage() (*sitter.Language, error) {
	// Load PHP language from tree-sitter
	language := php.GetLanguage()
	if language == nil {
		return nil, NewInternalError("language_init",
			"tree-sitter PHP language returned nil - check tree-sitter installation", nil)
	}

	// Validate language configuration
	if err := validateLanguage(language); err != nil {
		return nil, fmt.Errorf("PHP language validation failed: %w", err)
	}

	return language, nil
}

// validateLanguage performs basic validation on the loaded language to ensure
// it's properly configured and can be used for parsing operations.
func validateLanguage(language *sitter.Language) error {
	if language == nil {
		return NewInternalError("language_validation", "language is nil", nil)
	}

	// Skip version check as it may not be available in this tree-sitter version
	// The language validation will continue with symbol count check

	// Check if language has symbols (indicates proper grammar loading)
	symbolCount := language.SymbolCount()
	if symbolCount == 0 {
		return NewInternalError("language_validation",
			"language has no symbols - grammar not properly loaded", nil)
	}

	return nil
}

// IsLanguageInitialized returns true if the PHP language has been successfully initialized.
// This is useful for health checks and initialization verification.
func IsLanguageInitialized() bool {
	return phpLanguage != nil && languageInitError == nil
}

// GetLanguageInfo returns diagnostic information about the loaded PHP language.
// Useful for debugging and system health monitoring.
func GetLanguageInfo() LanguageInfo {
	lang := GetPHPLanguage()
	if lang == nil {
		return LanguageInfo{
			IsLoaded:    false,
			Version:     0,
			SymbolCount: 0,
			FieldCount:  0,
			LoadError:   languageInitError,
		}
	}

	return LanguageInfo{
		IsLoaded:    true,
		Version:     0, // Version method not available in this tree-sitter version
		SymbolCount: lang.SymbolCount(),
		FieldCount:  0, // FieldCount method not available in this tree-sitter version
		LoadError:   nil,
	}
}

// LanguageInfo contains diagnostic information about the tree-sitter PHP language.
type LanguageInfo struct {
	// IsLoaded indicates if the language was successfully loaded
	IsLoaded bool

	// Version is the tree-sitter language version
	Version uint32

	// SymbolCount is the number of grammar symbols
	SymbolCount uint32

	// FieldCount is the number of named fields
	FieldCount uint32

	// LoadError contains any error that occurred during loading
	LoadError error
}

// ResetLanguage resets the cached language instance for testing purposes.
// This function should only be used in tests to verify language initialization behavior.
func ResetLanguage() {
	phpLanguage = nil
	languageInitError = nil
	languageInitMutex = sync.Once{}
}

package parser

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

func TestGetPHPLanguage(t *testing.T) {
	tests := []struct {
		name           string
		resetBefore    bool
		expectNil      bool
		expectNonNil   bool
	}{
		{
			name:         "first call should initialize language",
			resetBefore:  true,
			expectNonNil: true,
		},
		{
			name:         "subsequent calls should return cached language", 
			resetBefore:  false,
			expectNonNil: true,
		},
		{
			name:         "language should be consistent across calls",
			resetBefore:  false,
			expectNonNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resetBefore {
				ResetLanguage()
			}

			language := GetPHPLanguage()

			if tt.expectNil && language != nil {
				t.Errorf("expected nil language, got %v", language)
			}

			if tt.expectNonNil && language == nil {
				t.Errorf("expected non-nil language, got nil")
			}

			if language != nil {
				// Validate language properties
				symbolCount := language.SymbolCount()
				if symbolCount == 0 {
					t.Errorf("expected non-zero symbol count, got %d", symbolCount)
				}
			}
		})
	}
}

func TestGetPHPLanguageWithError(t *testing.T) {
	tests := []struct {
		name        string
		resetBefore bool
		expectError bool
	}{
		{
			name:        "should return language and no error on success",
			resetBefore: true,
			expectError: false,
		},
		{
			name:        "should return cached result on subsequent calls",
			resetBefore: false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resetBefore {
				ResetLanguage()
			}

			language, err := GetPHPLanguageWithError()

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if !tt.expectError && language == nil {
				t.Errorf("expected non-nil language, got nil")
			}

			if language != nil {
				// Validate language is properly initialized
				if language.SymbolCount() == 0 {
					t.Errorf("language symbol count is 0")
				}
			}
		})
	}
}

func TestValidateLanguage(t *testing.T) {
	tests := []struct {
		name        string
		language    func() *sitter.Language
		expectError bool
		errorType   string
	}{
		{
			name: "valid language should pass validation",
			language: func() *sitter.Language {
				return GetPHPLanguage()
			},
			expectError: false,
		},
		{
			name: "nil language should fail validation",
			language: func() *sitter.Language {
				return nil
			},
			expectError: true,
			errorType:   "language_validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			language := tt.language()
			err := validateLanguage(language)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if tt.expectError && err != nil {
				// Check if it's an InternalError with expected component
				if internalErr, ok := err.(*InternalError); ok {
					if internalErr.Component != tt.errorType {
						t.Errorf("expected error component %s, got %s", tt.errorType, internalErr.Component)
					}
				}
			}
		})
	}
}

func TestIsLanguageInitialized(t *testing.T) {
	tests := []struct {
		name        string
		resetBefore bool
		initFirst   bool
		expected    bool
	}{
		{
			name:        "should return false before initialization",
			resetBefore: true,
			initFirst:   false,
			expected:    false,
		},
		{
			name:        "should return true after successful initialization",
			resetBefore: true,
			initFirst:   true,
			expected:    true,
		},
		{
			name:        "should return true for subsequent calls",
			resetBefore: false,
			initFirst:   false,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resetBefore {
				ResetLanguage()
			}

			if tt.initFirst {
				GetPHPLanguage() // Initialize language
			}

			result := IsLanguageInitialized()

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetLanguageInfo(t *testing.T) {
	tests := []struct {
		name         string
		resetBefore  bool
		expectLoaded bool
	}{
		{
			name:         "should return info after reset and call",
			resetBefore:  true,
			expectLoaded: true, // GetLanguageInfo will initialize the language
		},
		{
			name:         "should return info for initialized language",
			resetBefore:  false,
			expectLoaded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resetBefore {
				ResetLanguage()
			} else {
				GetPHPLanguage() // Ensure language is initialized
			}

			info := GetLanguageInfo()

			if info.IsLoaded != tt.expectLoaded {
				t.Errorf("expected IsLoaded=%v, got %v", tt.expectLoaded, info.IsLoaded)
			}

			if tt.expectLoaded {
				if info.SymbolCount == 0 {
					t.Errorf("expected non-zero symbol count for loaded language")
				}
				if info.LoadError != nil {
					t.Errorf("expected no load error for successfully loaded language, got %v", info.LoadError)
				}
			} else {
				// For uninitialized language, it will actually get initialized during GetLanguageInfo call
				// So we expect it to be loaded unless there's an actual error
				if !info.IsLoaded && info.LoadError == nil {
					t.Errorf("expected language to be loaded or have error")
				}
			}
		})
	}
}

func TestLanguageConcurrency(t *testing.T) {
	// Test that concurrent access to GetPHPLanguage is safe
	ResetLanguage()
	
	const numGoroutines = 10
	const callsPerGoroutine = 100
	
	results := make(chan *sitter.Language, numGoroutines*callsPerGoroutine)
	
	// Launch multiple goroutines calling GetPHPLanguage concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < callsPerGoroutine; j++ {
				language := GetPHPLanguage()
				results <- language
			}
		}()
	}
	
	// Collect all results
	var languages []*sitter.Language
	for i := 0; i < numGoroutines*callsPerGoroutine; i++ {
		language := <-results
		languages = append(languages, language)
	}
	
	// Verify all results are consistent (same pointer)
	if len(languages) == 0 {
		t.Fatal("no results received")
	}
	
	firstLanguage := languages[0]
	if firstLanguage == nil {
		t.Fatal("first language result is nil")
	}
	
	for i, language := range languages {
		if language != firstLanguage {
			t.Errorf("language result %d differs from first result", i)
		}
	}
}

func TestResetLanguage(t *testing.T) {
	// Initialize language first
	lang1 := GetPHPLanguage()
	if lang1 == nil {
		t.Fatal("failed to initialize language")
	}
	
	// Verify it's initialized
	if !IsLanguageInitialized() {
		t.Error("language should be initialized")
	}
	
	// Reset language
	ResetLanguage()
	
	// Verify it's reset (this will actually reinitialize it)
	lang2 := GetPHPLanguage()
	if lang2 == nil {
		t.Fatal("failed to initialize language after reset")
	}
	
	// Both should be valid PHP languages with same properties
	if lang1.SymbolCount() != lang2.SymbolCount() {
		t.Errorf("symbol count mismatch after reset: %d vs %d", lang1.SymbolCount(), lang2.SymbolCount())
	}
}

// Benchmark tests for performance monitoring
func BenchmarkGetPHPLanguage(b *testing.B) {
	ResetLanguage()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetPHPLanguage()
	}
}

func BenchmarkGetPHPLanguageParallel(b *testing.B) {
	ResetLanguage()
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			GetPHPLanguage()
		}
	})
}
package psr4

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultComposerLoader implements the ComposerLoader interface for loading
// and validating composer.json files with PSR-4 autoloading configuration.
type DefaultComposerLoader struct{}

// NewComposerLoader creates a new instance of DefaultComposerLoader.
func NewComposerLoader() ComposerLoader {
	return &DefaultComposerLoader{}
}

// LoadComposer loads a composer.json file from the specified path.
// Returns ComposerConfig or error if file cannot be loaded/parsed.
func (d *DefaultComposerLoader) LoadComposer(path string) (*ComposerConfig, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, NewComposerNotFoundError(path)
	}

	// Open and read the file
	file, err := os.Open(path)
	if err != nil {
		return nil, NewComposerNotFoundError(path)
	}
	defer file.Close()

	// Parse from reader
	return d.loadComposerFromReader(file, path)
}

// loadComposerFromReader loads composer configuration from an io.Reader.
// This internal method allows for easier testing and reuse.
func (d *DefaultComposerLoader) loadComposerFromReader(r io.Reader, path string) (*ComposerConfig, error) {
	// Read all data first
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, NewComposerMalformedError(path, err)
	}

	// Parse the complete composer.json structure first
	var composerData ComposerData
	if err := json.Unmarshal(data, &composerData); err != nil {
		return nil, NewComposerMalformedError(path, err)
	}

	// Convert to the interface-expected ComposerConfig format
	config := &ComposerConfig{
		Name: composerData.Name,
		Autoload: AutoloadSection{
			PSR4:     composerData.Autoload.PSR4,
			PSR0:     composerData.Autoload.PSR0,
			Classmap: composerData.Autoload.Classmap,
			Files:    composerData.Autoload.Files,
		},
		AutoloadDev: AutoloadSection{
			PSR4:     composerData.AutoloadDev.PSR4,
			PSR0:     composerData.AutoloadDev.PSR0,
			Classmap: composerData.AutoloadDev.Classmap,
			Files:    composerData.AutoloadDev.Files,
		},
	}

	return config, nil
}

// ValidateConfig validates a ComposerConfig for PSR-4 compliance.
// Checks namespace format, directory paths, and structural requirements.
func (d *DefaultComposerLoader) ValidateConfig(config *ComposerConfig) error {
	if config == nil {
		return fmt.Errorf("composer config cannot be nil")
	}

	// Check if there's at least some PSR-4 configuration
	hasPSR4 := len(config.Autoload.PSR4) > 0 ||
		len(config.AutoloadDev.PSR4) > 0

	if !hasPSR4 {
		return NewMissingPSR4Error("")
	}

	// Validate production autoload PSR-4 mappings
	if err := d.validatePSR4Section(config.Autoload.PSR4, "autoload.psr-4"); err != nil {
		return err
	}

	// Validate development autoload PSR-4 mappings
	if err := d.validatePSR4Section(config.AutoloadDev.PSR4, "autoload-dev.psr-4"); err != nil {
		return err
	}

	return nil
}

// validatePSR4Section validates a PSR-4 configuration section.
func (d *DefaultComposerLoader) validatePSR4Section(psr4Config map[string]any, section string) error {
	if psr4Config == nil {
		return nil // PSR-4 section is optional
	}

	for namespace, pathsValue := range psr4Config {
		// Validate namespace format
		if err := validateNamespaceFormat(namespace); err != nil {
			return NewInvalidNamespaceError(namespace,
				fmt.Errorf("in %s: %w", section, err))
		}

		// Validate paths format
		if err := d.validatePathsValue(pathsValue, namespace, section); err != nil {
			return err
		}
	}

	return nil
}

// validatePathsValue validates that paths are in correct format (string, []string, or []interface{}).
func (d *DefaultComposerLoader) validatePathsValue(pathsValue any, namespace, section string) error {
	switch paths := pathsValue.(type) {
	case string:
		// Single path as string - validate it's not empty
		if strings.TrimSpace(paths) == "" {
			return NewInvalidNamespaceError(namespace,
				fmt.Errorf("in %s: path cannot be empty", section))
		}

	case []string:
		// Multiple paths as string slice
		if len(paths) == 0 {
			return NewInvalidNamespaceError(namespace,
				fmt.Errorf("in %s: paths array cannot be empty", section))
		}
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return NewInvalidNamespaceError(namespace,
					fmt.Errorf("in %s: path cannot be empty", section))
			}
		}

	case []any:
		// Multiple paths as interface slice (from JSON unmarshaling)
		if len(paths) == 0 {
			return NewInvalidNamespaceError(namespace,
				fmt.Errorf("in %s: paths array cannot be empty", section))
		}
		for i, p := range paths {
			pathStr, ok := p.(string)
			if !ok {
				return NewInvalidNamespaceError(namespace,
					fmt.Errorf("in %s: path at index %d must be string, got %T", section, i, p))
			}
			if strings.TrimSpace(pathStr) == "" {
				return NewInvalidNamespaceError(namespace,
					fmt.Errorf("in %s: path at index %d cannot be empty", section, i))
			}
		}

	default:
		return NewInvalidNamespaceError(namespace,
			fmt.Errorf("in %s: paths must be string or array, got %T", section, pathsValue))
	}

	return nil
}

// LoadComposerFromReader is a helper function that creates a temporary ComposerLoader
// and loads configuration from an io.Reader. Useful for testing and when you have
// composer.json content from sources other than filesystem.
func LoadComposerFromReader(r io.Reader) (*ComposerConfig, error) {
	loader := NewComposerLoader()
	if defaultLoader, ok := loader.(*DefaultComposerLoader); ok {
		return defaultLoader.loadComposerFromReader(r, "<reader>")
	}
	return nil, fmt.Errorf("failed to create composer loader")
}

// MustLoadComposer loads a composer.json file and panics if it fails.
// This is a convenience function for testing and cases where composer.json
// is guaranteed to exist and be valid.
func MustLoadComposer(path string) *ComposerConfig {
	loader := NewComposerLoader()
	config, err := loader.LoadComposer(path)
	if err != nil {
		panic(fmt.Sprintf("failed to load composer.json from %s: %v", path, err))
	}

	if err := loader.ValidateConfig(config); err != nil {
		panic(fmt.Sprintf("invalid composer.json at %s: %v", path, err))
	}

	return config
}

// FindComposerFile searches for composer.json starting from the given directory
// and walking up the directory tree until found or reaching filesystem root.
// Returns the absolute path to composer.json or error if not found.
func FindComposerFile(startDir string) (string, error) {
	// Convert to absolute path
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	current := absDir

	for {
		composerPath := filepath.Join(current, "composer.json")
		if _, err := os.Stat(composerPath); err == nil {
			return composerPath, nil
		}

		parent := filepath.Dir(current)
		// Check if we've reached the root
		if parent == current {
			break
		}
		current = parent
	}

	return "", NewComposerNotFoundError(fmt.Sprintf("composer.json not found in %s or any parent directory", startDir))
}

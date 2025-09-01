package psr4

import (
	"path/filepath"
	"sort"
)

// DefaultClassMapper implements the ClassMapper interface for PSR-4 class resolution.
// It maps fully qualified class names to potential file paths using PSR-4 autoloading rules.
type DefaultClassMapper struct {
	// mappings contains the PSR-4 namespace to directory mappings
	mappings []NamespaceMapping
	// includeDev determines whether to include development dependencies
	includeDev bool
}

// NewClassMapper creates a new DefaultClassMapper instance.
// composerData contains the parsed composer.json configuration.
// includeDev controls whether autoload-dev mappings are included.
func NewClassMapper(composerData *ComposerData, includeDev bool) (ClassMapper, error) {
	if composerData == nil {
		return nil, NewMappingError("", "composer data cannot be nil", nil)
	}
	
	mappings, err := composerData.GetPSR4Mappings()
	if err != nil {
		return nil, err
	}
	
	// Filter mappings based on dev dependency preference
	filteredMappings := FilterMappingsByDev(mappings, includeDev)
	
	return &DefaultClassMapper{
		mappings:   filteredMappings,
		includeDev: includeDev,
	}, nil
}

// MapClass maps a fully qualified class name to potential file paths.
// Returns ordered list of candidates (most specific namespace first).
// FQCN format: "App\\Http\\Controllers\\UserController"
func (m *DefaultClassMapper) MapClass(fqcn string) ([]string, error) {
	// Validate FQCN format
	if err := ValidateFQCNFormat(fqcn); err != nil {
		return nil, err
	}
	
	// Get all namespace prefixes for matching
	namespaces := m.getNamespacePrefixes()
	if len(namespaces) == 0 {
		return nil, NewClassNotMappableError(fqcn)
	}
	
	// Find the best matching namespace prefix
	matchingNamespace, remainingPath := FindLongestMatchingPrefix(fqcn, namespaces)
	
	if matchingNamespace == "" && remainingPath == fqcn {
		// No namespace matched, this class cannot be mapped
		return nil, NewClassNotMappableError(fqcn)
	}
	
	// Get all directory paths for the matching namespace
	directoryPaths := m.getPathsForNamespace(matchingNamespace)
	if len(directoryPaths) == 0 {
		return nil, NewClassNotMappableError(fqcn)
	}
	
	// Convert remaining class path to file path
	classFilePath := m.convertClassPathToFilePath(remainingPath)
	
	// Generate candidate file paths
	var candidates []string
	for _, dirPath := range directoryPaths {
		// Combine directory path with class file path
		candidatePath := filepath.Join(dirPath, classFilePath)
		// Normalize path separators
		candidatePath = filepath.ToSlash(candidatePath)
		candidates = append(candidates, candidatePath)
	}
	
	// Sort candidates for deterministic output
	sort.Strings(candidates)
	
	return candidates, nil
}

// GetNamespaces returns all registered namespace prefixes.
// Useful for enumerating available namespaces.
func (m *DefaultClassMapper) GetNamespaces() []string {
	return GetAllNamespaces(m.mappings)
}

// getNamespacePrefixes extracts namespace prefixes from all mappings.
func (m *DefaultClassMapper) getNamespacePrefixes() []string {
	prefixes := make([]string, 0, len(m.mappings))
	
	for _, mapping := range m.mappings {
		prefixes = append(prefixes, mapping.Namespace)
	}
	
	return prefixes
}

// getPathsForNamespace returns all directory paths for a given namespace.
// Handles multiple paths per namespace as defined by PSR-4.
func (m *DefaultClassMapper) getPathsForNamespace(namespace string) []string {
	var paths []string
	
	for _, mapping := range m.mappings {
		if mapping.Namespace == namespace {
			// Copy paths to avoid modifying original mapping
			paths = append(paths, mapping.Paths...)
		}
	}
	
	// Sort for deterministic behavior
	sort.Strings(paths)
	
	return paths
}

// convertClassPathToFilePath converts a class path to a file path with .php extension.
// Example: "Http\\Controllers\\UserController" -> "Http/Controllers/UserController.php"
func (m *DefaultClassMapper) convertClassPathToFilePath(classPath string) string {
	if classPath == "" {
		return ""
	}
	
	// Convert namespace separators to path separators
	filePath := ConvertNamespaceToPath(classPath)
	
	// Remove trailing slash if present
	filePath = NormalizePath(filePath)
	
	// Add .php extension
	if filePath != "" {
		filePath += ".php"
	}
	
	return filePath
}

// StaticClassMapper provides additional utility methods for class mapping
// without requiring composer data initialization.
type StaticClassMapper struct{}

// MapClassToFile maps a fully qualified class name to a potential file path
// using the provided composer configuration.
func (s StaticClassMapper) MapClassToFile(fqcn string, composerConfig *ComposerConfig) (string, error) {
	if composerConfig == nil {
		return "", NewMappingError(fqcn, "composer config cannot be nil", nil)
	}
	
	// Convert ComposerConfig to ComposerData for processing
	composerData := convertConfigToData(composerConfig)
	
	// Create temporary mapper
	mapper, err := NewClassMapper(composerData, true) // Include dev for full mapping
	if err != nil {
		return "", err
	}
	
	// Get potential file paths
	candidates, err := mapper.MapClass(fqcn)
	if err != nil {
		return "", err
	}
	
	if len(candidates) == 0 {
		return "", NewClassNotMappableError(fqcn)
	}
	
	// Return first (most specific) candidate
	return candidates[0], nil
}

// MapFileToClass maps a file path to a fully qualified class name
// using the provided composer configuration.
func (s StaticClassMapper) MapFileToClass(filePath string, composerConfig *ComposerConfig) (string, error) {
	if composerConfig == nil {
		return "", NewFileNotMappableError(filePath)
	}
	
	if filePath == "" {
		return "", NewFileNotMappableError(filePath)
	}
	
	// Normalize file path
	normalizedPath := NormalizePath(filePath)
	
	// Remove .php extension if present
	if filepath.Ext(normalizedPath) == ".php" {
		normalizedPath = normalizedPath[:len(normalizedPath)-4]
	}
	
	// Convert ComposerConfig to ComposerData for processing
	composerData := convertConfigToData(composerConfig)
	
	mappings, err := composerData.GetPSR4Mappings()
	if err != nil {
		return "", err
	}
	
	// Try to match file path to a namespace mapping
	for _, mapping := range mappings {
		for _, dirPath := range mapping.Paths {
			normalizedDirPath := NormalizePath(dirPath)
			
			// Check if file path starts with this directory
			if len(normalizedPath) > len(normalizedDirPath) &&
				normalizedPath[:len(normalizedDirPath)] == normalizedDirPath {
				
				// Extract relative path
				relativePath := normalizedPath[len(normalizedDirPath):]
				if relativePath[0] == '/' {
					relativePath = relativePath[1:]
				}
				
				// Convert path to namespace
				relativeNamespace := ConvertPathToNamespace(relativePath)
				
				// Combine with base namespace
				fullNamespace := mapping.Namespace + relativeNamespace
				
				// Remove trailing backslash to get FQCN
				fqcn := fullNamespace
				if fqcn != "" && fqcn[len(fqcn)-1] == '\\' {
					fqcn = fqcn[:len(fqcn)-1]
				}
				
				return fqcn, nil
			}
		}
	}
	
	return "", NewFileNotMappableError(filePath)
}

// GetNamespaceForPath extracts the namespace for a given file path
// using the provided composer configuration.
func (s StaticClassMapper) GetNamespaceForPath(filePath string, composerConfig *ComposerConfig) (string, error) {
	if composerConfig == nil {
		return "", NewFileNotMappableError(filePath)
	}
	
	if filePath == "" {
		return "", NewFileNotMappableError(filePath)
	}
	
	// Normalize file path and remove extension
	normalizedPath := NormalizePath(filePath)
	if filepath.Ext(normalizedPath) == ".php" {
		normalizedPath = normalizedPath[:len(normalizedPath)-4]
	}
	
	// Convert ComposerConfig to ComposerData for processing
	composerData := convertConfigToData(composerConfig)
	
	mappings, err := composerData.GetPSR4Mappings()
	if err != nil {
		return "", err
	}
	
	// Find the best matching namespace mapping
	bestMatch := ""
	bestMatchLength := 0
	
	for _, mapping := range mappings {
		for _, dirPath := range mapping.Paths {
			normalizedDirPath := NormalizePath(dirPath)
			
			// Check if file path starts with this directory
			if len(normalizedPath) >= len(normalizedDirPath) &&
				normalizedPath[:len(normalizedDirPath)] == normalizedDirPath {
				
				// Choose the longest matching path
				if len(normalizedDirPath) > bestMatchLength {
					bestMatchLength = len(normalizedDirPath)
					
					// Extract relative path
					relativePath := normalizedPath[len(normalizedDirPath):]
					if len(relativePath) > 0 && relativePath[0] == '/' {
						relativePath = relativePath[1:]
					}
					
					// Get parent directory for namespace
					if relativePath != "" {
						parentDir := filepath.Dir(relativePath)
						if parentDir != "." {
							relativeNamespace := ConvertPathToNamespace(parentDir)
							bestMatch = mapping.Namespace + relativeNamespace
						} else {
							bestMatch = mapping.Namespace
						}
					} else {
						bestMatch = mapping.Namespace
					}
				}
			}
		}
	}
	
	if bestMatch == "" {
		return "", NewFileNotMappableError(filePath)
	}
	
	// Remove trailing backslash for namespace representation
	if bestMatch != "" && bestMatch[len(bestMatch)-1] == '\\' {
		bestMatch = bestMatch[:len(bestMatch)-1]
	}
	
	return bestMatch, nil
}

// ValidateClassMapping verifies that a class name correctly maps to the given file path
// using the provided composer configuration.
func (s StaticClassMapper) ValidateClassMapping(fqcn string, filePath string, composerConfig *ComposerConfig) error {
	if composerConfig == nil {
		return NewMappingError(fqcn, "composer config cannot be nil", nil)
	}
	
	// Validate FQCN format
	if err := ValidateFQCNFormat(fqcn); err != nil {
		return err
	}
	
	// Map FQCN to expected file path
	expectedPath, err := s.MapClassToFile(fqcn, composerConfig)
	if err != nil {
		return err
	}
	
	// Normalize paths for comparison
	normalizedExpected := NormalizePath(expectedPath)
	normalizedActual := NormalizePath(filePath)
	
	if normalizedExpected != normalizedActual {
		return NewMappingError(fqcn, 
			"class mapping validation failed: expected '"+normalizedExpected+"', got '"+normalizedActual+"'", 
			nil)
	}
	
	return nil
}

// convertConfigToData converts ComposerConfig to ComposerData for internal processing.
func convertConfigToData(config *ComposerConfig) *ComposerData {
	return &ComposerData{
		Name:        config.Name,
		Autoload:    PSR4Config{PSR4: config.Autoload.PSR4},
		AutoloadDev: PSR4Config{PSR4: config.AutoloadDev.PSR4},
	}
}
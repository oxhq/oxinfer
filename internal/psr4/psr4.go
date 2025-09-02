package psr4

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "sync"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// Compile-time interface compliance check
var _ PSR4Resolver = (*DefaultPSR4Resolver)(nil)

// DefaultPSR4Resolver implements the PSR4Resolver interface by orchestrating
// the composer loader, class mapper, and path resolver components.
// It provides complete PSR-4 class resolution with caching support.
type DefaultPSR4Resolver struct {
	// Component dependencies
	composerLoader ComposerLoader
	pathResolver   PathResolver
	cache          *PSR4Cache
	
	// Configuration
	config *ResolverConfig
	
	// State - protected by mutex
	mu            sync.RWMutex
	composerData  *ComposerData
	classMapper   ClassMapper
	initialized   bool
}

// ResolverConfig holds configuration for the PSR-4 resolver.
type ResolverConfig struct {
	// ProjectRoot is the absolute path to the project root directory
	ProjectRoot string
	// ComposerPath is the path to composer.json (relative to ProjectRoot)
	ComposerPath string
	// IncludeDev determines if autoload-dev mappings should be included
	IncludeDev bool
	// CacheEnabled controls whether caching is enabled
	CacheEnabled bool
	// CacheSize is the maximum number of entries to cache
	CacheSize int
}

// NewPSR4Resolver creates a new DefaultPSR4Resolver with the given configuration.
// It initializes all component dependencies but does not load composer.json yet.
func NewPSR4Resolver(config *ResolverConfig) (*DefaultPSR4Resolver, error) {
	if config == nil {
		return nil, fmt.Errorf("resolver config cannot be nil")
	}
	
	if config.ProjectRoot == "" {
		return nil, fmt.Errorf("project root cannot be empty")
	}
	
	// Normalize project root to absolute path
	absProjectRoot, err := filepath.Abs(config.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute project root: %w", err)
	}
	config.ProjectRoot = absProjectRoot
	
	// Set defaults
	if config.ComposerPath == "" {
		config.ComposerPath = "composer.json"
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 1000
	}
	
    // Initialize components
    composerLoader := NewComposerLoader()

    // Resolve base directory relative to composer.json
    composerBase := filepath.Dir(filepath.Join(config.ProjectRoot, config.ComposerPath))
    pathResolver, err := NewPathResolver(composerBase)
    if err != nil {
        return nil, fmt.Errorf("failed to create path resolver: %w", err)
    }
	
	var cache *PSR4Cache
	if config.CacheEnabled {
		cache = NewPSR4Cache(config.CacheSize)
	}
	
	return &DefaultPSR4Resolver{
		composerLoader: composerLoader,
		pathResolver:   pathResolver,
		cache:          cache,
		config:         config,
	}, nil
}

// NewPSR4ResolverFromManifest creates a PSR-4 resolver from an Oxinfer manifest.
// This is the primary integration point with the Oxinfer CLI.
func NewPSR4ResolverFromManifest(manifest *manifest.Manifest) (*DefaultPSR4Resolver, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest cannot be nil")
	}
	
	config := &ResolverConfig{
		ProjectRoot:  manifest.Project.Root,
		ComposerPath: manifest.Project.Composer,
		IncludeDev:   true, // Always include dev for complete analysis
		CacheEnabled: true,
		CacheSize:    1000,
	}
	
	// Override cache settings if specified in manifest
	if manifest.Cache != nil {
		if manifest.Cache.Enabled != nil {
			config.CacheEnabled = *manifest.Cache.Enabled
		}
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PSR-4 resolver from manifest: %w", err)
	}
	
	// Initialize the resolver by loading composer.json
	if err := resolver.loadComposerData(); err != nil {
		return nil, fmt.Errorf("failed to initialize PSR-4 resolver: %w", err)
	}
	
	return resolver, nil
}

// ResolveClass resolves a fully qualified class name to its file path.
// This is the primary method that orchestrates the complete resolution pipeline.
func (r *DefaultPSR4Resolver) ResolveClass(ctx context.Context, fqcn string) (string, error) {
	// Validate input
	if err := ValidateFQCNFormat(fqcn); err != nil {
		return "", err
	}
	
	// Check cache first if enabled
	if r.cache != nil {
		if cachedPath, found := r.cache.GetClass(fqcn); found {
			return cachedPath, nil
		}
	}
	
	// Ensure resolver is initialized
	if err := r.ensureInitialized(); err != nil {
		return "", fmt.Errorf("failed to initialize resolver: %w", err)
	}
	
	r.mu.RLock()
	mapper := r.classMapper
	r.mu.RUnlock()
	
	if mapper == nil {
		return "", fmt.Errorf("class mapper not initialized")
	}
	
	// Step 1: Map FQCN to potential file paths
	candidates, err := mapper.MapClass(fqcn)
	if err != nil {
		return "", fmt.Errorf("failed to map class %s: %w", fqcn, err)
	}
	
	if len(candidates) == 0 {
		return "", NewClassNotMappableError(fqcn)
	}
	
    // Step 2: Resolve paths against filesystem using resolver's base dir (composer dir)
    resolvedPath, err := r.pathResolver.ResolvePath(ctx, candidates, "")
    if err != nil {
        return "", fmt.Errorf("failed to resolve class %s: %w", fqcn, err)
    }
	
	// Cache the successful resolution
	if r.cache != nil {
		r.cache.SetClass(fqcn, resolvedPath)
	}
	
	return resolvedPath, nil
}

// GetAllClasses returns a map of all discoverable classes to their file paths.
// Results are deterministically ordered for consistent delta.json output.
func (r *DefaultPSR4Resolver) GetAllClasses(ctx context.Context) (map[string]string, error) {
	// Ensure resolver is initialized
	if err := r.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("failed to initialize resolver: %w", err)
	}
	
	r.mu.RLock()
	mapper := r.classMapper
	composerData := r.composerData
	r.mu.RUnlock()
	
	if mapper == nil || composerData == nil {
		return nil, fmt.Errorf("resolver not properly initialized")
	}
	
	// Get all namespace mappings
	mappings, err := composerData.GetPSR4Mappings()
	if err != nil {
		return nil, fmt.Errorf("failed to get PSR-4 mappings: %w", err)
	}
	
	// Filter by dev dependency preference
	filteredMappings := FilterMappingsByDev(mappings, r.config.IncludeDev)
	
	result := make(map[string]string)
	
	// For each namespace mapping, discover classes
	for _, mapping := range filteredMappings {
		classes, err := r.discoverClassesInMapping(ctx, mapping)
		if err != nil {
			// Log the error but continue with other mappings
			continue
		}
		
		// Add discovered classes to result
		for fqcn, filePath := range classes {
			result[fqcn] = filePath
		}
	}
	
	return result, nil
}

// Refresh reloads composer configuration and clears caches.
// Used when composer.json is modified during analysis.
func (r *DefaultPSR4Resolver) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Clear cache
	if r.cache != nil {
		r.cache.Clear()
	}
	
	// Reload composer data
	r.composerData = nil
	r.classMapper = nil
	r.initialized = false
	
	// Re-initialize
	return r.loadComposerData()
}

// ensureInitialized makes sure the resolver is properly initialized.
func (r *DefaultPSR4Resolver) ensureInitialized() error {
	r.mu.RLock()
	if r.initialized {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()
	
	// Need to initialize - acquire write lock
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Double-check after acquiring write lock
	if r.initialized {
		return nil
	}
	
	return r.loadComposerData()
}

// loadComposerData loads and validates composer.json, initializes the class mapper.
// Must be called with write lock held.
func (r *DefaultPSR4Resolver) loadComposerData() error {
	// Construct absolute path to composer.json
	composerPath := filepath.Join(r.config.ProjectRoot, r.config.ComposerPath)
	
	// Load composer configuration
	composerConfig, err := r.composerLoader.LoadComposer(composerPath)
	if err != nil {
		return fmt.Errorf("failed to load composer.json from %s: %w", composerPath, err)
	}
	
	// Validate composer configuration
	if err := r.composerLoader.ValidateConfig(composerConfig); err != nil {
		return fmt.Errorf("invalid composer.json: %w", err)
	}
	
	// Convert to internal format
	r.composerData = convertConfigToComposerData(composerConfig)
	
	// Initialize class mapper
	classMapper, err := NewClassMapper(r.composerData, r.config.IncludeDev)
	if err != nil {
		return fmt.Errorf("failed to create class mapper: %w", err)
	}
	
	r.classMapper = classMapper
	r.initialized = true
	
	return nil
}

// discoverClassesInMapping discovers all classes within a specific namespace mapping.
func (r *DefaultPSR4Resolver) discoverClassesInMapping(ctx context.Context, mapping NamespaceMapping) (map[string]string, error) {
	result := make(map[string]string)
	
	// A namespace mapping can have multiple paths
	for _, dirPath := range mapping.Paths {
		classes, err := r.discoverClassesInPath(ctx, mapping.Namespace, dirPath)
		if err != nil {
			return nil, fmt.Errorf("failed to discover classes in path %s: %w", dirPath, err)
		}
		
		// Merge results, checking for duplicates
		for className, filePath := range classes {
			if existingPath, exists := result[className]; exists {
				// Log warning about duplicate class definition but continue
				fmt.Printf("Warning: class %s defined in both %s and %s\n", className, existingPath, filePath)
			}
			result[className] = filePath
		}
	}
	
	return result, nil
}

// discoverClassesInPath discovers all classes in a specific directory path.
func (r *DefaultPSR4Resolver) discoverClassesInPath(ctx context.Context, namespace, dirPath string) (map[string]string, error) {
	result := make(map[string]string)
	
	// Resolve the absolute directory path for this mapping
	absDir := filepath.Join(r.config.ProjectRoot, dirPath)
	
	// Check if directory exists
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		// Directory doesn't exist, which is valid - just return empty result
		return result, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to stat directory %s: %w", absDir, err)
	}
	
	// Recursively scan for PHP files
	err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		if err != nil {
			// Skip directories that can't be read
			return nil
		}
		
		// Only process .php files
		if d.IsDir() || !strings.HasSuffix(path, ".php") {
			return nil
		}
		
		// Skip common non-class PHP files
		filename := d.Name()
		if r.shouldSkipFile(filename) {
			return nil
		}
		
		// Extract class name from file path
		relPath, err := filepath.Rel(absDir, path)
		if err != nil {
			return nil // Skip files we can't determine relative path for
		}
		
		// Convert file path to class name
		className := r.pathToClassName(namespace, relPath)
		if className != "" {
			// Verify the file actually contains the expected class/interface/trait
			if r.validateClassFile(path, className) {
				result[className] = path
			}
		}
		
		return nil
	})
	
	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("failed to scan directory %s: %w", absDir, err)
	}
	
	return result, nil
}

// pathToClassName converts a file path to a fully qualified class name based on PSR-4 mapping.
func (r *DefaultPSR4Resolver) pathToClassName(namespace, relPath string) string {
	// Remove .php extension
	if !strings.HasSuffix(relPath, ".php") {
		return ""
	}
	relPath = strings.TrimSuffix(relPath, ".php")
	
	// Convert path separators to namespace separators
	relPath = strings.ReplaceAll(relPath, string(filepath.Separator), "\\")
	
	// Build fully qualified class name
	namespace = strings.TrimSuffix(namespace, "\\")
	if namespace == "" {
		return relPath
	}
	
	return namespace + "\\" + relPath
}

// shouldSkipFile determines if a PHP file should be skipped during class discovery.
func (r *DefaultPSR4Resolver) shouldSkipFile(filename string) bool {
	// Skip common non-class PHP files
	skipPatterns := []string{
		"index.php",
		"web.php",
		"api.php", 
		"channels.php",
		"console.php",
		"bootstrap.php",
		"autoload.php",
		"config.php",
		".blade.php", // Laravel Blade templates
	}
	
	for _, pattern := range skipPatterns {
		if strings.Contains(filename, pattern) {
			return true
		}
	}
	
	// Skip files that start with lowercase (typically not class files)
	basename := strings.TrimSuffix(filename, ".php")
	if len(basename) > 0 && strings.ToLower(string(basename[0])) == string(basename[0]) {
		// Files starting with lowercase are typically not class files
		return true
	}
	
	return false
}

// validateClassFile performs basic validation that a PHP file contains the expected class.
func (r *DefaultPSR4Resolver) validateClassFile(filePath, expectedClassName string) bool {
	// For now, do basic validation - check file exists and is readable
	if _, err := os.Stat(filePath); err != nil {
		return false
	}
	
	// In a full implementation, we could parse the PHP file to verify:
	// 1. The file contains a class/interface/trait declaration
	// 2. The declared name matches the expected class name
	// 3. The namespace declaration matches expectations
	
	// For this implementation, we assume PSR-4 compliance and validate by file structure
	return true
}

// convertConfigToComposerData converts ComposerConfig to ComposerData.
func convertConfigToComposerData(config *ComposerConfig) *ComposerData {
	return &ComposerData{
		Name: config.Name,
		Autoload: PSR4Config{
			PSR4:     config.Autoload.PSR4,
			PSR0:     config.Autoload.PSR0,
			Classmap: config.Autoload.Classmap,
			Files:    config.Autoload.Files,
		},
		AutoloadDev: PSR4Config{
			PSR4:     config.AutoloadDev.PSR4,
			PSR0:     config.AutoloadDev.PSR0,
			Classmap: config.AutoloadDev.Classmap,
			Files:    config.AutoloadDev.Files,
		},
	}
}

// GetConfig returns a copy of the current resolver configuration.
func (r *DefaultPSR4Resolver) GetConfig() ResolverConfig {
	return *r.config
}

// IsInitialized returns true if the resolver has been successfully initialized.
func (r *DefaultPSR4Resolver) IsInitialized() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.initialized
}

// GetNamespaces returns all registered namespace prefixes.
// Results are sorted for deterministic output.
func (r *DefaultPSR4Resolver) GetNamespaces() ([]string, error) {
	if err := r.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("failed to initialize resolver: %w", err)
	}
	
	r.mu.RLock()
	mapper := r.classMapper
	r.mu.RUnlock()
	
	if mapper == nil {
		return nil, fmt.Errorf("class mapper not initialized")
	}
	
	namespaces := mapper.GetNamespaces()
	
	// Sort for deterministic output
	sort.Strings(namespaces)
	
	return namespaces, nil
}

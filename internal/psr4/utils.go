package psr4

import (
	"path/filepath"
	"sort"
	"strings"
)

// PSR-4 utility functions for namespace and path manipulation.

// NormalizeNamespace ensures PSR-4 namespace format consistency.
// Removes leading backslashes and ensures trailing backslash for non-empty namespaces.
func NormalizeNamespace(namespace string) string {
	// Remove leading backslashes
	namespace = strings.TrimLeft(namespace, "\\")

	// Empty namespace is valid (root namespace)
	if namespace == "" {
		return ""
	}

	// Ensure trailing backslash for PSR-4 compliance
	if !strings.HasSuffix(namespace, "\\") {
		namespace += "\\"
	}

	return namespace
}

// NormalizePath converts path to use forward slashes and removes trailing slashes.
// Handles cross-platform path compatibility.
func NormalizePath(path string) string {
	// Convert to forward slashes for consistency
	path = filepath.ToSlash(path)

	// Remove trailing slash (except for root)
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimRight(path, "/")
	}

	return path
}

// ConvertNamespaceToPath converts PSR-4 namespace to file system path.
// Example: "App\\Http\\Controllers\\" -> "App/Http/Controllers/"
func ConvertNamespaceToPath(namespace string) string {
	if namespace == "" {
		return ""
	}

	// Convert backslashes to forward slashes
	path := strings.ReplaceAll(namespace, "\\", "/")

	// Ensure trailing slash is preserved
	if !strings.HasSuffix(path, "/") && namespace != "" {
		path += "/"
	}

	return path
}

// ConvertPathToNamespace converts file system path to PSR-4 namespace.
// Example: "App/Http/Controllers/" -> "App\\Http\\Controllers\\"
func ConvertPathToNamespace(path string) string {
	if path == "" {
		return ""
	}

	// Normalize path first
	path = NormalizePath(path)

	// Convert forward slashes to backslashes
	namespace := strings.ReplaceAll(path, "/", "\\")

	// Ensure trailing backslash for non-empty namespace
	if namespace != "" && !strings.HasSuffix(namespace, "\\") {
		namespace += "\\"
	}

	return namespace
}

// FindLongestMatchingPrefix finds the longest namespace prefix that matches the given FQCN.
// Returns the matching namespace and remaining class path.
// Implements PSR-4 specification: longest prefix wins.
func FindLongestMatchingPrefix(fqcn string, namespaces []string) (string, string) {
	if len(namespaces) == 0 {
		return "", fqcn
	}

	// Sort namespaces by length (longest first) for deterministic behavior
	sortedNamespaces := make([]string, len(namespaces))
	copy(sortedNamespaces, namespaces)
	sort.Slice(sortedNamespaces, func(i, j int) bool {
		if len(sortedNamespaces[i]) != len(sortedNamespaces[j]) {
			return len(sortedNamespaces[i]) > len(sortedNamespaces[j])
		}
		// For same length, sort alphabetically for determinism
		return sortedNamespaces[i] < sortedNamespaces[j]
	})

	// Normalize FQCN for matching
	normalizedFQCN := strings.TrimLeft(fqcn, "\\")
	if !strings.HasSuffix(normalizedFQCN, "\\") {
		normalizedFQCN += "\\"
	}

	for _, namespace := range sortedNamespaces {
		normalizedNS := NormalizeNamespace(namespace)

		// Handle empty namespace (root namespace)
		if normalizedNS == "" {
			return "", normalizedFQCN
		}

		// Check if FQCN starts with this namespace
		if strings.HasPrefix(normalizedFQCN, normalizedNS) {
			remaining := strings.TrimPrefix(normalizedFQCN, normalizedNS)
			// Remove trailing backslash from remaining path
			remaining = strings.TrimSuffix(remaining, "\\")
			return normalizedNS, remaining
		}
	}

	// No matching namespace found
	return "", normalizedFQCN
}

// ExtractClassNameFromFQCN extracts the class name from a fully qualified class name.
// Example: "App\\Http\\Controllers\\UserController" -> "UserController"
func ExtractClassNameFromFQCN(fqcn string) string {
	// Normalize FQCN
	normalizedFQCN := strings.Trim(fqcn, "\\")

	// Find last backslash
	lastIndex := strings.LastIndex(normalizedFQCN, "\\")
	if lastIndex == -1 {
		return normalizedFQCN
	}

	return normalizedFQCN[lastIndex+1:]
}

// ExtractNamespaceFromFQCN extracts the namespace part from a fully qualified class name.
// Example: "App\\Http\\Controllers\\UserController" -> "App\\Http\\Controllers\\"
func ExtractNamespaceFromFQCN(fqcn string) string {
	// Normalize FQCN
	normalizedFQCN := strings.Trim(fqcn, "\\")

	// Find last backslash
	lastIndex := strings.LastIndex(normalizedFQCN, "\\")
	if lastIndex == -1 {
		return ""
	}

	return normalizedFQCN[:lastIndex+1]
}

// ValidateFQCNFormat checks if a fully qualified class name is properly formatted.
func ValidateFQCNFormat(fqcn string) error {
	if fqcn == "" {
		return NewMappingError(fqcn, "FQCN cannot be empty", nil)
	}

	// Remove leading/trailing backslashes for validation
	normalized := strings.Trim(fqcn, "\\")

	if normalized == "" {
		return NewMappingError(fqcn, "FQCN must contain class name", nil)
	}

	// Check for invalid characters
	if strings.Contains(normalized, "/") {
		return NewMappingError(fqcn, "FQCN must use backslashes, not forward slashes", nil)
	}

	// Check for consecutive backslashes
	if strings.Contains(normalized, "\\\\") {
		return NewMappingError(fqcn, "FQCN cannot contain consecutive backslashes", nil)
	}

	// Check that each part is a valid identifier
	parts := strings.Split(normalized, "\\")
	for _, part := range parts {
		if part == "" {
			return NewMappingError(fqcn, "FQCN cannot contain empty namespace parts", nil)
		}

		// Basic identifier validation (simplified)
		if !isValidIdentifier(part) {
			return NewMappingError(fqcn, "FQCN contains invalid identifier: "+part, nil)
		}
	}

	return nil
}

// isValidIdentifier performs basic PHP identifier validation.
func isValidIdentifier(identifier string) bool {
	if len(identifier) == 0 {
		return false
	}

	// First character must be letter or underscore
	first := identifier[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}

	// Remaining characters can be letters, digits, or underscores
	for i := 1; i < len(identifier); i++ {
		c := identifier[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}

	return true
}

// GetAllNamespaces extracts all namespace prefixes from PSR-4 mappings.
// Returns sorted list for deterministic behavior.
func GetAllNamespaces(mappings []NamespaceMapping) []string {
	namespaces := make([]string, 0, len(mappings))

	for _, mapping := range mappings {
		namespaces = append(namespaces, mapping.Namespace)
	}

	// Sort for deterministic output
	sort.Strings(namespaces)

	return namespaces
}

// FilterMappingsByDev filters namespace mappings based on dev dependency flag.
func FilterMappingsByDev(mappings []NamespaceMapping, includeDev bool) []NamespaceMapping {
	if includeDev {
		return mappings
	}

	filtered := make([]NamespaceMapping, 0)
	for _, mapping := range mappings {
		if !mapping.IsDevDependency {
			filtered = append(filtered, mapping)
		}
	}

	return filtered
}

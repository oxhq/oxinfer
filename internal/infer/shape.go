// Package infer provides shape inference implementation for Laravel request patterns.
// This file contains implementations of the shape inference interfaces.
package infer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/garaekz/oxinfer/internal/matchers"
)

// DefaultContentTypeDetector implements ContentTypeDetector interface.
type DefaultContentTypeDetector struct {
	config *InferenceConfig
}

// NewContentTypeDetector creates a new content type detector with the given configuration.
func NewContentTypeDetector(config *InferenceConfig) *DefaultContentTypeDetector {
	if config == nil {
		config = DefaultInferenceConfig()
	}

	return &DefaultContentTypeDetector{
		config: config,
	}
}

// DetectContentType analyzes patterns to determine the most appropriate content type.
// Priority: multipart/form-data > application/json > application/x-www-form-urlencoded
func (d *DefaultContentTypeDetector) DetectContentType(patterns []matchers.RequestUsageMatch) string {
	if len(patterns) == 0 {
		return "application/json" // Laravel default for APIs
	}

	// Count occurrences of each content type indicator
	contentTypeScores := map[string]int{
		"multipart/form-data":               0,
		"application/json":                  0,
		"application/x-www-form-urlencoded": 0,
	}

	// Sort patterns by confidence for deterministic processing
	sortedPatterns := make([]matchers.RequestUsageMatch, len(patterns))
	copy(sortedPatterns, patterns)
	sort.Slice(sortedPatterns, func(i, j int) bool {
		// Sort by methods count first for consistency
		if len(sortedPatterns[i].Methods) != len(sortedPatterns[j].Methods) {
			return len(sortedPatterns[i].Methods) > len(sortedPatterns[j].Methods)
		}
		// Then by body parameters count
		return len(sortedPatterns[i].Body) > len(sortedPatterns[j].Body)
	})

	for _, pattern := range sortedPatterns {
		// Check for explicit file upload patterns
		if d.hasFileUploadMethods(pattern.Methods) || len(pattern.Files) > 0 {
			contentTypeScores["multipart/form-data"] += 10
		}

		// Check for JSON-specific patterns
		if d.hasJSONMethods(pattern.Methods) {
			contentTypeScores["application/json"] += 5
		}

		// Check explicit content types already detected (highest priority)
		for _, contentType := range pattern.ContentTypes {
			if score, exists := contentTypeScores[contentType]; exists {
				contentTypeScores[contentType] = score + 8 // Higher than other patterns
			}
		}

		// Check for form data patterns (strong indicator for form handling)
		if d.hasFormMethods(pattern.Methods) {
			contentTypeScores["application/x-www-form-urlencoded"] += 4
		}

		// If no specific patterns, default to JSON for body parameters
		if len(pattern.Body) > 0 && !d.hasFileUploadMethods(pattern.Methods) && !d.hasJSONMethods(pattern.Methods) && !d.hasFormMethods(pattern.Methods) {
			contentTypeScores["application/json"] += 3
		}

		// Boost JSON for body parameters when no explicit file uploads
		if len(pattern.Body) > 0 && !d.hasFileUploadMethods(pattern.Methods) && len(pattern.Files) == 0 {
			contentTypeScores["application/json"] += 3 // Increased boost for JSON with body
		}
	}

	// Return the highest scoring content type based on Laravel priorities
	return d.selectHighestPriorityContentType(contentTypeScores)
}

// HasFileUploads checks if the patterns indicate file upload requirements.
func (d *DefaultContentTypeDetector) HasFileUploads(patterns []matchers.RequestUsageMatch) bool {
	for _, pattern := range patterns {
		// Check for explicit file parameters
		if len(pattern.Files) > 0 {
			return true
		}

		// Check for file-related methods
		if d.hasFileUploadMethods(pattern.Methods) {
			return true
		}

		// Check for multipart content type
		for _, contentType := range pattern.ContentTypes {
			if contentType == "multipart/form-data" {
				return true
			}
		}
	}
	return false
}

// hasFileUploadMethods checks if methods indicate file upload patterns.
func (d *DefaultContentTypeDetector) hasFileUploadMethods(methods []string) bool {
	fileUploadMethods := map[string]bool{
		"file":    true,
		"hasFile": true,
	}

	for _, method := range methods {
		if fileUploadMethods[method] {
			return true
		}
	}
	return false
}

// hasJSONMethods checks if methods indicate JSON content patterns.
func (d *DefaultContentTypeDetector) hasJSONMethods(methods []string) bool {
	jsonMethods := map[string]bool{
		"json": true,
	}

	for _, method := range methods {
		if jsonMethods[method] {
			return true
		}
	}
	return false
}

// hasFormMethods checks if methods indicate form data patterns.
func (d *DefaultContentTypeDetector) hasFormMethods(methods []string) bool {
	formMethods := map[string]bool{
		"validate":  true,
		"validated": true,
		"all":       true,
		"only":      true,
		"except":    true,
		// Note: "input" is NOT included here - it defaults to JSON in Laravel APIs
	}

	for _, method := range methods {
		if formMethods[method] {
			return true
		}
	}
	return false
}

// selectHighestPriorityContentType returns the content type with highest score,
// using Laravel convention priorities as tiebreakers.
func (d *DefaultContentTypeDetector) selectHighestPriorityContentType(scores map[string]int) string {
	// Priority order based on Laravel conventions
	priorityOrder := []string{
		"multipart/form-data",               // Highest priority - file uploads are explicit
		"application/json",                  // Second - Laravel API default
		"application/x-www-form-urlencoded", // Third - traditional forms
	}

	maxScore := -1
	selectedType := "application/json" // Default fallback

	// Check priorities in order, selecting the first with maximum score
	for _, contentType := range priorityOrder {
		if score := scores[contentType]; score > maxScore {
			maxScore = score
			selectedType = contentType
		}
	}

	// If no patterns detected, use config preferences
	if maxScore == 0 {
		for _, preferredType := range d.config.PreferredContentTypes {
			if _, exists := scores[preferredType]; exists {
				return preferredType
			}
		}
	}

	return selectedType
}

// DefaultKeyPathParser implements KeyPathParser interface.
type DefaultKeyPathParser struct {
	config *InferenceConfig
}

// NewKeyPathParser creates a new key path parser with the given configuration.
func NewKeyPathParser(config *InferenceConfig) *DefaultKeyPathParser {
	if config == nil {
		config = DefaultInferenceConfig()
	}

	return &DefaultKeyPathParser{
		config: config,
	}
}

// ParseKeyPath converts a dot notation path into PathSegment components.
func (p *DefaultKeyPathParser) ParseKeyPath(path string) ([]PathSegment, error) {
	if path == "" {
		return nil, NewShapeInferenceError(
			ErrorTypeKeyPathParsing,
			"empty path provided",
			"ParseKeyPath",
		)
	}

	// Split path by dots and process each segment
	parts := strings.Split(path, ".")
	segments := make([]PathSegment, 0, len(parts))

	for _, part := range parts {
		if part == "" {
			continue // Skip empty segments from consecutive dots
		}

		segment := PathSegment{
			Key:        part,
			IsArray:    false,
			IsWildcard: false,
		}

		// Check for array notation
		if isArray, arrayKey := p.IsArrayNotation(part); isArray {
			segment.IsArray = true
			segment.Key = strings.Split(part, "[")[0] // Base key without brackets
			segment.ArrayKey = arrayKey
		}

		// Check for wildcard notation
		if part == "*" {
			segment.IsWildcard = true
			segment.Key = "*"
		}

		segments = append(segments, segment)
	}

	if len(segments) > p.config.MaxDepth {
		return segments[:p.config.MaxDepth], NewShapeInferenceError(
			ErrorTypeKeyPathParsing,
			fmt.Sprintf("path depth %d exceeds maximum %d", len(segments), p.config.MaxDepth),
			fmt.Sprintf("path: %s", path),
		)
	}

	return segments, nil
}

// IsArrayNotation checks if a path segment represents array notation and extracts the key.
func (p *DefaultKeyPathParser) IsArrayNotation(segment string) (bool, string) {
	// Check for patterns like "users[0]", "items[key]", "data[]"
	if !strings.Contains(segment, "[") || !strings.Contains(segment, "]") {
		return false, ""
	}

	// Find the array key between brackets
	startIdx := strings.Index(segment, "[")
	endIdx := strings.LastIndex(segment, "]")

	if startIdx >= endIdx {
		return false, ""
	}

	arrayKey := segment[startIdx+1 : endIdx]

	// Empty brackets indicate array append operation
	if arrayKey == "" {
		return true, ""
	}

	// Remove quotes if present
	arrayKey = strings.Trim(arrayKey, `"'`)

	return true, arrayKey
}

// DefaultShapeInferencer implements ShapeInferencer interface.
type DefaultShapeInferencer struct {
	contentTypeDetector ContentTypeDetector
	keyPathParser       KeyPathParser
	propertyMerger      PropertyMerger
	config              *InferenceConfig
}

// NewShapeInferencer creates a new shape inferencer with the given components.
func NewShapeInferencer(
	contentTypeDetector ContentTypeDetector,
	keyPathParser KeyPathParser,
	propertyMerger PropertyMerger,
	config *InferenceConfig,
) *DefaultShapeInferencer {
	if config == nil {
		config = DefaultInferenceConfig()
	}
	if contentTypeDetector == nil {
		contentTypeDetector = NewContentTypeDetector(config)
	}
	if keyPathParser == nil {
		keyPathParser = NewKeyPathParser(config)
	}
	if propertyMerger == nil {
		propertyMerger = NewPropertyMerger(config)
	}

	return &DefaultShapeInferencer{
		contentTypeDetector: contentTypeDetector,
		keyPathParser:       keyPathParser,
		propertyMerger:      propertyMerger,
		config:              config,
	}
}

// InferRequestShape analyzes request usage patterns and produces a RequestInfo structure.
func (s *DefaultShapeInferencer) InferRequestShape(patterns []matchers.RequestUsageMatch) (*RequestInfo, error) {
	consolidated, err := s.ConsolidatePatterns(patterns)
	if err != nil {
		return nil, fmt.Errorf("failed to consolidate patterns: %w", err)
	}

	requestInfo := &RequestInfo{
		ContentTypes: consolidated.ContentTypes,
		Body:         *CreateEmptyOrderedObject(),
		Query:        *CreateEmptyOrderedObject(),
		Files:        *CreateEmptyOrderedObject(),
	}

	// Convert body properties
	if len(consolidated.Body) > 0 {
		s.convertPropertyMapToOrderedObject(consolidated.Body, &requestInfo.Body)

		// Process Laravel-specific patterns (like only() calls)
		if err := s.processLaravelPatterns(patterns, &requestInfo.Body); err != nil {
			return nil, fmt.Errorf("failed to process Laravel patterns for body: %w", err)
		}
	}

	// Convert query properties
	if len(consolidated.Query) > 0 {
		s.convertPropertyMapToOrderedObject(consolidated.Query, &requestInfo.Query)
	}

	// Convert file properties
	if len(consolidated.Files) > 0 {
		s.convertPropertyMapToOrderedObject(consolidated.Files, &requestInfo.Files)
	}

	return requestInfo, nil
}

// processLaravelPatterns processes Laravel-specific patterns like only() calls to create nested structures.
// This method handles the T11 acceptance criteria for interpreting dot notation paths.
func (s *DefaultShapeInferencer) processLaravelPatterns(patterns []matchers.RequestUsageMatch, target *OrderedObject) error {
	// Extract Laravel method patterns that indicate nested structures
	laravelPaths := make([]string, 0)
	pathsToRemove := make(map[string]bool)

	for _, pattern := range patterns {
		// Look for Laravel methods that suggest path-based access
		for _, method := range pattern.Methods {
			if method == "only" || method == "except" {
				// These methods often contain path specifications
				// Check if body contains path-like keys
				for key := range pattern.Body {
					if s.isPathLikeKey(key) {
						laravelPaths = append(laravelPaths, key)
						pathsToRemove[key] = true
					}
				}
			}
		}

		// Also check for direct path-like keys in body parameters
		for key := range pattern.Body {
			if s.isPathLikeKey(key) && !s.containsPath(laravelPaths, key) {
				laravelPaths = append(laravelPaths, key)
				pathsToRemove[key] = true
			}
		}
	}

	// Remove the path-like keys from target since they will be replaced with nested structure
	for pathKey := range pathsToRemove {
		s.removePropertyFromTarget(target, pathKey)
	}

	// If we found Laravel paths, create nested structures
	if len(laravelPaths) > 0 {
		nestedObj, err := MergePaths(s.keyPathParser, laravelPaths)
		if err != nil {
			return NewShapeInferenceError(
				ErrorTypeKeyPathParsing,
				fmt.Sprintf("failed to merge Laravel paths: %s", err.Error()),
				fmt.Sprintf("paths: %v", laravelPaths),
			)
		}

		// Merge the nested structure into the target
		if err := mergeNestedObjects(target, nestedObj); err != nil {
			return NewShapeInferenceError(
				ErrorTypePatternMerge,
				fmt.Sprintf("failed to merge nested Laravel patterns: %s", err.Error()),
				"processLaravelPatterns",
			)
		}
	}

	return nil
}

// removePropertyFromTarget removes a property key from the target OrderedObject.
func (s *DefaultShapeInferencer) removePropertyFromTarget(target *OrderedObject, key string) {
	// Remove from properties map
	delete(target.Properties, key)

	// Remove from order slice
	newOrder := make([]string, 0, len(target.Order))
	for _, orderKey := range target.Order {
		if orderKey != key {
			newOrder = append(newOrder, orderKey)
		}
	}
	target.Order = newOrder
}

// isPathLikeKey checks if a key looks like a dot notation path (e.g., "users.*.email").
func (s *DefaultShapeInferencer) isPathLikeKey(key string) bool {
	// Check for dot notation
	if strings.Contains(key, ".") {
		return true
	}

	// Check for wildcard notation
	if strings.Contains(key, "*") {
		return true
	}

	// Check for array notation
	if strings.Contains(key, "[") && strings.Contains(key, "]") {
		return true
	}

	return false
}

// containsPath checks if a path already exists in the slice.
func (s *DefaultShapeInferencer) containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

// convertPropertyMapToOrderedObject converts a map of PropertyInfo to OrderedObject.
func (s *DefaultShapeInferencer) convertPropertyMapToOrderedObject(source map[string]*PropertyInfo, target *OrderedObject) {
	if len(source) == 0 {
		return
	}

	// Sort keys for deterministic processing
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		prop := source[key]
		if prop == nil {
			prop = CreateStringProperty("", "") // Default to string type
		}
		target.AddProperty(key, prop)
	}
}

// ConsolidatePatterns merges multiple request usage patterns into a unified structure.
func (s *DefaultShapeInferencer) ConsolidatePatterns(patterns []matchers.RequestUsageMatch) (*ConsolidatedRequest, error) {
	if len(patterns) == 0 {
		return &ConsolidatedRequest{
			ContentTypes: []string{"application/json"},
			Body:         make(map[string]*PropertyInfo),
			Query:        make(map[string]*PropertyInfo),
			Files:        make(map[string]*PropertyInfo),
			Methods:      make([]string, 0),
			Sources:      make([]*RequestUsageSource, 0),
		}, nil
	}

	consolidated := &ConsolidatedRequest{
		Body:    make(map[string]*PropertyInfo),
		Query:   make(map[string]*PropertyInfo),
		Files:   make(map[string]*PropertyInfo),
		Methods: make([]string, 0),
		Sources: make([]*RequestUsageSource, 0),
	}

	// Detect content type using the content type detector
	primaryContentType := s.contentTypeDetector.DetectContentType(patterns)
	consolidated.ContentTypes = []string{primaryContentType}

	// Merge all patterns
	for _, pattern := range patterns {
		// Merge methods
		for _, method := range pattern.Methods {
			if !s.containsString(consolidated.Methods, method) {
				consolidated.Methods = append(consolidated.Methods, method)
			}
		}

		// Merge body parameters
		s.mergeProperties(pattern.Body, consolidated.Body)

		// Merge query parameters
		s.mergeProperties(pattern.Query, consolidated.Query)

		// Merge file parameters
		s.mergeProperties(pattern.Files, consolidated.Files)
	}

	// Sort for determinism
	sort.Strings(consolidated.Methods)
	sort.Strings(consolidated.ContentTypes)

	return consolidated, nil
}

// mergeProperties merges properties from source into target map using intelligent type inference.
func (s *DefaultShapeInferencer) mergeProperties(source map[string]any, target map[string]*PropertyInfo) {
	// Convert the source map to PropertyInfo using PropertyMerger
	sourceObj, err := s.propertyMerger.ConvertToOrderedObject(source)
	if err != nil {
		// If conversion fails, fall back to simple property creation
		for key, value := range source {
			if _, exists := target[key]; !exists {
				target[key] = s.inferPropertyType(value)
			}
		}
		return
	}

	// Merge the converted properties
	for key, newProp := range sourceObj.Properties {
		if existing, exists := target[key]; exists {
			// Property already exists - merge using PropertyMerger
			merged, err := s.propertyMerger.MergeProperties([]*PropertyInfo{existing, newProp})
			if err != nil {
				// If merge fails, keep existing
				continue
			}
			target[key] = merged
		} else {
			// Add new property
			target[key] = newProp.Clone()
		}
	}
}

// inferPropertyType infers PropertyInfo from an interface{} value (fallback method).
func (s *DefaultShapeInferencer) inferPropertyType(value any) *PropertyInfo {
	switch v := value.(type) {
	case string:
		return CreateStringProperty("", "")
	case int, int64, float64:
		return CreateNumberProperty("", "")
	case bool:
		return CreateStringProperty("", "") // Laravel often treats booleans as strings
	case map[string]any:
		if len(v) == 0 {
			return CreateStringProperty("", "") // Default for empty objects
		}
		// For complex nested objects, use PropertyMerger
		if nestedObj, err := s.propertyMerger.ConvertToOrderedObject(v); err == nil {
			return CreateObjectProperty(nestedObj, "")
		}
		return CreateStringProperty("", "") // Fallback
	default:
		return CreateStringProperty("", "") // Default fallback
	}
}

// containsString checks if a string slice contains a specific string.
func (s *DefaultShapeInferencer) containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// BuildNestedObject converts a slice of PathSegments into a nested OrderedObject structure.
// This is a utility function to help build nested objects from parsed paths.
func BuildNestedObject(segments []PathSegment) *OrderedObject {
	if len(segments) == 0 {
		return CreateEmptyOrderedObject()
	}

	// Start with the root object
	root := CreateEmptyOrderedObject()
	current := root

	// Process all segments except the last one
	for i := 0; i < len(segments)-1; i++ {
		segment := segments[i]
		nextSegment := segments[i+1]

		var prop *PropertyInfo

		// Check if the NEXT segment is a wildcard - if so, current segment should be an array
		if nextSegment.IsWildcard {
			// Create array property - the next segment is a wildcard, so this should be an array
			nestedObj := CreateEmptyOrderedObject()
			itemProp := CreateObjectProperty(nestedObj, "")
			prop = CreateArrayProperty(itemProp, "")

			// Add the property with the current segment key
			current.AddProperty(segment.Key, prop)
			// Move to the array item properties for next iteration
			current = prop.Items.Properties

			// Skip the wildcard segment since we've handled it
			i++ // This will skip the next iteration which would be the wildcard
		} else if segment.IsArray {
			// Current segment itself is an array notation like [0]
			nestedObj := CreateEmptyOrderedObject()
			itemProp := CreateObjectProperty(nestedObj, "")
			prop = CreateArrayProperty(itemProp, "")

			current.AddProperty(segment.Key, prop)
			current = prop.Items.Properties
		} else {
			// Regular object property
			nestedObj := CreateEmptyOrderedObject()
			prop = CreateObjectProperty(nestedObj, "")

			current.AddProperty(segment.Key, prop)
			current = prop.Properties
		}
	}

	// Handle the final segment - this becomes a terminal property
	finalSegment := segments[len(segments)-1]

	// Skip if the final segment is a wildcard (it was already handled)
	if finalSegment.IsWildcard {
		return root
	}

	var finalProp *PropertyInfo

	if finalSegment.IsArray {
		// Terminal array - assume string items for now
		stringProp := CreateStringProperty("", "")
		finalProp = CreateArrayProperty(stringProp, "")
	} else {
		// Terminal string property
		finalProp = CreateStringProperty("", "")
	}

	current.AddProperty(finalSegment.Key, finalProp)

	return root
}

// PathSegmentsToNestedObject is a convenience function that combines parsing and building.
func PathSegmentsToNestedObject(parser KeyPathParser, path string) (*OrderedObject, error) {
	segments, err := parser.ParseKeyPath(path)
	if err != nil {
		return nil, err
	}

	return BuildNestedObject(segments), nil
}

// MergePaths merges multiple key paths into a single nested structure.
// This is useful for processing Laravel only() calls with multiple paths.
func MergePaths(parser KeyPathParser, paths []string) (*OrderedObject, error) {
	if len(paths) == 0 {
		return CreateEmptyOrderedObject(), nil
	}

	root := CreateEmptyOrderedObject()

	for _, path := range paths {
		pathObject, err := PathSegmentsToNestedObject(parser, path)
		if err != nil {
			return nil, NewShapeInferenceError(
				ErrorTypeKeyPathParsing,
				fmt.Sprintf("failed to parse path '%s': %s", path, err.Error()),
				"merging multiple paths",
			)
		}

		if err := mergeNestedObjects(root, pathObject); err != nil {
			return nil, NewShapeInferenceError(
				ErrorTypeKeyPathParsing,
				fmt.Sprintf("failed to merge path '%s': %s", path, err.Error()),
				"merging multiple paths",
			)
		}
	}

	return root, nil
}

// mergeNestedObjects recursively merges source into target.
func mergeNestedObjects(target, source *OrderedObject) error {
	if source == nil || source.IsEmpty() {
		return nil
	}

	// Process properties in deterministic order
	keys := source.Order
	if len(keys) == 0 {
		keys = make([]string, 0, len(source.Properties))
		for k := range source.Properties {
			keys = append(keys, k)
		}
		// Sort for deterministic processing
		for i := 0; i < len(keys)-1; i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[i] > keys[j] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
	}

	for _, key := range keys {
		sourceProp := source.Properties[key]
		if sourceProp == nil {
			continue
		}

		if existingProp, exists := target.GetProperty(key); exists {
			// Property exists, need to merge
			if err := mergePropertyInfos(existingProp, sourceProp); err != nil {
				return fmt.Errorf("failed to merge property '%s': %w", key, err)
			}
		} else {
			// Property doesn't exist, add it
			target.AddProperty(key, sourceProp.Clone())
		}
	}

	return nil
}

// mergePropertyInfos merges two PropertyInfo structures.
func mergePropertyInfos(target, source *PropertyInfo) error {
	// If types don't match, prefer object type for flexibility
	if target.Type != source.Type {
		if target.Type != PropertyTypeObject && source.Type == PropertyTypeObject {
			target.Type = PropertyTypeObject
			target.Properties = source.Properties.Clone()
		}
		return nil
	}

	// Handle object type merging
	if target.Type == PropertyTypeObject && source.Type == PropertyTypeObject {
		if target.Properties == nil {
			target.Properties = source.Properties.Clone()
		} else if source.Properties != nil {
			return mergeNestedObjects(target.Properties, source.Properties)
		}
	}

	// Handle array type merging
	if target.Type == PropertyTypeArray && source.Type == PropertyTypeArray {
		if target.Items != nil && source.Items != nil {
			return mergePropertyInfos(target.Items, source.Items)
		}
	}

	return nil
}

// DefaultPropertyMerger implements PropertyMerger interface.
type DefaultPropertyMerger struct {
	config *InferenceConfig
}

// NewPropertyMerger creates a new property merger with the given configuration.
func NewPropertyMerger(config *InferenceConfig) *DefaultPropertyMerger {
	if config == nil {
		config = DefaultInferenceConfig()
	}

	return &DefaultPropertyMerger{
		config: config,
	}
}

// MergeProperties combines multiple PropertyInfo structures into one with intelligent conflict resolution.
func (m *DefaultPropertyMerger) MergeProperties(props []*PropertyInfo) (*PropertyInfo, error) {
	if len(props) == 0 {
		return nil, NewShapeInferenceError(
			ErrorTypePropertyConversion,
			"no properties to merge",
			"MergeProperties",
		)
	}

	if len(props) == 1 {
		return props[0].Clone(), nil
	}

	// Start with the first property as base
	result := props[0].Clone()

	// Merge remaining properties
	for i := 1; i < len(props); i++ {
		if err := m.mergeIntoProperty(result, props[i]); err != nil {
			return nil, NewShapeInferenceError(
				ErrorTypePropertyConversion,
				fmt.Sprintf("failed to merge property at index %d: %s", i, err.Error()),
				"MergeProperties",
			)
		}
	}

	return result, nil
}

// ConvertToOrderedObject converts raw map data from matchers to structured OrderedObject.
func (m *DefaultPropertyMerger) ConvertToOrderedObject(data map[string]any) (*OrderedObject, error) {
	if len(data) == 0 {
		return CreateEmptyOrderedObject(), nil
	}

	result := CreateEmptyOrderedObject()

	// Sort keys for deterministic processing
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := data[key]

		// Convert the value to PropertyInfo
		propInfo, err := m.convertValueToPropertyInfo(value)
		if err != nil {
			return nil, NewShapeInferenceError(
				ErrorTypePropertyConversion,
				fmt.Sprintf("failed to convert value for key '%s': %s", key, err.Error()),
				fmt.Sprintf("ConvertToOrderedObject: %T", value),
			)
		}

		result.AddProperty(key, propInfo)
	}

	return result, nil
}

// mergeIntoProperty merges source PropertyInfo into target PropertyInfo.
func (m *DefaultPropertyMerger) mergeIntoProperty(target, source *PropertyInfo) error {
	if source == nil {
		return nil
	}

	// Handle type conflicts with intelligent resolution
	resolvedType, err := m.resolveTypeConflict(target.Type, source.Type)
	if err != nil {
		return err
	}

	target.Type = resolvedType

	// Merge descriptions (prefer non-empty)
	if target.Description == "" && source.Description != "" {
		target.Description = source.Description
	}

	// Merge formats (prefer non-empty)
	if target.Format == "" && source.Format != "" {
		target.Format = source.Format
	}

	// Handle type-specific merging
	switch target.Type {
	case PropertyTypeObject:
		return m.mergeObjectProperties(target, source)
	case PropertyTypeArray:
		return m.mergeArrayProperties(target, source)
	default:
		// For primitive types, target wins (already resolved above)
		return nil
	}
}

// resolveTypeConflict resolves conflicts between two property types using Laravel conventions.
func (m *DefaultPropertyMerger) resolveTypeConflict(type1, type2 PropertyType) (PropertyType, error) {
	if type1 == type2 {
		return type1, nil
	}

	if !m.config.MergeSimilarTypes {
		// Return the first type if merging is disabled
		return type1, nil
	}

	// Define priority order: Object > Array > File > Number > String
	typePriority := map[PropertyType]int{
		PropertyTypeObject: 5,
		PropertyTypeArray:  4,
		PropertyTypeFile:   3,
		PropertyTypeNumber: 2,
		PropertyTypeString: 1,
	}

	priority1, exists1 := typePriority[type1]
	priority2, exists2 := typePriority[type2]

	if !exists1 || !exists2 {
		return PropertyTypeString, nil // Default fallback
	}

	// Return the type with higher priority
	if priority1 >= priority2 {
		return type1, nil
	}

	return type2, nil
}

// mergeObjectProperties merges object properties from source into target.
func (m *DefaultPropertyMerger) mergeObjectProperties(target, source *PropertyInfo) error {
	// Ensure target has object properties
	if target.Properties == nil {
		target.Properties = CreateEmptyOrderedObject()
	}

	// If source is an object, merge its properties
	if source.Type == PropertyTypeObject && source.Properties != nil {
		return mergeNestedObjects(target.Properties, source.Properties)
	}

	return nil
}

// mergeArrayProperties merges array item properties from source into target.
func (m *DefaultPropertyMerger) mergeArrayProperties(target, source *PropertyInfo) error {
	// If source is also an array, merge the item types
	if source.Type == PropertyTypeArray && source.Items != nil {
		if target.Items == nil {
			target.Items = source.Items.Clone()
		} else {
			return m.mergeIntoProperty(target.Items, source.Items)
		}
	}

	return nil
}

// convertValueToPropertyInfo converts an interface{} value to PropertyInfo.
func (m *DefaultPropertyMerger) convertValueToPropertyInfo(value any) (*PropertyInfo, error) {
	switch v := value.(type) {
	case string:
		return CreateStringProperty("", ""), nil

	case int, int32, int64:
		return CreateNumberProperty("", "integer"), nil

	case float32, float64:
		return CreateNumberProperty("", "float"), nil

	case bool:
		// Laravel often treats booleans as strings in request validation
		return CreateStringProperty("", ""), nil

	case []any:
		// Handle arrays
		if len(v) == 0 {
			// Empty array - assume string items
			return CreateArrayProperty(CreateStringProperty("", ""), ""), nil
		}

		// Infer item type from first element
		itemProp, err := m.convertValueToPropertyInfo(v[0])
		if err != nil {
			return nil, err
		}

		// Merge with other elements if they exist
		if len(v) > 1 {
			allItems := make([]*PropertyInfo, len(v))
			allItems[0] = itemProp

			for i := 1; i < len(v); i++ {
				itemProp2, err := m.convertValueToPropertyInfo(v[i])
				if err != nil {
					return nil, err
				}
				allItems[i] = itemProp2
			}

			mergedItem, err := m.MergeProperties(allItems)
			if err != nil {
				return nil, err
			}
			itemProp = mergedItem
		}

		return CreateArrayProperty(itemProp, ""), nil

	case map[string]any:
		// Handle nested objects
		nestedObj, err := m.ConvertToOrderedObject(v)
		if err != nil {
			return nil, err
		}

		return CreateObjectProperty(nestedObj, ""), nil

	case nil:
		// Null values default to string
		return CreateStringProperty("", ""), nil

	default:
		// Unknown types default to string
		return CreateStringProperty("", ""), nil
	}
}

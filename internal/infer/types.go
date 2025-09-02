// Package infer provides shape inference functionality for Laravel request patterns.
// It analyzes request usage patterns and consolidates them into OpenAPI-compatible shapes.
package infer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/garaekz/oxinfer/internal/matchers"
)

// PropertyType represents the data type of a request property.
type PropertyType string

const (
	PropertyTypeString = "string"
	PropertyTypeNumber = "number"
	PropertyTypeArray  = "array"
	PropertyTypeObject = "object"
	PropertyTypeFile   = "file"
)

// OrderedObject represents a structured object with deterministic property ordering.
// It maintains both the properties and their insertion order for consistent JSON output.
type OrderedObject struct {
	Properties map[string]*PropertyInfo `json:"properties"`
	Required   []string                 `json:"required,omitempty"`
	Order      []string                 `json:"order"` // Property insertion order for determinism
}

// PropertyInfo describes the characteristics of a request property including type and metadata.
type PropertyInfo struct {
	Type        PropertyType   `json:"type"`
	Description string         `json:"description,omitempty"`
	Format      string         `json:"format,omitempty"`
	Items       *PropertyInfo  `json:"items,omitempty"`       // For arrays
	Properties  *OrderedObject `json:"properties,omitempty"` // For nested objects
}

// MarshalJSON implements deterministic JSON encoding for OrderedObject.
// Properties are marshaled in the order specified by the Order field.
func (o *OrderedObject) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("{}"), nil
	}

	var buf bytes.Buffer
	buf.WriteByte('{')

	// Determine property order - use Order if available, otherwise sort keys
	keys := o.Order
	if len(keys) == 0 {
		keys = make([]string, 0, len(o.Properties))
		for k := range o.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}

	written := 0
	for _, k := range keys {
		if prop, exists := o.Properties[k]; exists {
			if written > 0 {
				buf.WriteByte(',')
			}

			// Encode property key
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal property key %q: %w", k, err)
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')

			// Encode property value
			propBytes, err := json.Marshal(prop)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal property %q: %w", k, err)
			}
			buf.Write(propBytes)
			written++
		}
	}

	// Marshal required array if present and non-empty
	if len(o.Required) > 0 {
		if written > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`"required":`)
		reqBytes, err := json.Marshal(o.Required)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal required array: %w", err)
		}
		buf.Write(reqBytes)
		written++
	}

	// Marshal order array
	if written > 0 {
		buf.WriteByte(',')
	}
	buf.WriteString(`"order":`)
	orderBytes, err := json.Marshal(o.Order)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order array: %w", err)
	}
	buf.Write(orderBytes)

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ConsolidatedRequest represents merged request patterns from multiple matchers.
// It aggregates all detected request usage patterns into a unified structure.
type ConsolidatedRequest struct {
	ContentTypes []string                   `json:"contentTypes"`
	Body         map[string]*PropertyInfo   `json:"body,omitempty"`
	Query        map[string]*PropertyInfo   `json:"query,omitempty"`
	Files        map[string]*PropertyInfo   `json:"files,omitempty"`
	Methods      []string                   `json:"methods"`
	Sources      []*RequestUsageSource      `json:"sources"` // Track source patterns
}

// RequestUsageSource tracks the origin of request usage patterns for debugging.
type RequestUsageSource struct {
	FilePath   string  `json:"filePath"`
	Method     string  `json:"method,omitempty"`
	Confidence float64 `json:"confidence"`
	Pattern    string  `json:"pattern,omitempty"`
}

// PathSegment represents a component of a dot notation path like "user.profile.name".
type PathSegment struct {
	Key       string `json:"key"`
	IsArray   bool   `json:"isArray"`
	ArrayKey  string `json:"arrayKey,omitempty"` // For associative arrays
	IsWildcard bool  `json:"isWildcard"`         // For paths like "users.*"
}

// RequestInfo represents the final shape inference result for a controller method.
// It contains the inferred structure that will be converted to OpenAPI format.
type RequestInfo struct {
	ContentTypes []string      `json:"contentTypes,omitempty"`
	Body         OrderedObject `json:"body,omitempty"`
	Query        OrderedObject `json:"query,omitempty"`
	Files        OrderedObject `json:"files,omitempty"`
}

// ShapeInferenceError represents errors that occur during shape inference.
type ShapeInferenceError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Context string `json:"context,omitempty"`
}

func (e *ShapeInferenceError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s: %s (context: %s)", e.Type, e.Message, e.Context)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Shape inference error types
const (
	ErrorTypeKeyPathParsing     = "KEY_PATH_PARSING"
	ErrorTypeContentTypeInfer   = "CONTENT_TYPE_INFER"
	ErrorTypePatternMerge       = "PATTERN_MERGE"
	ErrorTypePropertyConversion = "PROPERTY_CONVERSION"
	ErrorTypeValidation         = "VALIDATION"
)

// NewShapeInferenceError creates a new shape inference error.
func NewShapeInferenceError(errorType, message, context string) *ShapeInferenceError {
	return &ShapeInferenceError{
		Type:    errorType,
		Message: message,
		Context: context,
	}
}

// ShapeInferencer provides the main interface for inferring request shapes from patterns.
type ShapeInferencer interface {
	// InferRequestShape analyzes request usage patterns and produces a RequestInfo structure
	InferRequestShape(patterns []matchers.RequestUsageMatch) (*RequestInfo, error)

	// ConsolidatePatterns merges multiple request usage patterns into a unified structure
	ConsolidatePatterns(patterns []matchers.RequestUsageMatch) (*ConsolidatedRequest, error)
}

// KeyPathParser provides functionality for parsing dot notation paths like "user.profile.name".
type KeyPathParser interface {
	// ParseKeyPath converts a dot notation path into PathSegment components
	ParseKeyPath(path string) ([]PathSegment, error)

	// IsArrayNotation checks if a path segment represents array notation and extracts the key
	IsArrayNotation(segment string) (bool, string)
}

// ContentTypeDetector provides content type inference from request patterns.
type ContentTypeDetector interface {
	// DetectContentType analyzes patterns to determine the most appropriate content type
	DetectContentType(patterns []matchers.RequestUsageMatch) string

	// HasFileUploads checks if the patterns indicate file upload requirements
	HasFileUploads(patterns []matchers.RequestUsageMatch) bool
}

// PropertyMerger handles merging and consolidation of property information.
type PropertyMerger interface {
	// MergeProperties combines multiple PropertyInfo structures into one
	MergeProperties(props []*PropertyInfo) (*PropertyInfo, error)

	// ConvertToOrderedObject converts nested map structures to OrderedObject
	ConvertToOrderedObject(data map[string]interface{}) (*OrderedObject, error)
}

// ValidationRule represents a rule for validating inferred shapes.
type ValidationRule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ShapeValidator validates inferred request shapes against business rules.
type ShapeValidator interface {
	// ValidateRequestInfo checks if a RequestInfo structure is valid
	ValidateRequestInfo(info *RequestInfo) error

	// ValidatePropertyInfo checks if a PropertyInfo structure is valid
	ValidatePropertyInfo(prop *PropertyInfo) error

	// GetValidationRules returns the rules used for validation
	GetValidationRules() []ValidationRule
}

// InferenceConfig configures shape inference behavior.
type InferenceConfig struct {
	// Maximum depth for nested object inference
	MaxDepth int `json:"maxDepth"`

	// Maximum number of properties per object
	MaxProperties int `json:"maxProperties"`

	// Whether to infer required properties based on usage frequency
	InferRequired bool `json:"inferRequired"`

	// Minimum confidence threshold for property inclusion
	MinPropertyConfidence float64 `json:"minPropertyConfidence"`

	// Whether to merge similar property types
	MergeSimilarTypes bool `json:"mergeSimilarTypes"`

	// Content type preferences in order
	PreferredContentTypes []string `json:"preferredContentTypes"`
}

// DefaultInferenceConfig returns sensible defaults for shape inference.
func DefaultInferenceConfig() *InferenceConfig {
	return &InferenceConfig{
		MaxDepth:              5,
		MaxProperties:         100,
		InferRequired:         true,
		MinPropertyConfidence: 0.7,
		MergeSimilarTypes:     true,
		PreferredContentTypes: []string{
			"application/json",
			"multipart/form-data",
			"application/x-www-form-urlencoded",
		},
	}
}

// InferenceStats tracks statistics about the shape inference process.
type InferenceStats struct {
	PatternsProcessed   int     `json:"patternsProcessed"`
	PropertiesInferred  int     `json:"propertiesInferred"`
	ContentTypesFound   int     `json:"contentTypesFound"`
	AverageConfidence   float64 `json:"averageConfidence"`
	ProcessingTimeMs    int64   `json:"processingTimeMs"`
	ErrorsEncountered   int     `json:"errorsEncountered"`
}

// String returns a human-readable representation of InferenceStats.
func (s *InferenceStats) String() string {
	return fmt.Sprintf(
		"InferenceStats{patterns: %d, properties: %d, contentTypes: %d, confidence: %.2f, time: %dms, errors: %d}",
		s.PatternsProcessed, s.PropertiesInferred, s.ContentTypesFound,
		s.AverageConfidence, s.ProcessingTimeMs, s.ErrorsEncountered,
	)
}

// CreateEmptyOrderedObject creates a new empty OrderedObject.
func CreateEmptyOrderedObject() *OrderedObject {
	return &OrderedObject{
		Properties: make(map[string]*PropertyInfo),
		Required:   make([]string, 0),
		Order:      make([]string, 0),
	}
}

// AddProperty adds a new property to an OrderedObject while maintaining order.
func (o *OrderedObject) AddProperty(key string, prop *PropertyInfo) {
	if o.Properties == nil {
		o.Properties = make(map[string]*PropertyInfo)
	}
	
	// Add property
	o.Properties[key] = prop
	
	// Add to order if not already present
	for _, existing := range o.Order {
		if existing == key {
			return
		}
	}
	o.Order = append(o.Order, key)
}

// GetProperty retrieves a property by key.
func (o *OrderedObject) GetProperty(key string) (*PropertyInfo, bool) {
	if o.Properties == nil {
		return nil, false
	}
	prop, exists := o.Properties[key]
	return prop, exists
}

// HasProperty checks if a property exists.
func (o *OrderedObject) HasProperty(key string) bool {
	_, exists := o.GetProperty(key)
	return exists
}

// PropertyCount returns the number of properties.
func (o *OrderedObject) PropertyCount() int {
	return len(o.Properties)
}

// IsEmpty checks if the OrderedObject has no properties.
func (o *OrderedObject) IsEmpty() bool {
	return len(o.Properties) == 0
}

// AddRequired adds a property to the required list if not already present.
func (o *OrderedObject) AddRequired(key string) {
	for _, existing := range o.Required {
		if existing == key {
			return
		}
	}
	o.Required = append(o.Required, key)
	sort.Strings(o.Required) // Keep required list sorted for determinism
}

// IsRequired checks if a property is in the required list.
func (o *OrderedObject) IsRequired(key string) bool {
	for _, req := range o.Required {
		if req == key {
			return true
		}
	}
	return false
}

// CreateStringProperty creates a PropertyInfo for a string type.
func CreateStringProperty(description, format string) *PropertyInfo {
	return &PropertyInfo{
		Type:        PropertyTypeString,
		Description: description,
		Format:      format,
	}
}

// CreateNumberProperty creates a PropertyInfo for a number type.
func CreateNumberProperty(description, format string) *PropertyInfo {
	return &PropertyInfo{
		Type:        PropertyTypeNumber,
		Description: description,
		Format:      format,
	}
}

// CreateFileProperty creates a PropertyInfo for a file type.
func CreateFileProperty(description string) *PropertyInfo {
	return &PropertyInfo{
		Type:        PropertyTypeFile,
		Description: description,
		Format:      "binary",
	}
}

// CreateArrayProperty creates a PropertyInfo for an array type.
func CreateArrayProperty(itemsType *PropertyInfo, description string) *PropertyInfo {
	return &PropertyInfo{
		Type:        PropertyTypeArray,
		Description: description,
		Items:       itemsType,
	}
}

// CreateObjectProperty creates a PropertyInfo for an object type.
func CreateObjectProperty(properties *OrderedObject, description string) *PropertyInfo {
	return &PropertyInfo{
		Type:        PropertyTypeObject,
		Description: description,
		Properties:  properties,
	}
}

// Clone creates a deep copy of PropertyInfo.
func (p *PropertyInfo) Clone() *PropertyInfo {
	if p == nil {
		return nil
	}
	
	clone := &PropertyInfo{
		Type:        p.Type,
		Description: p.Description,
		Format:      p.Format,
	}
	
	if p.Items != nil {
		clone.Items = p.Items.Clone()
	}
	
	if p.Properties != nil {
		clone.Properties = p.Properties.Clone()
	}
	
	return clone
}

// Clone creates a deep copy of OrderedObject.
func (o *OrderedObject) Clone() *OrderedObject {
	if o == nil {
		return nil
	}
	
	clone := &OrderedObject{
		Properties: make(map[string]*PropertyInfo, len(o.Properties)),
		Required:   make([]string, len(o.Required)),
		Order:      make([]string, len(o.Order)),
	}
	
	// Copy properties
	for k, v := range o.Properties {
		clone.Properties[k] = v.Clone()
	}
	
	// Copy required and order slices
	copy(clone.Required, o.Required)
	copy(clone.Order, o.Order)
	
	return clone
}
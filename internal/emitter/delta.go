// Package emitter provides delta emission functionality for oxinfer.
// It defines the structured output format that matches delta.schema.json.
package emitter

import (
    "bytes"
    "encoding/json"
    "sort"
)

// Delta represents the complete analysis output structure matching delta.schema.json.
// It contains metadata about the analysis and categorized findings from Laravel/PHP code.
type Delta struct {
	Meta        MetaInfo      `json:"meta"`
	Controllers []Controller  `json:"controllers"`
	Models      []Model       `json:"models"`
	Polymorphic []Polymorphic `json:"polymorphic"`
	Broadcast   []Broadcast   `json:"broadcast"`
}

// MetaInfo contains analysis metadata including completion status and statistics.
type MetaInfo struct {
    Partial bool      `json:"partial"`
    Stats   MetaStats `json:"stats"`
    Version     *string `json:"version,omitempty"`
    GeneratedAt *string `json:"generatedAt,omitempty"`
}

// MetaStats provides quantitative analysis results and performance metrics.
type MetaStats struct {
	FilesParsed int `json:"filesParsed"`
	Skipped     int `json:"skipped"`
	DurationMs  int `json:"durationMs"`
}

// Controller represents a Laravel controller class with its methods and detected patterns.
type Controller struct {
	FQCN        string                  `json:"fqcn"`
	Method      string                  `json:"method"`
	HTTP        *HTTPInfo               `json:"http,omitempty"`
	Request     *RequestInfo            `json:"request,omitempty"`
	Resources   []Resource              `json:"resources,omitempty"`
	ScopesUsed  []ScopeUsed             `json:"scopesUsed,omitempty"`
	Polymorphic []PolymorphicRelation   `json:"polymorphic,omitempty"`
}

// HTTPInfo captures HTTP-related metadata for controller methods.
type HTTPInfo struct {
	Status   *int  `json:"status,omitempty"`
	Explicit *bool `json:"explicit,omitempty"`
}

// RequestInfo describes request handling patterns detected in controller methods.
type RequestInfo struct {
    ContentTypes []string     `json:"contentTypes,omitempty"`
    Body         OrderedObject `json:"body,omitempty"`
    Query        OrderedObject `json:"query,omitempty"`
    Files        OrderedObject `json:"files,omitempty"`
}

// OrderedObject is a recursive map that marshals to JSON with stable key ordering.
type OrderedObject map[string]OrderedObject

// MarshalJSON implements deterministic JSON encoding for OrderedObject.
func (o OrderedObject) MarshalJSON() ([]byte, error) {
    // Collect and sort keys
    keys := make([]string, 0, len(o))
    for k := range o {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    // Manually build JSON object
    var buf bytes.Buffer
    buf.WriteByte('{')
    for i, k := range keys {
        // Encode key
        keyBytes, err := json.Marshal(k)
        if err != nil {
            return nil, err
        }
        buf.Write(keyBytes)
        buf.WriteByte(':')
        // Encode value (recursive). For zero children, emit empty object {}
        child := o[k]
        if child == nil {
            buf.WriteString("{}")
        } else {
            valBytes, err := child.MarshalJSON()
            if err != nil {
                return nil, err
            }
            buf.Write(valBytes)
        }
        if i != len(keys)-1 {
            buf.WriteByte(',')
        }
    }
    buf.WriteByte('}')
    return buf.Bytes(), nil
}

// NewOrderedObjectFromMap converts a nested map[string]interface{} into OrderedObject.
func NewOrderedObjectFromMap(m map[string]interface{}) OrderedObject {
    if m == nil {
        return nil
    }
    out := make(OrderedObject, len(m))
    // build keys sorted for consistency of construction
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        v := m[k]
        // Expect nested map or empty object
        if childMap, ok := v.(map[string]interface{}); ok {
            out[k] = NewOrderedObjectFromMap(childMap)
        } else {
            // Leaf
            out[k] = OrderedObject{}
        }
    }
    return out
}

// Resource represents a Laravel API resource usage pattern.
type Resource struct {
	Class      string `json:"class"`
	Collection bool   `json:"collection"`
}

// ScopeUsed represents detected Eloquent scope usage in controllers.
type ScopeUsed struct {
	On   string   `json:"on"`
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

// Model represents an Eloquent model class with its detected features.
type Model struct {
	FQCN        string                  `json:"fqcn"`
	WithPivot   []PivotInfo             `json:"withPivot,omitempty"`
	Attributes  []Attribute             `json:"attributes,omitempty"`
	Polymorphic []PolymorphicRelation   `json:"polymorphic,omitempty"`
}

// PivotInfo describes pivot table configurations in many-to-many relationships.
type PivotInfo struct {
	Relation   string   `json:"relation"`
	Columns    []string `json:"columns"`
	Alias      *string  `json:"alias,omitempty"`
	Timestamps *bool    `json:"timestamps,omitempty"`
}

// Attribute represents Laravel model attributes created via Attribute::make.
type Attribute struct {
	Name string `json:"name"`
	Via  string `json:"via"`
}

// PolymorphicRelation represents polymorphic relationship patterns in controllers and models.
type PolymorphicRelation struct {
	Relation         string                     `json:"relation"`                   // Relationship method name
	Type             string                     `json:"type"`                       // Polymorphic type (morphTo, morphOne, morphMany)
	MorphType        string                     `json:"morphType,omitempty"`        // Morph type column
	MorphId          string                     `json:"morphId,omitempty"`          // Morph ID column
	Model            *string                    `json:"model,omitempty"`            // Target model class
	Discriminator    *PolymorphicDiscriminator  `json:"discriminator,omitempty"`    // Discriminator mapping
	RelatedModels    []string                   `json:"relatedModels,omitempty"`    // Related model classes
	DepthTruncated   *bool                      `json:"depthTruncated,omitempty"`   // True if max depth reached
	MaxDepth         *int                       `json:"maxDepth,omitempty"`         // Maximum traversal depth
}

// PolymorphicDiscriminator contains discriminator mapping information for polymorphic relationships.
type PolymorphicDiscriminator struct {
	PropertyName string            `json:"propertyName"`           // Discriminator property name
	Mapping      map[string]string `json:"mapping"`                // Type mappings
	Source       string            `json:"source"`                 // Source of mapping (morphMap, explicit, inferred)
	IsExplicit   bool              `json:"isExplicit"`             // Whether mapping is explicitly defined
	DefaultType  *string           `json:"defaultType,omitempty"`  // Default type if no mapping matches
}

// Polymorphic represents detected polymorphic relationship configurations.
type Polymorphic struct {
	Parent         string        `json:"parent"`
	Morph          MorphInfo     `json:"morph"`
	Discriminator  Discriminator `json:"discriminator"`
	DepthTruncated *bool         `json:"depthTruncated,omitempty"`
}

// MorphInfo describes polymorphic relationship column configuration.
type MorphInfo struct {
	Key        string `json:"key"`
	TypeColumn string `json:"typeColumn"`
	IDColumn   string `json:"idColumn"`
}

// Discriminator provides type mapping for polymorphic relationships.
type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping"`
}

// Broadcast represents Laravel broadcasting channel configurations.
type Broadcast struct {
	File           *string  `json:"file,omitempty"`
	Channel        string   `json:"channel"`
	Params         []string `json:"params,omitempty"`
	Visibility     string   `json:"visibility"`
	PayloadLiteral *bool    `json:"payloadLiteral,omitempty"`
}

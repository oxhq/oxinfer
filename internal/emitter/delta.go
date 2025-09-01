// Package emitter provides delta emission functionality for oxinfer.
// It defines the structured output format that matches delta.schema.json.
package emitter

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
}

// MetaStats provides quantitative analysis results and performance metrics.
type MetaStats struct {
	FilesParsed int `json:"filesParsed"`
	Skipped     int `json:"skipped"`
	DurationMs  int `json:"durationMs"`
}

// Controller represents a Laravel controller class with its methods and detected patterns.
type Controller struct {
	FQCN        string       `json:"fqcn"`
	Method      string       `json:"method"`
	HTTP        *HTTPInfo    `json:"http,omitempty"`
	Request     *RequestInfo `json:"request,omitempty"`
	Resources   []Resource   `json:"resources,omitempty"`
	ScopesUsed  []ScopeUsed  `json:"scopesUsed,omitempty"`
}

// HTTPInfo captures HTTP-related metadata for controller methods.
type HTTPInfo struct {
	Status   *int  `json:"status,omitempty"`
	Explicit *bool `json:"explicit,omitempty"`
}

// RequestInfo describes request handling patterns detected in controller methods.
type RequestInfo struct {
	ContentTypes []string               `json:"contentTypes,omitempty"`
	Body         map[string]interface{} `json:"body,omitempty"`
	Query        map[string]interface{} `json:"query,omitempty"`
	Files        map[string]interface{} `json:"files,omitempty"`
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
	FQCN       string      `json:"fqcn"`
	WithPivot  []PivotInfo `json:"withPivot,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
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
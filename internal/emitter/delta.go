// Package emitter provides delta emission functionality for oxinfer.
// It defines the structured output format that matches delta.schema.json.
//go:build goexperiment.jsonv2

package emitter

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"sort"
)

// Delta represents the complete analysis output structure matching delta.schema.json.
// It contains metadata about the analysis and categorized findings from Laravel/PHP code.
type Delta struct {
	Meta        MetaInfo      `json:"meta"`
	Controllers []Controller  `json:"controllers"`
	Models      []Model       `json:"models"`
	Resources   []ResourceDef `json:"resources,omitempty"`
	Polymorphic []Polymorphic `json:"polymorphic"`
	Broadcast   []Broadcast   `json:"broadcast"`
}

// MetaInfo contains analysis metadata including completion status and statistics.
type MetaInfo struct {
	Partial     bool      `json:"partial"`
	Stats       MetaStats `json:"stats"`
	Version     *string   `json:"version,omitempty"`
	GeneratedAt *string   `json:"generatedAt,omitempty"`
}

// MetaStats provides quantitative analysis results and performance metrics.
// Enhanced to support comprehensive pipeline statistics from the stats package.
type MetaStats struct {
	FilesParsed        int64            `json:"filesParsed"`
	Skipped            int64            `json:"skipped"`
	DurationMs         int64            `json:"durationMs"`
	TotalFiles         int64            `json:"totalFiles,omitempty"`
	PhaseStats         map[string]int64 `json:"phaseStats,omitempty"`
	MatchStats         map[string]int   `json:"matchStats,omitempty"`
	ErrorCount         int64            `json:"errorCount,omitempty"`
	StartTime          int64            `json:"startTime,omitempty"`
	EndTime            int64            `json:"endTime,omitempty"`
	InferenceOps       int64            `json:"inferenceOps,omitempty"`
	PropertiesInferred int64            `json:"propertiesInferred,omitempty"`
	CacheHits          int64            `json:"cacheHits,omitempty"`
	CacheMisses        int64            `json:"cacheMisses,omitempty"`
	AssemblerStats     *AssemblerStats  `json:"assemblerStats,omitempty"`
}

// AssemblerStats tracks delta assembly effectiveness for debugging.
type AssemblerStats struct {
	SkippedControllers  int `json:"skippedControllers"`  // Controllers that couldn't be processed
	SkippedModels       int `json:"skippedModels"`       // Models that couldn't be processed
	SkippedPatterns     int `json:"skippedPatterns"`     // Patterns that couldn't be matched to methods
	UnresolvableMatches int `json:"unresolvableMatches"` // Matches that couldn't be resolved to valid keys
}

// MarshalJSON implements deterministic JSON encoding for MetaStats, ensuring
// stable ordering of map fields (phaseStats, matchStats) and consistent field order.
func (m MetaStats) MarshalJSON() ([]byte, error) {
	// Collect sorted keys for maps first
	// PhaseStats
	phaseKeys := make([]string, 0, len(m.PhaseStats))
	for k := range m.PhaseStats {
		phaseKeys = append(phaseKeys, k)
	}
	if len(phaseKeys) > 1 {
		sort.Strings(phaseKeys)
	}
	// MatchStats
	matchKeys := make([]string, 0, len(m.MatchStats))
	for k := range m.MatchStats {
		matchKeys = append(matchKeys, k)
	}
	if len(matchKeys) > 1 {
		sort.Strings(matchKeys)
	}

	// Build JSON manually for deterministic key order
	var buf bytes.Buffer
	buf.WriteByte('{')
	wrote := false

	// Required fields (always present)
	// filesParsed
	buf.WriteString("\"filesParsed\":")
	buf.WriteString(intToString(m.FilesParsed))
	wrote = true
	// skipped
	buf.WriteByte(',')
	buf.WriteString("\"skipped\":")
	buf.WriteString(intToString(m.Skipped))
	// durationMs
	buf.WriteByte(',')
	buf.WriteString("\"durationMs\":")
	buf.WriteString(intToString(m.DurationMs))

	// Optional numeric fields if non-zero
	if m.TotalFiles != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"totalFiles\":")
		buf.WriteString(intToString(m.TotalFiles))
	}
	if m.ErrorCount != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"errorCount\":")
		buf.WriteString(intToString(m.ErrorCount))
	}
	if m.StartTime != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"startTime\":")
		buf.WriteString(intToString(m.StartTime))
	}
	if m.EndTime != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"endTime\":")
		buf.WriteString(intToString(m.EndTime))
	}
	if m.InferenceOps != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"inferenceOps\":")
		buf.WriteString(intToString(m.InferenceOps))
	}
	if m.PropertiesInferred != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"propertiesInferred\":")
		buf.WriteString(intToString(m.PropertiesInferred))
	}
	if m.CacheHits != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"cacheHits\":")
		buf.WriteString(intToString(m.CacheHits))
	}
	if m.CacheMisses != 0 {
		buf.WriteByte(',')
		buf.WriteString("\"cacheMisses\":")
		buf.WriteString(intToString(m.CacheMisses))
	}

	// PhaseStats map (sorted keys)
	if len(phaseKeys) > 0 {
		buf.WriteByte(',')
		buf.WriteString("\"phaseStats\":{")
		for i, k := range phaseKeys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(k, json.Deterministic(true))
			buf.Write(keyBytes)
			buf.WriteByte(':')
			buf.WriteString(intToString(m.PhaseStats[k]))
		}
		buf.WriteByte('}')
	}

	// MatchStats map (sorted keys)
	if len(matchKeys) > 0 {
		buf.WriteByte(',')
		buf.WriteString("\"matchStats\":{")
		for i, k := range matchKeys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(k, json.Deterministic(true))
			buf.Write(keyBytes)
			buf.WriteByte(':')
			buf.WriteString(intToString(int64(m.MatchStats[k])))
		}
		buf.WriteByte('}')
	}

	// AssemblerStats (if not nil and has useful data)
	if m.AssemblerStats != nil && (m.AssemblerStats.SkippedControllers > 0 ||
		m.AssemblerStats.SkippedModels > 0 ||
		m.AssemblerStats.SkippedPatterns > 0 ||
		m.AssemblerStats.UnresolvableMatches > 0) {

		buf.WriteByte(',')
		buf.WriteString("\"assemblerStats\":{")

		assemblerWrote := false
		if m.AssemblerStats.SkippedControllers > 0 {
			buf.WriteString("\"skippedControllers\":")
			buf.WriteString(intToString(int64(m.AssemblerStats.SkippedControllers)))
			assemblerWrote = true
		}
		if m.AssemblerStats.SkippedModels > 0 {
			if assemblerWrote {
				buf.WriteByte(',')
			}
			buf.WriteString("\"skippedModels\":")
			buf.WriteString(intToString(int64(m.AssemblerStats.SkippedModels)))
			assemblerWrote = true
		}
		if m.AssemblerStats.SkippedPatterns > 0 {
			if assemblerWrote {
				buf.WriteByte(',')
			}
			buf.WriteString("\"skippedPatterns\":")
			buf.WriteString(intToString(int64(m.AssemblerStats.SkippedPatterns)))
			assemblerWrote = true
		}
		if m.AssemblerStats.UnresolvableMatches > 0 {
			if assemblerWrote {
				buf.WriteByte(',')
			}
			buf.WriteString("\"unresolvableMatches\":")
			buf.WriteString(intToString(int64(m.AssemblerStats.UnresolvableMatches)))
		}

		buf.WriteByte('}')
	}

	buf.WriteByte('}')
	_ = wrote
	return buf.Bytes(), nil
}

// intToString formats integers without allocation-heavy fmt where possible.
func intToString(v int64) string {
	// Using json.Marshal on number would add quotes; use fmt to keep it simple
	// However, since this is in a hot path rarely, keep it straightforward
	return fmt.Sprintf("%d", v)
}

// NewMetaStatsFromProcessingStats converts processing stats to MetaStats format.
// This ensures deterministic JSON output by sorting all map keys consistently.
func NewMetaStatsFromProcessingStats(processingStats any) MetaStats {
	// Use reflection-free approach by defining interface methods we need
	type statsReader interface {
		GetFilesProcessed() int64
		GetFilesSkipped() int64
		GetProcessingTime() int64
		IsPartial() bool
		GetErrorCount() int64
	}

	type phaseStatsReader interface {
		GetPhaseStats() any // Will cast to map[string]int64
	}

	type matchStatsReader interface {
		GetMatchStats() any // Will cast to map[string]int
	}

	// Create default empty stats
	meta := MetaStats{}

	// Try to extract basic stats
	if stats, ok := processingStats.(statsReader); ok {
		meta.FilesParsed = stats.GetFilesProcessed()
		meta.Skipped = stats.GetFilesSkipped()
		meta.DurationMs = stats.GetProcessingTime()
		meta.ErrorCount = stats.GetErrorCount()
	}

	// Try to extract phase stats
	if phaseReader, ok := processingStats.(phaseStatsReader); ok {
		if phaseStatsRaw := phaseReader.GetPhaseStats(); phaseStatsRaw != nil {
			if phaseStats, ok := phaseStatsRaw.(map[string]int64); ok && len(phaseStats) > 0 {
				meta.PhaseStats = make(map[string]int64, len(phaseStats))
				// Sort phases for deterministic output
				phases := make([]string, 0, len(phaseStats))
				for phase := range phaseStats {
					phases = append(phases, phase)
				}
				// Simple sort without external dependencies
				for i := 0; i < len(phases); i++ {
					for j := i + 1; j < len(phases); j++ {
						if phases[i] > phases[j] {
							phases[i], phases[j] = phases[j], phases[i]
						}
					}
				}
				for _, phase := range phases {
					if duration := phaseStats[phase]; duration > 0 {
						meta.PhaseStats[phase] = duration
					}
				}
				// Don't include empty phase stats
				if len(meta.PhaseStats) == 0 {
					meta.PhaseStats = nil
				}
			}
		}
	}

	// Try to extract match stats
	if matchReader, ok := processingStats.(matchStatsReader); ok {
		if matchStatsRaw := matchReader.GetMatchStats(); matchStatsRaw != nil {
			if matchStats, ok := matchStatsRaw.(map[string]int); ok && len(matchStats) > 0 {
				meta.MatchStats = make(map[string]int, len(matchStats))
				// Sort match types for deterministic output
				matchTypes := make([]string, 0, len(matchStats))
				for matchType := range matchStats {
					matchTypes = append(matchTypes, matchType)
				}
				// Simple sort without external dependencies
				for i := 0; i < len(matchTypes); i++ {
					for j := i + 1; j < len(matchTypes); j++ {
						if matchTypes[i] > matchTypes[j] {
							matchTypes[i], matchTypes[j] = matchTypes[j], matchTypes[i]
						}
					}
				}
				for _, matchType := range matchTypes {
					if count := matchStats[matchType]; count > 0 {
						meta.MatchStats[matchType] = count
					}
				}
				// Don't include empty match stats
				if len(meta.MatchStats) == 0 {
					meta.MatchStats = nil
				}
			}
		}
	}

	// Try to extract additional fields if they exist in the struct
	type totalFilesInterface interface{ GetTotalFiles() int64 }
	type startTimeInterface interface{ GetStartTime() int64 }
	type endTimeInterface interface{ GetEndTime() int64 }
	type inferenceOpsInterface interface{ GetInferenceOps() int64 }
	type propertiesInferredInterface interface{ GetPropertiesInferred() int64 }
	type cacheHitsInterface interface{ GetCacheHits() int64 }
	type cacheMissesInterface interface{ GetCacheMisses() int64 }

	if v, ok := processingStats.(totalFilesInterface); ok {
		meta.TotalFiles = v.GetTotalFiles()
	}
	if v, ok := processingStats.(startTimeInterface); ok {
		meta.StartTime = v.GetStartTime()
	}
	if v, ok := processingStats.(endTimeInterface); ok {
		meta.EndTime = v.GetEndTime()
	}
	if v, ok := processingStats.(inferenceOpsInterface); ok {
		meta.InferenceOps = v.GetInferenceOps()
	}
	if v, ok := processingStats.(propertiesInferredInterface); ok {
		meta.PropertiesInferred = v.GetPropertiesInferred()
	}
	if v, ok := processingStats.(cacheHitsInterface); ok {
		meta.CacheHits = v.GetCacheHits()
	}
	if v, ok := processingStats.(cacheMissesInterface); ok {
		meta.CacheMisses = v.GetCacheMisses()
	}

	return meta
}

// Controller represents a Laravel controller method with its detected patterns.
type Controller struct {
	FQCN          string                `json:"fqcn"`
	Method        string                `json:"method"`
	HTTP          *HTTPInfo             `json:"http,omitempty"`
	Request       *RequestInfo          `json:"request,omitempty"`
	Responses     []Response            `json:"responses,omitempty"`
	Authorization []AuthorizationHint   `json:"authorization,omitempty"`
	Resources     []Resource            `json:"resources,omitempty"`
	ScopesUsed    []ScopeUsed           `json:"scopesUsed,omitempty"`
	Polymorphic   []PolymorphicRelation `json:"polymorphic,omitempty"`
}

// HTTPInfo captures HTTP-related metadata for controller methods.
type HTTPInfo struct {
	Status   *int  `json:"status,omitempty"`
	Explicit *bool `json:"explicit,omitempty"`
}

// RequestInfo describes request handling patterns detected in controller methods.
type RequestInfo struct {
	ContentTypes []string       `json:"contentTypes,omitempty"`
	Body         OrderedObject  `json:"body,omitempty"`
	Query        OrderedObject  `json:"query,omitempty"`
	Files        OrderedObject  `json:"files,omitempty"`
	Fields       []RequestField `json:"fields,omitempty"`
}

// Response captures detected response semantics for a controller method.
// It is additive to the existing HTTP status detection and resource catalog.
type Response struct {
	Kind        string              `json:"kind"`
	Status      *int                `json:"status,omitempty"`
	Explicit    *bool               `json:"explicit,omitempty"`
	ContentType string              `json:"contentType,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	BodySchema  *ResourceSchemaNode `json:"bodySchema,omitempty"`
	Redirect    *RedirectInfo       `json:"redirect,omitempty"`
	Download    *DownloadInfo       `json:"download,omitempty"`
	Inertia     *InertiaInfo        `json:"inertia,omitempty"`
	Source      string              `json:"source,omitempty"`
	Via         string              `json:"via,omitempty"`
}

type RedirectInfo struct {
	TargetKind string  `json:"targetKind"`
	Target     *string `json:"target,omitempty"`
}

type DownloadInfo struct {
	Disposition string  `json:"disposition"`
	Filename    *string `json:"filename,omitempty"`
}

type InertiaInfo struct {
	Component   string              `json:"component"`
	PropsSchema *ResourceSchemaNode `json:"propsSchema,omitempty"`
	RootView    *string             `json:"rootView,omitempty"`
	Version     *string             `json:"version,omitempty"`
}

type AuthorizationHint struct {
	Kind                    string  `json:"kind"`
	Ability                 *string `json:"ability,omitempty"`
	TargetKind              *string `json:"targetKind,omitempty"`
	Target                  *string `json:"target,omitempty"`
	Parameter               *string `json:"parameter,omitempty"`
	Source                  string  `json:"source"`
	Resolution              string  `json:"resolution"`
	EnforcesFailureResponse bool    `json:"enforcesFailureResponse"`
}

func (r *Response) InertiaPropsSchema() *ResourceSchemaNode {
	if r == nil || r.Inertia == nil {
		return nil
	}
	return r.Inertia.PropsSchema
}

// RequestField captures richer metadata for a single request field path.
// It is additive to the existing body/query/files shapes and does not replace them.
type RequestField struct {
	Location      string   `json:"location"`
	Path          string   `json:"path"`
	Kind          string   `json:"kind,omitempty"`
	Type          string   `json:"type,omitempty"`
	ScalarType    string   `json:"scalarType,omitempty"`
	Format        string   `json:"format,omitempty"`
	ItemType      string   `json:"itemType,omitempty"`
	Wrappers      []string `json:"wrappers,omitempty"`
	AllowedValues []string `json:"allowedValues,omitempty"`
	Required      *bool    `json:"required,omitempty"`
	Optional      *bool    `json:"optional,omitempty"`
	Nullable      *bool    `json:"nullable,omitempty"`
	IsArray       *bool    `json:"isArray,omitempty"`
	Collection    *bool    `json:"collection,omitempty"`
	Source        string   `json:"source,omitempty"`
	Via           string   `json:"via,omitempty"`
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
		keyBytes, err := json.Marshal(k, json.Deterministic(true))
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
func NewOrderedObjectFromMap(m map[string]any) OrderedObject {
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
		if childMap, ok := v.(map[string]any); ok {
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
	FQCN       string `json:"fqcn,omitempty"`
	Collection bool   `json:"collection"`
}

// ResourceDef represents a response resource schema extracted from Laravel API resources.
type ResourceDef struct {
	FQCN   string             `json:"fqcn"`
	Class  string             `json:"class"`
	Schema ResourceSchemaNode `json:"schema"`
}

// ResourceSchemaNode captures a reusable response schema node for resources/components.
type ResourceSchemaNode struct {
	Type       string                        `json:"type,omitempty"`
	Format     string                        `json:"format,omitempty"`
	Ref        string                        `json:"ref,omitempty"`
	Nullable   *bool                         `json:"nullable,omitempty"`
	Properties map[string]ResourceSchemaNode `json:"properties,omitempty"`
	Required   []string                      `json:"required,omitempty"`
	Items      *ResourceSchemaNode           `json:"items,omitempty"`
}

// ScopeUsed represents detected Eloquent scope usage in controllers.
type ScopeUsed struct {
	On   string   `json:"on"`
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

// Model represents an Eloquent model class with its detected features.
type Model struct {
	FQCN        string                `json:"fqcn"`
	WithPivot   []PivotInfo           `json:"withPivot,omitempty"`
	Attributes  []Attribute           `json:"attributes,omitempty"`
	Polymorphic []PolymorphicRelation `json:"polymorphic,omitempty"`
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
	Relation       string                    `json:"relation"`                 // Relationship method name
	Type           string                    `json:"type"`                     // Polymorphic type (morphTo, morphOne, morphMany)
	MorphType      string                    `json:"morphType,omitempty"`      // Morph type column
	MorphId        string                    `json:"morphId,omitempty"`        // Morph ID column
	Model          *string                   `json:"model,omitempty"`          // Target model class
	Discriminator  *PolymorphicDiscriminator `json:"discriminator,omitempty"`  // Discriminator mapping
	RelatedModels  []string                  `json:"relatedModels,omitempty"`  // Related model classes
	DepthTruncated *bool                     `json:"depthTruncated,omitempty"` // True if max depth reached
	MaxDepth       *int                      `json:"maxDepth,omitempty"`       // Maximum traversal depth
}

// PolymorphicDiscriminator contains discriminator mapping information for polymorphic relationships.
type PolymorphicDiscriminator struct {
	PropertyName string            `json:"propertyName"`          // Discriminator property name
	Mapping      map[string]string `json:"mapping"`               // Type mappings
	Source       string            `json:"source"`                // Source of mapping (morphMap, explicit, inferred)
	IsExplicit   bool              `json:"isExplicit"`            // Whether mapping is explicitly defined
	DefaultType  *string           `json:"defaultType,omitempty"` // Default type if no mapping matches
}

// MarshalJSON ensures deterministic ordering for Mapping keys and object fields
func (pd PolymorphicDiscriminator) MarshalJSON() ([]byte, error) {
	// Sort mapping keys
	keys := make([]string, 0, len(pd.Mapping))
	for k := range pd.Mapping {
		keys = append(keys, k)
	}
	if len(keys) > 1 {
		sort.Strings(keys)
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	// propertyName
	buf.WriteString("\"propertyName\":")
	nameBytes, _ := json.Marshal(pd.PropertyName, json.Deterministic(true))
	buf.Write(nameBytes)
	// mapping
	buf.WriteByte(',')
	buf.WriteString("\"mapping\":{")
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, _ := json.Marshal(k, json.Deterministic(true))
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, _ := json.Marshal(pd.Mapping[k], json.Deterministic(true))
		buf.Write(valBytes)
	}
	buf.WriteByte('}')
	// source
	buf.WriteByte(',')
	buf.WriteString("\"source\":")
	srcBytes, _ := json.Marshal(pd.Source, json.Deterministic(true))
	buf.Write(srcBytes)
	// isExplicit
	buf.WriteByte(',')
	buf.WriteString("\"isExplicit\":")
	if pd.IsExplicit {
		buf.WriteString("true")
	} else {
		buf.WriteString("false")
	}
	// defaultType (optional)
	if pd.DefaultType != nil {
		buf.WriteByte(',')
		buf.WriteString("\"defaultType\":")
		defBytes, _ := json.Marshal(*pd.DefaultType, json.Deterministic(true))
		buf.Write(defBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
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
	IdColumn   string `json:"idColumn"`
}

// Discriminator provides type mapping for polymorphic relationships.
type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping"`
}

// MarshalJSON ensures deterministic ordering for Mapping keys
func (d Discriminator) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(d.Mapping))
	for k := range d.Mapping {
		keys = append(keys, k)
	}
	if len(keys) > 1 {
		sort.Strings(keys)
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	// propertyName first
	buf.WriteString("\"propertyName\":")
	nameBytes, _ := json.Marshal(d.PropertyName, json.Deterministic(true))
	buf.Write(nameBytes)
	// mapping
	buf.WriteByte(',')
	buf.WriteString("\"mapping\":{")
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, _ := json.Marshal(k, json.Deterministic(true))
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, _ := json.Marshal(d.Mapping[k], json.Deterministic(true))
		buf.Write(valBytes)
	}
	buf.WriteByte('}')
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Broadcast represents Laravel broadcasting channel configurations.
type Broadcast struct {
	File           *string  `json:"file,omitempty"`
	Channel        string   `json:"channel"`
	Params         []string `json:"params"`
	Visibility     string   `json:"visibility"`
	PayloadLiteral *bool    `json:"payloadLiteral,omitempty"`
}

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
}

// NewMetaStatsFromProcessingStats converts processing stats to MetaStats format.
// This ensures deterministic JSON output by sorting all map keys consistently.
func NewMetaStatsFromProcessingStats(processingStats interface{}) MetaStats {
	// Use reflection-free approach by defining interface methods we need
	type statsReader interface {
		GetFilesProcessed() int64
		GetFilesSkipped() int64
		GetProcessingTime() int64
		IsPartial() bool
		GetErrorCount() int64
	}

	type phaseStatsReader interface {
		GetPhaseStats() interface{} // Will cast to map[string]int64
	}

	type matchStatsReader interface {
		GetMatchStats() interface{} // Will cast to map[string]int
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

// Controller represents a Laravel controller class with its methods and detected patterns.
type Controller struct {
	FQCN        string                `json:"fqcn"`
	Method      string                `json:"method"`
	HTTP        *HTTPInfo             `json:"http,omitempty"`
	Request     *RequestInfo          `json:"request,omitempty"`
	Resources   []Resource            `json:"resources,omitempty"`
	ScopesUsed  []ScopeUsed           `json:"scopesUsed,omitempty"`
	Polymorphic []PolymorphicRelation `json:"polymorphic,omitempty"`
}

// HTTPInfo captures HTTP-related metadata for controller methods.
type HTTPInfo struct {
	Status   *int  `json:"status,omitempty"`
	Explicit *bool `json:"explicit,omitempty"`
}

// RequestInfo describes request handling patterns detected in controller methods.
type RequestInfo struct {
	ContentTypes []string      `json:"contentTypes,omitempty"`
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

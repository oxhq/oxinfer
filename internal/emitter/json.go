// Package emitter provides deterministic JSON marshaling for oxinfer deltas.
// It ensures byte-for-byte identical output for the same input data.
package emitter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// DeltaEmitter defines the interface for emitting deterministic delta JSON.
type DeltaEmitter interface {
    EmitStub() (*Delta, error)
    WriteJSON(w io.Writer, delta *Delta) error
    MarshalDeterministic(delta *Delta) ([]byte, error)
    CanonicalBytes(delta *Delta) ([]byte, error)
}

// JSONEmitter implements deterministic JSON marshaling for Delta structures.
type JSONEmitter struct{}

// NewJSONEmitter creates a new JSONEmitter instance.
func NewJSONEmitter() *JSONEmitter {
	return &JSONEmitter{}
}

// EmitStub generates an empty but schema-valid Delta.
// This provides a baseline structure that validates against delta.schema.json.
func (e *JSONEmitter) EmitStub() (*Delta, error) {
	return &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 0,
				Skipped:     0,
				DurationMs:  0,
			},
		},
		Controllers: []Controller{},
		Models:      []Model{},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}, nil
}

// WriteJSON writes a Delta as JSON to the provided writer with deterministic ordering.
// This ensures byte-identical output for the same Delta structure.
func (e *JSONEmitter) WriteJSON(w io.Writer, delta *Delta) error {
	data, err := e.MarshalDeterministic(delta)
	if err != nil {
		return fmt.Errorf("failed to marshal delta: %w", err)
	}

	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	return nil
}

// MarshalDeterministic converts a Delta to JSON bytes with consistent ordering.
// It ensures the same input always produces identical byte output by:
// - Sorting all object keys alphabetically
// - Sorting arrays consistently where order doesn't matter semantically
// - Using stable JSON encoding without randomization
func (e *JSONEmitter) MarshalDeterministic(delta *Delta) ([]byte, error) {
	if delta == nil {
		return nil, fmt.Errorf("delta cannot be nil")
	}

	// Ensure deterministic ordering of all collections
	sortedDelta := e.normalizeDelta(delta)

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "")

	if err := encoder.Encode(sortedDelta); err != nil {
		return nil, fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Remove the trailing newline that encoder.Encode adds
	result := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return result, nil
}

// CanonicalBytes returns a canonical JSON form excluding volatile fields
// like meta.stats.durationMs and meta.generatedAt, while preserving deterministic ordering.
func (e *JSONEmitter) CanonicalBytes(delta *Delta) ([]byte, error) {
    if delta == nil {
        return nil, fmt.Errorf("delta cannot be nil")
    }
    // Clone lightweight
    d := *delta
    d.Meta = delta.Meta
    // Zero out duration and generatedAt
    d.Meta.Stats.DurationMs = 0
    d.Meta.GeneratedAt = nil
    return e.MarshalDeterministic(&d)
}

// normalizeDelta ensures all collections are sorted for deterministic output.
// This is critical for achieving byte-identical results across runs.
func (e *JSONEmitter) normalizeDelta(delta *Delta) *Delta {
	normalized := &Delta{
		Meta:        delta.Meta,
		Controllers: make([]Controller, len(delta.Controllers)),
		Models:      make([]Model, len(delta.Models)),
		Polymorphic: make([]Polymorphic, len(delta.Polymorphic)),
		Broadcast:   make([]Broadcast, len(delta.Broadcast)),
	}

	// Sort controllers by FQCN, then by method for deterministic ordering
	copy(normalized.Controllers, delta.Controllers)
	sort.Slice(normalized.Controllers, func(i, j int) bool {
		if normalized.Controllers[i].FQCN != normalized.Controllers[j].FQCN {
			return normalized.Controllers[i].FQCN < normalized.Controllers[j].FQCN
		}
		return normalized.Controllers[i].Method < normalized.Controllers[j].Method
	})

	// Sort controller sub-collections
	for i := range normalized.Controllers {
		e.normalizeController(&normalized.Controllers[i])
	}

	// Sort models by FQCN
	copy(normalized.Models, delta.Models)
	sort.Slice(normalized.Models, func(i, j int) bool {
		return normalized.Models[i].FQCN < normalized.Models[j].FQCN
	})

	// Sort model sub-collections
	for i := range normalized.Models {
		e.normalizeModel(&normalized.Models[i])
	}

	// Sort polymorphic relationships by parent
	copy(normalized.Polymorphic, delta.Polymorphic)
	sort.Slice(normalized.Polymorphic, func(i, j int) bool {
		return normalized.Polymorphic[i].Parent < normalized.Polymorphic[j].Parent
	})

	// Sort broadcast channels by channel name
	copy(normalized.Broadcast, delta.Broadcast)
	sort.Slice(normalized.Broadcast, func(i, j int) bool {
		return normalized.Broadcast[i].Channel < normalized.Broadcast[j].Channel
	})

	// Sort broadcast sub-collections
	for i := range normalized.Broadcast {
		e.normalizeBroadcast(&normalized.Broadcast[i])
	}

	return normalized
}

// normalizeController ensures deterministic ordering of controller sub-collections.
func (e *JSONEmitter) normalizeController(controller *Controller) {
	// Sort request content types
	if controller.Request != nil && controller.Request.ContentTypes != nil {
		contentTypes := make([]string, len(controller.Request.ContentTypes))
		copy(contentTypes, controller.Request.ContentTypes)
		sort.Strings(contentTypes)
		controller.Request.ContentTypes = contentTypes
	}

	// Sort resources by class name
	if controller.Resources != nil {
		sort.Slice(controller.Resources, func(i, j int) bool {
			return controller.Resources[i].Class < controller.Resources[j].Class
		})
	}

	// Sort scopes by name, then by model
	if controller.ScopesUsed != nil {
		sort.Slice(controller.ScopesUsed, func(i, j int) bool {
			if controller.ScopesUsed[i].On != controller.ScopesUsed[j].On {
				return controller.ScopesUsed[i].On < controller.ScopesUsed[j].On
			}
			return controller.ScopesUsed[i].Name < controller.ScopesUsed[j].Name
		})

		// Sort arguments within each scope
		for i := range controller.ScopesUsed {
			if controller.ScopesUsed[i].Args != nil {
				args := make([]string, len(controller.ScopesUsed[i].Args))
				copy(args, controller.ScopesUsed[i].Args)
				sort.Strings(args)
				controller.ScopesUsed[i].Args = args
			}
		}
	}
}

// normalizeModel ensures deterministic ordering of model sub-collections.
func (e *JSONEmitter) normalizeModel(model *Model) {
	// Sort pivot configurations by relation name
	if model.WithPivot != nil {
		sort.Slice(model.WithPivot, func(i, j int) bool {
			return model.WithPivot[i].Relation < model.WithPivot[j].Relation
		})

		// Sort columns within each pivot configuration
		for i := range model.WithPivot {
			if model.WithPivot[i].Columns != nil {
				columns := make([]string, len(model.WithPivot[i].Columns))
				copy(columns, model.WithPivot[i].Columns)
				sort.Strings(columns)
				model.WithPivot[i].Columns = columns
			}
		}
	}

	// Sort attributes by name
	if model.Attributes != nil {
		sort.Slice(model.Attributes, func(i, j int) bool {
			return model.Attributes[i].Name < model.Attributes[j].Name
		})
	}
}

// normalizeBroadcast ensures deterministic ordering of broadcast sub-collections.
func (e *JSONEmitter) normalizeBroadcast(broadcast *Broadcast) {
	// Sort parameters
	if broadcast.Params != nil {
		params := make([]string, len(broadcast.Params))
		copy(params, broadcast.Params)
		sort.Strings(params)
		broadcast.Params = params
	}
}

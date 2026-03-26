// Package emitter provides deterministic JSON marshaling for oxinfer deltas.
// It ensures byte-for-byte identical output for the same input data.
//go:build goexperiment.jsonv2

package emitter

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"sort"
	"strings"
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
		Resources:   []ResourceDef{},
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

	result, err := json.Marshal(sortedDelta, json.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("failed to encode JSON: %w", err)
	}

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
		Resources:   make([]ResourceDef, len(delta.Resources)),
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

	// Sort models by FQCN for deterministic ordering
	copy(normalized.Models, delta.Models)
	sort.Slice(normalized.Models, func(i, j int) bool {
		return normalized.Models[i].FQCN < normalized.Models[j].FQCN
	})

	// Sort model sub-collections
	for i := range normalized.Models {
		e.normalizeModel(&normalized.Models[i])
	}

	// Sort resources by FQCN for deterministic ordering
	copy(normalized.Resources, delta.Resources)
	sort.Slice(normalized.Resources, func(i, j int) bool {
		if normalized.Resources[i].FQCN != normalized.Resources[j].FQCN {
			return normalized.Resources[i].FQCN < normalized.Resources[j].FQCN
		}
		return normalized.Resources[i].Class < normalized.Resources[j].Class
	})
	for i := range normalized.Resources {
		e.normalizeResourceDef(&normalized.Resources[i])
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

	if controller.Request != nil && controller.Request.Fields != nil {
		fields := make([]RequestField, len(controller.Request.Fields))
		copy(fields, controller.Request.Fields)
		for i := range fields {
			if len(fields[i].Wrappers) > 1 {
				sort.Strings(fields[i].Wrappers)
			}
			if len(fields[i].AllowedValues) > 1 {
				sort.Strings(fields[i].AllowedValues)
			}
		}
		sort.Slice(fields, func(i, j int) bool {
			if fields[i].Location != fields[j].Location {
				return fields[i].Location < fields[j].Location
			}
			if fields[i].Path != fields[j].Path {
				return fields[i].Path < fields[j].Path
			}
			if fields[i].Kind != fields[j].Kind {
				return fields[i].Kind < fields[j].Kind
			}
			if fields[i].Type != fields[j].Type {
				return fields[i].Type < fields[j].Type
			}
			if fields[i].ScalarType != fields[j].ScalarType {
				return fields[i].ScalarType < fields[j].ScalarType
			}
			if fields[i].Format != fields[j].Format {
				return fields[i].Format < fields[j].Format
			}
			if fields[i].ItemType != fields[j].ItemType {
				return fields[i].ItemType < fields[j].ItemType
			}
			if strings.Join(fields[i].AllowedValues, ",") != strings.Join(fields[j].AllowedValues, ",") {
				return strings.Join(fields[i].AllowedValues, ",") < strings.Join(fields[j].AllowedValues, ",")
			}
			if fields[i].Required != nil || fields[j].Required != nil {
				left := false
				right := false
				if fields[i].Required != nil {
					left = *fields[i].Required
				}
				if fields[j].Required != nil {
					right = *fields[j].Required
				}
				if left != right {
					return left && !right
				}
			}
			if fields[i].Optional != nil || fields[j].Optional != nil {
				left := false
				right := false
				if fields[i].Optional != nil {
					left = *fields[i].Optional
				}
				if fields[j].Optional != nil {
					right = *fields[j].Optional
				}
				if left != right {
					return !left && right
				}
			}
			if fields[i].Nullable != nil || fields[j].Nullable != nil {
				left := false
				right := false
				if fields[i].Nullable != nil {
					left = *fields[i].Nullable
				}
				if fields[j].Nullable != nil {
					right = *fields[j].Nullable
				}
				if left != right {
					return !left && right
				}
			}
			if fields[i].IsArray != nil || fields[j].IsArray != nil {
				left := false
				right := false
				if fields[i].IsArray != nil {
					left = *fields[i].IsArray
				}
				if fields[j].IsArray != nil {
					right = *fields[j].IsArray
				}
				if left != right {
					return !left && right
				}
			}
			if fields[i].Collection != nil || fields[j].Collection != nil {
				left := false
				right := false
				if fields[i].Collection != nil {
					left = *fields[i].Collection
				}
				if fields[j].Collection != nil {
					right = *fields[j].Collection
				}
				if left != right {
					return !left && right
				}
			}
			if strings.Join(fields[i].Wrappers, ",") != strings.Join(fields[j].Wrappers, ",") {
				return strings.Join(fields[i].Wrappers, ",") < strings.Join(fields[j].Wrappers, ",")
			}
			if fields[i].Via != fields[j].Via {
				return fields[i].Via < fields[j].Via
			}
			return fields[i].Source < fields[j].Source
		})
		controller.Request.Fields = fields
	}

	// Sort resources by class name
	if controller.Resources != nil {
		sort.Slice(controller.Resources, func(i, j int) bool {
			if controller.Resources[i].FQCN != controller.Resources[j].FQCN {
				return controller.Resources[i].FQCN < controller.Resources[j].FQCN
			}
			if controller.Resources[i].Class != controller.Resources[j].Class {
				return controller.Resources[i].Class < controller.Resources[j].Class
			}
			return !controller.Resources[i].Collection && controller.Resources[j].Collection
		})
	}

	// Sort responses by status, then by kind and schema fingerprint.
	if controller.Responses != nil {
		sort.Slice(controller.Responses, func(i, j int) bool {
			leftStatus := responseStatusValue(controller.Responses[i].Status)
			rightStatus := responseStatusValue(controller.Responses[j].Status)
			if leftStatus != rightStatus {
				return leftStatus < rightStatus
			}
			if controller.Responses[i].Kind != controller.Responses[j].Kind {
				return controller.Responses[i].Kind < controller.Responses[j].Kind
			}
			if controller.Responses[i].ContentType != controller.Responses[j].ContentType {
				return controller.Responses[i].ContentType < controller.Responses[j].ContentType
			}
			if controller.Responses[i].Source != controller.Responses[j].Source {
				return controller.Responses[i].Source < controller.Responses[j].Source
			}
			if controller.Responses[i].Via != controller.Responses[j].Via {
				return controller.Responses[i].Via < controller.Responses[j].Via
			}
			if responseHeaderSignature(controller.Responses[i].Headers) != responseHeaderSignature(controller.Responses[j].Headers) {
				return responseHeaderSignature(controller.Responses[i].Headers) < responseHeaderSignature(controller.Responses[j].Headers)
			}
			if responseRedirectSignature(controller.Responses[i].Redirect) != responseRedirectSignature(controller.Responses[j].Redirect) {
				return responseRedirectSignature(controller.Responses[i].Redirect) < responseRedirectSignature(controller.Responses[j].Redirect)
			}
			if responseDownloadSignature(controller.Responses[i].Download) != responseDownloadSignature(controller.Responses[j].Download) {
				return responseDownloadSignature(controller.Responses[i].Download) < responseDownloadSignature(controller.Responses[j].Download)
			}
			if responseInertiaSignature(controller.Responses[i].Inertia) != responseInertiaSignature(controller.Responses[j].Inertia) {
				return responseInertiaSignature(controller.Responses[i].Inertia) < responseInertiaSignature(controller.Responses[j].Inertia)
			}
			return responseSchemaSignature(controller.Responses[i].BodySchema) < responseSchemaSignature(controller.Responses[j].BodySchema)
		})
		for i := range controller.Responses {
			if controller.Responses[i].Headers != nil {
				headers := make(map[string]string, len(controller.Responses[i].Headers))
				for key, value := range controller.Responses[i].Headers {
					headers[key] = value
				}
				controller.Responses[i].Headers = headers
			}
			e.normalizeResponseSchema(controller.Responses[i].BodySchema)
			if controller.Responses[i].Inertia != nil {
				e.normalizeResponseSchema(controller.Responses[i].Inertia.PropsSchema)
			}
		}
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

func (e *JSONEmitter) normalizeResourceDef(resource *ResourceDef) {
	e.normalizeResourceSchemaNode(&resource.Schema)
}

func (e *JSONEmitter) normalizeResourceSchemaNode(node *ResourceSchemaNode) {
	if node == nil {
		return
	}

	if len(node.Required) > 1 {
		sort.Strings(node.Required)
	}
	if node.Items != nil {
		e.normalizeResourceSchemaNode(node.Items)
	}
	if len(node.Properties) == 0 {
		return
	}

	keys := make([]string, 0, len(node.Properties))
	for key := range node.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	normalized := make(map[string]ResourceSchemaNode, len(node.Properties))
	for _, key := range keys {
		child := node.Properties[key]
		e.normalizeResourceSchemaNode(&child)
		normalized[key] = child
	}
	node.Properties = normalized
}

func (e *JSONEmitter) normalizeResponseSchema(node *ResourceSchemaNode) {
	e.normalizeResourceSchemaNode(node)
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

// normalizePolymorphicRelation ensures deterministic ordering of polymorphic relation sub-collections.
func (e *JSONEmitter) normalizePolymorphicRelation(relation *PolymorphicRelation) {
	// Sort related models if present
	if relation.RelatedModels != nil {
		models := make([]string, len(relation.RelatedModels))
		copy(models, relation.RelatedModels)
		sort.Strings(models)
		relation.RelatedModels = models
	}

	// Sort discriminator mapping if present
	if relation.Discriminator != nil && relation.Discriminator.Mapping != nil {
		// The mapping is a map[string]string, so we need to ensure consistent ordering
		// when it's serialized. Go's JSON marshaler will automatically sort map keys,
		// but we can ensure the structure is consistent.
		mapping := make(map[string]string, len(relation.Discriminator.Mapping))
		for k, v := range relation.Discriminator.Mapping {
			mapping[k] = v
		}
		relation.Discriminator.Mapping = mapping
	}
}

// normalizeBroadcast ensures deterministic ordering of broadcast sub-collections.
func (e *JSONEmitter) normalizeBroadcast(broadcast *Broadcast) {
	// Preserve literal route order - do NOT sort parameters
	if broadcast.Params != nil {
		params := make([]string, len(broadcast.Params))
		copy(params, broadcast.Params)
		broadcast.Params = params
	}
}

func responseStatusValue(status *int) int {
	if status == nil {
		return 0
	}
	return *status
}

func responseSchemaSignature(node *ResourceSchemaNode) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(node.Type)
	builder.WriteByte('|')
	builder.WriteString(node.Format)
	builder.WriteByte('|')
	builder.WriteString(node.Ref)
	builder.WriteByte('|')
	if node.Nullable != nil {
		if *node.Nullable {
			builder.WriteString("1")
		} else {
			builder.WriteString("0")
		}
	}
	builder.WriteByte('|')
	builder.WriteString(strings.Join(node.Required, ","))
	builder.WriteByte('|')
	if len(node.Properties) > 0 {
		keys := make([]string, 0, len(node.Properties))
		for key := range node.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString(key)
			builder.WriteByte('=')
			child := node.Properties[key]
			builder.WriteString(responseSchemaSignature(&child))
			builder.WriteByte(';')
		}
	}
	builder.WriteByte('|')
	if node.Items != nil {
		builder.WriteString(responseSchemaSignature(node.Items))
	}
	return builder.String()
}

func responseHeaderSignature(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(headers[key])
		builder.WriteByte(';')
	}

	return builder.String()
}

func responseRedirectSignature(info *RedirectInfo) string {
	if info == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(info.TargetKind)
	builder.WriteByte('|')
	if info.Target != nil {
		builder.WriteString(*info.Target)
	}
	return builder.String()
}

func responseDownloadSignature(info *DownloadInfo) string {
	if info == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(info.Disposition)
	builder.WriteByte('|')
	if info.Filename != nil {
		builder.WriteString(*info.Filename)
	}
	return builder.String()
}

func responseInertiaSignature(info *InertiaInfo) string {
	if info == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(info.Component)
	builder.WriteByte('|')
	builder.WriteString(responseSchemaSignature(info.PropsSchema))
	builder.WriteByte('|')
	if info.RootView != nil {
		builder.WriteString(*info.RootView)
	}
	builder.WriteByte('|')
	if info.Version != nil {
		builder.WriteString(*info.Version)
	}
	return builder.String()
}

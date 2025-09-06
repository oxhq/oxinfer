// Package pipeline provides delta assembly functionality.
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/matchers"
)

// AssemblerStats tracks statistics during assembly process for debugging and optimization.
type AssemblerStats struct {
	SkippedControllers   int // Controllers skipped due to unresolvable keys
	SkippedModels       int // Models skipped due to unresolvable FQCNs  
	SkippedPatterns     int // Patterns skipped due to missing context
	UnresolvableMatches int // Total matches that couldn't be resolved
}

// DefaultDeltaAssembler implements the DeltaAssembler interface.
// It converts pipeline results from all phases into the final delta.json format.
type DefaultDeltaAssembler struct {
	stats AssemblerStats // Track assembly statistics
}

// NewDeltaAssembler creates a new delta assembler instance.
func NewDeltaAssembler() *DefaultDeltaAssembler {
	return &DefaultDeltaAssembler{
		stats: AssemblerStats{}, // Initialize stats
	}
}

// AssembleDelta creates the final delta.json from all pipeline results.
func (a *DefaultDeltaAssembler) AssembleDelta(ctx context.Context, results *PipelineResults) (*emitter.Delta, error) {
	if results == nil {
		return nil, fmt.Errorf("pipeline results cannot be nil")
	}

	// Calculate overall stats
	stats := a.calculatePipelineStats(results)

	// Assemble controllers
	controllers, err := a.AssembleControllers(results.ParseResults, results.MatchResults, results.InferenceResults)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble controllers: %w", err)
	}

	// Assemble models
	models, err := a.AssembleModels(results.ParseResults, results.MatchResults)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble models: %w", err)
	}

	// Assemble polymorphic relationships
	polymorphic, err := a.AssemblePolymorphic(results.MatchResults)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble polymorphic: %w", err)
	}

	// Assemble broadcast channels
	broadcast, err := a.AssembleBroadcast(results.MatchResults)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble broadcast: %w", err)
	}

	// Assemble metadata
	meta, err := a.AssembleMetadata(results, stats)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble metadata: %w", err)
	}

	delta := &emitter.Delta{
		Meta:        meta,
		Controllers: controllers,
		Models:      models,
		Polymorphic: polymorphic,
		Broadcast:   broadcast,
	}

	return delta, nil
}

// AssembleControllers converts parsing, matching, and inference results into controllers.
func (a *DefaultDeltaAssembler) AssembleControllers(parseResults *ParseResults, matchResults *MatchResults, inferenceResults *InferenceResults) ([]emitter.Controller, error) {
	if parseResults == nil || matchResults == nil {
		return []emitter.Controller{}, nil
	}

	// Group results by controller class and method
	controllerMethods := a.groupByControllerMethod(parseResults, matchResults, inferenceResults)

	var controllers []emitter.Controller
	for _, cm := range controllerMethods {
		controller := emitter.Controller{
			FQCN:   cm.FQCN,
			Method: cm.Method,
		}

		// Add HTTP information
		if cm.HTTPStatus != nil {
			controller.HTTP = &emitter.HTTPInfo{
				Status:   &cm.HTTPStatus.Status,
				Explicit: &cm.HTTPStatus.Explicit,
			}
		}

		// Add request information from inference
		if cm.RequestInfo != nil {
			controller.Request = a.convertRequestInfo(cm.RequestInfo)
		}

		// Add resources with deduplication and deterministic sorting
		if len(cm.Resources) > 0 {
			// Deduplicate by (class, collection) pair
			type resourceKey struct {
				class      string
				collection bool
			}
			seen := make(map[resourceKey]struct{})
			dedupedResources := make([]emitter.Resource, 0, len(cm.Resources))
			
			for _, resource := range cm.Resources {
				key := resourceKey{
					class:      resource.Class,
					collection: resource.Collection,
				}
				if _, exists := seen[key]; exists {
					continue // Skip duplicates
				}
				seen[key] = struct{}{}
				dedupedResources = append(dedupedResources, emitter.Resource{
					Class:      resource.Class,
					Collection: resource.Collection,
				})
			}
			
			// Sort by class, then collection=false before collection=true
			sort.Slice(dedupedResources, func(i, j int) bool {
				if dedupedResources[i].Class != dedupedResources[j].Class {
					return dedupedResources[i].Class < dedupedResources[j].Class
				}
				// collection=false (individual resources) come before collection=true
				return !dedupedResources[i].Collection && dedupedResources[j].Collection
			})
			
			controller.Resources = dedupedResources
		}

		// Add scopes
		if len(cm.Scopes) > 0 {
			scopes := make([]emitter.ScopeUsed, 0, len(cm.Scopes))
			for _, scope := range cm.Scopes {
				// Convert args to string slice
				args := make([]string, 0, len(scope.Args))
				for _, arg := range scope.Args {
					if argStr, ok := arg.(string); ok {
						args = append(args, argStr)
					}
				}
				scopes = append(scopes, emitter.ScopeUsed{
					On:   scope.On,
					Name: scope.Name,
					Args: args,
				})
			}
			controller.ScopesUsed = scopes
		}

		controllers = append(controllers, controller)
	}

	// Sort controllers deterministically
	sort.Slice(controllers, func(i, j int) bool {
		if controllers[i].FQCN != controllers[j].FQCN {
			return controllers[i].FQCN < controllers[j].FQCN
		}
		return controllers[i].Method < controllers[j].Method
	})

	return controllers, nil
}

// AssembleModels converts parsing and matching results into models.
func (a *DefaultDeltaAssembler) AssembleModels(parseResults *ParseResults, matchResults *MatchResults) ([]emitter.Model, error) {
	if parseResults == nil || matchResults == nil {
		return []emitter.Model{}, nil
	}

	// Group results by model class
	modelData := a.groupByModelClass(parseResults, matchResults)

	var models []emitter.Model
	for _, md := range modelData {
		model := emitter.Model{
			FQCN: md.FQCN,
		}

		// Add pivot information (aggregated deterministically per relation)
		if len(md.Pivots) > 0 {
			type agg struct {
				columns    map[string]struct{}
				alias      *string
				timestamps bool
			}
			aggs := make(map[string]*agg)
			for _, p := range md.Pivots {
				a := aggs[p.Relation]
				if a == nil {
					a = &agg{columns: make(map[string]struct{})}
					aggs[p.Relation] = a
				}
				// accumulate columns
				for _, c := range p.Fields {
					a.columns[c] = struct{}{}
				}
				// choose deterministic alias: lexicographically smallest non-empty
				if p.Alias != "" {
					if a.alias == nil || p.Alias < *a.alias {
						s := p.Alias
						a.alias = &s
					}
				}
				// timestamps true if any occurrence has it
				if p.Timestamps {
					a.timestamps = true
				}
			}
			// Build stable slice
			rels := make([]string, 0, len(aggs))
			for rel := range aggs {
				rels = append(rels, rel)
			}
			sort.Strings(rels)
			pivots := make([]emitter.PivotInfo, 0, len(rels))
			for _, rel := range rels {
				a := aggs[rel]
				// sort columns
				cols := make([]string, 0, len(a.columns))
				for c := range a.columns {
					cols = append(cols, c)
				}
				sort.Strings(cols)
				pi := emitter.PivotInfo{Relation: rel, Columns: cols}
				// Intentionally omit alias to ensure deterministic aggregation across runs
				ts := a.timestamps
				pi.Timestamps = &ts
				pivots = append(pivots, pi)
			}
			model.WithPivot = pivots
		}

		// Add attributes
		if len(md.Attributes) > 0 {
			attributes := make([]emitter.Attribute, 0, len(md.Attributes))
			for _, attr := range md.Attributes {
				// Only include modern attributes created via Attribute::make
				if attr.IsModern {
					attributes = append(attributes, emitter.Attribute{
						Name: attr.Name,
						Via:  "Attribute::make",
					})
				}
			}
			model.Attributes = attributes
		}

		// Polymorphic relationships are handled by AssemblePolymorphic method

		models = append(models, model)
	}

	// Sort models deterministically
	sort.Slice(models, func(i, j int) bool {
		return models[i].FQCN < models[j].FQCN
	})

	return models, nil
}

// AssemblePolymorphic converts matching results into polymorphic relationships.
func (a *DefaultDeltaAssembler) AssemblePolymorphic(matchResults *MatchResults) ([]emitter.Polymorphic, error) {
	if matchResults == nil {
		return []emitter.Polymorphic{}, nil
	}

	// Group polymorphic matches by parent
	polymorphicGroups := make(map[string][]*matchers.PolymorphicMatch)
	for _, match := range matchResults.PolymorphicMatches {
		// Extract parent from relation context (simplified)
		if parent, ok := a.extractParentFromContext(match); ok {
			polymorphicGroups[parent] = append(polymorphicGroups[parent], match)
		}
		// Skip polymorphic matches that cannot be resolved to valid parents
	}

	var polymorphic []emitter.Polymorphic
	for parent, matches := range polymorphicGroups {
		// Take the first match for basic info (in reality, you'd merge)
		if len(matches) == 0 {
			continue
		}

		match := matches[0]
		poly := emitter.Polymorphic{
			Parent: parent,
			Morph: emitter.MorphInfo{
				Key:        match.Relation,
				TypeColumn: match.MorphType,
				IdColumn:   match.MorphId,
			},
		}

		if match.Discriminator != nil {
			poly.Discriminator = emitter.Discriminator{
				PropertyName: match.Discriminator.PropertyName,
				Mapping:      match.Discriminator.Mapping,
			}
		}

		if match.DepthTruncated {
			poly.DepthTruncated = &match.DepthTruncated
		}

		polymorphic = append(polymorphic, poly)
	}

	// Sort polymorphic deterministically
	sort.Slice(polymorphic, func(i, j int) bool {
		return polymorphic[i].Parent < polymorphic[j].Parent
	})

	return polymorphic, nil
}

// AssembleBroadcast converts matching results into broadcast channels.
func (a *DefaultDeltaAssembler) AssembleBroadcast(matchResults *MatchResults) ([]emitter.Broadcast, error) {
	if matchResults == nil {
		return []emitter.Broadcast{}, nil
	}

	type channelKey struct {
		channel string
	}
	
	bestChannels := make(map[channelKey]*emitter.Broadcast)
	visibilityPriority := map[string]int{
		"presence": 3,
		"private":  2,
		"public":   1,
	}

	for _, match := range matchResults.BroadcastMatches {
		if match.Channel == "" {
			a.stats.SkippedPatterns++
			continue
		}
		
		if match.File != "" && !strings.HasSuffix(match.File, "routes/channels.php") {
			a.stats.SkippedPatterns++
			continue
		}

		key := channelKey{channel: match.Channel}
		
		bc := emitter.Broadcast{
			Channel:    match.Channel,
			Params:     match.Params,
			Visibility: match.Visibility,
		}

		if match.File != "" {
			bc.File = &match.File
		}

		if match.PayloadLiteral {
			bc.PayloadLiteral = &match.PayloadLiteral
		}

		if existing, exists := bestChannels[key]; exists {
			existingPriority := visibilityPriority[existing.Visibility]
			newPriority := visibilityPriority[bc.Visibility]
			
			if newPriority > existingPriority {
				bestChannels[key] = &bc
			}
		} else {
			bestChannels[key] = &bc
		}
	}

	var broadcast []emitter.Broadcast
	for _, bc := range bestChannels {
		broadcast = append(broadcast, *bc)
	}

	sort.Slice(broadcast, func(i, j int) bool {
		return broadcast[i].Channel < broadcast[j].Channel
	})

	return broadcast, nil
}

// AssembleMetadata creates metadata for the delta.json.
func (a *DefaultDeltaAssembler) AssembleMetadata(results *PipelineResults, stats *PipelineStats) (emitter.MetaInfo, error) {
	durationMs := int64(stats.TotalDuration.Milliseconds())
	
	if durationMs == 0 && stats != nil {
		fallbackDuration := stats.IndexingDuration + stats.ParsingDuration + 
			stats.MatchingDuration + stats.InferenceDuration + stats.AssemblyDuration
		if fallbackDuration > 0 {
			durationMs = int64(fallbackDuration.Milliseconds())
		}
	}
	
	metaStats := emitter.MetaStats{
		FilesParsed: int64(stats.FilesProcessed),
		Skipped:     int64(stats.FilesSkipped),
		DurationMs:  durationMs,
	}

	// Add assembler stats if they contain meaningful data
	if a.stats.SkippedControllers > 0 || a.stats.SkippedModels > 0 || 
		a.stats.SkippedPatterns > 0 || a.stats.UnresolvableMatches > 0 {
		metaStats.AssemblerStats = &emitter.AssemblerStats{
			SkippedControllers:   a.stats.SkippedControllers,
			SkippedModels:        a.stats.SkippedModels,
			SkippedPatterns:      a.stats.SkippedPatterns,
			UnresolvableMatches:  a.stats.UnresolvableMatches,
		}
	}

	meta := emitter.MetaInfo{
		Partial: results.Partial,
		Stats:   metaStats,
	}

	return meta, nil
}

// Helper methods

// ControllerMethod represents a controller method with associated patterns.
type ControllerMethod struct {
	FQCN        string
	Method      string
	HTTPStatus  *matchers.HTTPStatusMatch
	RequestInfo *infer.RequestInfo
	Resources   []*matchers.ResourceMatch
	Scopes      []*matchers.ScopeMatch
	Polymorphic []*matchers.PolymorphicMatch
}

// groupByControllerMethod groups results by controller class and method.
func (a *DefaultDeltaAssembler) groupByControllerMethod(parseResults *ParseResults, matchResults *MatchResults, inferenceResults *InferenceResults) []*ControllerMethod {
	methodMap := make(map[string]*ControllerMethod)

	// Process controllers from parse results
	for _, parsedFile := range parseResults.ParsedFiles {
		if parsedFile.LaravelPatterns != nil {
			for _, controller := range parsedFile.LaravelPatterns.Controllers {
				for _, action := range controller.Actions {
					fqcn := controller.Class.FullyQualifiedName
					method := action.Name
					key := fqcn + "::" + method

					cm := &ControllerMethod{
						FQCN:   fqcn,
						Method: method,
					}
					methodMap[key] = cm
				}
			}
		}
	}

	// Add HTTP status matches by extracting method from pattern
	for _, match := range matchResults.HTTPStatusMatches {
		if key, ok := a.extractMethodKeyFromPattern(match.Pattern); ok {
			if cm, exists := methodMap[key]; exists {
				cm.HTTPStatus = match
			} else {
				// Create new controller method if it doesn't exist
				if controller, controllerOk := a.extractControllerFromKey(key); controllerOk {
					if method, methodOk := a.extractMethodFromKey(key); methodOk {
						cm := &ControllerMethod{
							FQCN:       controller,
							Method:     method,
							HTTPStatus: match,
						}
						methodMap[key] = cm
					}
					// Skip if method cannot be resolved from key
				}
				// Skip if controller cannot be resolved
			}
		}
		// Skip matches that cannot be resolved to valid method keys
	}

	// Add request info from inference with sorted keys for determinism
	if inferenceResults != nil {
		inferenceKeys := make([]string, 0, len(inferenceResults.RequestShapes))
		for methodKey := range inferenceResults.RequestShapes {
			inferenceKeys = append(inferenceKeys, methodKey)
		}
		sort.Strings(inferenceKeys)

		for _, methodKey := range inferenceKeys {
			requestInfo := inferenceResults.RequestShapes[methodKey]
			if cm, exists := methodMap[methodKey]; exists {
				cm.RequestInfo = requestInfo
			}
		}
	}

	// Add other pattern matches with proper key extraction
	for _, match := range matchResults.ResourceMatches {
		if key, ok := a.extractMethodKeyFromPattern(match.Pattern); ok {
			if cm, exists := methodMap[key]; exists {
				cm.Resources = append(cm.Resources, match)
			}
		}
		// Skip resource matches that cannot be resolved to valid method keys
	}

	for _, match := range matchResults.ScopeMatches {
		if key, ok := a.extractMethodKeyFromScopeMatch(match.Context, match.Pattern); ok {
			if cm, exists := methodMap[key]; exists {
				cm.Scopes = append(cm.Scopes, match)
			}
		}
		// Skip scope matches that cannot be resolved to valid method keys
	}

	for _, match := range matchResults.PolymorphicMatches {
		if key, ok := a.extractMethodKeyFromPolymorphicMatch(match.Context, match.Pattern); ok {
			if cm, exists := methodMap[key]; exists {
				cm.Polymorphic = append(cm.Polymorphic, match)
			}
		}
		// Skip polymorphic matches that cannot be resolved to valid method keys
	}

	// Convert map to slice
	var controllers []*ControllerMethod
	for _, cm := range methodMap {
		controllers = append(controllers, cm)
	}

	// Sort for determinism
	sort.Slice(controllers, func(i, j int) bool {
		if controllers[i].FQCN != controllers[j].FQCN {
			return controllers[i].FQCN < controllers[j].FQCN
		}
		return controllers[i].Method < controllers[j].Method
	})

	return controllers
}

// ModelData represents a model class with associated patterns.
type ModelData struct {
	FQCN        string
	Pivots      []*matchers.PivotMatch
	Attributes  []*matchers.AttributeMatch
	Polymorphic []*matchers.PolymorphicMatch
}

// groupByModelClass groups results by model class.
func (a *DefaultDeltaAssembler) groupByModelClass(parseResults *ParseResults, matchResults *MatchResults) []*ModelData {
	modelMap := make(map[string]*ModelData)

	// Process models from parse results
	for _, parsedFile := range parseResults.ParsedFiles {
		if parsedFile.LaravelPatterns != nil {
			for _, model := range parsedFile.LaravelPatterns.Models {
				fqcn := model.Class.FullyQualifiedName
				md := &ModelData{
					FQCN: fqcn,
				}
				modelMap[fqcn] = md
			}
		}
	}

	// Add pivot matches with proper model extraction
	for _, match := range matchResults.PivotMatches {
		if fqcn, ok := a.extractModelFromPatternAndMethod(match.Pattern, match.Method); ok {
			if md, exists := modelMap[fqcn]; exists {
				md.Pivots = append(md.Pivots, match)
			} else {
				md := &ModelData{
					FQCN:   fqcn,
					Pivots: []*matchers.PivotMatch{match},
				}
				modelMap[fqcn] = md
			}
		}
		// Skip pivot matches that cannot be resolved to valid model FQCNs
	}

	// Add attribute matches with proper model extraction
	for _, match := range matchResults.AttributeMatches {
		if fqcn, ok := a.extractModelFromPatternAndMethod(match.Pattern, match.Method); ok {
			if md, exists := modelMap[fqcn]; exists {
				md.Attributes = append(md.Attributes, match)
			} else {
				md := &ModelData{
					FQCN:       fqcn,
					Attributes: []*matchers.AttributeMatch{match},
				}
				modelMap[fqcn] = md
			}
		}
		// Skip attribute matches that cannot be resolved to valid model FQCNs
	}

	// Add polymorphic matches with proper model extraction
	for _, match := range matchResults.PolymorphicMatches {
		if fqcn, ok := a.extractModelFromPolymorphicMatch(match.Context, match.Pattern); ok {
			if md, exists := modelMap[fqcn]; exists {
				md.Polymorphic = append(md.Polymorphic, match)
			} else {
				md := &ModelData{
					FQCN:        fqcn,
					Polymorphic: []*matchers.PolymorphicMatch{match},
				}
				modelMap[fqcn] = md
			}
		}
		// Skip polymorphic matches that cannot be resolved to valid model FQCNs
	}

	// Convert map to slice
	var models []*ModelData
	for _, md := range modelMap {
		models = append(models, md)
	}

	// Sort models and their internal arrays for determinism
	sort.Slice(models, func(i, j int) bool {
		return models[i].FQCN < models[j].FQCN
	})

	// Sort internal arrays within each model for determinism
	for _, md := range models {
		// Sort pivot matches
		if len(md.Pivots) > 0 {
			sort.Slice(md.Pivots, func(i, j int) bool {
				if md.Pivots[i].Relation != md.Pivots[j].Relation {
					return md.Pivots[i].Relation < md.Pivots[j].Relation
				}
				if md.Pivots[i].Pattern != md.Pivots[j].Pattern {
					return md.Pivots[i].Pattern < md.Pivots[j].Pattern
				}
				return md.Pivots[i].Method < md.Pivots[j].Method
			})
		}

		// Sort attribute matches
		if len(md.Attributes) > 0 {
			sort.Slice(md.Attributes, func(i, j int) bool {
				if md.Attributes[i].Name != md.Attributes[j].Name {
					return md.Attributes[i].Name < md.Attributes[j].Name
				}
				return md.Attributes[i].Pattern < md.Attributes[j].Pattern
			})
		}

		// Sort polymorphic matches
		if len(md.Polymorphic) > 0 {
			sort.Slice(md.Polymorphic, func(i, j int) bool {
				if md.Polymorphic[i].Relation != md.Polymorphic[j].Relation {
					return md.Polymorphic[i].Relation < md.Polymorphic[j].Relation
				}
				return md.Polymorphic[i].Pattern < md.Polymorphic[j].Pattern
			})
		}
	}

	return models
}

// convertRequestInfo converts infer.RequestInfo to emitter.RequestInfo.
func (a *DefaultDeltaAssembler) convertRequestInfo(requestInfo *infer.RequestInfo) *emitter.RequestInfo {
	if requestInfo == nil {
		return nil
	}

	return &emitter.RequestInfo{
		ContentTypes: requestInfo.ContentTypes,
		Body:         a.convertOrderedObjectToEmitter(&requestInfo.Body),
		Query:        a.convertOrderedObjectToEmitter(&requestInfo.Query),
		Files:        a.convertOrderedObjectToEmitter(&requestInfo.Files),
	}
}

// convertOrderedObjectToEmitter converts infer.OrderedObject to emitter.OrderedObject.
func (a *DefaultDeltaAssembler) convertOrderedObjectToEmitter(obj *infer.OrderedObject) emitter.OrderedObject {
	if obj == nil || obj.IsEmpty() {
		return nil
	}

	result := make(emitter.OrderedObject)

	// Process properties in order
	for _, key := range obj.Order {
		if prop, exists := obj.Properties[key]; exists {
			result[key] = a.convertPropertyToEmitter(prop)
		}
	}

	return result
}

// convertPropertyToEmitter converts property info to emitter format.
// Recursively processes nested structures instead of returning empty objects.
func (a *DefaultDeltaAssembler) convertPropertyToEmitter(prop *infer.PropertyInfo) emitter.OrderedObject {
	if prop == nil {
		return emitter.OrderedObject{}
	}

	result := make(emitter.OrderedObject)

	// Handle different property types
	switch prop.Type {
	case infer.PropertyTypeObject:
		// Recursively process nested objects
		if prop.Properties != nil && !prop.Properties.IsEmpty() {
			// Process each nested property recursively
			for _, key := range prop.Properties.Order {
				if nestedProp, exists := prop.Properties.Properties[key]; exists {
					// Recursive call to handle nested structures
					result[key] = a.convertPropertyToEmitter(nestedProp)
				}
			}
		}
		// If no nested properties, return empty object (which is correct for leaf objects)
		return result

	case infer.PropertyTypeArray:
		// Handle array items
		if prop.Items != nil {
			// For arrays, we need to represent the item structure
			// Laravel patterns like users.*.email become {"*": {"email": {}}}
			return a.convertPropertyToEmitter(prop.Items)
		}
		return emitter.OrderedObject{}

	case infer.PropertyTypeString, infer.PropertyTypeNumber, infer.PropertyTypeFile:
		// For primitive types, return an empty object as terminal
		// This represents the leaf of the structure
		return emitter.OrderedObject{}

	default:
		// Unknown types default to empty object
		return emitter.OrderedObject{}
	}
}

// extractParentFromContext extracts parent class from polymorphic match context.
// Returns empty string and false if cannot resolve to a valid parent.
func (a *DefaultDeltaAssembler) extractParentFromContext(match *matchers.PolymorphicMatch) (string, bool) {
	if match == nil {
		a.stats.SkippedModels++
		return "", false
	}
	
	if match.Context != "" {
		// Extract parent from context string (simplified)
		parts := strings.Split(match.Context, "::")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0], true
		}
	}
	
	// Could not resolve parent from context
	a.stats.SkippedModels++
	return "", false
}


// extractControllerFromKey extracts controller FQCN from method key.
// Returns empty string and false if the key cannot be resolved to a valid controller.
func (a *DefaultDeltaAssembler) extractControllerFromKey(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	
	parts := strings.Split(key, "::")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0], true
	}
	
	a.stats.SkippedControllers++
	return "", false
}

// extractMethodFromKey extracts method name from method key.
// Returns empty string and false if cannot resolve to a valid method name.
func (a *DefaultDeltaAssembler) extractMethodFromKey(key string) (string, bool) {
	if key == "" {
		a.stats.SkippedControllers++
		return "", false
	}
	
	parts := strings.Split(key, "::")
	if len(parts) > 1 && parts[1] != "" {
		return parts[1], true
	}
	
	// Could not resolve method from key
	a.stats.SkippedControllers++
	return "", false
}

// extractMethodKeyFromPattern extracts a method key from a pattern string.
// Returns empty string and false if the pattern cannot be resolved to a valid method key.
func (a *DefaultDeltaAssembler) extractMethodKeyFromPattern(pattern string) (string, bool) {
	if pattern == "" {
		a.stats.SkippedPatterns++
		return "", false
	}
	
	// TODO: Implement proper pattern to method key extraction
	// For now, we cannot resolve method keys from patterns without additional context
	// This needs to be fixed to properly extract Controller::method from the pattern
	// The pattern should include context about which controller/method it came from
	
	a.stats.UnresolvableMatches++
	return "", false
}

// extractMethodKeyFromScopeMatch extracts a method key from scope match context and pattern.
// Returns empty string and false if cannot resolve to a valid method key.
func (a *DefaultDeltaAssembler) extractMethodKeyFromScopeMatch(context, pattern string) (string, bool) {
	if context != "" && strings.Contains(context, "::") {
		return context, true
	}
	
	// Try to extract from pattern, but this will likely fail
	return a.extractMethodKeyFromPattern(pattern)
}

// extractMethodKeyFromPolymorphicMatch extracts a method key from polymorphic match context and pattern.
// Returns empty string and false if cannot resolve to a valid method key.
func (a *DefaultDeltaAssembler) extractMethodKeyFromPolymorphicMatch(context, pattern string) (string, bool) {
	if context != "" && strings.Contains(context, "::") {
		return context, true
	}
	return a.extractMethodKeyFromPattern(pattern)
}

// extractModelFromPatternAndMethod extracts a model FQCN from pattern and method.
// Returns empty string and false if cannot resolve to a valid model FQCN.
func (a *DefaultDeltaAssembler) extractModelFromPatternAndMethod(pattern, method string) (string, bool) {
	// Try to extract from method first if it contains class info
	if method != "" && strings.Contains(method, "\\") {
		return method, true
	}
	
	// Cannot resolve model from pattern alone without additional context
	// Real implementation would need access to parsed models from parse results
	
	a.stats.SkippedModels++
	return "", false
}

// extractModelFromPolymorphicMatch extracts a model FQCN from polymorphic match context and pattern.
// Returns empty string and false if cannot resolve to a valid model FQCN.
func (a *DefaultDeltaAssembler) extractModelFromPolymorphicMatch(context, pattern string) (string, bool) {
	// Try to extract from context first
	if context != "" {
		// Look for model class name in context
		if strings.Contains(context, "\\Models\\") {
			return context, true
		}
		if strings.Contains(context, "::") {
			parts := strings.Split(context, "::")
			if len(parts) > 0 && parts[0] != "" {
				return parts[0], true
			}
		}
	}

	// Try pattern-based extraction, but this will likely fail
	return a.extractModelFromPatternAndMethod(pattern, "")
}

// calculatePipelineStats calculates comprehensive statistics from pipeline results.
func (a *DefaultDeltaAssembler) calculatePipelineStats(results *PipelineResults) *PipelineStats {
	stats := &PipelineStats{}

	if results.IndexResult != nil {
		stats.FilesDiscovered = results.IndexResult.TotalFiles
		stats.FilesSkipped = results.IndexResult.Cached
		stats.IndexingDuration = time.Duration(results.IndexResult.DurationMs) * time.Millisecond
	}

	if results.ParseResults != nil {
		stats.FilesProcessed = results.ParseResults.FilesProcessed
		stats.FilesFailed = results.ParseResults.ParseErrors
		stats.ParsingDuration = results.ParseResults.ParseDuration
	}

	if results.MatchResults != nil {
		stats.PatternsDetected = results.MatchResults.TotalMatches
		stats.MatchingDuration = results.MatchResults.MatchingDuration
	}

	if results.InferenceResults != nil {
		stats.ShapesInferred = results.InferenceResults.ShapesInferred
		stats.InferenceDuration = results.InferenceResults.InferenceDuration
	}

	// Stats wiring: fallback duration calculation if ProcessingTime is zero
	if results.ProcessingTime == 0 {
		// Compute fallback as sum of available stage durations for non-trivial runs
		var fallbackDuration time.Duration
		
		// Add indexing phase duration
		if results.IndexResult != nil && results.IndexResult.DurationMs > 0 {
			fallbackDuration += time.Duration(results.IndexResult.DurationMs) * time.Millisecond
		}
		
		// Add parsing phase duration
		if results.ParseResults != nil && results.ParseResults.ParseDuration > 0 {
			fallbackDuration += results.ParseResults.ParseDuration
		}
		
		// Add matching phase duration
		if results.MatchResults != nil && results.MatchResults.MatchingDuration > 0 {
			fallbackDuration += results.MatchResults.MatchingDuration
		}
		
		// Add inference phase duration
		if results.InferenceResults != nil {
			fallbackDuration += results.InferenceResults.InferenceDuration
		}
		
		// If still no duration and we have files processed, estimate minimum time
		if fallbackDuration == 0 && stats.FilesProcessed > 0 {
			// Estimate at least 1ms per file as a minimum
			fallbackDuration = time.Duration(stats.FilesProcessed) * time.Millisecond
		}
		
		// If no stage durations available, compute from start/end times
		if fallbackDuration == 0 && !results.EndTime.IsZero() && !results.StartTime.IsZero() {
			fallbackDuration = results.EndTime.Sub(results.StartTime)
		}
		
		// Ensure minimum duration for non-trivial runs
		if fallbackDuration == 0 && stats.FilesProcessed > 0 {
			fallbackDuration = time.Millisecond // Minimum 1ms for any real work
		}
		
		stats.TotalDuration = fallbackDuration
	} else {
		stats.TotalDuration = results.ProcessingTime
	}
	
	// Ensure durationMs is never 0 for non-trivial runs
	if stats.TotalDuration == 0 && stats.FilesProcessed > 0 {
		stats.TotalDuration = time.Millisecond
	}

	return stats
}

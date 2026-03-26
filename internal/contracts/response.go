//go:build goexperiment.jsonv2

package contracts

import "github.com/oxhq/oxinfer/internal/emitter"

func BuildAnalysisResponse(request *AnalysisRequest, delta *emitter.Delta, oxinferVersion string) *AnalysisResponse {
	controllerKeys := make(map[string]struct{}, len(delta.Controllers))
	for _, controller := range delta.Controllers {
		controllerKeys[ControllerActionKey(controller)] = struct{}{}
	}

	routeMatches := make([]RouteMatch, 0, len(request.Runtime.Routes))
	diagnostics := make([]Diagnostic, 0)
	matchedActionKeys := make(map[string]struct{})
	partial := delta.Meta.Partial

	for _, route := range request.Runtime.Routes {
		match := RouteMatch{
			RouteID:    route.RouteID,
			ActionKind: route.Action.Kind,
		}

		switch route.Action.Kind {
		case ActionKindControllerMethod, ActionKindInvokableController:
			if actionKey, ok := route.Action.ActionKey(); ok {
				match.ActionKey = &actionKey
				if _, exists := controllerKeys[actionKey]; exists {
					match.MatchStatus = MatchStatusMatched
					matchedActionKeys[actionKey] = struct{}{}
				} else {
					match.MatchStatus = MatchStatusMissingStatic
					match.ReasonCode = stringPtr(ReasonCodeMissingStaticAction)
					diagnostics = append(diagnostics, Diagnostic{
						Code:      DiagnosticCodeRouteActionMissingStatic,
						Severity:  SeverityWarn,
						Scope:     ScopeAction,
						Message:   "runtime route action has no matching static controller analysis",
						RouteID:   stringPtr(route.RouteID),
						ActionKey: &actionKey,
					})
					partial = true
				}
			} else {
				match.MatchStatus = MatchStatusUnsupported
				match.ReasonCode = stringPtr(ReasonCodeUnknownAction)
				diagnostics = append(diagnostics, Diagnostic{
					Code:     DiagnosticCodeRouteActionUnsupported,
					Severity: SeverityWarn,
					Scope:    ScopeRoute,
					Message:  "runtime route action is invalid for static join",
					RouteID:  stringPtr(route.RouteID),
				})
				partial = true
			}
		case ActionKindClosure:
			match.MatchStatus = MatchStatusRuntimeOnly
			match.ReasonCode = stringPtr(ReasonCodeClosureAction)
			diagnostics = append(diagnostics, Diagnostic{
				Code:     DiagnosticCodeRouteRuntimeOnlyClosure,
				Severity: SeverityInfo,
				Scope:    ScopeRoute,
				Message:  "runtime route uses a closure action and is runtime-only in contract v2",
				RouteID:  stringPtr(route.RouteID),
			})
			partial = true
		default:
			match.MatchStatus = MatchStatusUnsupported
			match.ReasonCode = stringPtr(ReasonCodeUnknownAction)
			diagnostics = append(diagnostics, Diagnostic{
				Code:     DiagnosticCodeRouteActionUnsupported,
				Severity: SeverityWarn,
				Scope:    ScopeRoute,
				Message:  "runtime route action kind is unsupported in contract v2",
				RouteID:  stringPtr(route.RouteID),
			})
			partial = true
		}

		if match.MatchStatus != MatchStatusMatched {
			partial = true
		}

		routeMatches = append(routeMatches, match)
	}

	filteredDelta := *delta
	filteredDelta.Controllers = filterControllers(delta.Controllers, matchedActionKeys)
	filteredDelta.Resources = filterResources(delta.Resources, filteredDelta.Controllers)

	if delta.Meta.Partial {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     DiagnosticCodeStaticAnalysisPartial,
			Severity: SeverityWarn,
			Scope:    ScopeGlobal,
			Message:  "static analysis completed with partial results",
		})
	}

	counts := countDiagnostics(diagnostics)
	status := ResponseStatusOK
	if partial {
		status = ResponseStatusPartial
	}

	return &AnalysisResponse{
		ContractVersion:    ContractVersion,
		RequestID:          request.RequestID,
		RuntimeFingerprint: request.RuntimeFingerprint,
		Status:             status,
		Meta: ResponseMeta{
			OxinferVersion:   oxinferVersion,
			Partial:          partial,
			Stats:            filteredDelta.Meta.Stats,
			DiagnosticCounts: counts,
		},
		Delta:        filteredDelta,
		RouteMatches: routeMatches,
		Diagnostics:  diagnostics,
	}
}

func filterControllers(controllers []emitter.Controller, matchedActionKeys map[string]struct{}) []emitter.Controller {
	filtered := make([]emitter.Controller, 0, len(matchedActionKeys))
	for _, controller := range controllers {
		if _, ok := matchedActionKeys[ControllerActionKey(controller)]; ok {
			filtered = append(filtered, controller)
		}
	}
	return filtered
}

func filterResources(resources []emitter.ResourceDef, controllers []emitter.Controller) []emitter.ResourceDef {
	if len(resources) == 0 || len(controllers) == 0 {
		return nil
	}

	required := make(map[string]struct{})
	for _, controller := range controllers {
		for _, resource := range controller.Resources {
			fqcn := resource.FQCN
			if fqcn == "" {
				continue
			}
			required[fqcn] = struct{}{}
		}
		for _, response := range controller.Responses {
			if response.BodySchema != nil {
				for _, nested := range collectResourceRefs(*response.BodySchema) {
					if nested == "" {
						continue
					}
					required[nested] = struct{}{}
				}
			}
			if response.Inertia != nil && response.Inertia.PropsSchema != nil {
				for _, nested := range collectResourceRefs(*response.Inertia.PropsSchema) {
					if nested == "" {
						continue
					}
					required[nested] = struct{}{}
				}
			}
		}
	}

	resourceIndex := make(map[string]emitter.ResourceDef, len(resources))
	for _, resource := range resources {
		resourceIndex[resource.FQCN] = resource
	}

	queue := make([]string, 0, len(required))
	for fqcn := range required {
		queue = append(queue, fqcn)
	}
	for len(queue) > 0 {
		fqcn := queue[0]
		queue = queue[1:]

		resource, ok := resourceIndex[fqcn]
		if !ok {
			continue
		}
		for _, nested := range collectResourceRefs(resource.Schema) {
			if _, seen := required[nested]; seen {
				continue
			}
			required[nested] = struct{}{}
			queue = append(queue, nested)
		}
	}

	filtered := make([]emitter.ResourceDef, 0, len(required))
	for _, resource := range resources {
		if _, ok := required[resource.FQCN]; ok {
			filtered = append(filtered, resource)
		}
	}

	return filtered
}

func collectResourceRefs(node emitter.ResourceSchemaNode) []string {
	var refs []string
	if node.Ref != "" {
		refs = append(refs, node.Ref)
	}
	if node.Items != nil {
		refs = append(refs, collectResourceRefs(*node.Items)...)
	}
	for _, property := range node.Properties {
		refs = append(refs, collectResourceRefs(property)...)
	}
	return refs
}

func countDiagnostics(diagnostics []Diagnostic) DiagnosticCounts {
	var counts DiagnosticCounts
	for _, diagnostic := range diagnostics {
		switch diagnostic.Severity {
		case SeverityInfo:
			counts.Info++
		case SeverityWarn:
			counts.Warn++
		case SeverityError:
			counts.Error++
		}
	}
	return counts
}

func stringPtr(value string) *string {
	return &value
}

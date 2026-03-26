//go:build goexperiment.jsonv2

package response

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
	"github.com/oxhq/oxinfer/internal/psr4"
)

const maxResourceNestingDepth = 6

type phpFileMetadata struct {
	Namespace string
	Uses      map[string]string
	ClassName string
	Extends   string
}

type extractor struct {
	resolver      psr4.PSR4Resolver
	sourceCache   map[string]string
	metadataCache map[string]*phpFileMetadata
	definitions   map[string]emitter.ResourceDef
	building      map[string]struct{}
}

func EnrichDelta(ctx context.Context, manifestData *manifest.Manifest, delta *emitter.Delta) error {
	if manifestData == nil || delta == nil || len(delta.Controllers) == 0 {
		return nil
	}

	resolver, err := psr4.NewPSR4ResolverFromManifest(manifestData)
	if err != nil {
		return nil
	}

	ex := &extractor{
		resolver:      resolver,
		sourceCache:   make(map[string]string),
		metadataCache: make(map[string]*phpFileMetadata),
		definitions:   make(map[string]emitter.ResourceDef),
		building:      make(map[string]struct{}),
	}

	for i := range delta.Controllers {
		controller := &delta.Controllers[i]
		for _, resource := range controller.Resources {
			fqcn := strings.TrimSpace(resource.FQCN)
			if fqcn == "" {
				continue
			}
			ex.buildDefinition(ctx, fqcn, 0)
		}
		methodCtx, ok := ex.controllerMethodContext(ctx, controller.FQCN, controller.Method)
		if ok {
			controller.Authorization = ex.buildAuthorization(ctx, controller.FQCN, controller.Method, methodCtx)
		}
		if responses, ok := ex.buildResponses(ctx, controller.FQCN, controller.Method, methodCtx, controller.Authorization); ok && len(responses) > 0 {
			controller.Responses = responses
		}
	}

	if len(ex.definitions) > 0 {
		resourceDefs := make([]emitter.ResourceDef, 0, len(ex.definitions))
		for _, definition := range ex.definitions {
			resourceDefs = append(resourceDefs, definition)
		}
		sort.Slice(resourceDefs, func(i, j int) bool {
			if resourceDefs[i].FQCN != resourceDefs[j].FQCN {
				return resourceDefs[i].FQCN < resourceDefs[j].FQCN
			}
			return resourceDefs[i].Class < resourceDefs[j].Class
		})

		delta.Resources = resourceDefs
	}
	return nil
}

func (e *extractor) buildResponses(ctx context.Context, controllerFQCN, method string, methodCtx *controllerMethodContext, authorization []emitter.AuthorizationHint) ([]emitter.Response, bool) {
	controllerFQCN = strings.TrimSpace(strings.TrimPrefix(controllerFQCN, `\`))
	method = strings.TrimSpace(method)
	if controllerFQCN == "" || method == "" || methodCtx == nil {
		return nil, false
	}

	locals := collectLocalAssignments(methodCtx.Body)
	expressions := extractReturnExpressions(methodCtx.Body)
	responses := make([]emitter.Response, 0, len(expressions)+4)
	for _, expression := range expressions {
		response, ok := e.responseFromExpression(ctx, controllerFQCN, expression, methodCtx.Meta, locals)
		if !ok {
			continue
		}
		responses = append(responses, response)
	}

	responses = append(responses, e.frameworkResponses(ctx, controllerFQCN, methodCtx, locals, authorization)...)
	if len(responses) == 0 {
		return nil, false
	}

	responses = dedupeResponses(responses)
	sort.Slice(responses, func(i, j int) bool {
		if responseStatusValue(responses[i].Status) != responseStatusValue(responses[j].Status) {
			return responseStatusValue(responses[i].Status) < responseStatusValue(responses[j].Status)
		}
		if responses[i].Kind != responses[j].Kind {
			return responses[i].Kind < responses[j].Kind
		}
		if responses[i].ContentType != responses[j].ContentType {
			return responses[i].ContentType < responses[j].ContentType
		}
		if responses[i].Source != responses[j].Source {
			return responses[i].Source < responses[j].Source
		}
		if responses[i].Via != responses[j].Via {
			return responses[i].Via < responses[j].Via
		}
		if responseHeadersSignature(responses[i].Headers) != responseHeadersSignature(responses[j].Headers) {
			return responseHeadersSignature(responses[i].Headers) < responseHeadersSignature(responses[j].Headers)
		}
		if responseRedirectSignature(responses[i].Redirect) != responseRedirectSignature(responses[j].Redirect) {
			return responseRedirectSignature(responses[i].Redirect) < responseRedirectSignature(responses[j].Redirect)
		}
		if responseDownloadSignature(responses[i].Download) != responseDownloadSignature(responses[j].Download) {
			return responseDownloadSignature(responses[i].Download) < responseDownloadSignature(responses[j].Download)
		}
		if responseInertiaSignature(responses[i].Inertia) != responseInertiaSignature(responses[j].Inertia) {
			return responseInertiaSignature(responses[i].Inertia) < responseInertiaSignature(responses[j].Inertia)
		}
		return responseSchemaSignature(responses[i].BodySchema) < responseSchemaSignature(responses[j].BodySchema)
	})

	return responses, len(responses) > 0
}

func (e *extractor) responseFromExpression(ctx context.Context, currentFQCN, expression string, meta *phpFileMetadata, locals map[string]string) (emitter.Response, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return emitter.Response{}, false
	}

	resolvedExpression, statusOverride, responseHeaders, _ := resolveResponseExpression(expression, locals, 0)

	if helper, args, ok := parseResponseHelperCall(resolvedExpression, currentFQCN, meta); ok {
		switch helper {
		case "noContent":
			status := 204
			if len(args) > 0 {
				if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[0], locals, 0)); ok {
					status = parsed
				}
			}
			if statusOverride != nil {
				status = *statusOverride
			}
			return emitter.Response{
				Kind:        "no_content",
				Status:      &status,
				Explicit:    boolPtr(true),
				ContentType: "",
				Headers:     responseHeaders,
				Source:      "response()->noContent",
				Via:         "response()->noContent",
			}, true
		case "json":
			return e.responseFromJSONCall(ctx, currentFQCN, args, meta, "response()->json", statusOverride, responseHeaders, locals)
		case "response":
			return e.responseFromJSONCall(ctx, currentFQCN, args, meta, "response()", statusOverride, responseHeaders, locals)
		case "redirect":
			return responseFromRedirectCall(args, "redirect()", statusOverride, responseHeaders, locals)
		case "redirectRoute":
			return responseFromRedirectRouteCall(args, "redirect()->route()", statusOverride, responseHeaders, locals)
		case "redirectTo":
			return responseFromRedirectCall(args, "redirect()->to()", statusOverride, responseHeaders, locals)
		case "redirectAway":
			return responseFromRedirectCall(args, "redirect()->away()", statusOverride, responseHeaders, locals)
		case "back":
			return responseFromBackCall(args, "back()", statusOverride, responseHeaders, locals)
		case "download":
			return responseFromDownloadCall(args, "response()->download", statusOverride, responseHeaders, locals)
		case "streamDownload":
			return responseFromStreamDownloadCall(args, "response()->streamDownload", statusOverride, responseHeaders, locals)
		case "file":
			return responseFromFileCall(args, "response()->file", statusOverride, responseHeaders, locals)
		case "stream":
			return responseFromStreamCall(args, "response()->stream", statusOverride, responseHeaders, locals)
		case "streamJson":
			return e.responseFromStreamJSONCall(ctx, currentFQCN, args, meta, "response()->streamJson", statusOverride, responseHeaders, locals)
		case "inertiaRender":
			return e.responseFromInertiaRenderCall(ctx, currentFQCN, args, meta, "Inertia::render", statusOverride, responseHeaders, locals)
		case "inertiaHelper":
			return e.responseFromInertiaRenderCall(ctx, currentFQCN, args, meta, "inertia()", statusOverride, responseHeaders, locals)
		case "inertiaLocation":
			return responseFromInertiaLocationCall(args, "Inertia::location", statusOverride, responseHeaders, locals)
		}
	}

	if schema, ok := e.responseSchemaFromBody(ctx, currentFQCN, resolvedExpression, meta, locals); ok {
		if responseKind, ok := responseKindFromSchema(schema); ok {
			status := 200
			explicit := false
			if statusOverride != nil {
				status = *statusOverride
				explicit = true
			}
			return emitter.Response{
				Kind:        responseKind,
				Status:      &status,
				Explicit:    boolPtr(explicit),
				ContentType: "application/json",
				Headers:     responseHeaders,
				BodySchema:  &schema,
				Source:      "direct_return",
				Via:         "return",
			}, true
		}
	}

	return emitter.Response{}, false
}

func (e *extractor) responseFromJSONCall(ctx context.Context, currentFQCN string, args []string, meta *phpFileMetadata, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	if len(args) == 0 {
		return emitter.Response{}, false
	}

	bodyExpr := resolveExpressionValue(args[0], locals, 0)
	status := 200
	explicit := false
	if len(args) > 1 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[1], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	if status == 204 && isEmptyPayloadExpression(bodyExpr) {
		return emitter.Response{
			Kind:        "no_content",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "",
			Headers:     responseHeaders,
			Source:      source,
			Via:         source,
		}, true
	}

	schema, ok := e.responseSchemaFromBody(ctx, currentFQCN, bodyExpr, meta, locals)
	if !ok {
		return emitter.Response{}, false
	}

	responseKind, ok := responseKindFromSchema(schema)
	if !ok {
		return emitter.Response{}, false
	}

	return emitter.Response{
		Kind:        responseKind,
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: "application/json",
		Headers:     responseHeaders,
		BodySchema:  &schema,
		Source:      source,
		Via:         source,
	}, true
}

func responseFromRedirectCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	status := 302
	explicit := false
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}

	if len(args) > 0 {
		location := describeExpressionValue(args[0], locals)
		if location != "" {
			headers["Location"] = location
		}
	}
	if len(args) > 1 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[1], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if len(args) > 2 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[2], locals, 0)))
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:     "redirect",
		Status:   &status,
		Explicit: boolPtr(explicit),
		Headers:  headers,
		Redirect: &emitter.RedirectInfo{
			TargetKind: "url",
			Target:     optionalStringPtr(headerValue(headers, "Location")),
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromRedirectRouteCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 0 {
		target := strings.TrimSpace(unquotePHPString(resolveExpressionValue(args[0], locals, 0)))
		if target != "" {
			headers["Location"] = "route:" + target
		}
	}

	status := 302
	explicit := false
	if len(args) > 2 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[2], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if len(args) > 3 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[3], locals, 0)))
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:     "redirect",
		Status:   &status,
		Explicit: boolPtr(explicit),
		Headers:  headers,
		Redirect: &emitter.RedirectInfo{
			TargetKind: "route",
			Target:     optionalStringPtr(strings.TrimPrefix(headerValue(headers, "Location"), "route:")),
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromBackCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	status := 302
	explicit := false
	headers := mergeResponseHeaders(responseHeaders, map[string]string{
		"Location": "back",
	})
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 0 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[0], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if len(args) > 1 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[1], locals, 0)))
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:     "redirect",
		Status:   &status,
		Explicit: boolPtr(explicit),
		Headers:  headers,
		Redirect: &emitter.RedirectInfo{
			TargetKind: "back",
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromDownloadCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 2 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[2], locals, 0)))
	}
	if len(args) > 1 {
		filename := unquotePHPString(resolveExpressionValue(args[1], locals, 0))
		if filename != "" {
			disposition := "attachment"
			if len(args) > 3 {
				if value := unquotePHPString(resolveExpressionValue(args[3], locals, 0)); value != "" {
					disposition = value
				}
			}
			headers["Content-Disposition"] = disposition + `; filename="` + filename + `"`
		}
	}
	contentType := headerValue(headers, "Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	status := 200
	explicit := false
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:        "download",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: contentType,
		Headers:     headers,
		Download: &emitter.DownloadInfo{
			Disposition: downloadDisposition(headers, "attachment"),
			Filename:    downloadFilenamePtr(headers),
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromStreamDownloadCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 2 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[2], locals, 0)))
	}
	if len(args) > 1 {
		filename := unquotePHPString(resolveExpressionValue(args[1], locals, 0))
		if filename != "" {
			disposition := "attachment"
			if len(args) > 3 {
				if value := unquotePHPString(resolveExpressionValue(args[3], locals, 0)); value != "" {
					disposition = value
				}
			}
			headers["Content-Disposition"] = disposition + `; filename="` + filename + `"`
		}
	}
	contentType := headerValue(headers, "Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	status := 200
	explicit := false
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:        "download",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: contentType,
		Headers:     headers,
		Download: &emitter.DownloadInfo{
			Disposition: downloadDisposition(headers, "attachment"),
			Filename:    downloadFilenamePtr(headers),
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromFileCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 1 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[1], locals, 0)))
	}
	contentType := headerValue(headers, "Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	status := 200
	explicit := false
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	return emitter.Response{
		Kind:        "download",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: contentType,
		Headers:     headers,
		Download: &emitter.DownloadInfo{
			Disposition: downloadDisposition(headers, "inline"),
			Filename:    downloadFilenamePtr(headers),
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromStreamCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	status := 200
	explicit := false
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 1 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[1], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if len(args) > 2 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[2], locals, 0)))
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}
	contentType := headerValue(headers, "Content-Type")

	return emitter.Response{
		Kind:        "stream",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: contentType,
		Headers:     headers,
		Source:      source,
		Via:         source,
	}, true
}

func (e *extractor) responseFromStreamJSONCall(ctx context.Context, currentFQCN string, args []string, meta *phpFileMetadata, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	status := 200
	explicit := false
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}
	if len(args) > 2 {
		headers = mergeResponseHeaders(headers, parseHeaderMap(resolveExpressionValue(args[2], locals, 0)))
	}
	if len(args) > 1 {
		if parsed, ok := parseHTTPStatusExpression(resolveExpressionValue(args[1], locals, 0)); ok {
			status = parsed
			explicit = true
		}
	}
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}
	contentType := headerValue(headers, "Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}

	var bodySchema *emitter.ResourceSchemaNode
	if len(args) > 0 {
		if schema, ok := e.responseSchemaFromBody(ctx, currentFQCN, resolveExpressionValue(args[0], locals, 0), meta, locals); ok {
			bodySchema = &schema
		}
	}

	return emitter.Response{
		Kind:        "stream",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: contentType,
		Headers:     headers,
		BodySchema:  bodySchema,
		Source:      source,
		Via:         source,
	}, true
}

func (e *extractor) responseFromInertiaRenderCall(ctx context.Context, currentFQCN string, args []string, meta *phpFileMetadata, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	if len(args) == 0 {
		return emitter.Response{}, false
	}

	component := unquotePHPString(resolveExpressionValue(args[0], locals, 0))
	if component == "" {
		component = describeExpressionValue(args[0], locals)
	}
	if component == "" {
		return emitter.Response{}, false
	}

	status := 200
	explicit := false
	if statusOverride != nil {
		status = *statusOverride
		explicit = true
	}

	var propsSchema *emitter.ResourceSchemaNode
	if len(args) > 1 {
		if schema, ok := e.responseSchemaFromBody(ctx, currentFQCN, resolveExpressionValue(args[1], locals, 0), meta, locals); ok {
			propsSchema = &schema
		}
	}

	return emitter.Response{
		Kind:        "inertia",
		Status:      &status,
		Explicit:    boolPtr(explicit),
		ContentType: "text/html",
		Headers:     responseHeaders,
		Inertia: &emitter.InertiaInfo{
			Component:   component,
			PropsSchema: propsSchema,
		},
		Source: source,
		Via:    source,
	}, true
}

func responseFromInertiaLocationCall(args []string, source string, statusOverride *int, responseHeaders map[string]string, locals map[string]string) (emitter.Response, bool) {
	headers := mergeResponseHeaders(responseHeaders, nil)
	if headers == nil {
		headers = map[string]string{}
	}

	target := ""
	if len(args) > 0 {
		target = describeExpressionValue(args[0], locals)
		if target != "" {
			headers["X-Inertia-Location"] = target
		}
	}

	status := 409
	explicit := true
	if statusOverride != nil {
		status = *statusOverride
	}

	return emitter.Response{
		Kind:     "redirect",
		Status:   &status,
		Explicit: boolPtr(explicit),
		Headers:  headers,
		Redirect: &emitter.RedirectInfo{
			TargetKind: "inertia_location",
			Target:     optionalStringPtr(target),
		},
		Source: source,
		Via:    source,
	}, true
}

func (e *extractor) responseSchemaFromBody(ctx context.Context, currentFQCN, expression string, meta *phpFileMetadata, locals map[string]string) (emitter.ResourceSchemaNode, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return emitter.ResourceSchemaNode{}, false
	}

	if base, args, ok := splitTrailingMethodCall(expression, "additional"); ok {
		baseSchema, ok := e.responseSchemaFromBody(ctx, currentFQCN, resolveExpressionValue(base, locals, 0), meta, locals)
		if !ok {
			return emitter.ResourceSchemaNode{}, false
		}
		if len(args) == 0 {
			return baseSchema, true
		}

		additionalSchema := e.schemaFromExpression(ctx, currentFQCN, resolveExpressionValue(args[0], locals, 0), meta, 0, locals)
		return e.mergeAdditionalSchema(ctx, baseSchema, additionalSchema), true
	}

	if strings.HasPrefix(expression, "[") {
		schema := e.schemaFromExpression(ctx, currentFQCN, expression, meta, 0, locals)
		if schema.Type != "" || schema.Ref != "" || len(schema.Properties) > 0 || schema.Items != nil {
			return schema, true
		}
		return emitter.ResourceSchemaNode{}, false
	}

	if helper, args, ok := parseResponseHelperCall(expression, currentFQCN, meta); ok && helper == "json" && len(args) > 0 {
		return e.responseSchemaFromBody(ctx, currentFQCN, resolveExpressionValue(args[0], locals, 0), meta, locals)
	}

	if resourceFQCN, collection, ok := detectResourceReference(expression, currentFQCN, meta); ok {
		if collection {
			if resourceFQCN != "" {
				e.buildDefinition(ctx, resourceFQCN, 0)
			}
			return emitter.ResourceSchemaNode{
				Type: "object",
				Properties: map[string]emitter.ResourceSchemaNode{
					"data": {
						Type: "array",
						Items: &emitter.ResourceSchemaNode{
							Ref: resourceFQCN,
						},
					},
				},
				Required: []string{"data"},
			}, true
		}

		if resourceFQCN != "" {
			e.buildDefinition(ctx, resourceFQCN, 0)
		}
		return emitter.ResourceSchemaNode{Ref: resourceFQCN}, true
	}

	if schemaType, format := inferScalarSchemaType("", expression); schemaType != "" && schemaType != "string" {
		return emitter.ResourceSchemaNode{Type: schemaType, Format: format}, true
	}

	return emitter.ResourceSchemaNode{}, false
}

func (e *extractor) mergeAdditionalSchema(ctx context.Context, baseSchema, additionalSchema emitter.ResourceSchemaNode) emitter.ResourceSchemaNode {
	expandedBase := e.expandSchemaReference(ctx, baseSchema)
	expandedAdditional := e.expandSchemaReference(ctx, additionalSchema)

	if expandedAdditional.Type != "object" {
		return expandedBase
	}

	if expandedBase.Type == "array" {
		properties := map[string]emitter.ResourceSchemaNode{
			"data": expandedBase,
		}
		required := []string{"data"}
		for key, property := range expandedAdditional.Properties {
			properties[key] = property
		}
		required = append(required, expandedAdditional.Required...)
		return emitter.ResourceSchemaNode{
			Type:       "object",
			Properties: properties,
			Required:   stableUniqueStrings(required),
		}
	}

	if expandedBase.Type != "object" {
		return expandedBase
	}

	properties := make(map[string]emitter.ResourceSchemaNode, len(expandedBase.Properties)+len(expandedAdditional.Properties))
	for key, property := range expandedBase.Properties {
		properties[key] = property
	}
	for key, property := range expandedAdditional.Properties {
		properties[key] = property
	}

	required := append([]string{}, expandedBase.Required...)
	required = append(required, expandedAdditional.Required...)

	return emitter.ResourceSchemaNode{
		Type:       "object",
		Properties: properties,
		Required:   stableUniqueStrings(required),
	}
}

func (e *extractor) expandSchemaReference(ctx context.Context, schema emitter.ResourceSchemaNode) emitter.ResourceSchemaNode {
	if schema.Ref == "" {
		return schema
	}

	if definition, ok := e.definitions[schema.Ref]; ok {
		return definition.Schema
	}
	if definition, ok := e.buildDefinition(ctx, schema.Ref, 0); ok {
		return definition.Schema
	}

	return schema
}

func responseKindFromSchema(schema emitter.ResourceSchemaNode) (string, bool) {
	switch schema.Type {
	case "array":
		return "json_array", true
	case "object":
		return "json_object", true
	default:
		if schema.Ref != "" {
			return "json_object", true
		}
		return "", false
	}
}

func (e *extractor) frameworkResponses(ctx context.Context, currentFQCN string, methodCtx *controllerMethodContext, locals map[string]string, authorization []emitter.AuthorizationHint) []emitter.Response {
	if methodCtx == nil {
		return nil
	}

	responses := make([]emitter.Response, 0, 8)
	responses = append(responses, extractAbortResponses(methodCtx.Body, locals)...)
	responses = append(responses, extractConditionalAbortResponses(methodCtx.Body, locals)...)
	responses = append(responses, e.extractFrameworkExceptionResponses(ctx, currentFQCN, methodCtx.Body, methodCtx.Meta, locals)...)
	responses = append(responses, e.extractConditionalThrowResponses(currentFQCN, methodCtx.Meta, methodCtx.Body, locals)...)
	responses = append(responses, extractFindOrFailResponses(methodCtx.Body)...)
	responses = append(responses, e.formRequestValidationResponses(ctx, methodCtx.Params, authorization)...)
	return responses
}

func extractAbortResponses(body string, locals map[string]string) []emitter.Response {
	return extractAbortFunctionResponses(body, locals, "abort", 0)
}

func extractConditionalAbortResponses(body string, locals map[string]string) []emitter.Response {
	responses := make([]emitter.Response, 0, 4)
	responses = append(responses, extractAbortFunctionResponses(body, locals, "abort_if", 1)...)
	responses = append(responses, extractAbortFunctionResponses(body, locals, "abort_unless", 1)...)
	return responses
}

func extractAbortFunctionResponses(body string, locals map[string]string, name string, statusArgIndex int) []emitter.Response {
	callArgs := findFunctionCallArguments(body, name)
	if len(callArgs) == 0 || statusArgIndex < 0 {
		return nil
	}

	responses := make([]emitter.Response, 0, len(callArgs))
	for _, args := range callArgs {
		if len(args) <= statusArgIndex {
			continue
		}

		status, ok := parseHTTPStatusExpression(resolveExpressionValue(args[statusArgIndex], locals, 0))
		if !ok {
			continue
		}

		responses = append(responses, emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      name + "()",
			Via:         name + "()",
		})
	}

	return responses
}

func (e *extractor) extractFrameworkExceptionResponses(ctx context.Context, currentFQCN, body string, meta *phpFileMetadata, locals map[string]string) []emitter.Response {
	responses := make([]emitter.Response, 0, 3)

	for _, call := range findStaticMethodCalls(body, "withMessages") {
		fqcn := resolveClassReferenceFQCN(call.Reference, currentFQCN, meta)
		if shortTypeName(fqcn) != "ValidationException" {
			continue
		}

		status := 422
		schema := validationErrorSchema(emitter.ResourceSchemaNode{Type: "object"})
		if len(call.Args) > 0 {
			if errorsSchema, ok := e.schemaFromValidationMessages(ctx, currentFQCN, resolveExpressionValue(call.Args[0], locals, 0), meta, locals); ok {
				schema = validationErrorSchema(errorsSchema)
			}
		}

		responses = append(responses, emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  &schema,
			Source:      "ValidationException::withMessages",
			Via:         "throw",
		})
	}

	for _, thrown := range findThrownConstructors(body) {
		fqcn := resolveClassReferenceFQCN(thrown.Class, currentFQCN, meta)
		switch shortTypeName(fqcn) {
		case "ValidationException":
			status := 422
			responses = append(responses, emitter.Response{
				Kind:        "json_object",
				Status:      &status,
				Explicit:    boolPtr(true),
				ContentType: "application/json",
				BodySchema:  genericErrorSchema(status),
				Source:      "throw new ValidationException",
				Via:         "throw",
			})
		case "AuthorizationException":
			status := 403
			responses = append(responses, emitter.Response{
				Kind:        "json_object",
				Status:      &status,
				Explicit:    boolPtr(true),
				ContentType: "application/json",
				BodySchema:  genericErrorSchema(status),
				Source:      "throw new AuthorizationException",
				Via:         "throw",
			})
		case "ModelNotFoundException":
			status := 404
			responses = append(responses, emitter.Response{
				Kind:        "json_object",
				Status:      &status,
				Explicit:    boolPtr(true),
				ContentType: "application/json",
				BodySchema:  genericErrorSchema(status),
				Source:      "throw new ModelNotFoundException",
				Via:         "throw",
			})
		}
	}

	return responses
}

func (e *extractor) extractConditionalThrowResponses(currentFQCN string, meta *phpFileMetadata, body string, locals map[string]string) []emitter.Response {
	responses := make([]emitter.Response, 0, 4)
	responses = append(responses, extractThrowFunctionResponses(currentFQCN, meta, body, locals, "throw_if")...)
	responses = append(responses, extractThrowFunctionResponses(currentFQCN, meta, body, locals, "throw_unless")...)
	return responses
}

func extractThrowFunctionResponses(currentFQCN string, meta *phpFileMetadata, body string, locals map[string]string, name string) []emitter.Response {
	callArgs := findFunctionCallArguments(body, name)
	if len(callArgs) == 0 {
		return nil
	}

	responses := make([]emitter.Response, 0, len(callArgs))
	for _, args := range callArgs {
		if len(args) < 2 {
			continue
		}

		fqcn := resolveExceptionClassArgument(currentFQCN, meta, resolveExpressionValue(args[1], locals, 0))
		response, ok := responseForFrameworkExceptionFQCN(fqcn, name+"()", "throw")
		if !ok {
			continue
		}
		responses = append(responses, response)
	}
	return responses
}

func extractFindOrFailResponses(body string) []emitter.Response {
	status := 404
	sources := []string{"findOrFail()", "firstOrFail()"}
	responses := make([]emitter.Response, 0, len(sources))
	for _, source := range sources {
		if !strings.Contains(body, "->"+strings.TrimSuffix(source, "()")+"(") {
			continue
		}
		responses = append(responses, emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      source,
			Via:         source,
		})
	}
	return responses
}

func resolveExceptionClassArgument(currentFQCN string, meta *phpFileMetadata, expression string) string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ""
	}

	if strings.HasPrefix(expression, "new ") {
		expression = strings.TrimSpace(strings.TrimPrefix(expression, "new "))
		if openIdx := strings.Index(expression, "("); openIdx != -1 {
			expression = strings.TrimSpace(expression[:openIdx])
		}
		return resolveClassReferenceFQCN(expression, currentFQCN, meta)
	}

	if strings.HasSuffix(expression, "::class") {
		return resolveClassReferenceFQCN(strings.TrimSuffix(expression, "::class"), currentFQCN, meta)
	}

	return resolveClassReferenceFQCN(expression, currentFQCN, meta)
}

func responseForFrameworkExceptionFQCN(fqcn, source, via string) (emitter.Response, bool) {
	switch shortTypeName(fqcn) {
	case "ValidationException":
		status := 422
		return emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      source,
			Via:         via,
		}, true
	case "AuthorizationException", "AuthenticationException":
		status := 403
		if shortTypeName(fqcn) == "AuthenticationException" {
			status = 401
		}
		return emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      source,
			Via:         via,
		}, true
	case "ModelNotFoundException":
		status := 404
		return emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      source,
			Via:         via,
		}, true
	default:
		return emitter.Response{}, false
	}
}

func (e *extractor) schemaFromValidationMessages(ctx context.Context, currentFQCN, expression string, meta *phpFileMetadata, locals map[string]string) (emitter.ResourceSchemaNode, bool) {
	schema := e.schemaFromExpression(ctx, currentFQCN, expression, meta, 0, locals)
	if schema.Type == "" && len(schema.Properties) == 0 {
		return emitter.ResourceSchemaNode{}, false
	}
	if schema.Type == "" {
		schema.Type = "object"
	}
	if schema.Type != "object" {
		return emitter.ResourceSchemaNode{}, false
	}

	properties := make(map[string]emitter.ResourceSchemaNode, len(schema.Properties))
	required := make([]string, 0, len(schema.Properties))
	for key, property := range schema.Properties {
		if property.Type != "array" {
			property = emitter.ResourceSchemaNode{
				Type: "array",
				Items: &emitter.ResourceSchemaNode{
					Type: "string",
				},
			}
		} else if property.Items == nil {
			property.Items = &emitter.ResourceSchemaNode{Type: "string"}
		}
		properties[key] = property
		required = append(required, key)
	}

	return emitter.ResourceSchemaNode{
		Type:       "object",
		Properties: properties,
		Required:   stableUniqueStrings(required),
	}, true
}

func validationErrorSchema(errorsSchema emitter.ResourceSchemaNode) emitter.ResourceSchemaNode {
	return emitter.ResourceSchemaNode{
		Type: "object",
		Properties: map[string]emitter.ResourceSchemaNode{
			"message": {Type: "string"},
			"errors":  errorsSchema,
		},
		Required: []string{"message", "errors"},
	}
}

func genericErrorSchema(status int) *emitter.ResourceSchemaNode {
	schema := emitter.ResourceSchemaNode{
		Type: "object",
		Properties: map[string]emitter.ResourceSchemaNode{
			"message": {Type: "string"},
		},
		Required: []string{"message"},
	}
	if status == 422 {
		schema.Properties["errors"] = emitter.ResourceSchemaNode{
			Type: "object",
		}
		schema.Required = []string{"message", "errors"}
	}
	return &schema
}

func parseResponseHelperCall(expression, currentFQCN string, meta *phpFileMetadata) (string, []string, bool) {
	expression = strings.TrimSpace(expression)
	expression = stripTrailingResponseChains(expression)
	if helper, args, ok := parseStaticResponseHelperCall(expression, currentFQCN, meta); ok {
		return helper, args, true
	}
	switch {
	case strings.HasPrefix(expression, "response()->noContent("):
		return "noContent", parseCallArguments(expression, len("response()->noContent")), true
	case strings.HasPrefix(expression, "response()->noContent"):
		return "noContent", nil, true
	case strings.HasPrefix(expression, "response()->json("):
		return "json", parseCallArguments(expression, len("response()->json")), true
	case strings.HasPrefix(expression, "response()->download("):
		return "download", parseCallArguments(expression, len("response()->download")), true
	case strings.HasPrefix(expression, "response()->streamDownload("):
		return "streamDownload", parseCallArguments(expression, len("response()->streamDownload")), true
	case strings.HasPrefix(expression, "response()->streamJson("):
		return "streamJson", parseCallArguments(expression, len("response()->streamJson")), true
	case strings.HasPrefix(expression, "response()->stream("):
		return "stream", parseCallArguments(expression, len("response()->stream")), true
	case strings.HasPrefix(expression, "response()->file("):
		return "file", parseCallArguments(expression, len("response()->file")), true
	case strings.HasPrefix(expression, "response("):
		return "response", parseCallArguments(expression, len("response")), true
	case strings.HasPrefix(expression, "redirect()->route("):
		return "redirectRoute", parseCallArguments(expression, len("redirect()->route")), true
	case strings.HasPrefix(expression, "redirect()->away("):
		return "redirectAway", parseCallArguments(expression, len("redirect()->away")), true
	case strings.HasPrefix(expression, "redirect()->to("):
		return "redirectTo", parseCallArguments(expression, len("redirect()->to")), true
	case strings.HasPrefix(expression, "redirect("):
		return "redirect", parseCallArguments(expression, len("redirect")), true
	case strings.HasPrefix(expression, "back("):
		return "back", parseCallArguments(expression, len("back")), true
	case expression == "back()":
		return "back", nil, true
	case strings.HasPrefix(expression, "inertia("):
		return "inertiaHelper", parseCallArguments(expression, len("inertia")), true
	default:
		return "", nil, false
	}
}

func parseStaticResponseHelperCall(expression, currentFQCN string, meta *phpFileMetadata) (string, []string, bool) {
	staticCallRe := regexp.MustCompile(`^([A-Za-z_\\][A-Za-z0-9_\\]*)::(render|location)\s*\(`)
	match := staticCallRe.FindStringSubmatch(expression)
	if len(match) != 3 {
		return "", nil, false
	}

	fqcn := resolveClassReferenceFQCN(match[1], currentFQCN, meta)
	if fqcn != `Inertia\Inertia` && fqcn != "" && shortTypeName(fqcn) != "Inertia" {
		return "", nil, false
	}
	if fqcn == "" && shortTypeName(strings.TrimPrefix(match[1], `\`)) != "Inertia" {
		return "", nil, false
	}

	switch match[2] {
	case "render":
		return "inertiaRender", parseCallArguments(expression, len(match[1]+"::render")), true
	case "location":
		return "inertiaLocation", parseCallArguments(expression, len(match[1]+"::location")), true
	default:
		return "", nil, false
	}
}

func stripTrailingResponseChains(expression string) string {
	expression = strings.TrimSpace(expression)
	for {
		updated := expression
		for _, method := range []string{"setStatusCode", "header", "withHeaders", "cookie", "withCookie"} {
			if base, _, ok := splitTrailingMethodCall(updated, method); ok {
				updated = base
				break
			}
		}
		if updated == expression {
			return expression
		}
		expression = strings.TrimSpace(updated)
	}
}

func parseCallArguments(expression string, prefixLen int) []string {
	if prefixLen >= len(expression) {
		return nil
	}
	openIdx := strings.Index(expression[prefixLen:], "(")
	if openIdx == -1 {
		return nil
	}
	openIdx += prefixLen
	closeIdx := findMatchingDelimiter(expression, openIdx, '(', ')')
	if closeIdx == -1 || closeIdx <= openIdx+1 {
		return nil
	}
	body := strings.TrimSpace(expression[openIdx+1 : closeIdx])
	if body == "" {
		return nil
	}
	return splitTopLevel(body, ',')
}

type staticMethodCall struct {
	Reference string
	Method    string
	Args      []string
}

type thrownConstructor struct {
	Class string
	Args  []string
}

func findFunctionCallArguments(source, name string) [][]string {
	if source == "" || name == "" {
		return nil
	}

	var calls [][]string
	for idx := 0; idx < len(source); {
		next := strings.Index(source[idx:], name)
		if next == -1 {
			break
		}
		next += idx

		if !boundaryBeforeKeyword(source, next) || !boundaryAfterKeyword(source, next+len(name)) {
			idx = next + len(name)
			continue
		}

		openIdx := next + len(name)
		for openIdx < len(source) && isWhitespaceByte(source[openIdx]) {
			openIdx++
		}
		if openIdx >= len(source) || source[openIdx] != '(' {
			idx = next + len(name)
			continue
		}

		closeIdx := findMatchingDelimiter(source, openIdx, '(', ')')
		if closeIdx == -1 {
			idx = openIdx + 1
			continue
		}

		body := strings.TrimSpace(source[openIdx+1 : closeIdx])
		if body == "" {
			calls = append(calls, nil)
		} else {
			calls = append(calls, splitTopLevel(body, ','))
		}
		idx = closeIdx + 1
	}

	return calls
}

func findStaticMethodCalls(source, method string) []staticMethodCall {
	if source == "" || method == "" {
		return nil
	}

	re := regexp.MustCompile(`([A-Za-z_\\][A-Za-z0-9_\\]*)::` + regexp.QuoteMeta(method) + `\s*\(`)
	matches := re.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return nil
	}

	calls := make([]staticMethodCall, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		reference := source[match[2]:match[3]]
		openIdx := strings.Index(source[match[0]:match[1]], "(")
		if openIdx == -1 {
			continue
		}
		openIdx += match[0]
		closeIdx := findMatchingDelimiter(source, openIdx, '(', ')')
		if closeIdx == -1 {
			continue
		}
		body := strings.TrimSpace(source[openIdx+1 : closeIdx])
		args := []string(nil)
		if body != "" {
			args = splitTopLevel(body, ',')
		}
		calls = append(calls, staticMethodCall{
			Reference: reference,
			Method:    method,
			Args:      args,
		})
	}

	return calls
}

func findThrownConstructors(source string) []thrownConstructor {
	if source == "" {
		return nil
	}

	re := regexp.MustCompile(`throw\s+new\s+([A-Za-z_\\][A-Za-z0-9_\\]*)`)
	matches := re.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return nil
	}

	throws := make([]thrownConstructor, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		className := source[match[2]:match[3]]
		cursor := match[1]
		for cursor < len(source) && isWhitespaceByte(source[cursor]) {
			cursor++
		}
		args := []string(nil)
		if cursor < len(source) && source[cursor] == '(' {
			closeIdx := findMatchingDelimiter(source, cursor, '(', ')')
			if closeIdx == -1 {
				continue
			}
			body := strings.TrimSpace(source[cursor+1 : closeIdx])
			if body != "" {
				args = splitTopLevel(body, ',')
			}
		}
		throws = append(throws, thrownConstructor{
			Class: className,
			Args:  args,
		})
	}

	return throws
}

func parseHTTPStatusExpression(expression string) (int, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return 0, false
	}
	if value, err := strconv.Atoi(expression); err == nil && value >= 100 && value <= 599 {
		return value, true
	}

	if match := regexp.MustCompile(`HTTP_[A-Z0-9_]+`).FindString(expression); match != "" {
		switch match {
		case "HTTP_OK":
			return 200, true
		case "HTTP_CREATED":
			return 201, true
		case "HTTP_ACCEPTED":
			return 202, true
		case "HTTP_NO_CONTENT":
			return 204, true
		case "HTTP_BAD_REQUEST":
			return 400, true
		case "HTTP_UNAUTHORIZED":
			return 401, true
		case "HTTP_FORBIDDEN":
			return 403, true
		case "HTTP_NOT_FOUND":
			return 404, true
		case "HTTP_UNPROCESSABLE_ENTITY":
			return 422, true
		}
	}

	return 0, false
}

func isEmptyPayloadExpression(expression string) bool {
	switch strings.TrimSpace(expression) {
	case "null", "''", `""`:
		return true
	default:
		return false
	}
}

func dedupeResponses(responses []emitter.Response) []emitter.Response {
	if len(responses) == 0 {
		return nil
	}

	type responseKey struct {
		kind        string
		contentType string
		source      string
		via         string
		status      int
		schema      string
		headers     string
		redirect    string
		download    string
		inertia     string
	}

	seen := make(map[responseKey]struct{}, len(responses))
	out := make([]emitter.Response, 0, len(responses))
	for _, response := range responses {
		key := responseKey{
			kind:        response.Kind,
			contentType: response.ContentType,
			source:      response.Source,
			via:         response.Via,
			status:      responseStatusValue(response.Status),
			schema:      responseSchemaSignature(response.BodySchema),
			headers:     responseHeadersSignature(response.Headers),
			redirect:    responseRedirectSignature(response.Redirect),
			download:    responseDownloadSignature(response.Download),
			inertia:     responseInertiaSignature(response.Inertia),
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, response)
	}
	return out
}

func mergeResponseHeaders(left, right map[string]string) map[string]string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}

	merged := make(map[string]string, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}

	return merged
}

func parseHeaderCallArguments(args []string, locals map[string]string) map[string]string {
	if len(args) < 2 {
		return nil
	}

	name := unquotePHPString(resolveExpressionValue(args[0], locals, 0))
	if name == "" {
		return nil
	}

	return map[string]string{
		name: describeExpressionValue(args[1], locals),
	}
}

func parseHeaderMap(expression string) map[string]string {
	expression = strings.TrimSpace(expression)
	if expression == "" || !strings.HasPrefix(expression, "[") {
		return nil
	}

	closeIdx := findMatchingDelimiter(expression, 0, '[', ']')
	if closeIdx == -1 {
		return nil
	}

	body := strings.TrimSpace(expression[1:closeIdx])
	if body == "" {
		return map[string]string{}
	}

	headers := make(map[string]string)
	for _, part := range splitTopLevel(body, ',') {
		key, value, ok := splitArrayEntry(strings.TrimSpace(part))
		if !ok {
			continue
		}

		name := normalizePHPArrayKey(key)
		if name == "" {
			continue
		}
		headers[name] = describeExpressionValue(value, nil)
	}

	if len(headers) == 0 {
		return nil
	}

	return headers
}

func describeExpressionValue(expression string, locals map[string]string) string {
	expression = strings.TrimSpace(resolveExpressionValue(expression, locals, 0))
	if expression == "" {
		return ""
	}

	return unquotePHPString(expression)
}

func optionalStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func unquotePHPString(expression string) string {
	expression = strings.TrimSpace(expression)
	if len(expression) >= 2 {
		if (expression[0] == '\'' && expression[len(expression)-1] == '\'') || (expression[0] == '"' && expression[len(expression)-1] == '"') {
			return expression[1 : len(expression)-1]
		}
	}

	return expression
}

func headerValue(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}

	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}

	return ""
}

func responseStatusValue(status *int) int {
	if status == nil {
		return 0
	}
	return *status
}

func responseHeadersSignature(headers map[string]string) string {
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

func responseRedirectSignature(info *emitter.RedirectInfo) string {
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

func responseDownloadSignature(info *emitter.DownloadInfo) string {
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

func responseInertiaSignature(info *emitter.InertiaInfo) string {
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

func responseSchemaSignature(node *emitter.ResourceSchemaNode) string {
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

func downloadDisposition(headers map[string]string, fallback string) string {
	contentDisposition := strings.TrimSpace(headerValue(headers, "Content-Disposition"))
	if contentDisposition == "" {
		return fallback
	}
	lower := strings.ToLower(contentDisposition)
	if strings.HasPrefix(lower, "inline") {
		return "inline"
	}
	if strings.HasPrefix(lower, "attachment") {
		return "attachment"
	}
	return fallback
}

func downloadFilenamePtr(headers map[string]string) *string {
	contentDisposition := strings.TrimSpace(headerValue(headers, "Content-Disposition"))
	if contentDisposition == "" {
		return nil
	}
	lower := strings.ToLower(contentDisposition)
	idx := strings.Index(lower, "filename=")
	if idx == -1 {
		return nil
	}
	filename := strings.TrimSpace(contentDisposition[idx+len("filename="):])
	filename = strings.Trim(filename, `"`)
	filename = strings.Trim(filename, `'`)
	return optionalStringPtr(filename)
}

func extractReturnExpressions(body string) []string {
	expressions := make([]string, 0, 2)
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(body); i++ {
		ch := body[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		if !strings.HasPrefix(body[i:], "return") || !boundaryBeforeKeyword(body, i) || !boundaryAfterKeyword(body, i+len("return")) {
			continue
		}

		exprStart := i + len("return")
		for exprStart < len(body) && isWhitespaceByte(body[exprStart]) {
			exprStart++
		}
		if exprStart >= len(body) {
			break
		}

		exprDepthParen := 0
		exprDepthBracket := 0
		exprDepthBrace := 0
		exprInSingle := false
		exprInDouble := false
		exprEscaped := false

		for exprEnd := exprStart; exprEnd < len(body); exprEnd++ {
			exprCh := body[exprEnd]
			if exprEscaped {
				exprEscaped = false
				continue
			}
			if exprCh == '\\' && (exprInSingle || exprInDouble) {
				exprEscaped = true
				continue
			}
			if exprCh == '\'' && !exprInDouble {
				exprInSingle = !exprInSingle
				continue
			}
			if exprCh == '"' && !exprInSingle {
				exprInDouble = !exprInDouble
				continue
			}
			if exprInSingle || exprInDouble {
				continue
			}

			switch exprCh {
			case '(':
				exprDepthParen++
			case ')':
				exprDepthParen--
			case '[':
				exprDepthBracket++
			case ']':
				exprDepthBracket--
			case '{':
				exprDepthBrace++
			case '}':
				exprDepthBrace--
			case ';':
				if exprDepthParen == 0 && exprDepthBracket == 0 && exprDepthBrace == 0 {
					expressions = append(expressions, strings.TrimSpace(body[exprStart:exprEnd]))
					i = exprEnd
					goto nextReturnScan
				}
			}
		}

	nextReturnScan:
		continue
	}

	if len(expressions) == 0 {
		return nil
	}
	return expressions
}

func collectLocalAssignments(body string) map[string]string {
	statements := extractTopLevelStatements(body)
	if len(statements) == 0 {
		return nil
	}

	locals := make(map[string]string)
	for _, statement := range statements {
		if name, expression, ok := splitAssignmentStatement(statement); ok {
			locals[name] = expression
		}
	}

	if len(locals) == 0 {
		return nil
	}

	return locals
}

func extractTopLevelStatements(body string) []string {
	statements := make([]string, 0, 8)
	inSingle := false
	inDouble := false
	escaped := false
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	statementStart := 0

	for i := 0; i < len(body); i++ {
		ch := body[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		switch ch {
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case ';':
			if depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
				statement := strings.TrimSpace(body[statementStart:i])
				if statement != "" {
					statements = append(statements, statement)
				}
				statementStart = i + 1
			}
		}
	}

	if tail := strings.TrimSpace(body[statementStart:]); tail != "" {
		statements = append(statements, tail)
	}

	return statements
}

func splitAssignmentStatement(statement string) (string, string, bool) {
	statement = strings.TrimSpace(statement)
	if statement == "" || strings.HasPrefix(statement, "return ") {
		return "", "", false
	}

	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(statement)-1; i++ {
		ch := statement[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		switch ch {
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		}

		if statement[i] == '=' && statement[i+1] == '=' {
			continue
		}
		if statement[i] == '=' && statement[i+1] == '>' {
			continue
		}
		if statement[i] == '=' && depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
			left := strings.TrimSpace(statement[:i])
			right := strings.TrimSpace(statement[i+1:])
			if strings.HasPrefix(left, "$") && right != "" {
				return strings.TrimPrefix(left, "$"), right, true
			}
		}
	}

	return "", "", false
}

func resolveResponseExpression(expression string, locals map[string]string, depth int) (string, *int, map[string]string, bool) {
	if depth > 8 {
		return strings.TrimSpace(expression), nil, nil, false
	}

	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", nil, nil, false
	}

	expression = stripOuterParentheses(expression)

	if base, status, headers, ok := splitTrailingResponseChain(expression, locals); ok {
		resolvedBase, inheritedStatus, inheritedHeaders, resolved := resolveResponseExpression(base, locals, depth+1)
		mergedHeaders := mergeResponseHeaders(inheritedHeaders, headers)
		if !resolved {
			return resolvedBase, status, mergedHeaders, true
		}
		if status != nil {
			return resolvedBase, status, mergedHeaders, true
		}
		return resolvedBase, inheritedStatus, mergedHeaders, true
	}

	if locals != nil && strings.HasPrefix(expression, "$") {
		name := strings.TrimPrefix(expression, "$")
		if resolved, ok := locals[name]; ok {
			return resolveResponseExpression(resolved, locals, depth+1)
		}
	}

	return expression, nil, nil, true
}

func resolveExpressionValue(expression string, locals map[string]string, depth int) string {
	resolved, _, _, ok := resolveResponseExpression(expression, locals, depth)
	if !ok {
		return strings.TrimSpace(expression)
	}

	return resolved
}

func splitTrailingResponseChain(expression string, locals map[string]string) (string, *int, map[string]string, bool) {
	if base, args, ok := splitTrailingMethodCall(expression, "setStatusCode"); ok {
		if len(args) == 0 {
			return "", nil, nil, false
		}
		status, parsed := parseHTTPStatusExpression(resolveExpressionValue(args[0], locals, 0))
		if !parsed {
			return "", nil, nil, false
		}
		return base, &status, nil, true
	}

	if base, args, ok := splitTrailingMethodCall(expression, "header"); ok {
		headers := parseHeaderCallArguments(args, locals)
		if headers == nil {
			return "", nil, nil, false
		}
		return base, nil, headers, true
	}

	if base, args, ok := splitTrailingMethodCall(expression, "withHeaders"); ok {
		if len(args) == 0 {
			return "", nil, nil, false
		}
		return base, nil, parseHeaderMap(resolveExpressionValue(args[0], locals, 0)), true
	}

	for _, method := range []string{"cookie", "withCookie"} {
		if base, _, ok := splitTrailingMethodCall(expression, method); ok {
			return base, nil, nil, true
		}
	}

	return "", nil, nil, false
}

func splitTrailingMethodCall(expression, method string) (string, []string, bool) {
	expression = strings.TrimSpace(expression)
	idx := strings.LastIndex(expression, "->"+method+"(")
	if idx == -1 {
		return "", nil, false
	}

	openIdx := idx + len("->"+method)
	closeIdx := findMatchingDelimiter(expression, openIdx, '(', ')')
	if closeIdx == -1 {
		return "", nil, false
	}

	tail := strings.TrimSpace(expression[closeIdx+1:])
	if tail != "" {
		return "", nil, false
	}

	body := strings.TrimSpace(expression[openIdx+1 : closeIdx])
	if body == "" {
		return strings.TrimSpace(expression[:idx]), nil, true
	}

	return strings.TrimSpace(expression[:idx]), splitTopLevel(body, ','), true
}

func stripOuterParentheses(expression string) string {
	for {
		expression = strings.TrimSpace(expression)
		if len(expression) < 2 || expression[0] != '(' || expression[len(expression)-1] != ')' {
			return expression
		}
		closeIdx := findMatchingDelimiter(expression, 0, '(', ')')
		if closeIdx != len(expression)-1 {
			return expression
		}
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
}

func (e *extractor) buildDefinition(ctx context.Context, fqcn string, depth int) (emitter.ResourceDef, bool) {
	fqcn = strings.TrimSpace(strings.TrimPrefix(fqcn, `\`))
	if fqcn == "" || depth > maxResourceNestingDepth {
		return emitter.ResourceDef{}, false
	}
	if definition, ok := e.definitions[fqcn]; ok {
		return definition, true
	}
	if _, ok := e.building[fqcn]; ok {
		return emitter.ResourceDef{}, false
	}
	e.building[fqcn] = struct{}{}
	defer delete(e.building, fqcn)

	path, err := e.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return emitter.ResourceDef{}, false
	}
	source, err := e.readFile(path)
	if err != nil {
		return emitter.ResourceDef{}, false
	}
	meta := e.fileMetadata(path, source)
	if !e.isResourceMeta(meta, fqcn) {
		return emitter.ResourceDef{}, false
	}

	_, body, ok := extractMethodSignatureAndBody(source, "toArray")
	if !ok {
		return emitter.ResourceDef{}, false
	}
	expression, ok := extractTopLevelReturnExpression(body)
	if !ok {
		return emitter.ResourceDef{}, false
	}

	schema := e.schemaFromExpression(ctx, fqcn, expression, meta, depth, nil)
	if schema.Type == "" && schema.Ref == "" && len(schema.Properties) == 0 && schema.Items == nil {
		return emitter.ResourceDef{}, false
	}

	definition := emitter.ResourceDef{
		FQCN:   fqcn,
		Class:  shortTypeName(fqcn),
		Schema: schema,
	}
	e.definitions[fqcn] = definition
	return definition, true
}

func (e *extractor) schemaFromExpression(ctx context.Context, currentFQCN, expression string, meta *phpFileMetadata, depth int, locals map[string]string) emitter.ResourceSchemaNode {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return emitter.ResourceSchemaNode{}
	}

	if strings.HasPrefix(expression, "[") {
		return e.arrayLiteralSchema(ctx, currentFQCN, expression, meta, depth, locals)
	}

	if resourceFQCN, collection, ok := detectResourceReference(expression, currentFQCN, meta); ok {
		if resourceFQCN != "" {
			e.buildDefinition(ctx, resourceFQCN, depth+1)
		}
		if collection {
			return emitter.ResourceSchemaNode{
				Type: "array",
				Items: &emitter.ResourceSchemaNode{
					Ref: resourceFQCN,
				},
			}
		}

		node := emitter.ResourceSchemaNode{
			Ref: resourceFQCN,
		}
		if strings.Contains(expression, "whenLoaded(") {
			node.Nullable = boolPtr(true)
		}
		return node
	}

	if strings.Contains(expression, "$this->collection") {
		item := emitter.ResourceSchemaNode{Type: "object"}
		if itemFQCN, ok := e.guessCollectionItemFQCN(ctx, currentFQCN, meta); ok {
			e.buildDefinition(ctx, itemFQCN, depth+1)
			item = emitter.ResourceSchemaNode{Ref: itemFQCN}
		}
		return emitter.ResourceSchemaNode{
			Type:  "array",
			Items: &item,
		}
	}

	schemaType, format := inferScalarSchemaType("", expression)
	node := emitter.ResourceSchemaNode{
		Type: schemaType,
	}
	if format != "" {
		node.Format = format
	}
	if isNullableExpression(expression) {
		node.Nullable = boolPtr(true)
	}
	return node
}

func (e *extractor) arrayLiteralSchema(ctx context.Context, currentFQCN, expression string, meta *phpFileMetadata, depth int, locals map[string]string) emitter.ResourceSchemaNode {
	trimmed := strings.TrimSpace(expression)
	if !strings.HasPrefix(trimmed, "[") {
		return emitter.ResourceSchemaNode{}
	}
	closeIdx := findMatchingDelimiter(trimmed, 0, '[', ']')
	if closeIdx == -1 {
		return emitter.ResourceSchemaNode{}
	}

	body := strings.TrimSpace(trimmed[1:closeIdx])
	if body == "" {
		return emitter.ResourceSchemaNode{Type: "object"}
	}

	parts := splitTopLevel(body, ',')
	properties := make(map[string]emitter.ResourceSchemaNode)
	required := make([]string, 0)
	arrayItems := make([]emitter.ResourceSchemaNode, 0)
	hasKeyedEntries := false

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		key, value, ok := splitArrayEntry(part)
		if !ok {
			arrayItems = append(arrayItems, e.schemaFromExpression(ctx, currentFQCN, part, meta, depth+1, locals))
			continue
		}

		hasKeyedEntries = true
		propertyName := normalizePHPArrayKey(key)
		if propertyName == "" {
			continue
		}
		schema := e.schemaFromExpression(ctx, currentFQCN, value, meta, depth+1, locals)
		if schema.Ref == "" && schema.Items == nil && len(schema.Properties) == 0 && schema.Type != "array" && schema.Type != "object" {
			schemaType, format := inferScalarSchemaType(propertyName, value)
			if schema.Type == "" || schemaType != "string" || format != "" {
				schema.Type = schemaType
			}
			if format != "" {
				schema.Format = format
			}
		}
		if isNullableExpression(value) && schema.Nullable == nil {
			schema.Nullable = boolPtr(true)
		}
		properties[propertyName] = schema
		if !isOptionalExpression(value) {
			required = append(required, propertyName)
		}
	}

	if !hasKeyedEntries {
		itemSchema := emitter.ResourceSchemaNode{Type: "string"}
		for _, candidate := range arrayItems {
			if candidate.Type != "" || candidate.Ref != "" || candidate.Items != nil || len(candidate.Properties) > 0 {
				itemSchema = candidate
				break
			}
		}
		return emitter.ResourceSchemaNode{
			Type:  "array",
			Items: &itemSchema,
		}
	}

	required = stableUniqueStrings(required)
	return emitter.ResourceSchemaNode{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

func (e *extractor) guessCollectionItemFQCN(ctx context.Context, currentFQCN string, meta *phpFileMetadata) (string, bool) {
	className := shortTypeName(currentFQCN)
	if !strings.HasSuffix(className, "Collection") {
		return "", false
	}

	candidate := strings.TrimSuffix(className, "Collection") + "Resource"
	itemFQCN := candidate
	if meta != nil && meta.Namespace != "" {
		itemFQCN = meta.Namespace + `\` + candidate
	}
	if _, err := e.resolver.ResolveClass(ctx, itemFQCN); err != nil {
		return "", false
	}
	return itemFQCN, true
}

func (e *extractor) isResourceMeta(meta *phpFileMetadata, fqcn string) bool {
	if meta == nil {
		return false
	}
	extends := shortTypeName(resolveTypeName(meta.Extends, meta))
	if extends == "JsonResource" || extends == "ResourceCollection" {
		return true
	}
	return strings.Contains(fqcn, `\Resources\`) && (strings.HasSuffix(fqcn, "Resource") || strings.HasSuffix(fqcn, "Collection"))
}

func (e *extractor) readFile(path string) (string, error) {
	if source, ok := e.sourceCache[path]; ok {
		return source, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	source := string(data)
	e.sourceCache[path] = source
	return source, nil
}

func (e *extractor) fileMetadata(path, source string) *phpFileMetadata {
	if meta, ok := e.metadataCache[path]; ok {
		return meta
	}
	meta := parsePHPFileMetadata(source)
	e.metadataCache[path] = meta
	return meta
}

func detectResourceReference(expression, currentFQCN string, meta *phpFileMetadata) (string, bool, bool) {
	resourceCollectionRe := regexp.MustCompile(`([A-Za-z_\\][A-Za-z0-9_\\]*)::collection\s*\(`)
	if match := resourceCollectionRe.FindStringSubmatch(expression); len(match) == 2 {
		return resolveClassReferenceFQCN(match[1], currentFQCN, meta), true, true
	}

	resourceMakeRe := regexp.MustCompile(`([A-Za-z_\\][A-Za-z0-9_\\]*)::make\s*\(`)
	if match := resourceMakeRe.FindStringSubmatch(expression); len(match) == 2 {
		return resolveClassReferenceFQCN(match[1], currentFQCN, meta), false, true
	}

	newResourceRe := regexp.MustCompile(`new\s+([A-Za-z_\\][A-Za-z0-9_\\]*)\s*\(`)
	if match := newResourceRe.FindStringSubmatch(expression); len(match) == 2 {
		return resolveClassReferenceFQCN(match[1], currentFQCN, meta), false, true
	}

	return "", false, false
}

func inferScalarSchemaType(propertyName, expression string) (string, string) {
	name := strings.ToLower(strings.TrimSpace(propertyName))
	expr := strings.TrimSpace(expression)
	lowerExpr := strings.ToLower(expr)

	if expr != "" {
		if (strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")) || (strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`)) {
			unquoted := strings.ToLower(strings.Trim(expr, `'"`))
			if strings.HasPrefix(unquoted, "http://") || strings.HasPrefix(unquoted, "https://") {
				return "string", "uri"
			}
		} else if _, err := strconv.Atoi(expr); err == nil {
			return "integer", ""
		} else if _, err := strconv.ParseFloat(expr, 64); err == nil && (strings.ContainsAny(expr, ".eE") || strings.Contains(lowerExpr, "nan") || strings.Contains(lowerExpr, "inf")) {
			return "number", ""
		}
	}

	switch {
	case strings.Contains(lowerExpr, "count()"),
		strings.Contains(lowerExpr, "->total()"),
		strings.Contains(lowerExpr, "->perpage()"),
		strings.Contains(lowerExpr, "->currentpage()"),
		strings.Contains(lowerExpr, "->lastpage()"),
		name == "id",
		strings.HasSuffix(name, "_id"),
		strings.HasSuffix(name, "_count"),
		strings.HasSuffix(name, "_page"),
		strings.HasSuffix(name, "_quantity"),
		strings.HasPrefix(name, "total"):
		return "integer", ""
	case strings.Contains(lowerExpr, "avg("),
		strings.Contains(lowerExpr, "average"),
		strings.Contains(name, "price"),
		strings.Contains(name, "amount"),
		strings.Contains(name, "rating"):
		return "number", ""
	case lowerExpr == "true",
		lowerExpr == "false",
		strings.HasPrefix(name, "is_"),
		strings.HasPrefix(name, "has_"),
		strings.HasPrefix(name, "can_"),
		strings.HasPrefix(name, "should_"),
		strings.HasPrefix(name, "is"):
		return "boolean", ""
	case strings.HasSuffix(name, "_at"),
		strings.HasSuffix(name, "_date"),
		strings.Contains(lowerExpr, "created_at"),
		strings.Contains(lowerExpr, "updated_at"):
		return "string", "date-time"
	case strings.Contains(lowerExpr, "url("),
		strings.HasSuffix(name, "_url"),
		name == "first" || name == "last" || name == "prev" || name == "next":
		return "string", "uri"
	default:
		return "string", ""
	}
}

func isOptionalExpression(expression string) bool {
	trimmed := strings.TrimSpace(expression)
	return strings.Contains(trimmed, "whenLoaded(") ||
		strings.Contains(trimmed, "->when(") ||
		strings.Contains(trimmed, "$this->when(") ||
		strings.Contains(trimmed, "mergeWhen(")
}

func isNullableExpression(expression string) bool {
	trimmed := strings.TrimSpace(expression)
	return strings.Contains(trimmed, "whenLoaded(") ||
		strings.Contains(trimmed, "previousPageUrl()") ||
		strings.Contains(trimmed, "nextPageUrl()")
}

func splitArrayEntry(source string) (string, string, bool) {
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(source)-1; i++ {
		ch := source[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		switch ch {
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		}

		if source[i] == '=' && source[i+1] == '>' && depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
			return strings.TrimSpace(source[:i]), strings.TrimSpace(source[i+2:]), true
		}
	}

	return "", "", false
}

func normalizePHPArrayKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.TrimSuffix(strings.TrimPrefix(key, "'"), "'")
	key = strings.TrimSuffix(strings.TrimPrefix(key, `"`), `"`)
	key = strings.TrimPrefix(key, "$")
	return strings.TrimSpace(key)
}

func parsePHPFileMetadata(source string) *phpFileMetadata {
	header := source
	classStart := regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+`).FindStringIndex(source)
	if classStart != nil {
		header = source[:classStart[0]]
	}

	meta := &phpFileMetadata{
		Uses: make(map[string]string),
	}

	namespaceRe := regexp.MustCompile(`(?m)^\s*namespace\s+([^;]+);`)
	if match := namespaceRe.FindStringSubmatch(header); len(match) == 2 {
		meta.Namespace = strings.TrimSpace(match[1])
	}

	classNameRe := regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+(\w+)`)
	if match := classNameRe.FindStringSubmatch(source); len(match) == 2 {
		meta.ClassName = strings.TrimSpace(match[1])
	}

	useRe := regexp.MustCompile(`(?m)^\s*use\s+([^;]+);`)
	for _, match := range useRe.FindAllStringSubmatch(header, -1) {
		statement := strings.TrimSpace(match[1])
		if statement == "" || strings.Contains(statement, "{") {
			continue
		}

		fqcn := statement
		alias := ""
		if before, after, ok := strings.Cut(statement, " as "); ok {
			fqcn = strings.TrimSpace(before)
			alias = strings.TrimSpace(after)
		}
		if alias == "" {
			parts := strings.Split(fqcn, `\`)
			alias = parts[len(parts)-1]
		}
		meta.Uses[alias] = strings.TrimPrefix(fqcn, `\`)
	}

	extendsRe := regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?class\s+\w+\s+extends\s+([^\s{]+)`)
	if match := extendsRe.FindStringSubmatch(source); len(match) == 2 {
		meta.Extends = strings.TrimSpace(match[1])
	}

	return meta
}

func extractMethodSignatureAndBody(source, method string) (string, string, bool) {
	search := "function " + method
	idx := strings.Index(source, search)
	if idx == -1 {
		return "", "", false
	}

	openParen := strings.Index(source[idx:], "(")
	if openParen == -1 {
		return "", "", false
	}
	openParen += idx
	closeParen := findMatchingDelimiter(source, openParen, '(', ')')
	if closeParen == -1 {
		return "", "", false
	}

	openBrace := strings.Index(source[closeParen:], "{")
	if openBrace == -1 {
		return "", "", false
	}
	openBrace += closeParen
	closeBrace := findMatchingDelimiter(source, openBrace, '{', '}')
	if closeBrace == -1 {
		return "", "", false
	}

	return source[idx:openBrace], source[openBrace+1 : closeBrace], true
}

func extractTopLevelReturnExpression(body string) (string, bool) {
	expressions := extractReturnExpressions(body)
	if len(expressions) == 0 {
		return "", false
	}
	return expressions[0], true
}

func findMatchingDelimiter(source string, openIdx int, open, close byte) int {
	depth := 0
	inSingle := false
	inDouble := false
	escaped := false

	for i := openIdx; i < len(source); i++ {
		ch := source[i]
		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		if ch == open {
			depth++
			continue
		}
		if ch == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func splitTopLevel(source string, separator rune) []string {
	var parts []string
	var current strings.Builder
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range source {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && (inSingle || inDouble) {
			current.WriteRune(ch)
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
			continue
		}
		if inSingle || inDouble {
			current.WriteRune(ch)
			continue
		}

		switch ch {
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		}

		if ch == separator && depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}

		current.WriteRune(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func resolveClassReferenceFQCN(reference, currentFQCN string, meta *phpFileMetadata) string {
	switch reference {
	case "self", "static":
		return currentFQCN
	case "parent":
		if meta == nil || meta.Extends == "" {
			return ""
		}
		return resolveTypeName(meta.Extends, meta)
	default:
		return resolveTypeName(reference, meta)
	}
}

func resolveTypeName(typeName string, meta *phpFileMetadata) string {
	typeName = strings.TrimSpace(typeName)
	typeName = strings.TrimPrefix(typeName, `\`)
	typeName = strings.TrimPrefix(typeName, "?")
	if typeName == "" {
		return ""
	}

	switch strings.ToLower(typeName) {
	case "string", "int", "float", "bool", "array", "mixed", "callable", "iterable", "object", "self", "parent", "static", "null", "false", "true":
		return ""
	}

	if meta != nil {
		if fqcn, ok := meta.Uses[typeName]; ok {
			return fqcn
		}
		if strings.Contains(typeName, `\`) {
			return typeName
		}
		if meta.Namespace != "" {
			return meta.Namespace + `\` + typeName
		}
	}

	return typeName
}

func shortTypeName(typeName string) string {
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), `\`)
	if typeName == "" {
		return ""
	}
	parts := strings.Split(typeName, `\`)
	return strings.TrimSpace(parts[len(parts)-1])
}

func stableUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func boundaryBeforeKeyword(source string, idx int) bool {
	if idx == 0 {
		return true
	}
	return !isIdentifierByte(source[idx-1])
}

func boundaryAfterKeyword(source string, idx int) bool {
	if idx >= len(source) {
		return true
	}
	return !isIdentifierByte(source[idx])
}

func isIdentifierByte(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func isWhitespaceByte(ch byte) bool {
	switch ch {
	case ' ', '\n', '\r', '\t', '\f', '\v':
		return true
	default:
		return false
	}
}

func boolPtr(value bool) *bool {
	return &value
}

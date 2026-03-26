//go:build goexperiment.jsonv2

package response

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/oxhq/oxinfer/internal/emitter"
)

const maxTypeResolutionDepth = 8

var authorizeResourceAbilityMap = map[string]string{
	"index":   "viewAny",
	"show":    "view",
	"create":  "create",
	"store":   "create",
	"edit":    "update",
	"update":  "update",
	"destroy": "delete",
}

type methodParam struct {
	Name     string
	TypeFQCN string
}

type controllerMethodContext struct {
	Source    string
	Meta      *phpFileMetadata
	Signature string
	Body      string
	Params    []methodParam
}

type instanceMethodCall struct {
	Receiver string
	Method   string
	Args     []string
}

func (e *extractor) controllerMethodContext(ctx context.Context, controllerFQCN, method string) (*controllerMethodContext, bool) {
	controllerFQCN = strings.TrimSpace(strings.TrimPrefix(controllerFQCN, `\`))
	method = strings.TrimSpace(method)
	if controllerFQCN == "" || method == "" {
		return nil, false
	}

	path, err := e.resolver.ResolveClass(ctx, controllerFQCN)
	if err != nil {
		return nil, false
	}
	source, err := e.readFile(path)
	if err != nil {
		return nil, false
	}
	meta := e.fileMetadata(path, source)
	signature, body, ok := extractMethodSignatureAndBody(source, method)
	if !ok {
		return nil, false
	}

	return &controllerMethodContext{
		Source:    source,
		Meta:      meta,
		Signature: signature,
		Body:      body,
		Params:    parseMethodParameters(signature, controllerFQCN, meta),
	}, true
}

func (e *extractor) buildAuthorization(ctx context.Context, controllerFQCN, method string, methodCtx *controllerMethodContext) []emitter.AuthorizationHint {
	if methodCtx == nil {
		return nil
	}

	hints := make([]emitter.AuthorizationHint, 0, 6)
	hints = append(hints, buildDirectAuthorizationHints(controllerFQCN, methodCtx.Meta, methodCtx.Body, methodCtx.Params)...)
	hints = append(hints, buildGateAuthorizationHints(controllerFQCN, methodCtx.Meta, methodCtx.Body, methodCtx.Params)...)
	hints = append(hints, buildAuthorizeResourceHints(controllerFQCN, method, methodCtx.Source, methodCtx.Meta)...)
	hints = append(hints, e.buildFormRequestAuthorizationHints(ctx, methodCtx.Params)...)
	if len(hints) == 0 {
		return nil
	}

	return sortAuthorizationHints(dedupeAuthorizationHints(hints))
}

func buildDirectAuthorizationHints(currentFQCN string, meta *phpFileMetadata, body string, params []methodParam) []emitter.AuthorizationHint {
	calls := findInstanceMethodCalls(body, "$this", "authorize")
	if len(calls) == 0 {
		return nil
	}

	hints := make([]emitter.AuthorizationHint, 0, len(calls))
	for _, call := range calls {
		hint, ok := buildAuthorizationHintFromCall(currentFQCN, meta, params, call.Args, "$this->authorize", "direct", true)
		if ok {
			hints = append(hints, hint)
		}
	}

	return hints
}

func buildGateAuthorizationHints(currentFQCN string, meta *phpFileMetadata, body string, params []methodParam) []emitter.AuthorizationHint {
	var hints []emitter.AuthorizationHint

	for _, method := range []string{"authorize", "allows"} {
		calls := findStaticMethodCalls(body, method)
		for _, call := range calls {
			fqcn := resolveClassReferenceFQCN(call.Reference, currentFQCN, meta)
			if fqcn != `Illuminate\Support\Facades\Gate` && shortTypeName(fqcn) != "Gate" && call.Reference != "Gate" {
				continue
			}

			source := "Gate::" + method
			enforces := method == "authorize"
			hint, ok := buildAuthorizationHintFromCall(currentFQCN, meta, params, call.Args, source, "direct", enforces)
			if ok {
				hints = append(hints, hint)
			}
		}
	}

	return hints
}

func buildAuthorizeResourceHints(currentFQCN, method, source string, meta *phpFileMetadata) []emitter.AuthorizationHint {
	ability, ok := authorizeResourceAbilityMap[strings.TrimSpace(method)]
	if !ok {
		return nil
	}

	calls := findInstanceMethodCalls(source, "$this", "authorizeResource")
	if len(calls) == 0 {
		return nil
	}

	hints := make([]emitter.AuthorizationHint, 0, len(calls))
	for _, call := range calls {
		if len(call.Args) == 0 {
			continue
		}

		target := resolveModelClassArgument(call.Args[0], currentFQCN, meta)
		if target == "" {
			continue
		}

		parameter := defaultRouteParameterForModel(target)
		if len(call.Args) > 1 {
			if explicit := unquotePHPString(strings.TrimSpace(call.Args[1])); explicit != "" && explicit != call.Args[1] {
				parameter = explicit
			}
		}

		only, except := parseAuthorizeResourceOptions(call.Args)
		if len(only) > 0 && !containsString(only, method) {
			continue
		}
		if len(except) > 0 && containsString(except, method) {
			continue
		}

		hint := emitter.AuthorizationHint{
			Kind:                    "authorize_resource",
			Ability:                 optionalStringPtr(ability),
			TargetKind:              optionalStringPtr("model"),
			Target:                  optionalStringPtr(target),
			Parameter:               optionalStringPtr(parameter),
			Source:                  "$this->authorizeResource",
			Resolution:              "resource_map",
			EnforcesFailureResponse: true,
		}
		hints = append(hints, hint)
	}

	return hints
}

func (e *extractor) buildFormRequestAuthorizationHints(ctx context.Context, params []methodParam) []emitter.AuthorizationHint {
	hints := make([]emitter.AuthorizationHint, 0, len(params))
	for _, param := range params {
		if param.TypeFQCN == "" || !e.isFormRequestType(ctx, param.TypeFQCN, 0) {
			continue
		}
		if !e.formRequestOverridesAuthorize(ctx, param.TypeFQCN) {
			continue
		}

		hints = append(hints, emitter.AuthorizationHint{
			Kind:                    "form_request_authorize",
			TargetKind:              optionalStringPtr("form_request"),
			Target:                  optionalStringPtr(param.TypeFQCN),
			Parameter:               optionalStringPtr(param.Name),
			Source:                  "FormRequest::authorize",
			Resolution:              "parameter",
			EnforcesFailureResponse: true,
		})
	}

	return hints
}

func (e *extractor) formRequestValidationResponses(ctx context.Context, params []methodParam, authorization []emitter.AuthorizationHint) []emitter.Response {
	hasFormRequest := false
	for _, param := range params {
		if param.TypeFQCN == "" || !e.isFormRequestType(ctx, param.TypeFQCN, 0) {
			continue
		}
		hasFormRequest = true
		break
	}

	var responses []emitter.Response
	if hasFormRequest {
		status := 422
		responses = append(responses, emitter.Response{
			Kind:        "json_object",
			Status:      &status,
			Explicit:    boolPtr(true),
			ContentType: "application/json",
			BodySchema:  genericErrorSchema(status),
			Source:      "FormRequest::failedValidation",
			Via:         "form_request",
		})
	}

	for _, hint := range authorization {
		if !hint.EnforcesFailureResponse {
			continue
		}
		switch hint.Kind {
		case "authorize", "authorize_resource", "form_request_authorize":
			status := 403
			responses = append(responses, emitter.Response{
				Kind:        "json_object",
				Status:      &status,
				Explicit:    boolPtr(true),
				ContentType: "application/json",
				BodySchema:  genericErrorSchema(status),
				Source:      hint.Source,
				Via:         "authorization",
			})
		}
	}

	return responses
}

func (e *extractor) isFormRequestType(ctx context.Context, fqcn string, depth int) bool {
	if depth > maxTypeResolutionDepth {
		return false
	}
	fqcn = strings.TrimSpace(strings.TrimPrefix(fqcn, `\`))
	if fqcn == "" {
		return false
	}
	if fqcn == `Illuminate\Foundation\Http\FormRequest` {
		return true
	}

	path, err := e.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return false
	}
	source, err := e.readFile(path)
	if err != nil {
		return false
	}
	meta := e.fileMetadata(path, source)
	if meta == nil || meta.Extends == "" {
		return false
	}

	parent := resolveTypeName(meta.Extends, meta)
	if parent == "" {
		return false
	}

	return e.isFormRequestType(ctx, parent, depth+1)
}

func (e *extractor) formRequestOverridesAuthorize(ctx context.Context, fqcn string) bool {
	path, err := e.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return false
	}
	source, err := e.readFile(path)
	if err != nil {
		return false
	}
	_, _, ok := extractMethodSignatureAndBody(source, "authorize")
	return ok
}

func parseMethodParameters(signature, currentFQCN string, meta *phpFileMetadata) []methodParam {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil
	}

	openIdx := strings.Index(signature, "(")
	if openIdx == -1 {
		return nil
	}
	closeIdx := findMatchingDelimiter(signature, openIdx, '(', ')')
	if closeIdx == -1 || closeIdx <= openIdx+1 {
		return nil
	}

	body := strings.TrimSpace(signature[openIdx+1 : closeIdx])
	if body == "" {
		return nil
	}

	parts := splitTopLevel(body, ',')
	params := make([]methodParam, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if cut := strings.Index(part, "="); cut != -1 {
			part = strings.TrimSpace(part[:cut])
		}
		nameMatch := regexp.MustCompile(`\$(\w+)`).FindStringSubmatch(part)
		if len(nameMatch) != 2 {
			continue
		}

		name := strings.TrimSpace(nameMatch[1])
		typePart := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(part[:strings.Index(part, "$"+name)], "&", ""), "...", ""), "readonly ", ""))
		for _, modifier := range []string{"public ", "protected ", "private "} {
			typePart = strings.ReplaceAll(typePart, modifier, "")
		}
		typePart = strings.TrimSpace(typePart)

		params = append(params, methodParam{
			Name:     name,
			TypeFQCN: resolveParameterType(typePart, currentFQCN, meta),
		})
	}

	return params
}

func resolveParameterType(typePart, currentFQCN string, meta *phpFileMetadata) string {
	typePart = strings.TrimSpace(typePart)
	if typePart == "" {
		return ""
	}

	typePart = strings.TrimPrefix(typePart, "?")
	replacer := strings.NewReplacer("&", "|", "(", "", ")", "")
	typePart = replacer.Replace(typePart)
	for _, candidate := range strings.Split(typePart, "|") {
		fqcn := resolveClassReferenceFQCN(strings.TrimSpace(candidate), currentFQCN, meta)
		if fqcn != "" {
			return fqcn
		}
	}

	return ""
}

func buildAuthorizationHintFromCall(currentFQCN string, meta *phpFileMetadata, params []methodParam, args []string, source, resolution string, enforces bool) (emitter.AuthorizationHint, bool) {
	if len(args) == 0 {
		return emitter.AuthorizationHint{}, false
	}

	ability := parseAbilityValue(args[0])
	targetKind, target, parameter, targetResolution := parseAuthorizationTarget(currentFQCN, meta, params, args[1:])
	if targetResolution != "" {
		resolution = targetResolution
	}

	kind := "authorize"
	if source == "Gate::allows" {
		kind = "allows"
	}

	return emitter.AuthorizationHint{
		Kind:                    kind,
		Ability:                 optionalStringPtr(ability),
		TargetKind:              optionalStringPtr(targetKind),
		Target:                  optionalStringPtr(target),
		Parameter:               optionalStringPtr(parameter),
		Source:                  source,
		Resolution:              resolution,
		EnforcesFailureResponse: enforces,
	}, true
}

func parseAbilityValue(expression string) string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ""
	}
	if value := unquotePHPString(expression); value != expression {
		return value
	}
	return describeExpressionValue(expression, nil)
}

func parseAuthorizationTarget(currentFQCN string, meta *phpFileMetadata, params []methodParam, args []string) (string, string, string, string) {
	if len(args) == 0 {
		return "", "", "", ""
	}

	expression := strings.TrimSpace(args[0])
	if expression == "" {
		return "", "", "", ""
	}

	if strings.HasPrefix(expression, "$") {
		name := strings.TrimPrefix(expression, "$")
		for _, param := range params {
			if param.Name != name {
				continue
			}
			target := param.TypeFQCN
			if target == "" {
				target = name
			}
			return "route_parameter", target, name, "parameter"
		}
		return "expression", expression, "", "expression"
	}

	if strings.HasSuffix(expression, "::class") {
		target := resolveClassReferenceFQCN(strings.TrimSuffix(expression, "::class"), currentFQCN, meta)
		if target != "" {
			return "class", target, "", "direct"
		}
	}

	if value := unquotePHPString(expression); value != expression {
		return "literal", value, "", "literal"
	}

	return "expression", describeExpressionValue(expression, nil), "", "expression"
}

func resolveModelClassArgument(expression, currentFQCN string, meta *phpFileMetadata) string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ""
	}

	if strings.HasSuffix(expression, "::class") {
		return resolveClassReferenceFQCN(strings.TrimSuffix(expression, "::class"), currentFQCN, meta)
	}

	if unquoted := unquotePHPString(expression); unquoted != expression {
		return resolveClassReferenceFQCN(unquoted, currentFQCN, meta)
	}

	return resolveClassReferenceFQCN(expression, currentFQCN, meta)
}

func parseAuthorizeResourceOptions(args []string) ([]string, []string) {
	if len(args) < 3 {
		return nil, nil
	}

	options := strings.TrimSpace(args[2])
	if !strings.HasPrefix(options, "[") {
		return nil, nil
	}

	closeIdx := findMatchingDelimiter(options, 0, '[', ']')
	if closeIdx == -1 {
		return nil, nil
	}

	body := strings.TrimSpace(options[1:closeIdx])
	if body == "" {
		return nil, nil
	}

	var only []string
	var except []string
	for _, part := range splitTopLevel(body, ',') {
		key, value, ok := splitArrayEntry(strings.TrimSpace(part))
		if !ok {
			continue
		}

		switch normalizePHPArrayKey(key) {
		case "only":
			only = arrayLiteralStringValues(value)
		case "except":
			except = arrayLiteralStringValues(value)
		}
	}

	return only, except
}

func arrayLiteralStringValues(expression string) []string {
	expression = strings.TrimSpace(expression)
	if !strings.HasPrefix(expression, "[") {
		return nil
	}
	closeIdx := findMatchingDelimiter(expression, 0, '[', ']')
	if closeIdx == -1 {
		return nil
	}
	body := strings.TrimSpace(expression[1:closeIdx])
	if body == "" {
		return nil
	}

	values := make([]string, 0)
	for _, part := range splitTopLevel(body, ',') {
		value := unquotePHPString(strings.TrimSpace(part))
		if value == "" || value == part {
			continue
		}
		values = append(values, value)
	}

	return stableUniqueStrings(values)
}

func defaultRouteParameterForModel(fqcn string) string {
	short := shortTypeName(fqcn)
	if short == "" {
		return ""
	}

	var builder strings.Builder
	for i, r := range short {
		if i > 0 && r >= 'A' && r <= 'Z' {
			builder.WriteByte('_')
		}
		builder.WriteRune(r)
	}

	return strings.ToLower(builder.String())
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func findInstanceMethodCalls(source, receiver, method string) []instanceMethodCall {
	if source == "" || receiver == "" || method == "" {
		return nil
	}

	needle := receiver + "->" + method
	calls := make([]instanceMethodCall, 0)
	for idx := 0; idx < len(source); {
		next := strings.Index(source[idx:], needle)
		if next == -1 {
			break
		}
		next += idx
		openIdx := next + len(needle)
		for openIdx < len(source) && isWhitespaceByte(source[openIdx]) {
			openIdx++
		}
		if openIdx >= len(source) || source[openIdx] != '(' {
			idx = next + len(needle)
			continue
		}
		closeIdx := findMatchingDelimiter(source, openIdx, '(', ')')
		if closeIdx == -1 {
			idx = openIdx + 1
			continue
		}
		body := strings.TrimSpace(source[openIdx+1 : closeIdx])
		args := []string(nil)
		if body != "" {
			args = splitTopLevel(body, ',')
		}
		calls = append(calls, instanceMethodCall{
			Receiver: receiver,
			Method:   method,
			Args:     args,
		})
		idx = closeIdx + 1
	}

	return calls
}

func dedupeAuthorizationHints(hints []emitter.AuthorizationHint) []emitter.AuthorizationHint {
	if len(hints) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(hints))
	out := make([]emitter.AuthorizationHint, 0, len(hints))
	for _, hint := range hints {
		key := strings.Join([]string{
			hint.Kind,
			ptrValue(hint.Ability),
			ptrValue(hint.TargetKind),
			ptrValue(hint.Target),
			ptrValue(hint.Parameter),
			hint.Source,
			hint.Resolution,
			boolKey(hint.EnforcesFailureResponse),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, hint)
	}

	return out
}

func sortAuthorizationHints(hints []emitter.AuthorizationHint) []emitter.AuthorizationHint {
	sort.Slice(hints, func(i, j int) bool {
		if hints[i].Kind != hints[j].Kind {
			return hints[i].Kind < hints[j].Kind
		}
		if ptrValue(hints[i].Ability) != ptrValue(hints[j].Ability) {
			return ptrValue(hints[i].Ability) < ptrValue(hints[j].Ability)
		}
		if ptrValue(hints[i].TargetKind) != ptrValue(hints[j].TargetKind) {
			return ptrValue(hints[i].TargetKind) < ptrValue(hints[j].TargetKind)
		}
		if ptrValue(hints[i].Target) != ptrValue(hints[j].Target) {
			return ptrValue(hints[i].Target) < ptrValue(hints[j].Target)
		}
		if ptrValue(hints[i].Parameter) != ptrValue(hints[j].Parameter) {
			return ptrValue(hints[i].Parameter) < ptrValue(hints[j].Parameter)
		}
		if hints[i].Source != hints[j].Source {
			return hints[i].Source < hints[j].Source
		}
		if hints[i].Resolution != hints[j].Resolution {
			return hints[i].Resolution < hints[j].Resolution
		}
		return !hints[i].EnforcesFailureResponse && hints[j].EnforcesFailureResponse
	})
	return hints
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

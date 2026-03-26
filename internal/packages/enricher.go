package packages

import (
	"context"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
	"github.com/oxhq/oxinfer/internal/psr4"
)

const maxLaravelDataDepth = 4

type phpFileMetadata struct {
	Namespace string
	Uses      map[string]string
	ClassName string
	Extends   string
}

type sourceRuntime struct {
	resolver       psr4.PSR4Resolver
	sourceCache    map[string]string
	metadataCache  map[string]*phpFileMetadata
	dataClassCache map[string]bool
	dataShapeCache map[string]emitter.OrderedObject
}

type queryBuilderRequestIR struct {
	Filters  []queryBuilderFilterSpec
	Includes []queryBuilderPathSpec
	Sorts    []queryBuilderPathSpec
	Fields   []queryBuilderPathSpec
}

type queryBuilderFilterSpec struct {
	Name           string
	Variant        string
	Column         string
	Source         string
	NameSegments   []string
	ColumnSegments []string
}

type queryBuilderPathSpec struct {
	Name       string
	Variant    string
	Source     string
	Descending bool
	Segments   []string
}

func EnrichDelta(ctx context.Context, manifestData *manifest.Manifest, detected []DetectedPackage, delta *emitter.Delta) error {
	if manifestData == nil || delta == nil || len(detected) == 0 {
		return nil
	}

	enabled := make(map[string]struct{}, len(detected))
	for _, pkg := range detected {
		enabled[pkg.Name] = struct{}{}
	}
	if len(delta.Controllers) == 0 && len(delta.Models) == 0 {
		if _, ok := enabled[PackageSpatieLaravelTranslatable]; !ok {
			return nil
		}
	}

	resolver, err := psr4.NewPSR4ResolverFromManifest(manifestData)
	if err != nil {
		return nil
	}

	runtime := &sourceRuntime{
		resolver:       resolver,
		sourceCache:    make(map[string]string),
		metadataCache:  make(map[string]*phpFileMetadata),
		dataClassCache: make(map[string]bool),
		dataShapeCache: make(map[string]emitter.OrderedObject),
	}

	for i := range delta.Controllers {
		controller := &delta.Controllers[i]
		filePath, err := runtime.resolver.ResolveClass(ctx, controller.FQCN)
		if err != nil {
			continue
		}

		source, err := runtime.readFile(filePath)
		if err != nil {
			continue
		}
		meta := runtime.fileMetadata(filePath, source)
		signature, body, ok := extractMethodSignatureAndBody(source, controller.Method)
		if !ok {
			continue
		}

		if _, ok := enabled[PackageSpatieLaravelQueryBuilder]; ok {
			queryIR := runtime.extractQueryBuilderRequestIR(ctx, controller.FQCN, source, body, meta)
			queryShape := runtime.queryBuilderShapeFromIR(queryIR)
			if len(queryShape) > 0 {
				request := ensureRequestInfo(controller)
				request.Query = mergeOrderedObjects(request.Query, queryShape)
				request.Fields = mergeRequestFields(request.Fields, runtime.queryBuilderFieldsFromIR(queryIR))
			}
		}

		if _, ok := enabled[PackageSpatieLaravelData]; ok {
			bodyShape := runtime.extractLaravelDataRequestShape(ctx, signature, meta)
			if len(bodyShape) > 0 {
				request := ensureRequestInfo(controller)
				request.Body = mergeOrderedObjects(request.Body, bodyShape)
				if len(request.ContentTypes) == 0 {
					request.ContentTypes = []string{"application/json"}
				}
				request.Fields = mergeRequestFields(request.Fields, runtime.extractLaravelDataRequestFields(ctx, signature, meta))
			}
		}

		if _, ok := enabled[PackageSpatieLaravelMediaLibrary]; ok {
			fileShape := extractMediaLibraryFileShape(body)
			if len(fileShape) > 0 {
				request := ensureRequestInfo(controller)
				request.Files = mergeOrderedObjects(request.Files, fileShape)
				request.ContentTypes = ensureContentType(request.ContentTypes, "multipart/form-data")
				request.Fields = mergeRequestFields(request.Fields, mediaLibraryFieldsFromShape(fileShape))
			}
		}
	}

	if _, ok := enabled[PackageSpatieLaravelTranslatable]; ok {
		runtime.enrichTranslatableModels(ctx, delta)
	}

	return nil
}

func (r *sourceRuntime) extractLaravelDataRequestShape(ctx context.Context, signature string, meta *phpFileMetadata) emitter.OrderedObject {
	shape := emitter.OrderedObject{}
	for _, param := range extractResolvedParameters(signature, meta) {
		dataShape := r.directLaravelDataShape(ctx, param.TypeCandidates, 0, nil)
		if len(dataShape) > 0 {
			shape = mergeOrderedObjects(shape, dataShape)
			continue
		}

		if len(param.CollectionItemCandidates) == 0 || param.Name == "" {
			continue
		}

		collectionShape := r.collectionLaravelDataShape(ctx, param.CollectionItemCandidates, 0, nil)
		if len(collectionShape) == 0 {
			continue
		}
		shape[param.Name] = mergeOrderedObjects(shape[param.Name], collectionShape)
	}
	return shape
}

func (r *sourceRuntime) extractLaravelDataRequestFields(ctx context.Context, signature string, meta *phpFileMetadata) []emitter.RequestField {
	var fields []emitter.RequestField
	for _, param := range extractResolvedParameters(signature, meta) {
		if dataFQCN := r.firstLaravelDataCandidate(ctx, param.TypeCandidates); dataFQCN != "" {
			fields = mergeRequestFields(fields, r.inferDataFields(ctx, dataFQCN, "", 0, nil))
			continue
		}
		if param.Name == "" {
			continue
		}
		if itemFQCN := r.firstLaravelDataCandidate(ctx, param.CollectionItemCandidates); itemFQCN != "" {
			fields = mergeRequestFields(fields, r.requestFieldsForCollection(itemFQCN, param.Name, param.CollectionScalarType, param.Wrappers, param.Nullable, param.HasDefault))
			fields = mergeRequestFields(fields, r.inferDataFields(ctx, itemFQCN, param.Name+"[]", 1, nil))
		}
	}
	return fields
}

func (r *sourceRuntime) inferDataFields(ctx context.Context, fqcn, prefix string, depth int, visited map[string]struct{}) []emitter.RequestField {
	if fqcn == "" || depth > maxLaravelDataDepth {
		return nil
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	if _, ok := visited[fqcn+"::"+prefix]; ok {
		return nil
	}
	visited[fqcn+"::"+prefix] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return nil
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return nil
	}
	meta := r.fileMetadata(filePath, source)

	var fields []emitter.RequestField
	seenMembers := make(map[string]struct{})
	if meta.Extends != "" {
		parentFQCN := resolveTypeName(meta.Extends, meta)
		if parentFQCN != "" && r.isLaravelDataClass(ctx, parentFQCN, cloneVisited(visited)) {
			fields = mergeRequestFields(fields, r.inferDataFields(ctx, parentFQCN, prefix, depth+1, cloneVisited(visited)))
		}
	}

	constructorSignature, _, hasConstructor := extractMethodSignatureAndBody(source, "__construct")
	if hasConstructor {
		for _, param := range extractResolvedParameters(constructorSignature, meta) {
			if !param.Promoted {
				continue
			}
			seenMembers[param.Name] = struct{}{}
			fields = mergeRequestFields(fields, r.requestFieldsForResolvedMember(ctx, prefix, resolvedMember{
				Name:                 param.Name,
				TypeFQCN:             param.TypeFQCN,
				TypeCandidates:       param.TypeCandidates,
				CollectionCandidates: param.CollectionItemCandidates,
				CollectionScalarType: param.CollectionScalarType,
				PrimitiveScalarType:  param.PrimitiveScalarType,
				Wrappers:             param.Wrappers,
				Nullable:             param.Nullable,
				HasDefault:           param.HasDefault,
			}, depth+1, cloneVisited(visited)))
		}
	}

	for _, property := range extractPublicProperties(source, meta) {
		if _, exists := seenMembers[property.Name]; exists {
			continue
		}
		fields = mergeRequestFields(fields, r.requestFieldsForResolvedMember(ctx, prefix, resolvedMember{
			Name:                 property.Name,
			TypeFQCN:             property.TypeFQCN,
			TypeCandidates:       property.TypeCandidates,
			CollectionCandidates: property.CollectionItemCandidates,
			CollectionScalarType: property.CollectionScalarType,
			PrimitiveScalarType:  property.PrimitiveScalarType,
			Wrappers:             property.Wrappers,
			Nullable:             property.Nullable,
			HasDefault:           property.HasDefault,
		}, depth+1, cloneVisited(visited)))
	}

	return fields
}

func (r *sourceRuntime) isLaravelDataClass(ctx context.Context, fqcn string, visited map[string]struct{}) bool {
	if fqcn == "" {
		return false
	}
	if cached, ok := r.dataClassCache[fqcn]; ok {
		return cached
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	if _, ok := visited[fqcn]; ok {
		return false
	}
	visited[fqcn] = struct{}{}

	if fqcn == `Spatie\LaravelData\Data` {
		r.dataClassCache[fqcn] = true
		return true
	}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		r.dataClassCache[fqcn] = false
		return false
	}

	source, err := r.readFile(filePath)
	if err != nil {
		r.dataClassCache[fqcn] = false
		return false
	}
	meta := r.fileMetadata(filePath, source)
	if meta.Extends == "" {
		r.dataClassCache[fqcn] = false
		return false
	}

	parentFQCN := resolveTypeName(meta.Extends, meta)
	if parentFQCN == "" {
		r.dataClassCache[fqcn] = false
		return false
	}

	isData := r.isLaravelDataClass(ctx, parentFQCN, visited)
	r.dataClassCache[fqcn] = isData
	return isData
}

func (r *sourceRuntime) inferDataShape(ctx context.Context, fqcn string, depth int, visited map[string]struct{}) emitter.OrderedObject {
	if fqcn == "" || depth > maxLaravelDataDepth {
		return emitter.OrderedObject{}
	}
	if cached, ok := r.dataShapeCache[fqcn]; ok {
		return cloneOrderedObject(cached)
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	if _, ok := visited[fqcn]; ok {
		return emitter.OrderedObject{}
	}
	visited[fqcn] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return emitter.OrderedObject{}
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return emitter.OrderedObject{}
	}
	meta := r.fileMetadata(filePath, source)

	shape := emitter.OrderedObject{}
	if meta.Extends != "" {
		parentFQCN := resolveTypeName(meta.Extends, meta)
		if parentFQCN != "" && r.isLaravelDataClass(ctx, parentFQCN, cloneVisited(visited)) {
			parentShape := r.inferDataShape(ctx, parentFQCN, depth+1, cloneVisited(visited))
			shape = mergeOrderedObjects(shape, parentShape)
		}
	}
	constructorSignature, _, hasConstructor := extractMethodSignatureAndBody(source, "__construct")
	if hasConstructor {
		for _, param := range extractResolvedParameters(constructorSignature, meta) {
			if !param.Promoted {
				continue
			}
			shape[param.Name] = r.propertyShape(ctx, param.TypeCandidates, param.CollectionItemCandidates, depth+1, cloneVisited(visited))
		}
	}

	for _, property := range extractPublicProperties(source, meta) {
		if _, exists := shape[property.Name]; exists {
			continue
		}
		shape[property.Name] = r.propertyShape(ctx, property.TypeCandidates, property.CollectionItemCandidates, depth+1, cloneVisited(visited))
	}

	r.dataShapeCache[fqcn] = cloneOrderedObject(shape)
	return shape
}

func (r *sourceRuntime) propertyShape(ctx context.Context, typeCandidates []string, collectionItemCandidates []string, depth int, visited map[string]struct{}) emitter.OrderedObject {
	if nested := r.directLaravelDataShape(ctx, typeCandidates, depth, visited); len(nested) > 0 {
		return nested
	}
	if nested := r.collectionLaravelDataShape(ctx, collectionItemCandidates, depth, visited); len(nested) > 0 {
		return nested
	}
	return emitter.OrderedObject{}
}

func (r *sourceRuntime) directLaravelDataShape(ctx context.Context, typeCandidates []string, depth int, visited map[string]struct{}) emitter.OrderedObject {
	for _, typeFQCN := range typeCandidates {
		if typeFQCN == "" || !r.isLaravelDataClass(ctx, typeFQCN, cloneVisited(visited)) {
			continue
		}

		nested := r.inferDataShape(ctx, typeFQCN, depth, visited)
		if len(nested) > 0 {
			return nested
		}
	}

	return emitter.OrderedObject{}
}

func (r *sourceRuntime) collectionLaravelDataShape(ctx context.Context, itemCandidates []string, depth int, visited map[string]struct{}) emitter.OrderedObject {
	for _, typeFQCN := range itemCandidates {
		if typeFQCN == "" || !r.isLaravelDataClass(ctx, typeFQCN, cloneVisited(visited)) {
			continue
		}

		nested := r.inferDataShape(ctx, typeFQCN, depth, visited)
		if len(nested) > 0 {
			return emitter.OrderedObject{"_item": nested}
		}
	}

	return emitter.OrderedObject{}
}

func (r *sourceRuntime) readFile(path string) (string, error) {
	if source, ok := r.sourceCache[path]; ok {
		return source, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	source := string(data)
	r.sourceCache[path] = source
	return source, nil
}

func (r *sourceRuntime) fileMetadata(path, source string) *phpFileMetadata {
	if meta, ok := r.metadataCache[path]; ok {
		return meta
	}
	meta := parsePHPFileMetadata(source)
	r.metadataCache[path] = meta
	return meta
}

func (r *sourceRuntime) extractQueryBuilderShape(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata) emitter.OrderedObject {
	spec := r.extractQueryBuilderRequestIR(ctx, fqcn, source, body, meta)
	return r.queryBuilderShapeFromIR(spec)
}

func (r *sourceRuntime) queryBuilderShapeFromIR(spec queryBuilderRequestIR) emitter.OrderedObject {
	shape := emitter.OrderedObject{}

	if len(spec.Filters) > 0 {
		filterShape := emitter.OrderedObject{}
		for _, filter := range spec.Filters {
			name := strings.TrimSpace(filter.Name)
			if name == "" {
				continue
			}
			filterShape[name] = emitter.OrderedObject{}
		}
		shape["filter"] = filterShape
	}

	if len(spec.Fields) > 0 {
		fieldsShape := emitter.OrderedObject{}
		for _, field := range spec.Fields {
			if field.Name == "" {
				continue
			}
			fieldsShape = mergeOrderedObjects(fieldsShape, nestedOrderedObject(queryFieldSegments(field.Name)))
		}
		shape["fields"] = fieldsShape
	}

	if len(spec.Includes) > 0 {
		includeShape := emitter.OrderedObject{}
		for _, include := range spec.Includes {
			if include.Name == "" {
				continue
			}
			includeShape = mergeOrderedObjects(includeShape, nestedOrderedObject(namePathSegments(include.Name)))
		}
		shape["include"] = includeShape
	}
	if len(spec.Sorts) > 0 {
		sortShape := emitter.OrderedObject{}
		for _, sortSpec := range spec.Sorts {
			name := strings.TrimSpace(sortSpec.Name)
			if name == "" {
				continue
			}
			sortShape = mergeOrderedObjects(sortShape, nestedOrderedObject(namePathSegments(name)))
		}
		shape["sort"] = sortShape
	}

	return shape
}

func (r *sourceRuntime) queryBuilderFieldsFromIR(spec queryBuilderRequestIR) []emitter.RequestField {
	var fields []emitter.RequestField

	filterNames := make([]string, 0, len(spec.Filters))
	for _, filter := range spec.Filters {
		name := strings.TrimSpace(filter.Name)
		if name == "" {
			continue
		}
		filterNames = append(filterNames, name)
		fields = append(fields, emitter.RequestField{
			Location:   "query",
			Path:       joinMetadataPath("filter", name),
			Kind:       "scalar",
			Type:       "string",
			ScalarType: "string",
			Required:   packageBoolPtr(false),
			Optional:   packageBoolPtr(true),
			Source:     PackageSpatieLaravelQueryBuilder,
			Via:        firstNonEmptyString(filter.Variant, "allowedFilters"),
		})
	}
	if len(filterNames) > 0 {
		fields = append(fields, emitter.RequestField{
			Location:      "query",
			Path:          "filter",
			Kind:          "object",
			Type:          "object",
			AllowedValues: stableUniqueQueryNames(filterNames),
			Required:      packageBoolPtr(false),
			Optional:      packageBoolPtr(true),
			Source:        PackageSpatieLaravelQueryBuilder,
			Via:           "allowedFilters",
		})
	}

	includeNames := pathSpecNames(spec.Includes)
	if len(includeNames) > 0 {
		fields = append(fields, emitter.RequestField{
			Location:      "query",
			Path:          "include",
			Kind:          "csv",
			Type:          "string",
			ScalarType:    "string",
			AllowedValues: includeNames,
			Required:      packageBoolPtr(false),
			Optional:      packageBoolPtr(true),
			Source:        PackageSpatieLaravelQueryBuilder,
			Via:           "allowedIncludes",
		})
	}

	sortNames := pathSpecNames(spec.Sorts)
	if len(sortNames) > 0 {
		fields = append(fields, emitter.RequestField{
			Location:      "query",
			Path:          "sort",
			Kind:          "csv",
			Type:          "string",
			ScalarType:    "string",
			AllowedValues: sortNames,
			Required:      packageBoolPtr(false),
			Optional:      packageBoolPtr(true),
			Source:        PackageSpatieLaravelQueryBuilder,
			Via:           "allowedSorts",
		})
	}

	fieldGroups := make(map[string][]string)
	for _, field := range spec.Fields {
		if field.Name == "" {
			continue
		}
		segments := queryFieldSegments(field.Name)
		if len(segments) < 2 {
			continue
		}
		group := segments[0]
		leaf := strings.Join(segments[1:], ".")
		if leaf == "" {
			continue
		}
		fieldGroups[group] = append(fieldGroups[group], leaf)
	}
	if len(fieldGroups) > 0 {
		groupNames := make([]string, 0, len(fieldGroups))
		for group, values := range fieldGroups {
			groupNames = append(groupNames, group)
			fields = append(fields, emitter.RequestField{
				Location:      "query",
				Path:          joinMetadataPath("fields", group),
				Kind:          "csv",
				Type:          "string",
				ScalarType:    "string",
				AllowedValues: stableUniqueQueryNames(values),
				Required:      packageBoolPtr(false),
				Optional:      packageBoolPtr(true),
				Source:        PackageSpatieLaravelQueryBuilder,
				Via:           "allowedFields",
			})
		}
		fields = append(fields, emitter.RequestField{
			Location:      "query",
			Path:          "fields",
			Kind:          "object",
			Type:          "object",
			AllowedValues: stableUniqueQueryNames(groupNames),
			Required:      packageBoolPtr(false),
			Optional:      packageBoolPtr(true),
			Source:        PackageSpatieLaravelQueryBuilder,
			Via:           "allowedFields",
		})
	}

	return mergeRequestFields(nil, fields)
}

func (r *sourceRuntime) extractQueryBuilderRequestIR(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata) queryBuilderRequestIR {
	return queryBuilderRequestIR{
		Filters:  r.extractAllowedFilterSpecs(ctx, fqcn, source, body, meta),
		Includes: r.extractAllowedPathSpecs(ctx, fqcn, source, body, meta, "allowedIncludes"),
		Sorts:    r.extractAllowedPathSpecs(ctx, fqcn, source, body, meta, "allowedSorts"),
		Fields:   r.extractAllowedPathSpecs(ctx, fqcn, source, body, meta, "allowedFields"),
	}
}

func (r *sourceRuntime) extractAllowedFilterNames(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata) []string {
	var names []string
	for _, spec := range r.extractAllowedFilterSpecs(ctx, fqcn, source, body, meta) {
		if spec.Name == "" {
			continue
		}
		names = append(names, spec.Name)
	}
	return stableUniqueQueryNames(names)
}

func (r *sourceRuntime) extractAllowedFilterSpecs(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata) []queryBuilderFilterSpec {
	var specs []queryBuilderFilterSpec
	for _, item := range r.extractCallItems(ctx, fqcn, source, body, meta, "allowedFilters") {
		spec, ok := r.extractAllowedFilterSpec(ctx, fqcn, source, body, meta, item)
		if !ok || spec.Name == "" {
			continue
		}
		specs = append(specs, spec)
	}
	return stableUniqueQueryBuilderFilterSpecs(specs)
}

func (r *sourceRuntime) extractAllowedFieldPaths(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata) []string {
	var groups []string
	for _, spec := range r.extractAllowedPathSpecs(ctx, fqcn, source, body, meta, "allowedFields") {
		if spec.Name == "" {
			continue
		}
		groups = append(groups, spec.Name)
	}
	return stableUniqueQueryNames(groups)
}

func (r *sourceRuntime) extractAllowedPathSpecs(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata, method string) []queryBuilderPathSpec {
	var specs []queryBuilderPathSpec
	for _, item := range r.extractCallItems(ctx, fqcn, source, body, meta, method) {
		spec, ok := r.extractAllowedPathSpec(ctx, fqcn, source, body, meta, item)
		if !ok || spec.Name == "" {
			continue
		}
		if method == "allowedSorts" && strings.HasPrefix(spec.Name, "-") {
			spec.Descending = true
			spec.Name = strings.TrimPrefix(strings.TrimSpace(spec.Name), "-")
			spec.Segments = namePathSegments(spec.Name)
		}
		specs = append(specs, spec)
	}
	return stableUniqueQueryBuilderPathSpecs(specs)
}

func extractMediaLibraryFileShape(body string) emitter.OrderedObject {
	shape := emitter.OrderedObject{}

	singleFieldRe := regexp.MustCompile(`addMediaFromRequest\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	for _, match := range singleFieldRe.FindAllStringSubmatch(body, -1) {
		if len(match) != 2 {
			continue
		}
		shape = mergeOrderedObjects(shape, nestedOrderedObject(fileFieldSegments(match[1])))
	}

	for _, item := range extractCallItems(body, "addMultipleMediaFromRequest") {
		literal, ok := extractSingleStringLiteral(item)
		if !ok {
			continue
		}
		shape = mergeOrderedObjects(shape, nestedOrderedObject(fileFieldSegments(literal)))
	}

	for _, args := range extractCallArguments(body, "addMedia") {
		fieldName, ok := extractUploadedFileFieldName(body, args)
		if !ok {
			continue
		}
		shape = mergeOrderedObjects(shape, nestedOrderedObject(fileFieldSegments(fieldName)))
	}

	return shape
}

func extractCallArguments(body, method string) []string {
	var argsList []string
	search := method + "("
	offset := 0
	for {
		idx := strings.Index(body[offset:], search)
		if idx == -1 {
			return argsList
		}

		openIdx := offset + idx + len(method)
		closeIdx := findMatchingDelimiter(body, openIdx, '(', ')')
		if closeIdx == -1 {
			return argsList
		}

		argsList = append(argsList, body[openIdx+1:closeIdx])
		offset = closeIdx + 1
	}
}

func extractStringLiterals(source string) []string {
	re := regexp.MustCompile(`'([^']+)'|"([^"]+)"`)
	matches := re.FindAllStringSubmatch(source, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		value := strings.TrimSpace(match[1] + match[2])
		if value == "" {
			continue
		}
		names = append(names, value)
	}
	return names
}

type resolvedParameter struct {
	Name                     string
	TypeFQCN                 string
	TypeCandidates           []string
	CollectionItemCandidates []string
	CollectionScalarType     string
	PrimitiveScalarType      string
	Wrappers                 []string
	Nullable                 bool
	HasDefault               bool
	Promoted                 bool
}

func extractResolvedParameterTypes(signature string, meta *phpFileMetadata) []string {
	params := extractResolvedParameters(signature, meta)
	types := make([]string, 0, len(params))
	for _, param := range params {
		types = append(types, param.TypeCandidates...)
		types = append(types, param.CollectionItemCandidates...)
	}
	return stableUniqueQueryNames(types)
}

func extractResolvedParameters(signature string, meta *phpFileMetadata) []resolvedParameter {
	start := strings.Index(signature, "(")
	if start == -1 {
		return nil
	}
	end := findMatchingDelimiter(signature, start, '(', ')')
	if end == -1 {
		return nil
	}

	var params []resolvedParameter
	for _, part := range splitTopLevel(signature[start+1:end], ',') {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		promoted := strings.Contains(trimmed, "public ") || strings.Contains(trimmed, "protected ") || strings.Contains(trimmed, "private ")
		nameIdx := strings.Index(trimmed, "$")
		if nameIdx == -1 {
			continue
		}

		nameStart := nameIdx + 1
		nameEnd := nameStart
		for nameEnd < len(trimmed) && isIdentifierChar(trimmed[nameEnd]) {
			nameEnd++
		}
		name := trimmed[nameStart:nameEnd]
		typePart := cleanResolvedTypePart(trimmed[:nameIdx])
		typeCandidates := resolveTypeNames(typePart, meta)
		collectionItemCandidates := extractCollectionItemTypeCandidates(trimmed, typePart, meta)
		collectionScalarType := extractCollectionScalarType(trimmed, typePart)
		typeFQCN := ""
		if len(typeCandidates) > 0 {
			typeFQCN = typeCandidates[0]
		}

		params = append(params, resolvedParameter{
			Name:                     name,
			TypeFQCN:                 typeFQCN,
			TypeCandidates:           typeCandidates,
			CollectionItemCandidates: collectionItemCandidates,
			CollectionScalarType:     collectionScalarType,
			PrimitiveScalarType:      extractPrimitiveScalarType(typePart),
			Wrappers:                 extractTypeWrappers(typePart),
			Nullable:                 isNullableType(trimmed[:nameIdx]),
			HasDefault:               strings.Contains(trimmed[nameEnd:], "="),
			Promoted:                 promoted,
		})
	}
	return params
}

type resolvedProperty struct {
	Name                     string
	TypeFQCN                 string
	TypeCandidates           []string
	CollectionItemCandidates []string
	CollectionScalarType     string
	PrimitiveScalarType      string
	Wrappers                 []string
	Nullable                 bool
	HasDefault               bool
}

func extractPublicProperties(source string, meta *phpFileMetadata) []resolvedProperty {
	re := regexp.MustCompile(`(?ms)(/\*\*.*?\*/\s*)?^\s*(?:#\[[^\]]+\]\s*)*public\s+(?:readonly\s+)?(?:static\s+)?([^$\n;]+)?\$(\w+)`)
	matches := re.FindAllStringSubmatch(source, -1)
	properties := make([]resolvedProperty, 0, len(matches))
	for _, match := range matches {
		rawSource := strings.TrimSpace(match[0])
		typePart := cleanResolvedTypePart(strings.TrimSpace(match[2]))
		typeCandidates := resolveTypeNames(typePart, meta)
		collectionScalarType := extractCollectionScalarType(rawSource, typePart)
		typeFQCN := ""
		if len(typeCandidates) > 0 {
			typeFQCN = typeCandidates[0]
		}
		properties = append(properties, resolvedProperty{
			Name:                     match[3],
			TypeFQCN:                 typeFQCN,
			TypeCandidates:           typeCandidates,
			CollectionItemCandidates: extractCollectionItemTypeCandidates(rawSource, typePart, meta),
			CollectionScalarType:     collectionScalarType,
			PrimitiveScalarType:      extractPrimitiveScalarType(typePart),
			Wrappers:                 extractTypeWrappers(typePart),
			Nullable:                 isNullableType(rawSource),
			HasDefault:               hasPropertyDefault(rawSource, match[3]),
		})
	}
	return properties
}

type resolvedMember struct {
	Name                 string
	TypeFQCN             string
	TypeCandidates       []string
	CollectionCandidates []string
	CollectionScalarType string
	PrimitiveScalarType  string
	Wrappers             []string
	Nullable             bool
	HasDefault           bool
}

func (r *sourceRuntime) requestFieldsForResolvedMember(ctx context.Context, prefix string, member resolvedMember, depth int, visited map[string]struct{}) []emitter.RequestField {
	path := joinMetadataPath(prefix, member.Name)
	if path == "" {
		return nil
	}

	required := !member.HasDefault && !hasWrapper(member.Wrappers, "Optional") && !hasWrapper(member.Wrappers, "Lazy")
	field := emitter.RequestField{
		Location: "body",
		Path:     path,
		Wrappers: stableUniqueQueryNames(member.Wrappers),
		Required: packageBoolPtr(required),
		Optional: packageBoolPtr(!required),
		Nullable: packageBoolPtr(member.Nullable),
		Source:   PackageSpatieLaravelData,
		Via:      "data",
	}

	if itemFQCN := r.firstLaravelDataCandidate(ctx, member.CollectionCandidates); itemFQCN != "" {
		fields := r.requestFieldsForCollection(itemFQCN, path, member.CollectionScalarType, member.Wrappers, member.Nullable, member.HasDefault)
		return mergeRequestFields(fields, r.inferDataFields(ctx, itemFQCN, path+"[]", depth, cloneVisited(visited)))
	}

	if dataFQCN := r.firstLaravelDataCandidate(ctx, member.TypeCandidates); dataFQCN != "" {
		field.Kind = "object"
		field.Type = dataFQCN
		fields := []emitter.RequestField{field}
		return mergeRequestFields(fields, r.inferDataFields(ctx, dataFQCN, path, depth, cloneVisited(visited)))
	}

	if member.CollectionScalarType != "" {
		isArray := true
		field.Kind = "collection"
		field.Type = "array"
		field.IsArray = packageBoolPtr(isArray)
		field.Collection = packageBoolPtr(isArray)
		field.ItemType = member.CollectionScalarType
		return []emitter.RequestField{field}
	}

	field.Kind = "scalar"
	field.ScalarType = firstNonEmptyString(member.PrimitiveScalarType, "string")
	field.Type = firstNonEmptyString(member.TypeFQCN, field.ScalarType)
	return []emitter.RequestField{field}
}

func (r *sourceRuntime) requestFieldsForCollection(itemFQCN, path, collectionScalarType string, wrappers []string, nullable, hasDefault bool) []emitter.RequestField {
	required := !hasDefault && !hasWrapper(wrappers, "Optional") && !hasWrapper(wrappers, "Lazy")
	isArray := true
	field := emitter.RequestField{
		Location:   "body",
		Path:       path,
		Kind:       "collection",
		Type:       "array",
		ItemType:   firstNonEmptyString(itemFQCN, collectionScalarType, "string"),
		Wrappers:   stableUniqueQueryNames(wrappers),
		Required:   packageBoolPtr(required),
		Optional:   packageBoolPtr(!required),
		Nullable:   packageBoolPtr(nullable),
		IsArray:    packageBoolPtr(isArray),
		Collection: packageBoolPtr(isArray),
		Source:     PackageSpatieLaravelData,
		Via:        "data",
	}
	return []emitter.RequestField{field}
}

func (r *sourceRuntime) firstLaravelDataCandidate(ctx context.Context, candidates []string) string {
	for _, candidate := range candidates {
		if candidate == "" || !r.isLaravelDataClass(ctx, candidate, nil) {
			continue
		}
		return candidate
	}
	return ""
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

func (r *sourceRuntime) enrichTranslatableModels(ctx context.Context, delta *emitter.Delta) {
	seenModels := make(map[string]struct{}, len(delta.Models))
	for i := range delta.Models {
		model := &delta.Models[i]
		seenModels[model.FQCN] = struct{}{}
		info := r.translatableModelInfo(ctx, model.FQCN, nil)
		if !info.UsesTrait || len(info.Fields) == 0 {
			continue
		}

		model.Attributes = mergeModelAttributes(model.Attributes, info.Fields, "spatie/laravel-translatable")
	}

	allClasses, err := r.resolver.GetAllClasses(ctx)
	if err != nil {
		return
	}

	for fqcn, filePath := range allClasses {
		if _, ok := seenModels[fqcn]; ok || !looksLikeModelClass(fqcn, filePath) {
			continue
		}

		info := r.translatableModelInfo(ctx, fqcn, nil)
		if !info.UsesTrait || len(info.Fields) == 0 {
			continue
		}

		delta.Models = append(delta.Models, emitter.Model{
			FQCN:       fqcn,
			Attributes: mergeModelAttributes(nil, info.Fields, "spatie/laravel-translatable"),
		})
	}

	sort.Slice(delta.Models, func(i, j int) bool {
		return delta.Models[i].FQCN < delta.Models[j].FQCN
	})
}

func usesHasTranslations(source string, meta *phpFileMetadata) bool {
	if meta == nil {
		return false
	}

	for alias, fqcn := range meta.Uses {
		if fqcn != `Spatie\Translatable\HasTranslations` {
			continue
		}

		traitPattern := regexp.MustCompile(`(?m)\buse\s+` + regexp.QuoteMeta(alias) + `\s*;`)
		if traitPattern.MatchString(source) {
			return true
		}
	}

	return strings.Contains(source, `use \Spatie\Translatable\HasTranslations;`) ||
		strings.Contains(source, `use Spatie\Translatable\HasTranslations;`)
}

type translatableInfo struct {
	UsesTrait bool
	Fields    []string
}

func (r *sourceRuntime) translatableModelInfo(ctx context.Context, fqcn string, visited map[string]struct{}) translatableInfo {
	if fqcn == "" {
		return translatableInfo{}
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	if _, ok := visited[fqcn]; ok {
		return translatableInfo{}
	}
	visited[fqcn] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return translatableInfo{}
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return translatableInfo{}
	}
	meta := r.fileMetadata(filePath, source)

	parentInfo := translatableInfo{}
	if meta.Extends != "" {
		parentFQCN := resolveTypeName(meta.Extends, meta)
		if parentFQCN != "" {
			parentInfo = r.translatableModelInfo(ctx, parentFQCN, cloneVisited(visited))
		}
	}

	info := translatableInfo{
		UsesTrait: usesHasTranslations(source, meta) || parentInfo.UsesTrait,
		Fields:    append([]string{}, parentInfo.Fields...),
	}
	if !info.UsesTrait {
		return info
	}

	fields := extractTranslatableFields(ctx, r, fqcn, source, meta, cloneVisited(visited))
	info.Fields = stableUniqueQueryNames(append(info.Fields, fields...))
	return info
}

func extractTranslatableFields(ctx context.Context, runtime *sourceRuntime, fqcn, source string, meta *phpFileMetadata, visited map[string]struct{}) []string {
	propertyRe := regexp.MustCompile(`(?s)(?:public|protected)\s+(?:static\s+)?(?:array\s+)?\$translatable\s*=\s*([^;]+);`)
	match := propertyRe.FindStringSubmatch(source)
	if len(match) != 2 {
		return nil
	}

	return stableUniqueQueryNames(resolveTranslatableFieldExpression(ctx, runtime, fqcn, strings.TrimSpace(match[1]), meta, visited))
}

func looksLikeModelClass(fqcn, filePath string) bool {
	if strings.Contains(fqcn, `\Models\`) {
		return true
	}

	normalizedPath := strings.ToLower(strings.ReplaceAll(filePath, `\`, `/`))
	return strings.Contains(normalizedPath, "/models/")
}

func resolveTranslatableFieldExpression(ctx context.Context, runtime *sourceRuntime, currentFQCN, expression string, meta *phpFileMetadata, visited map[string]struct{}) []string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil
	}
	if strings.HasPrefix(expression, "[") {
		closeIdx := findMatchingDelimiter(expression, 0, '[', ']')
		if closeIdx != len(expression)-1 {
			return nil
		}
		return extractStringLiterals(expression[1:closeIdx])
	}

	if strings.HasPrefix(expression, "array_merge(") {
		if openIdx := strings.IndexByte(expression, '('); openIdx != -1 {
			if closeIdx := findMatchingDelimiter(expression, openIdx, '(', ')'); closeIdx == len(expression)-1 {
				var fields []string
				for _, part := range splitTopLevel(expression[openIdx+1:closeIdx], ',') {
					fields = append(fields, resolveTranslatableFieldExpression(ctx, runtime, currentFQCN, part, meta, cloneVisited(visited))...)
				}
				return stableUniqueQueryNames(fields)
			}
		}
	}

	constRefRe := regexp.MustCompile(`^(self|static|parent|[A-Za-z_\\][A-Za-z0-9_\\]*)::([A-Z_][A-Z0-9_]*)$`)
	match := constRefRe.FindStringSubmatch(expression)
	if len(match) != 3 {
		return nil
	}

	targetFQCN := resolveClassReferenceFQCN(match[1], currentFQCN, meta)
	if targetFQCN == "" {
		return nil
	}

	return runtime.resolveStringArrayConstant(ctx, targetFQCN, match[2], visited)
}

func (r *sourceRuntime) resolveStringArrayConstant(ctx context.Context, fqcn, constName string, visited map[string]struct{}) []string {
	return stableUniqueQueryNames(extractPublicStringLiterals(r.resolveArrayConstantItems(ctx, fqcn, constName, visited)))
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
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
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

		if depthParen != 0 || depthBracket != 0 || depthBrace != 0 {
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
			return "", false
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
					return strings.TrimSpace(body[exprStart:exprEnd]), true
				}
			}
		}
		return "", false
	}

	return "", false
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

func resolveTypeName(typeName string, meta *phpFileMetadata) string {
	candidates := resolveTypeNames(typeName, meta)
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
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

func resolveTypeNames(typeName string, meta *phpFileMetadata) []string {
	trimmed := strings.TrimSpace(stripPHPAttributes(typeName))
	trimmed = strings.TrimPrefix(trimmed, "?")
	if trimmed == "" {
		return nil
	}

	var resolved []string
	for _, candidate := range splitTopLevel(trimmed, '|') {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.HasSuffix(candidate, "[]") || isGenericArrayType(candidate) {
			continue
		}
		if fqcn := resolveSingleType(candidate, meta); fqcn != "" {
			resolved = append(resolved, fqcn)
		}
	}

	return stableUniqueQueryNames(resolved)
}

func resolveSingleType(typeName string, meta *phpFileMetadata) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return ""
	}
	typeName = strings.TrimPrefix(typeName, `\`)

	switch strings.ToLower(typeName) {
	case "string", "int", "float", "bool", "array", "mixed", "callable", "iterable", "object", "self", "parent", "static", "null", "false", "true":
		return ""
	}

	if fqcn, ok := meta.Uses[typeName]; ok {
		return fqcn
	}
	if strings.Contains(typeName, `\`) {
		return typeName
	}
	if meta.Namespace == "" {
		return typeName
	}
	return meta.Namespace + `\` + typeName
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

func cleanResolvedTypePart(typePart string) string {
	typePart = stripPHPAttributes(typePart)
	for _, prefix := range []string{"public ", "protected ", "private ", "readonly ", "static "} {
		typePart = strings.ReplaceAll(typePart, prefix, "")
	}
	typePart = strings.TrimSpace(typePart)
	typePart = strings.TrimSuffix(typePart, "&")
	return strings.TrimSpace(typePart)
}

func extractTypeWrappers(typePart string) []string {
	var wrappers []string
	for _, candidate := range splitTopLevel(strings.TrimSpace(stripPHPAttributes(typePart)), '|') {
		name := strings.TrimPrefix(strings.TrimSpace(candidate), "?")
		if name == "" {
			continue
		}
		switch shortTypeName(name) {
		case "Optional", "Lazy":
			wrappers = append(wrappers, shortTypeName(name))
		}
	}
	return stableUniqueQueryNames(wrappers)
}

func isNullableType(typePart string) bool {
	typePart = strings.TrimSpace(stripPHPAttributes(typePart))
	if typePart == "" {
		return false
	}
	if strings.Contains(typePart, "?") {
		return true
	}
	for _, candidate := range splitTopLevel(typePart, '|') {
		switch strings.ToLower(strings.TrimSpace(candidate)) {
		case "null":
			return true
		}
	}
	return false
}

func extractPrimitiveScalarType(typePart string) string {
	typePart = strings.TrimSpace(stripPHPAttributes(typePart))
	if typePart == "" {
		return ""
	}
	for _, candidate := range splitTopLevel(typePart, '|') {
		if scalar := normalizeScalarType(candidate); scalar != "" {
			return scalar
		}
	}
	return ""
}

func extractCollectionScalarType(rawSource, typePart string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(string|int|integer|float|double|bool|boolean)\s*\[\]`),
		regexp.MustCompile(`\b(?:array|Collection|DataCollection)\s*<\s*(string|int|integer|float|double|bool|boolean)\s*>`),
		regexp.MustCompile(`@(?:phpstan-|psalm-)?var\s+(?:array|Collection|DataCollection)\s*<\s*(string|int|integer|float|double|bool|boolean)\s*>`),
		regexp.MustCompile(`@(?:phpstan-|psalm-)?var\s+(string|int|integer|float|double|bool|boolean)\[\]`),
	}
	for _, source := range []string{typePart, rawSource} {
		for _, pattern := range patterns {
			if match := pattern.FindStringSubmatch(source); len(match) == 2 {
				if scalar := normalizeScalarType(match[1]); scalar != "" {
					return scalar
				}
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(typePart), "array") {
		return "string"
	}
	return ""
}

func normalizeScalarType(typeName string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(typeName), "?")) {
	case "string":
		return "string"
	case "int", "integer":
		return "integer"
	case "float", "double":
		return "number"
	case "bool", "boolean":
		return "boolean"
	default:
		return ""
	}
}

func shortTypeName(typeName string) string {
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), `\`)
	if typeName == "" {
		return ""
	}
	if before, _, ok := strings.Cut(typeName, `<`); ok {
		typeName = before
	}
	parts := strings.Split(typeName, `\`)
	return strings.TrimSpace(parts[len(parts)-1])
}

func hasPropertyDefault(rawSource, propertyName string) bool {
	propertyName = strings.TrimSpace(propertyName)
	if propertyName == "" {
		return false
	}
	idx := strings.Index(rawSource, "$"+propertyName)
	if idx == -1 {
		return strings.Contains(rawSource, "=")
	}
	return strings.Contains(rawSource[idx+len(propertyName)+1:], "=")
}

func stripPHPAttributes(source string) string {
	attributeRe := regexp.MustCompile(`(?s)#\[[^\]]+\]\s*`)
	return attributeRe.ReplaceAllString(source, "")
}

func extractCollectionItemTypeCandidates(rawSource, typePart string, meta *phpFileMetadata) []string {
	var candidates []string

	attributeRe := regexp.MustCompile(`DataCollectionOf\s*\(\s*([A-Za-z_\\][A-Za-z0-9_\\]*)::class`)
	for _, match := range attributeRe.FindAllStringSubmatch(rawSource, -1) {
		if len(match) != 2 {
			continue
		}
		if fqcn := resolveTypeName(match[1], meta); fqcn != "" {
			candidates = append(candidates, fqcn)
		}
	}

	arrayTypeRe := regexp.MustCompile(`\b([A-Za-z_\\][A-Za-z0-9_\\]*)\s*\[\]`)
	for _, match := range arrayTypeRe.FindAllStringSubmatch(typePart, -1) {
		if len(match) != 2 {
			continue
		}
		if fqcn := resolveTypeName(match[1], meta); fqcn != "" {
			candidates = append(candidates, fqcn)
		}
	}

	genericArrayRe := regexp.MustCompile(`\b(?:array|Collection|DataCollection)\s*<\s*([A-Za-z_\\][A-Za-z0-9_\\]*)\s*>`)
	for _, match := range genericArrayRe.FindAllStringSubmatch(typePart, -1) {
		if len(match) != 2 {
			continue
		}
		if fqcn := resolveTypeName(match[1], meta); fqcn != "" {
			candidates = append(candidates, fqcn)
		}
	}

	docblockArrayRe := regexp.MustCompile(`@(?:phpstan-|psalm-)?var\s+(?:array|Collection|DataCollection)\s*<\s*([A-Za-z_\\][A-Za-z0-9_\\]*)\s*>`)
	for _, match := range docblockArrayRe.FindAllStringSubmatch(rawSource, -1) {
		if len(match) != 2 {
			continue
		}
		if fqcn := resolveTypeName(match[1], meta); fqcn != "" {
			candidates = append(candidates, fqcn)
		}
	}

	docblockShortArrayRe := regexp.MustCompile(`@(?:phpstan-|psalm-)?var\s+([A-Za-z_\\][A-Za-z0-9_\\]*)\[\]`)
	for _, match := range docblockShortArrayRe.FindAllStringSubmatch(rawSource, -1) {
		if len(match) != 2 {
			continue
		}
		if fqcn := resolveTypeName(match[1], meta); fqcn != "" {
			candidates = append(candidates, fqcn)
		}
	}

	return stableUniqueQueryNames(candidates)
}

func isGenericArrayType(typeName string) bool {
	return regexp.MustCompile(`\b(?:array|Collection|DataCollection)\s*<`).MatchString(typeName)
}

func extractCallItems(body, method string) []string {
	var items []string
	for _, args := range extractCallArguments(body, method) {
		items = append(items, expandLocalArrayItems(body, args)...)
	}
	return items
}

func expandLocalArrayItems(body, args string) []string {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}

	if strings.HasPrefix(args, "$") && !strings.Contains(args, ",") {
		if arrayBody, ok := findLocalArrayAssignment(body, strings.TrimPrefix(args, "$")); ok {
			return splitTopLevel(arrayBody, ',')
		}
	}

	if strings.HasPrefix(args, "[") {
		closeIdx := findMatchingDelimiter(args, 0, '[', ']')
		if closeIdx == len(args)-1 {
			return splitTopLevel(args[1:closeIdx], ',')
		}
	}

	return splitTopLevel(args, ',')
}

func (r *sourceRuntime) extractCallItems(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, method string) []string {
	var items []string
	for _, args := range extractCallArguments(body, method) {
		items = append(items, r.expandCallItems(ctx, currentFQCN, source, body, meta, args, nil)...)
	}
	return items
}

func (r *sourceRuntime) expandCallItems(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, args string, visited map[string]struct{}) []string {
	if visited == nil {
		visited = map[string]struct{}{}
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}

	cacheKey := currentFQCN + "::" + args
	if _, ok := visited[cacheKey]; ok {
		return nil
	}
	visited[cacheKey] = struct{}{}

	if strings.HasPrefix(args, "$") && !strings.Contains(args, ",") {
		if arrayBody, ok := findLocalArrayAssignment(body, strings.TrimPrefix(args, "$")); ok {
			return splitTopLevel(arrayBody, ',')
		}
	}

	if strings.HasPrefix(args, "[") {
		closeIdx := findMatchingDelimiter(args, 0, '[', ']')
		if closeIdx == len(args)-1 {
			return splitTopLevel(args[1:closeIdx], ',')
		}
	}

	if strings.HasPrefix(args, "array_merge(") {
		if openIdx := strings.IndexByte(args, '('); openIdx != -1 {
			if closeIdx := findMatchingDelimiter(args, openIdx, '(', ')'); closeIdx == len(args)-1 {
				var items []string
				for _, part := range splitTopLevel(args[openIdx+1:closeIdx], ',') {
					items = append(items, r.expandCallItems(ctx, currentFQCN, source, body, meta, part, cloneVisited(visited))...)
				}
				return items
			}
		}
	}

	if items, ok := r.resolveArrayReferenceItems(ctx, currentFQCN, source, body, meta, args, cloneVisited(visited)); ok {
		return items
	}

	return splitTopLevel(args, ',')
}

func (r *sourceRuntime) resolveArrayReferenceItems(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, expression string, visited map[string]struct{}) ([]string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, false
	}

	methodRefRe := regexp.MustCompile(`^(self|static|parent|[A-Za-z_\\][A-Za-z0-9_\\]*)::([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*\)$`)
	if match := methodRefRe.FindStringSubmatch(expression); len(match) == 3 {
		targetFQCN := resolveClassReferenceFQCN(match[1], currentFQCN, meta)
		if targetFQCN == "" {
			return nil, false
		}
		return r.resolveArrayMethodItems(ctx, targetFQCN, match[2], cloneVisited(visited)), true
	}

	constRefRe := regexp.MustCompile(`^(self|static|parent|[A-Za-z_\\][A-Za-z0-9_\\]*)::([A-Z_][A-Z0-9_]*)$`)
	if match := constRefRe.FindStringSubmatch(expression); len(match) == 3 {
		targetFQCN := resolveClassReferenceFQCN(match[1], currentFQCN, meta)
		if targetFQCN == "" {
			return nil, false
		}
		return r.resolveArrayConstantItems(ctx, targetFQCN, match[2], cloneVisited(visited)), true
	}

	return nil, false
}

func (r *sourceRuntime) resolveArrayMethodItems(ctx context.Context, fqcn, methodName string, visited map[string]struct{}) []string {
	if fqcn == "" || methodName == "" {
		return nil
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	cacheKey := fqcn + "::" + methodName + "()"
	if _, ok := visited[cacheKey]; ok {
		return nil
	}
	visited[cacheKey] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return nil
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return nil
	}
	meta := r.fileMetadata(filePath, source)
	_, methodBody, ok := extractMethodSignatureAndBody(source, methodName)
	if !ok {
		return nil
	}
	returnExpr, ok := extractTopLevelReturnExpression(methodBody)
	if !ok {
		return nil
	}
	return r.expandCallItems(ctx, fqcn, source, methodBody, meta, returnExpr, cloneVisited(visited))
}

func (r *sourceRuntime) resolveArrayConstantItems(ctx context.Context, fqcn, constName string, visited map[string]struct{}) []string {
	if fqcn == "" || constName == "" {
		return nil
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	cacheKey := fqcn + "::" + constName
	if _, ok := visited[cacheKey]; ok {
		return nil
	}
	visited[cacheKey] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return nil
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return nil
	}
	meta := r.fileMetadata(filePath, source)
	pattern := regexp.MustCompile(`(?s)(?:public|protected|private)\s+const\s+` + regexp.QuoteMeta(constName) + `\s*=\s*([^;]+);`)
	match := pattern.FindStringSubmatch(source)
	if len(match) == 2 {
		return r.expandCallItems(ctx, fqcn, source, source, meta, strings.TrimSpace(match[1]), cloneVisited(visited))
	}

	if meta.Extends != "" {
		parentFQCN := resolveTypeName(meta.Extends, meta)
		if parentFQCN != "" {
			return r.resolveArrayConstantItems(ctx, parentFQCN, constName, cloneVisited(visited))
		}
	}

	return nil
}

func findLocalArrayAssignment(body, variableName string) (string, bool) {
	if variableName == "" {
		return "", false
	}
	pattern := regexp.MustCompile(`\$` + regexp.QuoteMeta(variableName) + `\s*=\s*\[`)
	matches := pattern.FindAllStringIndex(body, -1)
	for idx := len(matches) - 1; idx >= 0; idx-- {
		start := matches[idx][1] - 1
		end := findMatchingDelimiter(body, start, '[', ']')
		if end == -1 {
			continue
		}
		return body[start+1 : end], true
	}
	return "", false
}

func findLocalStringAssignment(body, variableName string) (string, bool) {
	if variableName == "" {
		return "", false
	}
	pattern := regexp.MustCompile(`\$` + regexp.QuoteMeta(variableName) + `\s*=\s*([^;]+);`)
	matches := pattern.FindAllStringSubmatch(body, -1)
	for idx := len(matches) - 1; idx >= 0; idx-- {
		if len(matches[idx]) != 2 {
			continue
		}
		value := strings.TrimSpace(matches[idx][1])
		if value != "" {
			return value, true
		}
	}
	return "", false
}

func extractSingleStringLiteral(source string) (string, bool) {
	source = strings.TrimSpace(source)
	match := regexp.MustCompile(`^['"]([^'"]+)['"]$`).FindStringSubmatch(source)
	if len(match) != 2 {
		return "", false
	}
	value := strings.TrimSpace(match[1])
	return value, value != ""
}

func extractFirstTopLevelArgumentExpression(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false
	}

	openIdx := strings.IndexByte(source, '(')
	if openIdx == -1 {
		return source, true
	}
	closeIdx := findMatchingDelimiter(source, openIdx, '(', ')')
	if closeIdx == -1 {
		return "", false
	}
	args := splitTopLevel(source[openIdx+1:closeIdx], ',')
	if len(args) == 0 {
		return "", false
	}
	return strings.TrimSpace(args[0]), true
}

func (r *sourceRuntime) extractAllowedFilterName(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata, item string) string {
	if strings.Contains(item, "AllowedFilter::trashed(") {
		return "trashed"
	}
	literal, ok := r.extractPublicQueryName(ctx, fqcn, source, body, meta, item)
	if !ok {
		return ""
	}
	return literal
}

func (r *sourceRuntime) extractPublicQueryName(ctx context.Context, fqcn, source, body string, meta *phpFileMetadata, item string) (string, bool) {
	if literal, ok := extractSingleStringLiteral(strings.TrimSpace(item)); ok {
		return literal, true
	}

	firstArg, ok := extractFirstTopLevelArgumentExpression(item)
	if !ok {
		return "", false
	}

	return r.resolveStringExpression(ctx, fqcn, source, body, meta, firstArg, nil)
}

func (r *sourceRuntime) extractAllowedFilterSpec(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, item string) (queryBuilderFilterSpec, bool) {
	item = strings.TrimSpace(item)
	if item == "" {
		return queryBuilderFilterSpec{}, false
	}

	if literal, ok := extractSingleStringLiteral(item); ok {
		return queryBuilderFilterSpec{
			Name:         literal,
			Variant:      "plain",
			NameSegments: namePathSegments(literal),
		}, true
	}

	if resolved, ok := r.resolveQueryExpression(ctx, currentFQCN, source, body, meta, item); ok {
		return queryBuilderFilterSpec{
			Name:         resolved,
			Variant:      "resolved",
			NameSegments: namePathSegments(resolved),
		}, true
	}

	classRef, method, args, ok := extractClassCallExpression(item)
	if !ok {
		return queryBuilderFilterSpec{}, false
	}

	spec := queryBuilderFilterSpec{
		Name:         "",
		Variant:      method,
		Source:       classRef,
		NameSegments: nil,
	}
	if method == "trashed" {
		spec.Name = "trashed"
		spec.Variant = "trashed"
		spec.NameSegments = namePathSegments(spec.Name)
		return spec, true
	}

	nameExpr, ok := extractCallArgumentExpression(args, 0)
	if !ok {
		return queryBuilderFilterSpec{}, false
	}
	name, ok := r.resolveQueryExpression(ctx, currentFQCN, source, body, meta, nameExpr)
	if !ok {
		return queryBuilderFilterSpec{}, false
	}
	spec.Name = name
	spec.NameSegments = namePathSegments(name)
	if method == "exact" && len(args) > 1 {
		if columnExpr, ok := extractCallArgumentExpression(args, 1); ok {
			if column, ok := r.resolveQueryExpression(ctx, currentFQCN, source, body, meta, columnExpr); ok {
				spec.Column = column
				spec.ColumnSegments = queryFieldSegments(column)
			}
		}
	}

	return spec, true
}

func (r *sourceRuntime) extractAllowedPathSpec(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, item string) (queryBuilderPathSpec, bool) {
	item = strings.TrimSpace(item)
	if item == "" {
		return queryBuilderPathSpec{}, false
	}

	if literal, ok := extractSingleStringLiteral(item); ok {
		spec := queryBuilderPathSpec{
			Name:     strings.TrimSpace(literal),
			Variant:  "plain",
			Segments: namePathSegments(strings.TrimSpace(literal)),
		}
		if strings.HasPrefix(spec.Name, "-") {
			spec.Descending = true
			spec.Name = strings.TrimPrefix(spec.Name, "-")
			spec.Segments = namePathSegments(spec.Name)
		}
		return spec, true
	}

	if resolved, ok := r.resolveQueryExpression(ctx, currentFQCN, source, body, meta, item); ok {
		spec := queryBuilderPathSpec{
			Name:     strings.TrimSpace(resolved),
			Variant:  "resolved",
			Segments: namePathSegments(strings.TrimSpace(resolved)),
		}
		if strings.HasPrefix(spec.Name, "-") {
			spec.Descending = true
			spec.Name = strings.TrimPrefix(spec.Name, "-")
			spec.Segments = namePathSegments(spec.Name)
		}
		return spec, true
	}

	classRef, method, args, ok := extractClassCallExpression(item)
	if !ok {
		return queryBuilderPathSpec{}, false
	}

	nameExpr, ok := extractCallArgumentExpression(args, 0)
	if !ok {
		return queryBuilderPathSpec{}, false
	}
	name, ok := r.resolveQueryExpression(ctx, currentFQCN, source, body, meta, nameExpr)
	if !ok {
		return queryBuilderPathSpec{}, false
	}

	spec := queryBuilderPathSpec{
		Name:     strings.TrimSpace(name),
		Variant:  method,
		Source:   classRef,
		Segments: namePathSegments(strings.TrimSpace(name)),
	}
	if strings.HasPrefix(spec.Name, "-") {
		spec.Descending = true
		spec.Name = strings.TrimPrefix(spec.Name, "-")
		spec.Segments = namePathSegments(spec.Name)
	}
	return spec, true
}

func extractCallArgumentExpression(args []string, index int) (string, bool) {
	if index < 0 || index >= len(args) {
		return "", false
	}
	return strings.TrimSpace(args[index]), true
}

func (r *sourceRuntime) resolveQueryExpression(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", false
	}
	if literal, ok := extractSingleStringLiteral(expression); ok {
		return literal, true
	}
	return r.resolveStringExpression(ctx, currentFQCN, source, body, meta, expression, nil)
}

func extractClassCallExpression(source string) (string, string, []string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", "", nil, false
	}

	openIdx := strings.IndexByte(source, '(')
	if openIdx == -1 {
		return "", "", nil, false
	}
	closeIdx := findMatchingDelimiter(source, openIdx, '(', ')')
	if closeIdx == -1 {
		return "", "", nil, false
	}

	prefix := strings.TrimSpace(source[:openIdx])
	classRef, method, ok := strings.Cut(prefix, "::")
	if !ok {
		return "", "", nil, false
	}
	if strings.TrimSpace(classRef) == "" || strings.TrimSpace(method) == "" {
		return "", "", nil, false
	}

	args := splitTopLevel(source[openIdx+1:closeIdx], ',')
	return strings.TrimSpace(classRef), strings.TrimSpace(method), args, true
}

func extractTopLevelStringArgument(args []string, index int) (string, bool) {
	if index < 0 || index >= len(args) {
		return "", false
	}
	literal, ok := extractSingleStringLiteral(strings.TrimSpace(args[index]))
	if !ok {
		return "", false
	}
	return literal, true
}

func stableUniqueQueryBuilderFilterSpecs(specs []queryBuilderFilterSpec) []queryBuilderFilterSpec {
	seen := make(map[string]struct{}, len(specs))
	out := make([]queryBuilderFilterSpec, 0, len(specs))
	for _, spec := range specs {
		key := strings.Join([]string{
			spec.Name,
			spec.Variant,
			spec.Column,
			strings.Join(spec.NameSegments, "."),
			strings.Join(spec.ColumnSegments, "."),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, spec)
	}
	return out
}

func stableUniqueQueryBuilderPathSpecs(specs []queryBuilderPathSpec) []queryBuilderPathSpec {
	seen := make(map[string]struct{}, len(specs))
	out := make([]queryBuilderPathSpec, 0, len(specs))
	for _, spec := range specs {
		key := strings.Join([]string{
			spec.Name,
			spec.Variant,
			strconv.FormatBool(spec.Descending),
			strings.Join(spec.Segments, "."),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, spec)
	}
	return out
}

func pathSpecNames(specs []queryBuilderPathSpec) []string {
	values := make([]string, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		values = append(values, strings.TrimSpace(spec.Name))
	}
	return stableUniqueQueryNames(values)
}

func (r *sourceRuntime) resolveStringExpression(ctx context.Context, currentFQCN, source, body string, meta *phpFileMetadata, expression string, visited map[string]struct{}) (string, bool) {
	if visited == nil {
		visited = map[string]struct{}{}
	}
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", false
	}

	cacheKey := currentFQCN + "::" + expression
	if _, ok := visited[cacheKey]; ok {
		return "", false
	}
	visited[cacheKey] = struct{}{}

	if literal, ok := extractSingleStringLiteral(expression); ok {
		return literal, true
	}

	if strings.HasPrefix(expression, "$") && regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*$`).MatchString(expression) {
		if assigned, ok := findLocalStringAssignment(body, strings.TrimPrefix(expression, "$")); ok {
			return r.resolveStringExpression(ctx, currentFQCN, source, body, meta, assigned, cloneVisited(visited))
		}
	}

	constRefRe := regexp.MustCompile(`^(self|static|parent|[A-Za-z_\\][A-Za-z0-9_\\]*)::([A-Z_][A-Z0-9_]*)$`)
	if match := constRefRe.FindStringSubmatch(expression); len(match) == 3 {
		targetFQCN := resolveClassReferenceFQCN(match[1], currentFQCN, meta)
		if targetFQCN == "" {
			return "", false
		}
		return r.resolveStringConstant(ctx, targetFQCN, match[2], cloneVisited(visited))
	}

	return "", false
}

func (r *sourceRuntime) resolveStringConstant(ctx context.Context, fqcn, constName string, visited map[string]struct{}) (string, bool) {
	if fqcn == "" || constName == "" {
		return "", false
	}
	if visited == nil {
		visited = map[string]struct{}{}
	}
	cacheKey := fqcn + "::" + constName
	if _, ok := visited[cacheKey]; ok {
		return "", false
	}
	visited[cacheKey] = struct{}{}

	filePath, err := r.resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		return "", false
	}
	source, err := r.readFile(filePath)
	if err != nil {
		return "", false
	}
	meta := r.fileMetadata(filePath, source)
	pattern := regexp.MustCompile(`(?s)(?:public|protected|private)\s+const\s+` + regexp.QuoteMeta(constName) + `\s*=\s*([^;]+);`)
	match := pattern.FindStringSubmatch(source)
	if len(match) != 2 {
		if meta.Extends != "" {
			parentFQCN := resolveTypeName(meta.Extends, meta)
			if parentFQCN != "" {
				return r.resolveStringConstant(ctx, parentFQCN, constName, cloneVisited(visited))
			}
		}
		return "", false
	}

	return r.resolveStringExpression(ctx, fqcn, source, source, meta, strings.TrimSpace(match[1]), cloneVisited(visited))
}

func extractUploadedFileFieldName(body, expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", false
	}

	requestFileRe := regexp.MustCompile(`(?:\$\w+|request\s*\(\s*\))\s*->\s*file\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	if match := requestFileRe.FindStringSubmatch(expression); len(match) == 2 {
		fieldName := strings.TrimSpace(match[1])
		return fieldName, fieldName != ""
	}

	if strings.HasPrefix(expression, "$") && regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*$`).MatchString(expression) {
		return findLocalRequestFileAssignment(body, strings.TrimPrefix(expression, "$"))
	}

	return "", false
}

func mediaLibraryFieldsFromShape(shape emitter.OrderedObject) []emitter.RequestField {
	var fields []emitter.RequestField
	for _, info := range collectMediaLibraryFieldInfo(shape, "") {
		if info.Path == "" {
			continue
		}
		if info.IsArray {
			isArray := true
			fields = append(fields, emitter.RequestField{
				Location:   "files",
				Path:       info.Path,
				Kind:       "collection",
				Type:       "array",
				ItemType:   "file",
				Required:   packageBoolPtr(false),
				Optional:   packageBoolPtr(true),
				IsArray:    packageBoolPtr(isArray),
				Collection: packageBoolPtr(isArray),
				Source:     PackageSpatieLaravelMediaLibrary,
				Via:        "media-library",
			})
			continue
		}
		fields = append(fields, emitter.RequestField{
			Location:   "files",
			Path:       info.Path,
			Kind:       "file",
			Type:       "file",
			ScalarType: "string",
			Format:     "binary",
			Required:   packageBoolPtr(false),
			Optional:   packageBoolPtr(true),
			Source:     PackageSpatieLaravelMediaLibrary,
			Via:        "media-library",
		})
	}
	return mergeRequestFields(nil, fields)
}

type mediaLibraryFieldInfo struct {
	Path    string
	IsArray bool
}

func collectMediaLibraryFieldInfo(shape emitter.OrderedObject, prefix string) []mediaLibraryFieldInfo {
	if len(shape) == 0 {
		if prefix == "" {
			return nil
		}
		return []mediaLibraryFieldInfo{{Path: prefix}}
	}
	if len(shape) == 1 {
		if _, ok := shape["_item"]; ok {
			path := joinMetadataPath(prefix, "_item")
			return []mediaLibraryFieldInfo{{Path: trimCollectionPath(path), IsArray: true}}
		}
	}

	keys := make([]string, 0, len(shape))
	for key := range shape {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []mediaLibraryFieldInfo
	for _, key := range keys {
		out = append(out, collectMediaLibraryFieldInfo(shape[key], joinMetadataPath(prefix, key))...)
	}
	return out
}

func trimCollectionPath(path string) string {
	if strings.HasSuffix(path, "[]") {
		return strings.TrimSuffix(path, "[]")
	}
	return path
}

func findLocalRequestFileAssignment(body, variableName string) (string, bool) {
	if variableName == "" {
		return "", false
	}
	pattern := regexp.MustCompile(`\$` + regexp.QuoteMeta(variableName) + `\s*=\s*(?:\$\w+|request\s*\(\s*\))\s*->\s*file\s*\(\s*['"]([^'"]+)['"]\s*\)\s*;`)
	matches := pattern.FindAllStringSubmatch(body, -1)
	for idx := len(matches) - 1; idx >= 0; idx-- {
		if len(matches[idx]) != 2 {
			continue
		}
		fieldName := strings.TrimSpace(matches[idx][1])
		if fieldName != "" {
			return fieldName, true
		}
	}
	return "", false
}

func queryFieldSegments(field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil
	}
	if before, after, ok := strings.Cut(field, "."); ok && before != "" && after != "" {
		return append([]string{before}, namePathSegments(after)...)
	}
	return []string{"resource", field}
}

func fileFieldSegments(field string) []string {
	return namePathSegments(strings.TrimSpace(field))
}

func namePathSegments(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	var segments []string
	var current strings.Builder
	for i := 0; i < len(name); i++ {
		switch name[i] {
		case '.':
			appendPathSegment(&segments, current.String())
			current.Reset()
		case '[':
			appendPathSegment(&segments, current.String())
			current.Reset()
			closeOffset := strings.IndexByte(name[i:], ']')
			if closeOffset == -1 {
				continue
			}
			token := strings.TrimSpace(name[i+1 : i+closeOffset])
			if token == "" {
				token = "_item"
			}
			appendPathSegment(&segments, token)
			i += closeOffset
		default:
			current.WriteByte(name[i])
		}
	}
	appendPathSegment(&segments, current.String())
	return segments
}

func appendPathSegment(segments *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*segments = append(*segments, value)
}

func nestedOrderedObject(segments []string) emitter.OrderedObject {
	if len(segments) == 0 {
		return emitter.OrderedObject{}
	}

	root := emitter.OrderedObject{}
	current := root
	for idx, segment := range segments {
		if idx == len(segments)-1 {
			current[segment] = emitter.OrderedObject{}
			break
		}
		child := emitter.OrderedObject{}
		current[segment] = child
		current = child
	}

	return root
}

func isIdentifierChar(ch byte) bool {
	return ch == '_' ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9')
}

func isWhitespaceByte(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t'
}

func boundaryBeforeKeyword(source string, idx int) bool {
	if idx <= 0 {
		return true
	}
	return !isIdentifierChar(source[idx-1])
}

func boundaryAfterKeyword(source string, idx int) bool {
	if idx >= len(source) {
		return true
	}
	return !isIdentifierChar(source[idx])
}

func stableUniqueQueryNames(values []string) []string {
	var unique []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	slices.Sort(unique)
	return unique
}

func extractPublicStringLiterals(items []string) []string {
	var values []string
	for _, item := range items {
		if arg, ok := extractFirstTopLevelArgumentExpression(item); ok {
			if literal, ok := extractSingleStringLiteral(arg); ok {
				values = append(values, literal)
			}
		} else if literal, ok := extractSingleStringLiteral(item); ok {
			values = append(values, literal)
		}
	}
	return values
}

func ensureContentType(contentTypes []string, contentType string) []string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return contentTypes
	}
	if slices.Contains(contentTypes, contentType) {
		return contentTypes
	}

	contentTypes = append(contentTypes, contentType)
	sort.Strings(contentTypes)
	return contentTypes
}

func mergeModelAttributes(existing []emitter.Attribute, names []string, via string) []emitter.Attribute {
	if len(names) == 0 {
		return existing
	}

	seen := make(map[string]struct{}, len(existing)+len(names))
	merged := make([]emitter.Attribute, 0, len(existing)+len(names))
	for _, attribute := range existing {
		key := attribute.Name + "\x00" + attribute.Via
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, attribute)
	}

	for _, name := range names {
		key := name + "\x00" + via
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, emitter.Attribute{
			Name: name,
			Via:  via,
		})
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Name != merged[j].Name {
			return merged[i].Name < merged[j].Name
		}
		return merged[i].Via < merged[j].Via
	})

	return merged
}

func ensureRequestInfo(controller *emitter.Controller) *emitter.RequestInfo {
	if controller.Request != nil {
		return controller.Request
	}

	controller.Request = &emitter.RequestInfo{
		ContentTypes: []string{},
		Body:         emitter.OrderedObject{},
		Query:        emitter.OrderedObject{},
		Files:        emitter.OrderedObject{},
	}
	return controller.Request
}

func mergeRequestFields(existing, incoming []emitter.RequestField) []emitter.RequestField {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}

	merged := make([]emitter.RequestField, 0, len(existing)+len(incoming))
	indexByKey := make(map[string]int, len(existing)+len(incoming))

	for _, field := range existing {
		field = normalizeRequestField(field)
		key := field.Location + "\x00" + field.Path
		if _, ok := indexByKey[key]; ok {
			continue
		}
		indexByKey[key] = len(merged)
		merged = append(merged, field)
	}

	for _, field := range incoming {
		field = normalizeRequestField(field)
		key := field.Location + "\x00" + field.Path
		if idx, ok := indexByKey[key]; ok {
			merged[idx] = overlayRequestField(merged[idx], field)
			continue
		}
		indexByKey[key] = len(merged)
		merged = append(merged, field)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Location != merged[j].Location {
			return merged[i].Location < merged[j].Location
		}
		return merged[i].Path < merged[j].Path
	})
	return merged
}

func normalizeRequestField(field emitter.RequestField) emitter.RequestField {
	field.Path = strings.TrimSpace(field.Path)
	field.Kind = strings.TrimSpace(field.Kind)
	field.Type = strings.TrimSpace(field.Type)
	field.ScalarType = strings.TrimSpace(field.ScalarType)
	field.Format = strings.TrimSpace(field.Format)
	field.ItemType = strings.TrimSpace(field.ItemType)
	field.Source = strings.TrimSpace(field.Source)
	field.Via = strings.TrimSpace(field.Via)
	field.Wrappers = stableUniqueQueryNames(field.Wrappers)
	field.AllowedValues = stableUniqueQueryNames(field.AllowedValues)
	return field
}

func overlayRequestField(base, incoming emitter.RequestField) emitter.RequestField {
	base.Kind = firstNonEmptyString(incoming.Kind, base.Kind)
	base.Type = firstNonEmptyString(incoming.Type, base.Type)
	base.ScalarType = firstNonEmptyString(incoming.ScalarType, base.ScalarType)
	base.Format = firstNonEmptyString(incoming.Format, base.Format)
	base.ItemType = firstNonEmptyString(incoming.ItemType, base.ItemType)
	base.Source = firstNonEmptyString(incoming.Source, base.Source)
	base.Via = firstNonEmptyString(incoming.Via, base.Via)
	base.Wrappers = stableUniqueQueryNames(append(base.Wrappers, incoming.Wrappers...))
	base.AllowedValues = stableUniqueQueryNames(append(base.AllowedValues, incoming.AllowedValues...))
	base.Required = mergeBoolField(base.Required, incoming.Required)
	base.Optional = mergeBoolField(base.Optional, incoming.Optional)
	base.Nullable = mergeBoolField(base.Nullable, incoming.Nullable)
	base.IsArray = mergeBoolField(base.IsArray, incoming.IsArray)
	base.Collection = mergeBoolField(base.Collection, incoming.Collection)
	return base
}

func mergeBoolField(base, incoming *bool) *bool {
	if incoming != nil {
		value := *incoming
		return &value
	}
	if base != nil {
		value := *base
		return &value
	}
	return nil
}

func joinMetadataPath(prefix, key string) string {
	key = strings.TrimSpace(key)
	switch key {
	case "", "_item", "*":
		if prefix == "" {
			return ""
		}
		return prefix + "[]"
	}
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func hasWrapper(wrappers []string, name string) bool {
	for _, wrapper := range wrappers {
		if strings.EqualFold(strings.TrimSpace(wrapper), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func packageBoolPtr(value bool) *bool {
	return &value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mergeOrderedObjects(dst, src emitter.OrderedObject) emitter.OrderedObject {
	if dst == nil {
		dst = emitter.OrderedObject{}
	}

	for key, child := range src {
		existing, ok := dst[key]
		if !ok {
			dst[key] = cloneOrderedObject(child)
			continue
		}
		dst[key] = mergeOrderedObjects(existing, child)
	}

	return dst
}

func cloneOrderedObject(source emitter.OrderedObject) emitter.OrderedObject {
	if source == nil {
		return emitter.OrderedObject{}
	}

	cloned := make(emitter.OrderedObject, len(source))
	for key, child := range source {
		cloned[key] = cloneOrderedObject(child)
	}
	return cloned
}

func cloneVisited(visited map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(visited))
	for key := range visited {
		cloned[key] = struct{}{}
	}
	return cloned
}

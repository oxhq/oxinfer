// Package matchers provides tree-sitter query definitions for Laravel pattern detection.
package matchers

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// QueryDefinition represents a tree-sitter query with metadata.
type QueryDefinition struct {
	Name        string
	Description string
	Pattern     string
	Confidence  float64
}

// HTTPStatusQueries contains tree-sitter queries for HTTP status detection.
var HTTPStatusQueries = []QueryDefinition{
    {
        Name:        "response_direct_status",
        Description: "Detect response(data, status) direct calls",
        Pattern: `
(function_call_expression
  function: (name) @function (#eq? @function "response")
  arguments: (arguments
    (argument)
    (argument (integer) @status)))
`,
        Confidence: 0.90,
    },
    {
        Name:        "response_status_method",
        Description: "Detect ->status() method calls on response objects",
        Pattern: `
(member_call_expression
  object: (function_call_expression
    function: (name) @function (#eq? @function "response"))
  name: (name) @method (#eq? @method "status")
  arguments: (arguments
    (argument (integer) @status)))
`,
        Confidence: 0.95,
    },
    {
        Name:        "response_json_with_status",
        Description: "Detect response()->json(data, status) patterns",
        Pattern: `
(member_call_expression
  object: (member_call_expression
    object: (function_call_expression
      function: (name) @function (#eq? @function "response"))
    name: (name) @method (#eq? @method "json"))
  arguments: (arguments
    (argument)
    (argument (integer) @status)))
`,
        Confidence: 0.95,
    },
    {
        Name:        "return_response_json_status",
        Description: "Detect return response()->json(data, status) patterns",
        Pattern: `
(return_statement
  (member_call_expression
    object: (member_call_expression
      object: (function_call_expression
        function: (name) @function (#eq? @function "response"))
      name: (name) @method (#eq? @method "json"))
    arguments: (arguments
      (argument)
      (argument (integer) @status))))
`,
        Confidence: 0.95,
    },
    {
        Name:        "variable_response_status",
        Description: "Detect status assignment to response variables",
        Pattern: `
(assignment_expression
  left: (variable_name)
  right: (member_call_expression
    object: (function_call_expression
      function: (name) @function (#eq? @function "response"))
    name: (name) @method (#eq? @method "status")
    arguments: (arguments
      (argument (integer) @status))))
`,
        Confidence: 0.85,
    },
    {
        Name:        "abort_call",
        Description: "Detect abort() calls with status codes",
        Pattern: `
(function_call_expression
  function: (name) @function (#eq? @function "abort")
  arguments: (arguments
    (argument (integer) @status)))
`,
        Confidence: 1.0,
    },
}

// RequestUsageQueries contains tree-sitter queries for request usage detection.
var RequestUsageQueries = []QueryDefinition{
	{
		Name:        "request_all",
		Description: "Detect $request->all() calls",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "all"))
`,
		Confidence: 0.90,
	},
	{
		Name:        "request_json",
		Description: "Detect $request->json() calls",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "json"))
`,
		Confidence: 0.95,
	},
	{
		Name:        "request_file",
		Description: "Detect $request->file() calls",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "file"))
`,
		Confidence: 0.95,
	},
	{
		Name:        "request_has_file",
		Description: "Detect $request->hasFile() calls",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "hasFile"))
`,
		Confidence: 0.95,
	},
	{
		Name:        "request_input",
		Description: "Detect $request->input() calls with parameters",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "input")
  arguments: (arguments
    (argument (string) @parameter)))
`,
		Confidence: 0.88,
	},
    {
        Name:        "request_only",
        Description: "Detect $request->only() calls with parameter arrays",
        Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "only")
  arguments: (arguments
    (argument (array_creation_expression) @arr)))
`,
        Confidence: 0.87,
    },
    {
        Name:        "request_except",
        Description: "Detect $request->except() calls with parameter arrays",
        Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "except")
  arguments: (arguments
    (argument (array_creation_expression) @arr)))
`,
        Confidence: 0.85,
    },
	{
		Name:        "request_validate",
		Description: "Detect $request->validate() calls",
		Pattern: `
(member_call_expression
  object: (variable_name) @request (#match? @request "\\$request")
  name: (name) @method (#eq? @method "validate"))
`,
		Confidence: 0.80,
	},
}

// ResourceUsageQueries contains tree-sitter queries for Laravel Resource detection.
var ResourceUsageQueries = []QueryDefinition{
    {
        Name:        "resource_collection_static",
        Description: "Detect ResourceClass::collection() static calls",
        Pattern: `
(scoped_call_expression
  scope: (_) @class (#match? @class ".*Resource$")
  name: (name) @method (#eq? @method "collection"))
`,
        Confidence: 1.0,
    },
    {
        Name:        "resource_make_static",
        Description: "Detect ResourceClass::make() static calls",
        Pattern: `
(scoped_call_expression
  scope: (_) @class (#match? @class ".*Resource$")
  name: (name) @method (#eq? @method "make"))
`,
        Confidence: 0.95,
    },
    {
        Name:        "new_resource_instantiation",
        Description: "Detect new ResourceClass() instantiation",
        Pattern: `
(object_creation_expression
  (_) @class (#match? @class ".*Resource$"))
`,
        Confidence: 0.95,
    },
    {
        Name:        "return_new_resource",
        Description: "Detect return new ResourceClass() patterns",
        Pattern: `
(return_statement
  (object_creation_expression
    (_) @class (#match? @class ".*Resource$")))
`,
        Confidence: 0.98,
    },
	{
		Name:        "return_resource_collection",
		Description: "Detect return ResourceClass::collection() patterns",
		Pattern: `
(return_statement
  (scoped_call_expression
    scope: (name) @class (#match? @class ".*Resource$")
    name: (name) @method (#eq? @method "collection")))
`,
		Confidence: 1.0,
	},
	{
		Name:        "variable_resource_assignment",
		Description: "Detect variable assignment to resource instances",
		Pattern: `
(assignment_expression
  left: (variable_name)
  right: (object_creation_expression
    (_) @class (#match? @class ".*Resource$")))
`,
		Confidence: 0.90,
	},
}

// PivotUsageQueries contains tree-sitter queries for Laravel pivot relationship detection.
var PivotUsageQueries = []QueryDefinition{
	{
		Name:        "with_pivot_method",
		Description: "Detect ->withPivot() method calls with field arguments",
		Pattern: `
(member_call_expression
  name: (name) @method (#eq? @method "withPivot")
  arguments: (arguments) @args)
`,
		Confidence: 1.0,
	},
	{
		Name:        "with_timestamps_method",
		Description: "Detect ->withTimestamps() method calls on relationships",
		Pattern: `
(member_call_expression
  name: (name) @method (#eq? @method "withTimestamps"))
`,
		Confidence: 1.0,
	},
	{
		Name:        "pivot_accessor_alias",
		Description: "Detect ->as() method calls for pivot table accessor naming",
		Pattern: `
(member_call_expression
  name: (name) @method (#eq? @method "as")
  arguments: (arguments
    (argument (string) @alias)))
`,
		Confidence: 0.95,
	},
	{
		Name:        "chained_pivot_methods",
		Description: "Detect chained pivot method calls on relationships",
		Pattern: `
(member_call_expression
  object: (member_call_expression
    name: (name) @first_method (#match? @first_method "^(belongsToMany|withPivot|withTimestamps|as)$"))
  name: (name) @second_method (#match? @second_method "^(withPivot|withTimestamps|as)$"))
`,
		Confidence: 0.95,
	},
	{
		Name:        "belongs_to_many_with_pivot",
		Description: "Detect belongsToMany relationships with chained pivot methods",
		Pattern: `
(member_call_expression
  object: (member_call_expression
    name: (name) @relation_method (#eq? @relation_method "belongsToMany"))
  name: (name) @pivot_method (#match? @pivot_method "^(withPivot|withTimestamps|as)$"))
`,
		Confidence: 1.0,
	},
}

// AttributeUsageQueries contains tree-sitter queries for Laravel attribute accessor detection.
var AttributeUsageQueries = []QueryDefinition{
	{
		Name:        "modern_attribute_method",
		Description: "Detect modern attribute accessor methods with return type Attribute",
		Pattern: `
(method_declaration
  name: (name) @method_name
  return_type: (named_type (name) @return_type (#eq? @return_type "Attribute")))
`,
		Confidence: 1.0,
	},
	{
		Name:        "legacy_get_attribute",
		Description: "Detect legacy get{Name}Attribute() accessor method patterns",
		Pattern: `
(method_declaration
  name: (name) @method_name (#match? @method_name "^get[A-Z][a-zA-Z0-9]*Attribute$")
  parameters: (formal_parameters)
  body: (compound_statement))
`,
		Confidence: 0.90,
	},
	{
		Name:        "legacy_set_attribute",
		Description: "Detect legacy set{Name}Attribute() mutator method patterns",
		Pattern: `
(method_declaration
  name: (name) @method_name (#match? @method_name "^set[A-Z][a-zA-Z0-9]*Attribute$")
  parameters: (formal_parameters
    (simple_parameter))
  body: (compound_statement))
`,
		Confidence: 0.90,
	},
	{
		Name:        "attribute_with_get",
		Description: "Detect Attribute::make()->get() method chains",
		Pattern: `
(member_call_expression
  object: (scoped_call_expression
    scope: (name) @class_name (#eq? @class_name "Attribute")
    name: (name) @method (#eq? @method "make"))
  name: (name) @chain_method (#eq? @chain_method "get")
  arguments: (arguments) @get_args)
`,
		Confidence: 0.95,
	},
	{
		Name:        "attribute_with_set",
		Description: "Detect Attribute::make()->set() method chains",
		Pattern: `
(member_call_expression
  object: (scoped_call_expression
    scope: (name) @class_name (#eq? @class_name "Attribute")
    name: (name) @method (#eq? @method "make"))
  name: (name) @chain_method (#eq? @chain_method "set")
  arguments: (arguments) @set_args)
`,
		Confidence: 0.95,
	},
	{
		Name:        "attribute_with_cast",
		Description: "Detect casted attributes in model $casts property",
		Pattern: `
(property_declaration
  (property_element
    (variable_name) @property (#eq? @property "$casts")))
`,
		Confidence: 0.85,
	},
}

// ScopeUsageQueries contains tree-sitter queries for Laravel query scope detection.
var ScopeUsageQueries = []QueryDefinition{
	{
		Name:        "local_scope_definition",
		Description: "Detect local scope method definitions in models (scopeXxx methods)",
		Pattern: `
(method_declaration
  name: (name) @method_name (#match? @method_name "^scope[A-Z][a-zA-Z0-9]*$")
  body: (compound_statement) @body)
`,
		Confidence: 0.95,
	},
	{
		Name:        "scope_method_call_on_query",
		Description: "Detect scope method calls on query builder instances (->scopeXxx())",
		Pattern: `
(member_call_expression
  object: (variable_name) @query_var
  name: (name) @scope_method (#match? @scope_method "^scope[A-Z][a-zA-Z0-9]*$")
  arguments: (arguments) @args)
`,
		Confidence: 0.95,
	},
	{
		Name:        "scope_method_call_on_model",
		Description: "Detect scope method calls on model classes (Model::scopeXxx())",
		Pattern: `
(scoped_call_expression
  scope: (name) @model_class
  name: (name) @scope_method (#match? @scope_method "^scope[A-Z][a-zA-Z0-9]*$")
  arguments: (arguments) @args)
`,
		Confidence: 0.90,
	},
	{
		Name:        "scope_without_prefix_on_query",
		Description: "Detect scope calls without 'scope' prefix on query builders (->active(), ->published())",
		Pattern: `
(member_call_expression
  object: (member_call_expression
    name: (name) @query_method (#eq? @query_method "query"))
  name: (name) @scope_name
  arguments: (arguments) @args)
`,
		Confidence: 0.70,
	},
	{
		Name:        "scope_without_prefix_on_model_query",
		Description: "Detect scope calls on Model::query() chains",
		Pattern: `
(member_call_expression
  object: (scoped_call_expression
    scope: (name) @model_class
    name: (name) @query_method (#eq? @query_method "query"))
  name: (name) @scope_name
  arguments: (arguments) @args)
`,
		Confidence: 0.85,
	},
	{
		Name:        "global_scope_class_definition",
		Description: "Detect global scope class definitions implementing Scope interface",
		Pattern: `
(class_declaration
  name: (name) @class_name (#match? @class_name ".*Scope$"))
`,
		Confidence: 0.85,
	},
	{
		Name:        "global_scope_apply_method",
		Description: "Detect apply method in global scope classes",
		Pattern: `
(method_declaration
  name: (name) @method_name (#eq? @method_name "apply")
  body: (compound_statement) @body)
`,
		Confidence: 0.90,
	},
	{
		Name:        "scope_registration_in_boot",
		Description: "Detect scope registration in model boot methods",
		Pattern: `
(member_call_expression
  object: (scoped_call_expression
    scope: (name) @static_class (#eq? @static_class "static")
    name: (name) @method (#eq? @method "addGlobalScope"))
  arguments: (arguments
    (argument) @scope_arg))
`,
		Confidence: 0.90,
	},
	{
		Name:        "has_many_with_scope",
		Description: "Detect scope usage in relationship definitions",
		Pattern: `
(member_call_expression
  object: (member_call_expression
    name: (name) @relation_method)
  name: (name) @scope_method
  arguments: (arguments) @args)
`,
		Confidence: 0.70,
	},
	{
		Name:        "whereable_scope_pattern",
		Description: "Detect dynamic scope patterns like whereActive(), wherePublished()",
		Pattern: `
(member_call_expression
  name: (name) @method_name)
`,
		Confidence: 0.50,
	},
}

// QueryCompiler manages compilation and caching of tree-sitter queries.
type QueryCompiler struct {
	language *sitter.Language
	cache    map[string]*sitter.Query
}

// NewQueryCompiler creates a new query compiler for the given language.
func NewQueryCompiler(language *sitter.Language) *QueryCompiler {
	return &QueryCompiler{
		language: language,
		cache:    make(map[string]*sitter.Query),
	}
}

// CompileQuery compiles a query definition into a tree-sitter query.
func (qc *QueryCompiler) CompileQuery(def QueryDefinition) (*sitter.Query, error) {
	// Check cache first
	if cached, exists := qc.cache[def.Name]; exists {
		return cached, nil
	}

	// Compile the query
	query, err := sitter.NewQuery([]byte(def.Pattern), qc.language)
	if err != nil {
		return nil, fmt.Errorf("failed to compile query '%s': %w", def.Name, err)
	}

	// Cache for reuse
	qc.cache[def.Name] = query
	return query, nil
}

// CompileQueries compiles multiple query definitions.
func (qc *QueryCompiler) CompileQueries(definitions []QueryDefinition) ([]*sitter.Query, error) {
	queries := make([]*sitter.Query, 0, len(definitions))
	
	for _, def := range definitions {
		query, err := qc.CompileQuery(def)
		if err != nil {
			return nil, fmt.Errorf("failed to compile queries: %w", err)
		}
		queries = append(queries, query)
	}
	
	return queries, nil
}

// GetCachedQuery returns a cached query by name.
func (qc *QueryCompiler) GetCachedQuery(name string) (*sitter.Query, bool) {
	query, exists := qc.cache[name]
	return query, exists
}

// ClearCache clears the query cache.
func (qc *QueryCompiler) ClearCache() {
	for name, query := range qc.cache {
		query.Close()
		delete(qc.cache, name)
	}
}

// Close releases resources held by the compiler.
func (qc *QueryCompiler) Close() error {
	qc.ClearCache()
	return nil
}

// GetQueryDefinition returns a query definition by name from the given set.
func GetQueryDefinition(definitions []QueryDefinition, name string) (QueryDefinition, bool) {
	for _, def := range definitions {
		if def.Name == name {
			return def, true
		}
	}
	return QueryDefinition{}, false
}

// GetHTTPStatusQuery returns a specific HTTP status query by name.
func GetHTTPStatusQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(HTTPStatusQueries, name)
}

// GetRequestUsageQuery returns a specific request usage query by name.
func GetRequestUsageQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(RequestUsageQueries, name)
}

// GetResourceUsageQuery returns a specific resource usage query by name.
func GetResourceUsageQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(ResourceUsageQueries, name)
}

// GetPivotUsageQuery returns a specific pivot usage query by name.
func GetPivotUsageQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(PivotUsageQueries, name)
}

// GetAttributeUsageQuery returns a specific attribute usage query by name.
func GetAttributeUsageQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(AttributeUsageQueries, name)
}

// GetScopeUsageQuery returns a specific scope usage query by name.
func GetScopeUsageQuery(name string) (QueryDefinition, bool) {
	return GetQueryDefinition(ScopeUsageQueries, name)
}

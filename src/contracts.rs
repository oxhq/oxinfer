use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;

use anyhow::{Result, bail};
use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::OXINFER_VERSION;
use crate::manifest::Manifest;
use crate::model::{ControllerMethod, ModelFacts, ModelRelationshipFact, ResourceUsageFact};
use crate::pipeline::PipelineResult;
use crate::source_index::{
    SourceClass, SourceIndex, extract_balanced_region, extract_return_array, split_top_level,
    split_top_level_key_value, strip_php_string,
};

pub const CONTRACT_VERSION: &str = "oxcribe.oxinfer.v2";
const ACTION_KIND_CONTROLLER_METHOD: &str = "controller_method";
const ACTION_KIND_INVOKABLE_CONTROLLER: &str = "invokable_controller";
const ACTION_KIND_CLOSURE: &str = "closure";
const RESPONSE_STATUS_OK: &str = "ok";
const RESPONSE_STATUS_PARTIAL: &str = "partial";

const MATCH_STATUS_MATCHED: &str = "matched";
const MATCH_STATUS_RUNTIME_ONLY: &str = "runtime_only";
const MATCH_STATUS_UNSUPPORTED: &str = "unsupported";
const MATCH_STATUS_MISSING_STATIC: &str = "missing_static";

const SEVERITY_INFO: &str = "info";
const SEVERITY_WARN: &str = "warn";

const SCOPE_ROUTE: &str = "route";
const SCOPE_ACTION: &str = "action";
const SCOPE_GLOBAL: &str = "global";

const REASON_CODE_CLOSURE_ACTION: &str = "closure_action";
const REASON_CODE_UNKNOWN_ACTION: &str = "unknown_action";
const REASON_CODE_MISSING_STATIC_ACTION: &str = "missing_static_action";

const DIAGNOSTIC_CODE_ROUTE_RUNTIME_ONLY_CLOSURE: &str = "route.runtime_only.closure";
const DIAGNOSTIC_CODE_ROUTE_ACTION_UNSUPPORTED: &str = "route.action.unsupported";
const DIAGNOSTIC_CODE_ROUTE_ACTION_MISSING_STATIC: &str = "route.action.missing_static";
const DIAGNOSTIC_CODE_ANALYSIS_STATIC_PARTIAL: &str = "analysis.static.partial";

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AnalysisRequest {
    #[serde(rename = "contractVersion")]
    pub contract_version: String,
    #[serde(rename = "requestId")]
    pub request_id: String,
    #[serde(rename = "runtimeFingerprint")]
    pub runtime_fingerprint: String,
    pub manifest: Manifest,
    pub runtime: RuntimeSnapshot,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RuntimeSnapshot {
    pub app: RuntimeApp,
    #[serde(default)]
    pub routes: Vec<RuntimeRoute>,
    #[serde(default)]
    pub packages: Vec<RuntimePackage>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RuntimeApp {
    #[serde(rename = "basePath")]
    pub base_path: String,
    #[serde(rename = "laravelVersion")]
    pub laravel_version: String,
    #[serde(rename = "phpVersion")]
    pub php_version: String,
    #[serde(rename = "appEnv")]
    pub app_env: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RuntimeRoute {
    #[serde(rename = "routeId")]
    pub route_id: String,
    #[serde(default)]
    pub methods: Vec<String>,
    pub uri: String,
    pub domain: Option<String>,
    pub name: Option<String>,
    pub prefix: Option<String>,
    #[serde(default)]
    pub middleware: Vec<String>,
    #[serde(rename = "where", default)]
    pub where_map: BTreeMap<String, Value>,
    #[serde(default)]
    pub defaults: BTreeMap<String, Value>,
    #[serde(default)]
    pub bindings: Vec<RouteBinding>,
    pub action: RouteAction,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RuntimePackage {
    pub name: String,
    pub version: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RouteBinding {
    pub parameter: String,
    pub kind: String,
    #[serde(rename = "targetFqcn")]
    pub target_fqcn: Option<String>,
    #[serde(rename = "isImplicit")]
    pub is_implicit: bool,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RouteAction {
    pub kind: String,
    pub fqcn: Option<String>,
    pub method: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct AnalysisResponse {
    #[serde(rename = "contractVersion")]
    pub contract_version: String,
    #[serde(rename = "requestId")]
    pub request_id: String,
    #[serde(rename = "runtimeFingerprint")]
    pub runtime_fingerprint: String,
    pub status: String,
    pub meta: ResponseMeta,
    pub delta: ContractDelta,
    #[serde(rename = "routeMatches")]
    pub route_matches: Vec<RouteMatch>,
    pub diagnostics: Vec<Diagnostic>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ResponseMeta {
    #[serde(rename = "oxinferVersion")]
    pub oxinfer_version: String,
    pub partial: bool,
    pub stats: ContractStats,
    #[serde(rename = "diagnosticCounts")]
    pub diagnostic_counts: DiagnosticCounts,
}

#[derive(Debug, Clone, Serialize)]
pub struct DiagnosticCounts {
    pub info: usize,
    pub warn: usize,
    pub error: usize,
}

#[derive(Debug, Clone, Serialize)]
pub struct RouteMatch {
    #[serde(rename = "routeId")]
    pub route_id: String,
    #[serde(rename = "actionKind")]
    pub action_kind: String,
    #[serde(rename = "actionKey", skip_serializing_if = "Option::is_none")]
    pub action_key: Option<String>,
    #[serde(rename = "matchStatus")]
    pub match_status: String,
    #[serde(rename = "reasonCode", skip_serializing_if = "Option::is_none")]
    pub reason_code: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct Diagnostic {
    pub code: String,
    pub severity: String,
    pub scope: String,
    pub message: String,
    #[serde(rename = "routeId", skip_serializing_if = "Option::is_none")]
    pub route_id: Option<String>,
    #[serde(rename = "actionKey", skip_serializing_if = "Option::is_none")]
    pub action_key: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractDelta {
    pub meta: ContractDeltaMeta,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub controllers: Vec<ContractController>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub models: Vec<ContractModel>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub resources: Vec<ContractResource>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub polymorphic: Vec<ContractPolymorphicGroup>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub broadcast: Vec<ContractBroadcast>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractDeltaMeta {
    pub partial: bool,
    pub stats: ContractStats,
    pub version: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractStats {
    #[serde(rename = "filesParsed")]
    pub files_parsed: usize,
    pub skipped: usize,
    #[serde(rename = "durationMs")]
    pub duration_ms: u128,
    #[serde(rename = "assemblerStats")]
    pub assembler_stats: AssemblerStats,
}

#[derive(Debug, Clone, Serialize)]
pub struct AssemblerStats {
    #[serde(rename = "unresolvableMatches")]
    pub unresolvable_matches: usize,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractController {
    pub fqcn: String,
    pub method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub http: Option<ContractHttpInfo>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request: Option<ContractRequest>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub responses: Vec<ContractResponse>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub authorization: Vec<ContractAuthorization>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub resources: Vec<ContractControllerResource>,
    #[serde(default, rename = "scopesUsed", skip_serializing_if = "Vec::is_empty")]
    pub scopes_used: Vec<ContractScopeUsed>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub polymorphic: Vec<ContractPolymorphicRelation>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractHttpInfo {
    pub status: u16,
    pub explicit: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractRequest {
    #[serde(
        default,
        rename = "contentTypes",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub content_types: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub body: Option<ShapeTree>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query: Option<ShapeTree>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub files: Option<ShapeTree>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub fields: Vec<ContractRequestField>,
}

#[derive(Debug, Clone, Serialize, Default)]
#[serde(transparent)]
pub struct ShapeTree(pub BTreeMap<String, ShapeTree>);

#[derive(Debug, Clone, Serialize)]
pub struct ContractRequestField {
    pub location: String,
    pub path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kind: Option<String>,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub type_name: Option<String>,
    #[serde(rename = "scalarType", skip_serializing_if = "Option::is_none")]
    pub scalar_type: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
    #[serde(rename = "itemType", skip_serializing_if = "Option::is_none")]
    pub item_type: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub wrappers: Vec<String>,
    #[serde(
        rename = "allowedValues",
        default,
        skip_serializing_if = "Vec::is_empty"
    )]
    pub allowed_values: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub required: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub optional: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub nullable: Option<bool>,
    #[serde(rename = "isArray", skip_serializing_if = "Option::is_none")]
    pub is_array: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub collection: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub via: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractResponse {
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub explicit: Option<bool>,
    #[serde(rename = "contentType", skip_serializing_if = "Option::is_none")]
    pub content_type: Option<String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    #[serde(rename = "bodySchema", skip_serializing_if = "Option::is_none")]
    pub body_schema: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub redirect: Option<ContractRedirect>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub download: Option<ContractDownload>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub inertia: Option<ContractInertia>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub via: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractRedirect {
    #[serde(rename = "targetKind")]
    pub target_kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub target: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractDownload {
    pub disposition: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub filename: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractInertia {
    pub component: String,
    #[serde(rename = "propsSchema", skip_serializing_if = "Option::is_none")]
    pub props_schema: Option<Value>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractAuthorization {
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ability: Option<String>,
    #[serde(rename = "targetKind", skip_serializing_if = "Option::is_none")]
    pub target_kind: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub target: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parameter: Option<String>,
    pub source: String,
    pub resolution: String,
    #[serde(rename = "enforcesFailureResponse")]
    pub enforces_failure_response: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractControllerResource {
    pub class: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub fqcn: Option<String>,
    pub collection: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractScopeUsed {
    pub on: String,
    pub name: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub args: Vec<Value>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractPolymorphicRelation {
    pub relation: String,
    #[serde(rename = "type")]
    pub relation_type: String,
    #[serde(rename = "morphType", skip_serializing_if = "Option::is_none")]
    pub morph_type: Option<String>,
    #[serde(rename = "morphId", skip_serializing_if = "Option::is_none")]
    pub morph_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub discriminator: Option<ContractDiscriminator>,
    #[serde(
        default,
        rename = "relatedModels",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub related_models: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractDiscriminator {
    #[serde(rename = "propertyName")]
    pub property_name: String,
    pub mapping: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractModel {
    pub fqcn: String,
    #[serde(default, rename = "withPivot", skip_serializing_if = "Vec::is_empty")]
    pub with_pivot: Vec<ContractPivot>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub attributes: Vec<ContractAttribute>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub polymorphic: Vec<ContractPolymorphicRelation>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractPivot {
    pub relation: String,
    pub columns: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub alias: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timestamps: Option<bool>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractAttribute {
    pub name: String,
    pub via: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractResource {
    pub fqcn: String,
    pub class: String,
    pub schema: Value,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractPolymorphicGroup {
    pub parent: String,
    pub morph: ContractMorph,
    pub discriminator: ContractDiscriminator,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractMorph {
    pub key: String,
    #[serde(rename = "typeColumn")]
    pub type_column: String,
    #[serde(rename = "idColumn")]
    pub id_column: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ContractBroadcast {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub file: Option<String>,
    pub channel: String,
    pub params: Vec<String>,
    pub visibility: String,
    #[serde(rename = "payloadLiteral", skip_serializing_if = "Option::is_none")]
    pub payload_literal: Option<bool>,
}

#[derive(Debug, Clone, Default)]
struct ParsedRuleField {
    location: String,
    path: String,
    kind: Option<String>,
    type_name: Option<String>,
    scalar_type: Option<String>,
    format: Option<String>,
    item_type: Option<String>,
    wrappers: Vec<String>,
    allowed_values: Vec<String>,
    required: Option<bool>,
    optional: Option<bool>,
    nullable: Option<bool>,
    is_array: Option<bool>,
    collection: Option<bool>,
    source: Option<String>,
    via: Option<String>,
}

#[derive(Debug, Clone, Default)]
struct ModelSchemaHint {
    fillable: BTreeSet<String>,
    hidden: BTreeSet<String>,
    casts: BTreeMap<String, String>,
}

#[derive(Debug, Clone)]
struct JsonResponseCall {
    payload: String,
    status: Option<u16>,
}

#[derive(Debug, Clone, Default)]
struct ControllerSourceState {
    parameters: BTreeMap<String, String>,
    assignments: BTreeMap<String, String>,
}

impl ShapeTree {
    fn insert_path(&mut self, path: &str) {
        if path.is_empty() || path == "*" {
            return;
        }

        let mut current = self;
        for segment in path.split('.').filter(|segment| !segment.is_empty()) {
            current = current.0.entry(segment.to_string()).or_default();
        }
    }

    fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

impl AnalysisRequest {
    pub fn normalize(&mut self) {
        for route in &mut self.runtime.routes {
            if route.action.kind == ACTION_KIND_INVOKABLE_CONTROLLER
                && route.action.method.is_none()
            {
                route.action.method = Some("__invoke".to_string());
            }
        }
    }
}

impl RouteAction {
    pub fn action_key(&self) -> Option<String> {
        match self.kind.as_str() {
            ACTION_KIND_CONTROLLER_METHOD | ACTION_KIND_INVOKABLE_CONTROLLER => {
                let fqcn = self.fqcn.as_ref()?;
                let method = self.method.as_ref()?;
                if fqcn.is_empty() || method.is_empty() {
                    return None;
                }
                Some(format!("{fqcn}::{method}"))
            }
            _ => None,
        }
    }
}

pub fn load_analysis_request_from_slice(
    data: &[u8],
    source_path: Option<&Path>,
) -> Result<AnalysisRequest> {
    let mut request: AnalysisRequest = serde_json::from_slice(data)?;
    if request.contract_version != CONTRACT_VERSION {
        bail!("analysis request validation failed: unsupported contractVersion");
    }
    if let Some(path) = source_path {
        request.manifest.resolve_paths(path);
    }
    request.normalize();
    validate_request_business_rules(&request)?;
    Ok(request)
}

pub fn build_analysis_response(
    request: &AnalysisRequest,
    result: &PipelineResult,
) -> AnalysisResponse {
    let source_index = SourceIndex::build(result);
    let runtime_routes = collect_runtime_routes_by_action(request);
    let static_actions = collect_static_controllers(result, &source_index, &runtime_routes);
    let models = collect_models(result, &source_index);
    let resources = collect_resources(result, &source_index);
    let polymorphic = collect_top_level_polymorphic(result);
    let broadcast = collect_broadcast(result);

    let mut route_matches = Vec::with_capacity(request.runtime.routes.len());
    let mut diagnostics = Vec::new();
    let mut matched_action_keys = BTreeSet::new();
    let mut partial = result.partial;

    if result.partial {
        diagnostics.push(Diagnostic {
            code: DIAGNOSTIC_CODE_ANALYSIS_STATIC_PARTIAL.to_string(),
            severity: SEVERITY_WARN.to_string(),
            scope: SCOPE_GLOBAL.to_string(),
            message: "static analysis completed with partial results before the runtime join"
                .to_string(),
            route_id: None,
            action_key: None,
        });
    }

    for route in &request.runtime.routes {
        let mut route_match = RouteMatch {
            route_id: route.route_id.clone(),
            action_kind: route.action.kind.clone(),
            action_key: None,
            match_status: MATCH_STATUS_UNSUPPORTED.to_string(),
            reason_code: Some(REASON_CODE_UNKNOWN_ACTION.to_string()),
        };

        match route.action.kind.as_str() {
            ACTION_KIND_CONTROLLER_METHOD | ACTION_KIND_INVOKABLE_CONTROLLER => {
                if let Some(action_key) = route.action.action_key() {
                    route_match.action_key = Some(action_key.clone());
                    if static_actions.contains_key(&action_key) {
                        route_match.match_status = MATCH_STATUS_MATCHED.to_string();
                        route_match.reason_code = None;
                        matched_action_keys.insert(action_key);
                    } else {
                        route_match.match_status = MATCH_STATUS_MISSING_STATIC.to_string();
                        route_match.reason_code =
                            Some(REASON_CODE_MISSING_STATIC_ACTION.to_string());
                        diagnostics.push(Diagnostic {
                            code: DIAGNOSTIC_CODE_ROUTE_ACTION_MISSING_STATIC.to_string(),
                            severity: SEVERITY_WARN.to_string(),
                            scope: SCOPE_ACTION.to_string(),
                            message:
                                "runtime route action has no matching static controller analysis"
                                    .to_string(),
                            route_id: Some(route.route_id.clone()),
                            action_key: route_match.action_key.clone(),
                        });
                        partial = true;
                    }
                } else {
                    diagnostics.push(Diagnostic {
                        code: DIAGNOSTIC_CODE_ROUTE_ACTION_UNSUPPORTED.to_string(),
                        severity: SEVERITY_WARN.to_string(),
                        scope: SCOPE_ROUTE.to_string(),
                        message: "runtime route action is invalid for static join".to_string(),
                        route_id: Some(route.route_id.clone()),
                        action_key: None,
                    });
                    partial = true;
                }
            }
            ACTION_KIND_CLOSURE => {
                route_match.match_status = MATCH_STATUS_RUNTIME_ONLY.to_string();
                route_match.reason_code = Some(REASON_CODE_CLOSURE_ACTION.to_string());
                diagnostics.push(Diagnostic {
                    code: DIAGNOSTIC_CODE_ROUTE_RUNTIME_ONLY_CLOSURE.to_string(),
                    severity: SEVERITY_INFO.to_string(),
                    scope: SCOPE_ROUTE.to_string(),
                    message:
                        "runtime route uses a closure action and is runtime-only in contract v2"
                            .to_string(),
                    route_id: Some(route.route_id.clone()),
                    action_key: None,
                });
                partial = true;
            }
            _ => {
                diagnostics.push(Diagnostic {
                    code: DIAGNOSTIC_CODE_ROUTE_ACTION_UNSUPPORTED.to_string(),
                    severity: SEVERITY_WARN.to_string(),
                    scope: SCOPE_ROUTE.to_string(),
                    message: "runtime route action kind is unsupported in contract v2".to_string(),
                    route_id: Some(route.route_id.clone()),
                    action_key: None,
                });
                partial = true;
            }
        }

        if route_match.match_status != MATCH_STATUS_MATCHED {
            partial = true;
        }
        route_matches.push(route_match);
    }

    let mut controllers = matched_action_keys
        .into_iter()
        .filter_map(|key| static_actions.get(&key).cloned())
        .collect::<Vec<_>>();
    controllers.sort_by(|a, b| (&a.fqcn, &a.method).cmp(&(&b.fqcn, &b.method)));

    let diagnostic_counts = count_diagnostics(&diagnostics);
    let status = if partial {
        RESPONSE_STATUS_PARTIAL
    } else {
        RESPONSE_STATUS_OK
    };

    let stats = contract_stats(result);
    AnalysisResponse {
        contract_version: CONTRACT_VERSION.to_string(),
        request_id: request.request_id.clone(),
        runtime_fingerprint: request.runtime_fingerprint.clone(),
        status: status.to_string(),
        meta: ResponseMeta {
            oxinfer_version: OXINFER_VERSION.to_string(),
            partial,
            stats: stats.clone(),
            diagnostic_counts,
        },
        delta: ContractDelta {
            meta: ContractDeltaMeta {
                partial: result.partial,
                stats,
                version: OXINFER_VERSION.to_string(),
            },
            controllers,
            models,
            resources,
            polymorphic,
            broadcast,
        },
        route_matches,
        diagnostics,
    }
}

fn validate_request_business_rules(request: &AnalysisRequest) -> Result<()> {
    let base_path = Path::new(&request.runtime.app.base_path);
    if !base_path.is_absolute() {
        bail!("runtime.app.basePath must be an absolute path");
    }

    let request_root = request.manifest.project.root.as_path();
    if request_root != base_path {
        bail!("runtime.app.basePath must match manifest.project.root after normalization");
    }

    let mut seen_route_ids = BTreeSet::new();
    for route in &request.runtime.routes {
        if !seen_route_ids.insert(route.route_id.clone()) {
            bail!(
                "runtime.routes contains duplicate routeId \"{}\"",
                route.route_id
            );
        }
    }

    Ok(())
}

fn collect_static_controllers(
    result: &PipelineResult,
    source_index: &SourceIndex,
    runtime_routes: &BTreeMap<String, Vec<RuntimeRoute>>,
) -> BTreeMap<String, ContractController> {
    let route_methods = collect_route_methods(result);
    let scope_owners = collect_scope_owners(result);
    let related_models_by_morph = collect_related_models_by_morph(result);
    let model_index = collect_model_index(result);

    let mut controllers = BTreeMap::new();

    for file in &result.files {
        for controller in &file.facts.controllers {
            let action_key = format!("{}::{}", controller.fqcn, controller.method_name);
            let action_routes = runtime_routes
                .get(&action_key)
                .map(Vec::as_slice)
                .unwrap_or(&[]);
            let http = build_http_info(controller, route_methods.get(&action_key), action_routes);
            controllers.insert(
                action_key,
                ContractController {
                    fqcn: controller.fqcn.clone(),
                    method: controller.method_name.clone(),
                    http: http.clone(),
                    request: build_contract_request(controller, source_index),
                    responses: build_contract_responses(
                        controller,
                        http.as_ref(),
                        source_index,
                        &model_index,
                    ),
                    authorization: build_authorization(controller, action_routes, source_index),
                    resources: build_controller_resources(controller),
                    scopes_used: build_controller_scopes(controller, &scope_owners),
                    polymorphic: build_controller_polymorphic(
                        controller,
                        &related_models_by_morph,
                        source_index,
                    ),
                },
            );
        }
    }

    controllers
}

fn build_http_info(
    controller: &ControllerMethod,
    route_methods: Option<&Vec<String>>,
    runtime_routes: &[RuntimeRoute],
) -> Option<ContractHttpInfo> {
    if let Some(status) = controller.http_status {
        return Some(ContractHttpInfo {
            status,
            explicit: true,
        });
    }

    let runtime_method_count = runtime_routes
        .iter()
        .flat_map(|route| route.methods.iter())
        .count();
    if runtime_method_count > 0 {
        return Some(ContractHttpInfo {
            status: 200,
            explicit: false,
        });
    }

    route_methods.and_then(|methods| {
        (!methods.is_empty()).then_some(ContractHttpInfo {
            status: 200,
            explicit: false,
        })
    })
}

fn build_contract_request(
    controller: &ControllerMethod,
    source_index: &SourceIndex,
) -> Option<ContractRequest> {
    let mut content_types = BTreeSet::new();
    let mut field_map = BTreeMap::<(String, String), ContractRequestField>::new();
    let mut body = ShapeTree::default();
    let mut query = ShapeTree::default();
    let mut files = ShapeTree::default();

    for field in parse_inline_validate_fields(&controller.body_text) {
        register_request_field(
            field,
            &mut field_map,
            &mut content_types,
            &mut body,
            &mut query,
            &mut files,
        );
    }

    for item in &controller.request_usage {
        if let Some(class_name) = &item.class_name {
            for field in parse_form_request_fields(class_name, source_index) {
                register_request_field(
                    field,
                    &mut field_map,
                    &mut content_types,
                    &mut body,
                    &mut query,
                    &mut files,
                );
            }
        }
    }

    for item in &controller.request_usage {
        match item.method.as_str() {
            "validate" => {
                content_types.insert("application/json".to_string());
                content_types.insert("application/x-www-form-urlencoded".to_string());
                for path in &item.rules {
                    register_request_field(
                        ParsedRuleField {
                            location: "body".to_string(),
                            path: path.clone(),
                            kind: Some("field".to_string()),
                            source: Some("request validation".to_string()),
                            via: Some("validate".to_string()),
                            ..ParsedRuleField::default()
                        },
                        &mut field_map,
                        &mut content_types,
                        &mut body,
                        &mut query,
                        &mut files,
                    );
                }
            }
            "validated" => {
                content_types.insert("application/json".to_string());
                content_types.insert("application/x-www-form-urlencoded".to_string());
            }
            "only" => {
                for path in &item.fields {
                    register_request_field(
                        ParsedRuleField {
                            location: "query".to_string(),
                            path: path.clone(),
                            kind: Some("field".to_string()),
                            source: Some("request extraction".to_string()),
                            via: Some("only".to_string()),
                            ..ParsedRuleField::default()
                        },
                        &mut field_map,
                        &mut content_types,
                        &mut body,
                        &mut query,
                        &mut files,
                    );
                }
            }
            "file" => {
                content_types.insert("multipart/form-data".to_string());
                for path in &item.fields {
                    register_request_field(
                        ParsedRuleField {
                            location: "files".to_string(),
                            path: path.clone(),
                            kind: Some("file".to_string()),
                            type_name: Some("string".to_string()),
                            scalar_type: Some("binary".to_string()),
                            source: Some("request upload".to_string()),
                            via: Some("file".to_string()),
                            ..ParsedRuleField::default()
                        },
                        &mut field_map,
                        &mut content_types,
                        &mut body,
                        &mut query,
                        &mut files,
                    );
                }
            }
            "body" => {
                content_types.insert("application/json".to_string());
                content_types.insert("application/x-www-form-urlencoded".to_string());
            }
            _ => {}
        }
    }

    let mut fields = field_map.into_values().collect::<Vec<_>>();
    if content_types.is_empty() && fields.is_empty() {
        return None;
    }
    fields.sort_by(|a, b| (&a.location, &a.path, &a.via).cmp(&(&b.location, &b.path, &b.via)));
    Some(ContractRequest {
        content_types: content_types.into_iter().collect(),
        body: (!body.is_empty()).then_some(body),
        query: (!query.is_empty()).then_some(query),
        files: (!files.is_empty()).then_some(files),
        fields,
    })
}

fn build_contract_responses(
    controller: &ControllerMethod,
    http: Option<&ContractHttpInfo>,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
) -> Vec<ContractResponse> {
    let mut responses = infer_responses_from_body(controller, http, source_index, model_index);
    if responses.is_empty() {
        responses.extend(infer_responses_from_resources(
            controller,
            http,
            source_index,
            model_index,
        ));
    }
    if responses.is_empty() {
        responses.extend(infer_fallback_response(http));
    }

    let mut seen = BTreeSet::new();
    responses.retain(|response| {
        let key = format!(
            "{}|{:?}|{:?}|{:?}|{:?}|{:?}",
            response.kind,
            response.status,
            response.content_type,
            response.source,
            response.via,
            response.body_schema
        );
        seen.insert(key)
    });
    responses.sort_by(|a, b| {
        (&a.kind, &a.status, &a.source, &a.via).cmp(&(&b.kind, &b.status, &b.source, &b.via))
    });
    responses
}

fn infer_responses_from_body(
    controller: &ControllerMethod,
    http: Option<&ContractHttpInfo>,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
) -> Vec<ContractResponse> {
    let mut responses = Vec::new();
    let text = &controller.body_text;
    let status = http.map(|item| item.status);
    let explicit = http.map(|item| item.explicit);

    if text.contains("noContent()") || status == Some(204) {
        responses.push(ContractResponse {
            kind: "no_content".to_string(),
            status,
            explicit,
            content_type: None,
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: None,
            download: None,
            inertia: None,
            source: Some("response()->noContent".to_string()),
            via: Some("response()->noContent".to_string()),
        });
        return responses;
    }

    if let Some(component) = extract_inertia_component(text) {
        responses.push(ContractResponse {
            kind: "inertia".to_string(),
            status: status.or(Some(200)),
            explicit: explicit.or(Some(false)),
            content_type: Some("text/html".to_string()),
            headers: BTreeMap::new(),
            body_schema: Some(json!({ "type": "object" })),
            redirect: None,
            download: None,
            inertia: Some(ContractInertia {
                component,
                props_schema: Some(json!({ "type": "object" })),
            }),
            source: Some("Inertia::render".to_string()),
            via: Some("Inertia::render".to_string()),
        });
    }

    if let Some(target) = extract_route_redirect_target(text) {
        responses.push(ContractResponse {
            kind: "redirect".to_string(),
            status: status.or(Some(302)),
            explicit: explicit.or(Some(false)),
            content_type: None,
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: Some(ContractRedirect {
                target_kind: "route".to_string(),
                target: Some(target),
            }),
            download: None,
            inertia: None,
            source: Some("redirect()->route".to_string()),
            via: Some("redirect()->route".to_string()),
        });
    } else if text.contains("back()") {
        responses.push(ContractResponse {
            kind: "redirect".to_string(),
            status: status.or(Some(302)),
            explicit: explicit.or(Some(false)),
            content_type: None,
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: Some(ContractRedirect {
                target_kind: "back".to_string(),
                target: None,
            }),
            download: None,
            inertia: None,
            source: Some("back".to_string()),
            via: Some("back".to_string()),
        });
    }

    if text.contains("streamDownload(") {
        responses.push(ContractResponse {
            kind: "stream".to_string(),
            status: status.or(Some(200)),
            explicit: explicit.or(Some(false)),
            content_type: Some("application/octet-stream".to_string()),
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: None,
            download: None,
            inertia: None,
            source: Some("response()->streamDownload".to_string()),
            via: Some("response()->streamDownload".to_string()),
        });
    } else if text.contains("download(") {
        responses.push(ContractResponse {
            kind: "download".to_string(),
            status: status.or(Some(200)),
            explicit: explicit.or(Some(false)),
            content_type: Some("application/octet-stream".to_string()),
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: None,
            download: Some(ContractDownload {
                disposition: "attachment".to_string(),
                filename: extract_download_filename(text),
            }),
            inertia: None,
            source: Some("response()->download".to_string()),
            via: Some("response()->download".to_string()),
        });
    }

    for call in extract_json_response_calls(text) {
        let inferred_schema = infer_schema_from_expression(
            &call.payload,
            controller,
            source_index,
            model_index,
            &mut BTreeSet::new(),
        );
        let kind = inferred_schema
            .as_ref()
            .and_then(schema_type)
            .map(|schema_type| {
                if schema_type == "array" {
                    "json_array".to_string()
                } else {
                    "json_object".to_string()
                }
            })
            .unwrap_or_else(|| "json_object".to_string());

        responses.push(ContractResponse {
            kind,
            status: call.status.or(status).or(Some(200)),
            explicit: Some(call.status.is_some() || explicit.unwrap_or(false)),
            content_type: Some("application/json".to_string()),
            headers: BTreeMap::new(),
            body_schema: inferred_schema.or_else(|| Some(json!({ "type": "object" }))),
            redirect: None,
            download: None,
            inertia: None,
            source: Some("response()->json".to_string()),
            via: Some("response()->json".to_string()),
        });
    }

    responses
}

fn infer_responses_from_resources(
    controller: &ControllerMethod,
    http: Option<&ContractHttpInfo>,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
) -> Vec<ContractResponse> {
    let default_status = http.map(|item| item.status).unwrap_or(200);
    let explicit = http.map(|item| item.explicit).unwrap_or(false);

    controller
        .resource_usage
        .iter()
        .map(|resource| {
            let collection = is_collection_resource(resource);
            let source = match resource.method.as_deref() {
                Some("collection") => "JsonResource::collection",
                Some("response") => "JsonResource::response",
                _ => "JsonResource",
            };
            let body_schema =
                build_response_schema(&resource.class_name, collection, source_index, model_index);
            ContractResponse {
                kind: if collection {
                    "json_array".to_string()
                } else {
                    "json_object".to_string()
                },
                status: Some(default_status),
                explicit: Some(explicit),
                content_type: Some("application/json".to_string()),
                headers: BTreeMap::new(),
                body_schema: Some(body_schema),
                redirect: None,
                download: None,
                inertia: None,
                source: Some(source.to_string()),
                via: Some(source.to_string()),
            }
        })
        .collect()
}

fn infer_fallback_response(http: Option<&ContractHttpInfo>) -> Vec<ContractResponse> {
    let Some(http) = http else {
        return Vec::new();
    };
    if http.status == 204 {
        return vec![ContractResponse {
            kind: "no_content".to_string(),
            status: Some(http.status),
            explicit: Some(http.explicit),
            content_type: None,
            headers: BTreeMap::new(),
            body_schema: None,
            redirect: None,
            download: None,
            inertia: None,
            source: None,
            via: None,
        }];
    }

    vec![ContractResponse {
        kind: "json_object".to_string(),
        status: Some(http.status),
        explicit: Some(http.explicit),
        content_type: Some("application/json".to_string()),
        headers: BTreeMap::new(),
        body_schema: Some(json!({ "type": "object" })),
        redirect: None,
        download: None,
        inertia: None,
        source: None,
        via: None,
    }]
}

fn build_authorization(
    controller: &ControllerMethod,
    runtime_routes: &[RuntimeRoute],
    source_index: &SourceIndex,
) -> Vec<ContractAuthorization> {
    let authorize_re =
        Regex::new(r#"(?:\$this->authorize|Gate::authorize)\(\s*['"]([^'"]+)['"]\s*,\s*([^\),]+)"#)
            .expect("authorize regex");

    let mut items = Vec::new();
    let mut seen = BTreeSet::new();
    for captures in authorize_re.captures_iter(&controller.body_text) {
        let Some(ability) = captures.get(1) else {
            continue;
        };
        let target = captures.get(2).map(|item| item.as_str().trim().to_string());
        let source = captures
            .get(0)
            .map(|item| item.as_str())
            .unwrap_or("$this->authorize");
        let key = format!("{}|{:?}|{}", ability.as_str(), target, source);
        if !seen.insert(key) {
            continue;
        }

        items.push(ContractAuthorization {
            kind: "policy".to_string(),
            ability: Some(ability.as_str().to_string()),
            target_kind: target.as_ref().map(|_| "expression".to_string()),
            target,
            parameter: None,
            source: if source.contains("Gate::authorize") {
                "Gate::authorize".to_string()
            } else {
                "$this->authorize".to_string()
            },
            resolution: "explicit".to_string(),
            enforces_failure_response: true,
        });
    }

    for route in runtime_routes {
        extend_route_authorization(route, &mut items, &mut seen);
    }

    for request in &controller.request_usage {
        let Some(class_name) = &request.class_name else {
            continue;
        };
        extend_form_request_authorization(class_name, source_index, &mut items, &mut seen);
    }

    items.sort_by(|a, b| {
        (&a.kind, &a.source, &a.ability, &a.target, &a.parameter).cmp(&(
            &b.kind,
            &b.source,
            &b.ability,
            &b.target,
            &b.parameter,
        ))
    });
    items
}

fn build_controller_resources(controller: &ControllerMethod) -> Vec<ContractControllerResource> {
    let mut resources = controller
        .resource_usage
        .iter()
        .map(|resource| ContractControllerResource {
            class: class_basename(&resource.class_name),
            fqcn: Some(resource.class_name.clone()),
            collection: is_collection_resource(resource),
        })
        .collect::<Vec<_>>();

    resources.sort_by(|a, b| {
        (&a.fqcn, &a.class, &a.collection).cmp(&(&b.fqcn, &b.class, &b.collection))
    });
    resources
        .dedup_by(|a, b| a.fqcn == b.fqcn && a.class == b.class && a.collection == b.collection);
    resources
}

fn build_controller_scopes(
    controller: &ControllerMethod,
    scope_owners: &BTreeMap<String, Option<String>>,
) -> Vec<ContractScopeUsed> {
    let mut scopes = controller
        .scopes_used
        .iter()
        .filter_map(|scope| {
            let owner = scope
                .on
                .clone()
                .or_else(|| scope_owners.get(&scope.name).cloned().flatten())?;
            Some(ContractScopeUsed {
                on: owner,
                name: scope.name.clone(),
                args: Vec::new(),
            })
        })
        .collect::<Vec<_>>();
    scopes.sort_by(|a, b| (&a.on, &a.name).cmp(&(&b.on, &b.name)));
    scopes.dedup_by(|a, b| a.on == b.on && a.name == b.name);
    scopes
}

fn build_controller_polymorphic(
    controller: &ControllerMethod,
    related_models_by_morph: &BTreeMap<String, BTreeSet<String>>,
    source_index: &SourceIndex,
) -> Vec<ContractPolymorphicRelation> {
    let inferred_models = controller
        .resource_usage
        .iter()
        .map(|resource| resolve_model_fqcn_from_resource(&resource.class_name, source_index))
        .collect::<Vec<_>>();

    let mut items = Vec::new();
    for model in inferred_models {
        if let Some(related) = related_models_by_morph.get(&model.1) {
            items.push(ContractPolymorphicRelation {
                relation: model.1.clone(),
                relation_type: "morphTo".to_string(),
                morph_type: Some(format!("{}_type", model.1)),
                morph_id: Some(format!("{}_id", model.1)),
                model: model.0.clone(),
                discriminator: Some(ContractDiscriminator {
                    property_name: format!("{}_type", model.1),
                    mapping: build_discriminator_mapping(related),
                }),
                related_models: related.iter().cloned().collect(),
            });
        }
    }

    items.sort_by(|a, b| {
        (&a.relation, &a.relation_type, &a.model).cmp(&(&b.relation, &b.relation_type, &b.model))
    });
    items.dedup_by(|a, b| {
        a.relation == b.relation
            && a.relation_type == b.relation_type
            && a.model == b.model
            && a.related_models == b.related_models
    });
    items
}

fn collect_models(result: &PipelineResult, source_index: &SourceIndex) -> Vec<ContractModel> {
    let related_models_by_morph = collect_related_models_by_morph(result);
    let mut models = BTreeMap::<String, ContractModel>::new();

    for file in &result.files {
        for model in &file.facts.models {
            let hint = model_schema_hint(&model.fqcn, source_index);
            let entry = models
                .entry(model.fqcn.clone())
                .or_insert_with(|| ContractModel {
                    fqcn: model.fqcn.clone(),
                    with_pivot: Vec::new(),
                    attributes: Vec::new(),
                    polymorphic: Vec::new(),
                });

            entry.with_pivot.extend(
                model
                    .relationships
                    .iter()
                    .filter(|relationship| !relationship.pivot_columns.is_empty())
                    .map(|relationship| ContractPivot {
                        relation: relationship.name.clone(),
                        columns: relationship.pivot_columns.clone(),
                        alias: relationship.pivot_alias.clone(),
                        timestamps: relationship.pivot_timestamps.then_some(true),
                    }),
            );

            entry
                .attributes
                .extend(model.attributes.iter().map(|attribute| ContractAttribute {
                    name: attribute.clone(),
                    via: "Attribute::make".to_string(),
                }));
            entry
                .attributes
                .extend(hint.fillable.iter().map(|attribute| ContractAttribute {
                    name: attribute.clone(),
                    via: "$fillable".to_string(),
                }));
            entry.attributes.extend(
                hint.casts
                    .iter()
                    .map(|(attribute, cast)| ContractAttribute {
                        name: attribute.clone(),
                        via: format!("$casts:{cast}"),
                    }),
            );

            entry
                .polymorphic
                .extend(model.relationships.iter().filter_map(|relationship| {
                    build_model_polymorphic_relation(relationship, &related_models_by_morph)
                }));
        }
    }

    let mut models = models
        .into_values()
        .map(|mut model| {
            model.with_pivot.sort_by(|a, b| {
                (&a.relation, &a.columns, &a.alias, &a.timestamps).cmp(&(
                    &b.relation,
                    &b.columns,
                    &b.alias,
                    &b.timestamps,
                ))
            });
            model.with_pivot.dedup_by(|a, b| {
                a.relation == b.relation
                    && a.columns == b.columns
                    && a.alias == b.alias
                    && a.timestamps == b.timestamps
            });
            model
                .attributes
                .sort_by(|a, b| (&a.name, &a.via).cmp(&(&b.name, &b.via)));
            model
                .attributes
                .dedup_by(|a, b| a.name == b.name && a.via == b.via);
            model.polymorphic.sort_by(|a, b| {
                (&a.relation, &a.relation_type, &a.model, &a.related_models).cmp(&(
                    &b.relation,
                    &b.relation_type,
                    &b.model,
                    &b.related_models,
                ))
            });
            model.polymorphic.dedup_by(|a, b| {
                a.relation == b.relation
                    && a.relation_type == b.relation_type
                    && a.model == b.model
                    && a.related_models == b.related_models
            });
            model
        })
        .collect::<Vec<_>>();

    models.sort_by(|a, b| a.fqcn.cmp(&b.fqcn));
    models
}

fn build_model_polymorphic_relation(
    relationship: &ModelRelationshipFact,
    related_models_by_morph: &BTreeMap<String, BTreeSet<String>>,
) -> Option<ContractPolymorphicRelation> {
    if !is_polymorphic_relation_type(&relationship.relation_type) {
        return None;
    }

    let morph_name = relationship
        .morph_name
        .clone()
        .unwrap_or_else(|| relationship.name.clone());
    let related_models = related_models_by_morph
        .get(&morph_name)
        .map(|items| items.iter().cloned().collect::<Vec<_>>())
        .unwrap_or_default();
    let discriminator = ContractDiscriminator {
        property_name: format!("{morph_name}_type"),
        mapping: build_discriminator_mapping_from_vec(&related_models),
    };

    Some(ContractPolymorphicRelation {
        relation: relationship.name.clone(),
        relation_type: normalize_polymorphic_relation_type(&relationship.relation_type),
        morph_type: Some(format!("{morph_name}_type")),
        morph_id: Some(format!("{morph_name}_id")),
        model: relationship.related.clone(),
        discriminator: Some(discriminator),
        related_models,
    })
}

fn collect_resources(result: &PipelineResult, source_index: &SourceIndex) -> Vec<ContractResource> {
    let model_index = collect_model_index(result);
    let mut resources = BTreeMap::<String, ContractResource>::new();

    for class in source_index.classes.values() {
        if !is_resource_class(class) {
            continue;
        }

        resources
            .entry(class.fqcn.clone())
            .or_insert_with(|| ContractResource {
                fqcn: class.fqcn.clone(),
                class: class.class_name.clone(),
                schema: build_resource_schema(
                    &class.fqcn,
                    source_index,
                    &model_index,
                    &mut BTreeSet::new(),
                ),
            });
    }

    let mut resources = resources.into_values().collect::<Vec<_>>();
    resources.sort_by(|a, b| a.fqcn.cmp(&b.fqcn));
    resources
}

fn collect_top_level_polymorphic(result: &PipelineResult) -> Vec<ContractPolymorphicGroup> {
    let related_models_by_morph = collect_related_models_by_morph(result);
    let mut items = Vec::new();
    let mut seen = BTreeSet::new();

    for file in &result.files {
        for relation in &file.facts.polymorphic {
            let key = (relation.model.clone(), relation.name.clone());
            if !seen.insert(key) {
                continue;
            }

            let related_models = related_models_by_morph
                .get(&relation.name)
                .cloned()
                .unwrap_or_default();
            items.push(ContractPolymorphicGroup {
                parent: relation.model.clone(),
                morph: ContractMorph {
                    key: relation.name.clone(),
                    type_column: relation.discriminator.clone(),
                    id_column: format!("{}_id", relation.name),
                },
                discriminator: ContractDiscriminator {
                    property_name: relation.discriminator.clone(),
                    mapping: build_discriminator_mapping(&related_models),
                },
            });
        }
    }

    items.sort_by(|a, b| (&a.parent, &a.morph.key).cmp(&(&b.parent, &b.morph.key)));
    items
}

fn collect_broadcast(result: &PipelineResult) -> Vec<ContractBroadcast> {
    let mut items = Vec::new();
    let mut seen = BTreeSet::new();

    for file in &result.files {
        for broadcast in &file.facts.broadcast {
            let mut params = broadcast
                .parameters
                .iter()
                .map(|item| item.name.clone())
                .collect::<Vec<_>>();
            if params.is_empty() {
                params = extract_channel_params(&broadcast.channel);
            }
            params.sort();
            params.dedup();

            let key = (broadcast.channel.clone(), file.relative_path.clone());
            if !seen.insert(key) {
                continue;
            }

            items.push(ContractBroadcast {
                file: Some(file.relative_path.clone()),
                channel: broadcast.channel.clone(),
                params,
                visibility: infer_broadcast_visibility(
                    &broadcast.channel,
                    broadcast.channel_type.as_deref(),
                ),
                payload_literal: None,
            });
        }
    }

    items.sort_by(|a, b| (&a.channel, &a.file).cmp(&(&b.channel, &b.file)));
    items
}

fn count_diagnostics(diagnostics: &[Diagnostic]) -> DiagnosticCounts {
    let mut counts = DiagnosticCounts {
        info: 0,
        warn: 0,
        error: 0,
    };

    for diagnostic in diagnostics {
        match diagnostic.severity.as_str() {
            SEVERITY_INFO => counts.info += 1,
            SEVERITY_WARN => counts.warn += 1,
            _ => counts.error += 1,
        }
    }

    counts
}

fn contract_stats(result: &PipelineResult) -> ContractStats {
    ContractStats {
        files_parsed: result.files.len(),
        skipped: 0,
        duration_ms: 0,
        assembler_stats: AssemblerStats {
            unresolvable_matches: 0,
        },
    }
}

fn collect_runtime_routes_by_action(
    request: &AnalysisRequest,
) -> BTreeMap<String, Vec<RuntimeRoute>> {
    let mut routes = BTreeMap::<String, Vec<RuntimeRoute>>::new();

    for route in &request.runtime.routes {
        let Some(action_key) = route.action.action_key() else {
            continue;
        };
        routes.entry(action_key).or_default().push(route.clone());
    }

    routes
}

fn collect_model_index(result: &PipelineResult) -> BTreeMap<String, ModelFacts> {
    let mut models = BTreeMap::new();

    for file in &result.files {
        for model in &file.facts.models {
            models.insert(model.fqcn.clone(), model.clone());
        }
    }

    models
}

fn register_request_field(
    field: ParsedRuleField,
    fields: &mut BTreeMap<(String, String), ContractRequestField>,
    content_types: &mut BTreeSet<String>,
    body: &mut ShapeTree,
    query: &mut ShapeTree,
    files: &mut ShapeTree,
) {
    let location = field.location.clone();
    let path = field.path.clone();
    if path.is_empty() {
        return;
    }

    match location.as_str() {
        "body" => {
            body.insert_path(&path);
            content_types.insert("application/json".to_string());
            content_types.insert("application/x-www-form-urlencoded".to_string());
        }
        "query" => {
            query.insert_path(&path);
        }
        "files" => {
            files.insert_path(&path);
            content_types.insert("multipart/form-data".to_string());
        }
        _ => {}
    }

    let key = (location.clone(), path.clone());
    let entry = fields.entry(key).or_insert_with(|| ContractRequestField {
        location,
        path,
        kind: None,
        type_name: None,
        scalar_type: None,
        format: None,
        item_type: None,
        wrappers: Vec::new(),
        allowed_values: Vec::new(),
        required: None,
        optional: None,
        nullable: None,
        is_array: None,
        collection: None,
        source: None,
        via: None,
    });

    merge_optional(&mut entry.kind, field.kind);
    merge_optional(&mut entry.type_name, field.type_name);
    merge_optional(&mut entry.scalar_type, field.scalar_type);
    merge_optional(&mut entry.format, field.format);
    merge_optional(&mut entry.item_type, field.item_type);
    merge_optional_bool(&mut entry.required, field.required);
    merge_optional_bool(&mut entry.optional, field.optional);
    merge_optional_bool(&mut entry.nullable, field.nullable);
    merge_optional_bool(&mut entry.is_array, field.is_array);
    merge_optional_bool(&mut entry.collection, field.collection);
    merge_optional(&mut entry.source, field.source);
    merge_optional(&mut entry.via, field.via);
    extend_unique(&mut entry.wrappers, field.wrappers);
    extend_unique(&mut entry.allowed_values, field.allowed_values);
}

fn merge_optional(target: &mut Option<String>, value: Option<String>) {
    if target.is_none() {
        *target = value;
    }
}

fn merge_optional_bool(target: &mut Option<bool>, value: Option<bool>) {
    if target.is_none() {
        *target = value;
    }
}

fn extend_unique(target: &mut Vec<String>, values: Vec<String>) {
    target.extend(values);
    target.sort();
    target.dedup();
}

fn parse_inline_validate_fields(method_text: &str) -> Vec<ParsedRuleField> {
    let mut fields = Vec::new();
    let mut offset = 0usize;
    let needle = "validate(";

    while let Some(index) = method_text[offset..].find(needle) {
        let call_start = offset + index + needle.len();
        let Some(array_index) = method_text[call_start..].find('[') else {
            offset = call_start;
            continue;
        };
        let array_start = call_start + array_index;
        let Some((array_body, _, end_index)) =
            extract_balanced_region(&method_text[array_start..], '[', ']')
        else {
            offset = array_start + 1;
            continue;
        };

        fields.extend(parse_rule_array(
            &array_body,
            "request validation",
            "validate",
        ));
        offset = array_start + end_index + 1;
    }

    fields
}

fn parse_form_request_fields(class_name: &str, source_index: &SourceIndex) -> Vec<ParsedRuleField> {
    let Some(class) = source_index.get(class_name) else {
        return Vec::new();
    };
    let Some(method_body) = class.method_body("rules") else {
        return Vec::new();
    };
    let Some(array_body) = extract_return_array(&method_body) else {
        return Vec::new();
    };

    parse_rule_array(
        &array_body,
        &format!("{}::rules", class.fqcn),
        "FormRequest",
    )
}

fn parse_rule_array(array_body: &str, source: &str, via: &str) -> Vec<ParsedRuleField> {
    let mut fields = Vec::new();

    for entry in split_top_level(array_body, ',') {
        let Some((raw_key, raw_value)) = split_top_level_key_value(&entry) else {
            continue;
        };
        let Some(path) = strip_php_string(&raw_key) else {
            continue;
        };

        let mut field = ParsedRuleField {
            location: "body".to_string(),
            path,
            kind: Some("field".to_string()),
            source: Some(source.to_string()),
            via: Some(via.to_string()),
            ..ParsedRuleField::default()
        };

        for rule in parse_rule_tokens(&raw_value) {
            apply_rule_token(&mut field, &rule);
        }

        if field.location == "files" && field.kind.is_none() {
            field.kind = Some("file".to_string());
        }
        if field.location == "files" && field.type_name.is_none() {
            field.type_name = Some("string".to_string());
        }
        if field.location == "files" && field.scalar_type.is_none() {
            field.scalar_type = Some("binary".to_string());
        }

        fields.push(field);
    }

    fields
}

fn parse_rule_tokens(raw_value: &str) -> Vec<String> {
    let value = raw_value.trim();

    if let Some(string_value) = strip_php_string(value) {
        return string_value
            .split('|')
            .map(str::trim)
            .filter(|item| !item.is_empty())
            .map(str::to_string)
            .collect();
    }

    if value.starts_with('[') {
        let Some((inner, _, _)) = extract_balanced_region(value, '[', ']') else {
            return vec![value.to_string()];
        };
        return split_top_level(&inner, ',')
            .into_iter()
            .map(|item| strip_php_string(&item).unwrap_or(item))
            .collect();
    }

    vec![value.to_string()]
}

fn apply_rule_token(field: &mut ParsedRuleField, rule: &str) {
    let normalized = rule.trim();
    if normalized.is_empty() {
        return;
    }

    match normalized {
        "required" => {
            field.required = Some(true);
            field.optional = Some(false);
        }
        "sometimes" => field.optional = Some(true),
        "nullable" => field.nullable = Some(true),
        "string" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("string".to_string());
            field.scalar_type = Some("string".to_string());
        }
        "integer" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("integer".to_string());
            field.scalar_type = Some("integer".to_string());
        }
        "numeric" | "decimal" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("number".to_string());
            field.scalar_type = Some("number".to_string());
        }
        "boolean" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("boolean".to_string());
            field.scalar_type = Some("boolean".to_string());
        }
        "array" => {
            field.kind = Some("array".to_string());
            field.type_name = Some("array".to_string());
            field.is_array = Some(true);
            field.collection = Some(true);
        }
        "email" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("string".to_string());
            field.scalar_type = Some("string".to_string());
            field.format = Some("email".to_string());
        }
        "date" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("string".to_string());
            field.scalar_type = Some("string".to_string());
            field.format = Some("date".to_string());
        }
        "datetime" => {
            field.kind = Some("scalar".to_string());
            field.type_name = Some("string".to_string());
            field.scalar_type = Some("string".to_string());
            field.format = Some("date-time".to_string());
        }
        "file" | "image" => {
            field.location = "files".to_string();
            field.kind = Some("file".to_string());
            field.type_name = Some("string".to_string());
            field.scalar_type = Some("binary".to_string());
            field.wrappers.push(normalized.to_string());
        }
        _ => {
            if let Some(values) = normalized.strip_prefix("in:") {
                field.allowed_values.extend(
                    values
                        .split(',')
                        .map(str::trim)
                        .filter(|item| !item.is_empty())
                        .map(str::to_string),
                );
                field.allowed_values.sort();
                field.allowed_values.dedup();
                field.wrappers.push("in".to_string());
                return;
            }

            if normalized.starts_with("mimes:") || normalized.starts_with("mimetypes:") {
                field.location = "files".to_string();
                field.kind = Some("file".to_string());
                field.type_name = Some("string".to_string());
                field.scalar_type = Some("binary".to_string());
            }

            if normalized.starts_with("exists:")
                || normalized.starts_with("unique:")
                || normalized.starts_with("max:")
                || normalized.starts_with("min:")
                || normalized.starts_with("confirmed")
                || normalized.starts_with("regex:")
            {
                field.wrappers.push(
                    normalized
                        .split(':')
                        .next()
                        .unwrap_or(normalized)
                        .to_string(),
                );
                return;
            }

            if normalized.starts_with("Rule::in(") {
                if let Some((inner, _, _)) = extract_balanced_region(normalized, '(', ')') {
                    let values = inner
                        .split_once('[')
                        .and_then(|(_, tail)| {
                            extract_balanced_region(&format!("[{tail}"), '[', ']')
                        })
                        .map(|(body, _, _)| {
                            split_top_level(&body, ',')
                                .into_iter()
                                .filter_map(|item| strip_php_string(&item))
                                .collect::<Vec<_>>()
                        })
                        .unwrap_or_default();
                    field.allowed_values.extend(values);
                    field.allowed_values.sort();
                    field.allowed_values.dedup();
                }
                field.wrappers.push("in".to_string());
                return;
            }

            if normalized.starts_with("Rule::unique(") {
                field.wrappers.push("unique".to_string());
                return;
            }

            if normalized.starts_with("Rule::exists(") {
                field.wrappers.push("exists".to_string());
                return;
            }
        }
    }
}

fn extend_route_authorization(
    route: &RuntimeRoute,
    items: &mut Vec<ContractAuthorization>,
    seen: &mut BTreeSet<String>,
) {
    for middleware in &route.middleware {
        if let Some(spec) = middleware.strip_prefix("can:") {
            let mut parts = spec.split(',');
            let ability = parts.next().map(str::trim).filter(|item| !item.is_empty());
            let target = parts.next().map(str::trim).filter(|item| !item.is_empty());
            let key = format!("middleware|{:?}|{:?}|{}", ability, target, middleware);
            if !seen.insert(key) {
                continue;
            }
            items.push(ContractAuthorization {
                kind: "policy".to_string(),
                ability: ability.map(str::to_string),
                target_kind: target.map(|value| {
                    if value.contains('\\') {
                        "class".to_string()
                    } else {
                        "route_parameter".to_string()
                    }
                }),
                target: target.map(str::to_string),
                parameter: target
                    .filter(|value| !value.contains('\\'))
                    .map(str::to_string),
                source: middleware.clone(),
                resolution: "runtime".to_string(),
                enforces_failure_response: true,
            });
            continue;
        }

        if middleware.starts_with("auth") || middleware == "verified" || middleware == "signed" {
            let key = format!("middleware|{}|{}", route.route_id, middleware);
            if !seen.insert(key) {
                continue;
            }
            items.push(ContractAuthorization {
                kind: "middleware".to_string(),
                ability: None,
                target_kind: None,
                target: None,
                parameter: None,
                source: middleware.clone(),
                resolution: "runtime".to_string(),
                enforces_failure_response: true,
            });
        }
    }
}

fn extend_form_request_authorization(
    class_name: &str,
    source_index: &SourceIndex,
    items: &mut Vec<ContractAuthorization>,
    seen: &mut BTreeSet<String>,
) {
    let Some(class) = source_index.get(class_name) else {
        return;
    };
    let Some(body) = class.method_body("authorize") else {
        return;
    };

    let compact = body.split_whitespace().collect::<String>();
    if compact.contains("returntrue;") {
        return;
    }

    let can_re = Regex::new(
        r#"(?:->can|Gate::allows|Gate::authorize)\(\s*['"]([^'"]+)['"]\s*(?:,\s*([^\)]+))?"#,
    )
    .expect("form request authorize regex");
    let mut matched = false;

    for captures in can_re.captures_iter(&body) {
        let ability = captures.get(1).map(|item| item.as_str().to_string());
        let target = captures.get(2).map(|item| item.as_str().trim().to_string());
        let key = format!("form_request|{}|{:?}|{:?}", class.fqcn, ability, target);
        if !seen.insert(key) {
            continue;
        }
        matched = true;
        items.push(ContractAuthorization {
            kind: "form_request".to_string(),
            ability,
            target_kind: target.as_ref().map(|_| "expression".to_string()),
            target,
            parameter: None,
            source: format!("{}::authorize", class.fqcn),
            resolution: "form_request".to_string(),
            enforces_failure_response: true,
        });
    }

    if matched {
        return;
    }

    if compact.contains("returnfalse;") || compact.contains("abort(") || compact.contains("throw") {
        let key = format!("form_request|{}|static", class.fqcn);
        if seen.insert(key) {
            items.push(ContractAuthorization {
                kind: "form_request".to_string(),
                ability: None,
                target_kind: None,
                target: None,
                parameter: None,
                source: format!("{}::authorize", class.fqcn),
                resolution: "form_request".to_string(),
                enforces_failure_response: true,
            });
        }
    }
}

fn collect_route_methods(result: &PipelineResult) -> BTreeMap<String, Vec<String>> {
    let mut route_methods = BTreeMap::<String, BTreeSet<String>>::new();

    for binding in &result.route_bindings {
        let action_key = format!("{}::{}", binding.controller_fqcn, binding.method_name);
        route_methods
            .entry(action_key)
            .or_default()
            .extend(binding.http_methods.iter().cloned());
    }

    route_methods
        .into_iter()
        .map(|(key, values)| (key, values.into_iter().collect()))
        .collect()
}

fn collect_scope_owners(result: &PipelineResult) -> BTreeMap<String, Option<String>> {
    let mut owners = BTreeMap::<String, BTreeSet<String>>::new();

    for file in &result.files {
        for model in &file.facts.models {
            for scope in &model.scopes {
                owners
                    .entry(scope.clone())
                    .or_default()
                    .insert(model.fqcn.clone());
            }
        }
    }

    owners
        .into_iter()
        .map(|(scope, models)| {
            let owner = if models.len() == 1 {
                models.into_iter().next()
            } else {
                None
            };
            (scope, owner)
        })
        .collect()
}

fn collect_related_models_by_morph(result: &PipelineResult) -> BTreeMap<String, BTreeSet<String>> {
    let mut mapping = BTreeMap::<String, BTreeSet<String>>::new();

    for file in &result.files {
        for model in &file.facts.models {
            extend_related_models_for_model(model, &mut mapping);
        }
    }

    mapping
}

fn extend_related_models_for_model(
    model: &ModelFacts,
    mapping: &mut BTreeMap<String, BTreeSet<String>>,
) {
    for relationship in &model.relationships {
        let Some(morph_name) = &relationship.morph_name else {
            continue;
        };
        let Some(related) = &relationship.related else {
            continue;
        };
        mapping
            .entry(morph_name.clone())
            .or_default()
            .insert(related.clone());
    }
}

fn class_basename(fqcn: &str) -> String {
    fqcn.rsplit('\\').next().unwrap_or(fqcn).to_string()
}

fn is_collection_resource(resource: &ResourceUsageFact) -> bool {
    matches!(resource.method.as_deref(), Some("collection"))
        || resource.class_name.ends_with("Collection")
}

fn is_polymorphic_relation_type(relation_type: &str) -> bool {
    matches!(
        relation_type,
        "morphTo" | "morphOne" | "morphMany" | "morphByMany" | "morphToMany" | "morphedByMany"
    )
}

fn normalize_polymorphic_relation_type(relation_type: &str) -> String {
    match relation_type {
        "morphedByMany" => "morphByMany".to_string(),
        other => other.to_string(),
    }
}

fn build_discriminator_mapping(values: &BTreeSet<String>) -> BTreeMap<String, String> {
    values
        .iter()
        .map(|fqcn| (class_basename(fqcn), fqcn.clone()))
        .collect()
}

fn build_discriminator_mapping_from_vec(values: &[String]) -> BTreeMap<String, String> {
    values
        .iter()
        .map(|fqcn| (class_basename(fqcn), fqcn.clone()))
        .collect()
}

fn build_response_schema(
    fqcn: &str,
    collection: bool,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
) -> Value {
    let schema = build_resource_schema(fqcn, source_index, model_index, &mut BTreeSet::new());
    if collection && !fqcn.ends_with("Collection") {
        schema_array(schema)
    } else {
        schema
    }
}

fn build_resource_schema(
    fqcn: &str,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    visited: &mut BTreeSet<String>,
) -> Value {
    if !visited.insert(fqcn.to_string()) {
        return schema_object(BTreeMap::new());
    }

    let schema = source_index
        .get(fqcn)
        .and_then(|class| {
            build_resource_schema_from_class(class, source_index, model_index, visited)
        })
        .unwrap_or_else(|| schema_object(BTreeMap::new()));

    visited.remove(fqcn);
    schema
}

fn build_resource_schema_from_class(
    class: &SourceClass,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    visited: &mut BTreeSet<String>,
) -> Option<Value> {
    let method_body = class.method_body("toArray")?;
    let array_body = extract_return_array(&method_body)?;
    let model_fqcn = resolve_model_fqcn_from_resource(&class.fqcn, source_index).0;

    Some(parse_php_array_schema(
        &array_body,
        source_index,
        model_index,
        Some(class),
        model_fqcn.as_deref(),
        visited,
    ))
}

fn parse_php_array_schema(
    array_body: &str,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    current_class: Option<&SourceClass>,
    current_model_fqcn: Option<&str>,
    visited: &mut BTreeSet<String>,
) -> Value {
    let entries = split_top_level(array_body, ',');
    let mut properties = BTreeMap::<String, Value>::new();
    let mut items = Vec::new();
    let mut associative = false;

    for entry in entries {
        if let Some((raw_key, raw_value)) = split_top_level_key_value(&entry) {
            associative = true;
            let key = strip_php_string(&raw_key).unwrap_or(raw_key.trim().to_string());
            properties.insert(
                key,
                infer_value_schema(
                    &raw_value,
                    source_index,
                    model_index,
                    current_class,
                    current_model_fqcn,
                    visited,
                ),
            );
        } else {
            items.push(infer_value_schema(
                &entry,
                source_index,
                model_index,
                current_class,
                current_model_fqcn,
                visited,
            ));
        }
    }

    if associative {
        schema_object(properties)
    } else {
        let item_schema = items
            .into_iter()
            .next()
            .unwrap_or_else(|| schema_object(BTreeMap::new()));
        schema_array(item_schema)
    }
}

fn infer_value_schema(
    raw_value: &str,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    current_class: Option<&SourceClass>,
    current_model_fqcn: Option<&str>,
    visited: &mut BTreeSet<String>,
) -> Value {
    let value = raw_value.trim();

    if let Some(_string_value) = strip_php_string(value) {
        return schema_string(None);
    }
    if value == "true" || value == "false" {
        return schema_boolean();
    }
    if value == "null" {
        return json!({ "nullable": true });
    }
    if value.parse::<i64>().is_ok() {
        return schema_integer();
    }
    if value.parse::<f64>().is_ok() && value.contains('.') {
        return schema_number();
    }
    if value.starts_with('[') {
        if let Some((inner, _, _)) = extract_balanced_region(value, '[', ']') {
            return parse_php_array_schema(
                &inner,
                source_index,
                model_index,
                current_class,
                current_model_fqcn,
                visited,
            );
        }
    }
    if let Some(captures) = Regex::new(r#"new\s+([A-Z][A-Za-z0-9_\\]*(?:Resource|Collection))\("#)
        .expect("resource new value regex")
        .captures(value)
    {
        if let Some(resource_name) = captures.get(1) {
            let fqcn = current_class
                .map(|class| class.resolve_name(resource_name.as_str()))
                .unwrap_or_else(|| resource_name.as_str().to_string());
            return build_resource_schema(&fqcn, source_index, model_index, visited);
        }
    }
    if let Some(captures) =
        Regex::new(r#"([A-Z][A-Za-z0-9_\\]*(?:Resource|Collection))::collection\("#)
            .expect("resource collection value regex")
            .captures(value)
    {
        if let Some(resource_name) = captures.get(1) {
            let fqcn = current_class
                .map(|class| class.resolve_name(resource_name.as_str()))
                .unwrap_or_else(|| resource_name.as_str().to_string());
            let item_schema = build_resource_schema(&fqcn, source_index, model_index, visited);
            return schema_array(item_schema);
        }
    }
    if value.contains("fn () =>") {
        if let Some((_, tail)) = value.split_once("fn () =>") {
            return infer_value_schema(
                tail.trim(),
                source_index,
                model_index,
                current_class,
                current_model_fqcn,
                visited,
            );
        }
    }
    if value.contains("->count()") {
        return schema_integer();
    }
    if value.contains("->avg(") {
        return schema_number();
    }
    if value.contains("->previousPageUrl()") || value.contains("->nextPageUrl()") {
        return schema_nullable(schema_string(None));
    }
    if value.contains("->url(") {
        return schema_string(None);
    }
    if value.contains("->total()")
        || value.contains("->perPage()")
        || value.contains("->currentPage()")
        || value.contains("->lastPage()")
    {
        return schema_integer();
    }
    if let Some(property) = value.strip_prefix("$this->") {
        let property = property
            .split(|ch: char| !ch.is_ascii_alphanumeric() && ch != '_')
            .next()
            .unwrap_or(property);
        if property == "collection" {
            if let Some(class) = current_class {
                if let Some(resource_fqcn) =
                    infer_collection_item_resource(&class.fqcn, source_index)
                {
                    let item_schema =
                        build_resource_schema(&resource_fqcn, source_index, model_index, visited);
                    return schema_array(item_schema);
                }
            }
            return schema_array(schema_object(BTreeMap::new()));
        }
        if let Some(model_fqcn) = current_model_fqcn {
            if let Some(schema) = infer_model_property_schema(
                model_fqcn,
                property,
                source_index,
                model_index,
                visited,
            ) {
                return schema;
            }
        }
        return schema_object(BTreeMap::new());
    }

    schema_object(BTreeMap::new())
}

fn infer_model_property_schema(
    model_fqcn: &str,
    property: &str,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    visited: &mut BTreeSet<String>,
) -> Option<Value> {
    let hint = model_schema_hint(model_fqcn, source_index);
    if property == "id" {
        return Some(schema_integer());
    }
    if property == "created_at" || property == "updated_at" || property.ends_with("_at") {
        return Some(schema_string(Some("date-time")));
    }
    if property.ends_with("_count") {
        return Some(schema_integer());
    }
    if property.starts_with("is_") {
        return Some(schema_boolean());
    }
    if let Some(cast) = hint.casts.get(property) {
        return Some(schema_from_cast(cast));
    }
    if hint.fillable.contains(property) {
        return Some(schema_string(None));
    }

    let model = model_index.get(model_fqcn)?;
    let relationship = model
        .relationships
        .iter()
        .find(|item| item.name == property)?;
    let related = relationship.related.as_ref()?;
    let related_schema = build_model_object_schema(
        related,
        source_index,
        model_index,
        &BTreeSet::new(),
        visited,
    );

    Some(if relation_is_collection(&relationship.relation_type) {
        schema_array(related_schema)
    } else {
        related_schema
    })
}

fn build_model_object_schema(
    model_fqcn: &str,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    loaded_relations: &BTreeSet<String>,
    visited: &mut BTreeSet<String>,
) -> Value {
    let visit_key = format!("model:{model_fqcn}");
    if !visited.insert(visit_key.clone()) {
        return schema_object(BTreeMap::new());
    }

    let hint = model_schema_hint(model_fqcn, source_index);
    let mut properties = BTreeMap::<String, Value>::new();

    for attribute in &hint.fillable {
        if hint.hidden.contains(attribute) {
            continue;
        }
        properties.insert(
            attribute.clone(),
            hint.casts
                .get(attribute)
                .map(|cast| schema_from_cast(cast))
                .unwrap_or_else(|| schema_string(None)),
        );
    }
    for (attribute, cast) in &hint.casts {
        if hint.hidden.contains(attribute) {
            continue;
        }
        properties
            .entry(attribute.clone())
            .or_insert_with(|| schema_from_cast(cast));
    }

    if let Some(model) = model_index.get(model_fqcn) {
        for relation_name in loaded_relations {
            let Some(relationship) = model
                .relationships
                .iter()
                .find(|item| &item.name == relation_name)
            else {
                continue;
            };
            let Some(related_fqcn) = &relationship.related else {
                continue;
            };
            let relation_schema = build_model_object_schema(
                related_fqcn,
                source_index,
                model_index,
                &BTreeSet::new(),
                visited,
            );
            properties.insert(
                relation_name.clone(),
                if relation_is_collection(&relationship.relation_type) {
                    schema_array(relation_schema)
                } else {
                    relation_schema
                },
            );
        }
    }

    visited.remove(&visit_key);
    schema_object(properties)
}

fn extract_json_response_calls(method_text: &str) -> Vec<JsonResponseCall> {
    let mut calls = Vec::new();
    let mut offset = 0usize;
    let needle = "response()->json(";

    while let Some(index) = method_text[offset..].find(needle) {
        let start = offset + index + "response()->json".len();
        let Some((args, _, end_index)) = extract_balanced_region(&method_text[start..], '(', ')')
        else {
            offset = start + 1;
            continue;
        };
        let arguments = split_top_level(&args, ',');
        let payload = arguments.first().cloned().unwrap_or_default();
        let status = arguments
            .get(1)
            .and_then(|value| value.trim().parse::<u16>().ok());
        calls.push(JsonResponseCall { payload, status });
        offset = start + end_index + 1;
    }

    calls
}

fn infer_schema_from_expression(
    expression: &str,
    controller: &ControllerMethod,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    visited: &mut BTreeSet<String>,
) -> Option<Value> {
    let expression = expression.trim();
    if expression.is_empty() {
        return None;
    }
    if let Some(string_value) = strip_php_string(expression) {
        let _ = string_value;
        return Some(schema_string(None));
    }
    if expression == "true" || expression == "false" {
        return Some(schema_boolean());
    }
    if expression.parse::<i64>().is_ok() {
        return Some(schema_integer());
    }
    if expression.parse::<f64>().is_ok() && expression.contains('.') {
        return Some(schema_number());
    }
    if expression.contains("->count()") {
        return Some(schema_integer());
    }
    if expression.contains("->avg(") {
        return Some(schema_number());
    }
    if expression.starts_with('[') {
        let (inner, _, _) = extract_balanced_region(expression, '[', ']')?;
        return Some(parse_php_array_schema(
            &inner,
            source_index,
            model_index,
            source_index.get(&controller.fqcn),
            resolve_primary_model_fqcn(controller, source_index).as_deref(),
            visited,
        ));
    }

    let class = source_index.get(&controller.fqcn);
    let state = collect_controller_source_state(controller, class);

    if let Some(captures) =
        Regex::new(r#"([A-Z][A-Za-z0-9_\\]*(?:Resource|Collection))::collection\("#)
            .expect("controller resource collection regex")
            .captures(expression)
    {
        let resource_name = captures.get(1)?.as_str();
        let fqcn = class
            .map(|value| value.resolve_name(resource_name))
            .unwrap_or_else(|| resource_name.to_string());
        return Some(schema_array(build_resource_schema(
            &fqcn,
            source_index,
            model_index,
            visited,
        )));
    }

    if let Some(captures) = Regex::new(r#"new\s+([A-Z][A-Za-z0-9_\\]*(?:Resource|Collection))\("#)
        .expect("controller new resource regex")
        .captures(expression)
    {
        let resource_name = captures.get(1)?.as_str();
        let fqcn = class
            .map(|value| value.resolve_name(resource_name))
            .unwrap_or_else(|| resource_name.to_string());
        return Some(build_resource_schema(
            &fqcn,
            source_index,
            model_index,
            visited,
        ));
    }

    if let Some(variable_name) = expression.strip_prefix('$') {
        let variable = variable_name
            .split(|ch: char| !ch.is_ascii_alphanumeric() && ch != '_')
            .next()
            .unwrap_or(variable_name);
        if let Some(parameter_type) = state.parameters.get(variable) {
            let loaded = extract_loaded_relations(expression);
            return Some(build_model_object_schema(
                parameter_type,
                source_index,
                model_index,
                &loaded,
                visited,
            ));
        }
        if let Some(assignment) = state.assignments.get(variable) {
            return infer_schema_from_assignment(
                assignment,
                controller,
                source_index,
                model_index,
                &state,
                visited,
            );
        }
    }

    infer_schema_from_assignment(
        expression,
        controller,
        source_index,
        model_index,
        &state,
        visited,
    )
}

fn infer_schema_from_assignment(
    expression: &str,
    controller: &ControllerMethod,
    source_index: &SourceIndex,
    model_index: &BTreeMap<String, ModelFacts>,
    state: &ControllerSourceState,
    visited: &mut BTreeSet<String>,
) -> Option<Value> {
    let expression = expression.trim();
    let controller_class = source_index.get(&controller.fqcn);

    if expression.contains("paginate(") {
        let model_fqcn =
            infer_model_fqcn_from_expression(expression, controller_class, source_index)?;
        let item_schema = build_model_object_schema(
            &model_fqcn,
            source_index,
            model_index,
            &BTreeSet::new(),
            visited,
        );
        return Some(schema_paginated(item_schema));
    }
    if expression.contains("::all()") || expression.contains("->get()") {
        let model_fqcn =
            infer_model_fqcn_from_expression(expression, controller_class, source_index)?;
        let item_schema = build_model_object_schema(
            &model_fqcn,
            source_index,
            model_index,
            &BTreeSet::new(),
            visited,
        );
        return Some(schema_array(item_schema));
    }
    if expression.contains("::create(") || expression.contains("->create(") {
        let model_fqcn =
            infer_model_fqcn_from_expression(expression, controller_class, source_index)?;
        return Some(build_model_object_schema(
            &model_fqcn,
            source_index,
            model_index,
            &extract_loaded_relations(expression),
            visited,
        ));
    }
    if expression.contains("->load(")
        || expression.contains("->fresh(")
        || expression.contains("->update(")
    {
        if let Some(variable_name) = expression.strip_prefix('$') {
            let variable = variable_name
                .split(|ch: char| !ch.is_ascii_alphanumeric() && ch != '_')
                .next()
                .unwrap_or(variable_name);
            if let Some(parameter_type) = state.parameters.get(variable) {
                return Some(build_model_object_schema(
                    parameter_type,
                    source_index,
                    model_index,
                    &extract_loaded_relations(expression),
                    visited,
                ));
            }
        }
    }

    None
}

fn collect_controller_source_state(
    controller: &ControllerMethod,
    class: Option<&SourceClass>,
) -> ControllerSourceState {
    let mut state = ControllerSourceState::default();
    let signature_re =
        Regex::new(r#"function\s+[A-Za-z_][A-Za-z0-9_]*\s*\(([^)]*)\)"#).expect("signature regex");
    if let Some(captures) = signature_re.captures(&controller.body_text) {
        if let Some(group) = captures.get(1) {
            for parameter in split_top_level(group.as_str(), ',') {
                let param_re =
                    Regex::new(r#"(?:(\??[A-Z][A-Za-z0-9_\\]*)\s+)?\$([A-Za-z_][A-Za-z0-9_]*)"#)
                        .expect("parameter regex");
                let Some(captures) = param_re.captures(&parameter) else {
                    continue;
                };
                let Some(name) = captures.get(2) else {
                    continue;
                };
                let Some(type_name) = captures.get(1) else {
                    continue;
                };
                let raw_type = type_name.as_str().trim_start_matches('?');
                let resolved = class
                    .map(|item| item.resolve_name(raw_type))
                    .unwrap_or_else(|| raw_type.to_string());
                state.parameters.insert(name.as_str().to_string(), resolved);
            }
        }
    }

    let method_body = controller
        .body_text
        .find('{')
        .and_then(|index| extract_balanced_region(&controller.body_text[index..], '{', '}'))
        .map(|(body, _, _)| body)
        .unwrap_or_else(|| controller.body_text.clone());

    for statement in split_top_level(&method_body, ';') {
        let assign_re =
            Regex::new(r#"^\s*\$([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$"#).expect("assign regex");
        let Some(captures) = assign_re.captures(&statement) else {
            continue;
        };
        let Some(name) = captures.get(1) else {
            continue;
        };
        let Some(value) = captures.get(2) else {
            continue;
        };
        state
            .assignments
            .insert(name.as_str().to_string(), value.as_str().trim().to_string());
    }

    state
}

fn infer_model_fqcn_from_expression(
    expression: &str,
    class: Option<&SourceClass>,
    source_index: &SourceIndex,
) -> Option<String> {
    let model_call_re =
        Regex::new(r#"([A-Z][A-Za-z0-9_\\]*)::(?:query|with|create|all|get|find|where)"#)
            .expect("model call regex");
    if let Some(captures) = model_call_re.captures(expression) {
        let raw = captures.get(1)?.as_str();
        let fqcn = class
            .map(|item| item.resolve_name(raw))
            .unwrap_or_else(|| raw.to_string());
        return Some(fqcn);
    }

    let relation_create_re =
        Regex::new(r#"->([a-z][A-Za-z0-9_]*)\(\)->create\("#).expect("relation create regex");
    if let Some(captures) = relation_create_re.captures(expression) {
        let relation = captures.get(1)?.as_str();
        let singular = singularize(relation);
        return source_index.find_model_by_basename(&singular);
    }

    None
}

fn resolve_primary_model_fqcn(
    controller: &ControllerMethod,
    source_index: &SourceIndex,
) -> Option<String> {
    let controller_name = class_basename(&controller.fqcn);
    let model_name = controller_name.trim_end_matches("Controller");
    let singular = singularize(model_name);
    source_index.find_model_by_basename(&singular)
}

fn extract_loaded_relations(expression: &str) -> BTreeSet<String> {
    let load_re =
        Regex::new(r#"->(?:load|fresh)\(\s*\[([^\]]*)\]"#).expect("loaded relation regex");
    let Some(captures) = load_re.captures(expression) else {
        return BTreeSet::new();
    };
    let Some(group) = captures.get(1) else {
        return BTreeSet::new();
    };

    split_top_level(group.as_str(), ',')
        .into_iter()
        .filter_map(|item| strip_php_string(&item))
        .map(|relation| relation.split('.').next().unwrap_or(&relation).to_string())
        .collect()
}

fn model_schema_hint(model_fqcn: &str, source_index: &SourceIndex) -> ModelSchemaHint {
    let Some(class) = source_index.get(model_fqcn) else {
        return ModelSchemaHint::default();
    };

    ModelSchemaHint {
        fillable: parse_string_array_property(&class.source_text, "fillable"),
        hidden: parse_string_array_property(&class.source_text, "hidden"),
        casts: parse_string_map_property(&class.source_text, "casts"),
    }
}

fn parse_string_array_property(source: &str, property: &str) -> BTreeSet<String> {
    extract_array_property(source, property)
        .map(|body| {
            split_top_level(&body, ',')
                .into_iter()
                .filter_map(|item| strip_php_string(&item))
                .collect()
        })
        .unwrap_or_default()
}

fn parse_string_map_property(source: &str, property: &str) -> BTreeMap<String, String> {
    let mut values = BTreeMap::new();
    let Some(body) = extract_array_property(source, property) else {
        return values;
    };

    for entry in split_top_level(&body, ',') {
        let Some((raw_key, raw_value)) = split_top_level_key_value(&entry) else {
            continue;
        };
        let Some(key) = strip_php_string(&raw_key) else {
            continue;
        };
        let value = strip_php_string(&raw_value).unwrap_or(raw_value.trim().to_string());
        values.insert(key, value);
    }

    values
}

fn extract_array_property(source: &str, property: &str) -> Option<String> {
    let property_re = Regex::new(&format!(
        r#"(?s)(?:public|protected|private)\s+\${}\s*=\s*\["#,
        regex::escape(property)
    ))
    .expect("array property regex");
    let property_match = property_re.find(source)?;
    let start = source[property_match.start()..].find('[')? + property_match.start();
    extract_balanced_region(&source[start..], '[', ']').map(|(body, _, _)| body)
}

fn is_resource_class(class: &SourceClass) -> bool {
    class.fqcn.contains("\\Http\\Resources\\")
        || class.extends.as_ref().is_some_and(|extends| {
            extends.ends_with("JsonResource") || extends.ends_with("ResourceCollection")
        })
        || class.class_name.ends_with("Resource")
        || class.class_name.ends_with("Collection")
}

fn resolve_model_fqcn_from_resource(
    resource_fqcn: &str,
    source_index: &SourceIndex,
) -> (Option<String>, String) {
    let model_name = class_basename(resource_fqcn)
        .trim_end_matches("Collection")
        .trim_end_matches("Resource")
        .to_string();
    let relation = model_name.to_lowercase();
    let model_fqcn = source_index.find_model_by_basename(&model_name);
    (model_fqcn, relation)
}

fn infer_collection_item_resource(
    collection_fqcn: &str,
    source_index: &SourceIndex,
) -> Option<String> {
    let candidate = class_basename(collection_fqcn)
        .trim_end_matches("Collection")
        .to_string();
    let resource_fqcn = source_index
        .classes
        .keys()
        .find(|fqcn| fqcn.ends_with(&format!("\\{}Resource", candidate)))
        .cloned();
    resource_fqcn
}

fn schema_from_cast(cast: &str) -> Value {
    let normalized = cast.trim().to_ascii_lowercase();
    if normalized.starts_with("int") {
        schema_integer()
    } else if normalized.starts_with("decimal") || normalized.starts_with("float") {
        schema_number()
    } else if normalized.starts_with("bool") {
        schema_boolean()
    } else if normalized.starts_with("date") || normalized.starts_with("datetime") {
        schema_string(Some("date-time"))
    } else if normalized == "array" || normalized == "collection" || normalized == "json" {
        schema_array(schema_object(BTreeMap::new()))
    } else {
        schema_string(None)
    }
}

fn schema_type(schema: &Value) -> Option<&str> {
    schema.get("type").and_then(Value::as_str)
}

fn schema_object(properties: BTreeMap<String, Value>) -> Value {
    if properties.is_empty() {
        json!({ "type": "object" })
    } else {
        json!({ "type": "object", "properties": properties })
    }
}

fn schema_array(items: Value) -> Value {
    json!({ "type": "array", "items": items })
}

fn schema_paginated(item_schema: Value) -> Value {
    json!({
        "type": "object",
        "properties": {
            "data": schema_array(item_schema),
            "meta": { "type": "object" },
            "links": { "type": "object" }
        }
    })
}

fn schema_string(format: Option<&str>) -> Value {
    match format {
        Some(format) => json!({ "type": "string", "format": format }),
        None => json!({ "type": "string" }),
    }
}

fn schema_integer() -> Value {
    json!({ "type": "integer" })
}

fn schema_number() -> Value {
    json!({ "type": "number" })
}

fn schema_boolean() -> Value {
    json!({ "type": "boolean" })
}

fn schema_nullable(schema: Value) -> Value {
    let mut value = schema;
    if let Some(object) = value.as_object_mut() {
        object.insert("nullable".to_string(), Value::Bool(true));
    }
    value
}

fn relation_is_collection(relation_type: &str) -> bool {
    matches!(
        relation_type,
        "hasMany" | "belongsToMany" | "morphMany" | "morphToMany" | "morphByMany"
    )
}

fn singularize(value: &str) -> String {
    value.strip_suffix('s').unwrap_or(value).to_string()
}

fn extract_inertia_component(text: &str) -> Option<String> {
    let inertia_re = Regex::new(r#"Inertia::render\(\s*['"]([^'"]+)['"]"#).expect("inertia regex");
    inertia_re
        .captures(text)
        .and_then(|captures| captures.get(1))
        .map(|item| item.as_str().to_string())
}

fn extract_route_redirect_target(text: &str) -> Option<String> {
    let redirect_re = Regex::new(r#"(?:redirect\(\)->route|to_route)\(\s*['"]([^'"]+)['"]"#)
        .expect("redirect regex");
    redirect_re
        .captures(text)
        .and_then(|captures| captures.get(1))
        .map(|item| item.as_str().to_string())
}

fn extract_download_filename(text: &str) -> Option<String> {
    let filename_re =
        Regex::new(r#"download\([^,]+,\s*['"]([^'"]+)['"]"#).expect("download filename regex");
    filename_re
        .captures(text)
        .and_then(|captures| captures.get(1))
        .map(|item| item.as_str().to_string())
}

fn extract_channel_params(channel: &str) -> Vec<String> {
    let channel_re = Regex::new(r#"\{([^}]+)\}"#).expect("channel params regex");
    let mut params = channel_re
        .captures_iter(channel)
        .filter_map(|captures| captures.get(1).map(|item| item.as_str().to_string()))
        .collect::<Vec<_>>();
    params.sort();
    params.dedup();
    params
}

fn infer_broadcast_visibility(channel: &str, explicit: Option<&str>) -> String {
    if let Some(explicit) = explicit {
        return explicit.to_string();
    }

    let normalized = channel.to_ascii_lowercase();
    if normalized.contains("presence") {
        "presence".to_string()
    } else if normalized.contains("public") {
        "public".to_string()
    } else {
        "private".to_string()
    }
}

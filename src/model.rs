use std::path::PathBuf;

use serde::Serialize;

#[derive(Debug, Clone)]
pub struct AnalyzedFile {
    pub path: PathBuf,
    pub relative_path: String,
    pub source_text: String,
    pub facts: FileFacts,
}

#[derive(Debug, Clone, Default)]
pub struct FileFacts {
    pub controllers: Vec<ControllerMethod>,
    pub models: Vec<ModelFacts>,
    pub polymorphic: Vec<PolymorphicFact>,
    pub broadcast: Vec<BroadcastFact>,
}

#[derive(Debug, Clone, Default)]
pub struct ControllerMethod {
    pub class_name: String,
    pub fqcn: String,
    pub method_name: String,
    pub body_text: String,
    pub http_status: Option<u16>,
    pub resource_usage: Vec<ResourceUsageFact>,
    pub request_usage: Vec<RequestUsageFact>,
    pub scopes_used: Vec<ScopeUsageFact>,
}

#[derive(Debug, Clone, Default)]
pub struct ModelFacts {
    pub class_name: String,
    pub fqcn: String,
    pub relationships: Vec<ModelRelationshipFact>,
    pub scopes: Vec<String>,
    pub attributes: Vec<String>,
}

#[derive(Debug, Clone, Default)]
pub struct PolymorphicFact {
    pub name: String,
    pub discriminator: String,
    pub model: String,
    pub relation: String,
}

#[derive(Debug, Clone, Default)]
pub struct BroadcastFact {
    pub channel: String,
    pub channel_type: Option<String>,
    pub parameters: Vec<BroadcastParameterFact>,
}

#[derive(Debug, Clone, Default)]
pub struct RequestUsageFact {
    pub method: String,
    pub rules: Vec<String>,
    pub fields: Vec<String>,
    pub location: Option<String>,
    pub class_name: Option<String>,
}

#[derive(Debug, Clone, Default)]
pub struct ResourceUsageFact {
    pub class_name: String,
    pub method: Option<String>,
}

#[derive(Debug, Clone, Default)]
pub struct ScopeUsageFact {
    pub name: String,
    pub on: Option<String>,
}

#[derive(Debug, Clone, Default)]
pub struct ModelRelationshipFact {
    pub name: String,
    pub relation_type: String,
    pub related: Option<String>,
    pub pivot_columns: Vec<String>,
    pub pivot_alias: Option<String>,
    pub pivot_timestamps: bool,
    pub morph_name: Option<String>,
}

#[derive(Debug, Clone, Default)]
pub struct BroadcastParameterFact {
    pub name: String,
    pub parameter_type: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct Delta {
    pub meta: DeltaMeta,
    pub controllers: Vec<ControllerOut>,
    pub models: Vec<ModelOut>,
    pub polymorphic: Vec<PolymorphicOut>,
    pub broadcast: Vec<BroadcastOut>,
}

#[derive(Debug, Serialize)]
pub struct DeltaMeta {
    pub partial: bool,
    pub stats: StatsOut,
}

#[derive(Debug, Serialize)]
pub struct StatsOut {
    #[serde(rename = "filesParsed")]
    pub files_parsed: usize,
    pub skipped: usize,
    #[serde(rename = "durationMs")]
    pub duration_ms: u128,
}

#[derive(Debug, Serialize)]
pub struct ControllerOut {
    #[serde(rename = "class")]
    pub fqcn: String,
    pub file: String,
    pub methods: Vec<ControllerMethodOut>,
}

#[derive(Debug, Serialize)]
pub struct ControllerMethodOut {
    pub name: String,
    #[serde(default, rename = "httpMethods", skip_serializing_if = "Vec::is_empty")]
    pub http_methods: Vec<String>,
    #[serde(default, rename = "httpStatus", skip_serializing_if = "Vec::is_empty")]
    pub http_status: Vec<u16>,
    #[serde(
        default,
        rename = "requestUsage",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub request_usage: Vec<RequestUsageOut>,
    #[serde(
        default,
        rename = "resourceUsage",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub resource_usage: Vec<ResourceUsageOut>,
    #[serde(default, rename = "scopesUsed", skip_serializing_if = "Vec::is_empty")]
    pub scopes_used: Vec<ScopeUsedOut>,
}

#[derive(Debug, Serialize)]
pub struct RequestUsageOut {
    pub method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub class: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub rules: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub fields: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub location: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ResourceUsageOut {
    pub class: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub method: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ScopeUsedOut {
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub on: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ModelOut {
    #[serde(rename = "class")]
    pub fqcn: String,
    pub file: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub relationships: Vec<ModelRelationshipOut>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub scopes: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub attributes: Vec<String>,
    #[serde(default, rename = "withPivot", skip_serializing_if = "Vec::is_empty")]
    pub with_pivot: Vec<PivotOut>,
}

#[derive(Debug, Serialize)]
pub struct PolymorphicOut {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub discriminator: Option<String>,
    pub relations: Vec<PolymorphicRelationOut>,
}

#[derive(Debug, Serialize)]
pub struct BroadcastOut {
    pub channel: String,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub channel_type: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub parameters: Vec<BroadcastParameterOut>,
}

#[derive(Debug, Serialize)]
pub struct ModelRelationshipOut {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub relation_type: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub related: Option<String>,
    #[serde(default, rename = "withPivot", skip_serializing_if = "Vec::is_empty")]
    pub with_pivot: Vec<PivotOut>,
}

#[derive(Debug, Serialize)]
pub struct PivotOut {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub relation: Option<String>,
    pub columns: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct PolymorphicRelationOut {
    pub model: String,
    #[serde(rename = "type")]
    pub relation_type: String,
}

#[derive(Debug, Serialize)]
pub struct BroadcastParameterOut {
    pub name: String,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub parameter_type: Option<String>,
}

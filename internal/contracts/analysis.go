//go:build goexperiment.jsonv2

package contracts

import (
	"fmt"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
)

const ContractVersion = "oxcribe.oxinfer.v2"

const (
	ActionKindControllerMethod    = "controller_method"
	ActionKindInvokableController = "invokable_controller"
	ActionKindClosure             = "closure"
	ActionKindUnknown             = "unknown"
)

const (
	ResponseStatusOK      = "ok"
	ResponseStatusPartial = "partial"
)

const (
	MatchStatusMatched       = "matched"
	MatchStatusRuntimeOnly   = "runtime_only"
	MatchStatusUnsupported   = "unsupported"
	MatchStatusMissingStatic = "missing_static"
)

const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

const (
	ScopeRequest = "request"
	ScopeRoute   = "route"
	ScopeAction  = "action"
	ScopeModel   = "model"
	ScopeGlobal  = "global"
)

const (
	ReasonCodeClosureAction       = "closure_action"
	ReasonCodeUnknownAction       = "unknown_action"
	ReasonCodeMissingStaticAction = "missing_static_action"
)

const (
	DiagnosticCodeRouteRuntimeOnlyClosure  = "route.runtime_only.closure"
	DiagnosticCodeRouteActionUnsupported   = "route.action.unsupported"
	DiagnosticCodeRouteActionMissingStatic = "route.action.missing_static"
	DiagnosticCodeStaticAnalysisPartial    = "analysis.static.partial"
)

type AnalysisRequest struct {
	ContractVersion    string            `json:"contractVersion"`
	RequestID          string            `json:"requestId"`
	RuntimeFingerprint string            `json:"runtimeFingerprint"`
	Manifest           manifest.Manifest `json:"manifest"`
	Runtime            RuntimeSnapshot   `json:"runtime"`
}

type RuntimeSnapshot struct {
	App      RuntimeApp      `json:"app"`
	Routes   []RuntimeRoute  `json:"routes"`
	Packages []RuntimePackage `json:"packages,omitempty"`
}

type RuntimeApp struct {
	BasePath       string `json:"basePath"`
	LaravelVersion string `json:"laravelVersion"`
	PHPVersion     string `json:"phpVersion"`
	AppEnv         string `json:"appEnv"`
}

type RuntimeRoute struct {
	RouteID    string         `json:"routeId"`
	Methods    []string       `json:"methods"`
	URI        string         `json:"uri"`
	Domain     *string        `json:"domain"`
	Name       *string        `json:"name"`
	Prefix     *string        `json:"prefix"`
	Middleware []string       `json:"middleware"`
	Where      map[string]any `json:"where"`
	Defaults   map[string]any `json:"defaults"`
	Bindings   []RouteBinding `json:"bindings"`
	Action     RouteAction    `json:"action"`
}

type RuntimePackage struct {
	Name    string  `json:"name"`
	Version *string `json:"version,omitempty"`
}

type RouteBinding struct {
	Parameter  string  `json:"parameter"`
	Kind       string  `json:"kind"`
	TargetFQCN *string `json:"targetFqcn,omitempty"`
	IsImplicit bool    `json:"isImplicit"`
}

type RouteAction struct {
	Kind   string  `json:"kind"`
	FQCN   *string `json:"fqcn,omitempty"`
	Method *string `json:"method,omitempty"`
}

type AnalysisResponse struct {
	ContractVersion    string        `json:"contractVersion"`
	RequestID          string        `json:"requestId"`
	RuntimeFingerprint string        `json:"runtimeFingerprint"`
	Status             string        `json:"status"`
	Meta               ResponseMeta  `json:"meta"`
	Delta              emitter.Delta `json:"delta"`
	RouteMatches       []RouteMatch  `json:"routeMatches"`
	Diagnostics        []Diagnostic  `json:"diagnostics"`
}

type ResponseMeta struct {
	OxinferVersion   string            `json:"oxinferVersion"`
	Partial          bool              `json:"partial"`
	Stats            emitter.MetaStats `json:"stats"`
	DiagnosticCounts DiagnosticCounts  `json:"diagnosticCounts"`
}

type DiagnosticCounts struct {
	Info  int `json:"info"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
}

type RouteMatch struct {
	RouteID     string  `json:"routeId"`
	ActionKind  string  `json:"actionKind"`
	ActionKey   *string `json:"actionKey,omitempty"`
	MatchStatus string  `json:"matchStatus"`
	ReasonCode  *string `json:"reasonCode,omitempty"`
}

type Diagnostic struct {
	Code      string  `json:"code"`
	Severity  string  `json:"severity"`
	Scope     string  `json:"scope"`
	Message   string  `json:"message"`
	RouteID   *string `json:"routeId,omitempty"`
	ActionKey *string `json:"actionKey,omitempty"`
	File      *string `json:"file,omitempty"`
	Line      *int    `json:"line,omitempty"`
}

func (r *AnalysisRequest) Normalize() {
	if r.Runtime.Routes == nil {
		r.Runtime.Routes = []RuntimeRoute{}
	}
	if r.Runtime.Packages == nil {
		r.Runtime.Packages = []RuntimePackage{}
	}

	for i := range r.Runtime.Routes {
		route := &r.Runtime.Routes[i]
		if route.Methods == nil {
			route.Methods = []string{}
		}
		if route.Middleware == nil {
			route.Middleware = []string{}
		}
		if route.Where == nil {
			route.Where = map[string]any{}
		}
		if route.Defaults == nil {
			route.Defaults = map[string]any{}
		}
		if route.Bindings == nil {
			route.Bindings = []RouteBinding{}
		}
		if route.Action.Kind == ActionKindInvokableController && route.Action.Method == nil {
			method := "__invoke"
			route.Action.Method = &method
		}
	}
}

func (a RouteAction) ActionKey() (string, bool) {
	switch a.Kind {
	case ActionKindControllerMethod, ActionKindInvokableController:
		if a.FQCN == nil || a.Method == nil || *a.FQCN == "" || *a.Method == "" {
			return "", false
		}
		return fmt.Sprintf("%s::%s", *a.FQCN, *a.Method), true
	default:
		return "", false
	}
}

func ControllerActionKey(controller emitter.Controller) string {
	return fmt.Sprintf("%s::%s", controller.FQCN, controller.Method)
}

//go:build goexperiment.jsonv2

package manifest

import (
	"encoding/json/v2"
	"encoding/json/jsontext"
)

// Manifest represents the structure of the manifest configuration file
// according to the JSON schema specification in plan.md section 10
type Manifest struct {
	Project  ProjectConfig  `json:"project"`
	Scan     ScanConfig     `json:"scan"`
	Limits   *LimitsConfig  `json:"limits,omitempty"`
	Cache    *CacheConfig   `json:"cache,omitempty"`
	Features *FeatureConfig `json:"features,omitempty"`
}

// ProjectConfig contains project-specific configuration
type ProjectConfig struct {
	Root     string `json:"root"`
	Composer string `json:"composer"`
}

// ScanConfig contains scanning configuration settings
type ScanConfig struct {
	Targets         []string `json:"targets"`
	VendorWhitelist []string `json:"vendor_whitelist,omitempty"`
	Globs           []string `json:"globs,omitempty"`
}

// LimitsConfig contains analysis limits and constraints
type LimitsConfig struct {
	MaxWorkers *int `json:"max_workers,omitempty"`
	MaxFiles   *int `json:"max_files,omitempty"`
	MaxDepth   *int `json:"max_depth,omitempty"`
}

// CacheConfig contains caching configuration
type CacheConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Kind    *string `json:"kind,omitempty"`
}

// FeatureConfig controls which analysis features are enabled
type FeatureConfig struct {
	HTTPStatus        *bool `json:"http_status,omitempty"`
	RequestUsage      *bool `json:"request_usage,omitempty"`
	ResourceUsage     *bool `json:"resource_usage,omitempty"`
	WithPivot         *bool `json:"with_pivot,omitempty"`
	AttributeMake     *bool `json:"attribute_make,omitempty"`
	ScopesUsed        *bool `json:"scopes_used,omitempty"`
	Polymorphic       *bool `json:"polymorphic,omitempty"`
	BroadcastChannels *bool `json:"broadcast_channels,omitempty"`
}

// ToJSON serializes the manifest to JSON bytes.
func (m *Manifest) ToJSON() ([]byte, error) {
	return json.Marshal(m, json.Deterministic(true), jsontext.WithIndent("  "))
}

// Backward compatibility type alias for legacy code
type Features = FeatureConfig

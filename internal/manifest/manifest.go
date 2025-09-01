package manifest

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
	MaxFiles    *int `json:"max_files,omitempty"`
	MaxFileSize *int `json:"max_file_size,omitempty"`
	Timeout     *int `json:"timeout,omitempty"`
}

// CacheConfig contains caching configuration
type CacheConfig struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir,omitempty"`
	TTL     *int   `json:"ttl,omitempty"`
}

// FeatureConfig controls which analysis features are enabled
type FeatureConfig struct {
	Routes      *bool `json:"routes,omitempty"`
	Controllers *bool `json:"controllers,omitempty"`
	Models      *bool `json:"models,omitempty"`
	Middleware  *bool `json:"middleware,omitempty"`
	Migrations  *bool `json:"migrations,omitempty"`
	Policies    *bool `json:"policies,omitempty"`
	Events      *bool `json:"events,omitempty"`
	Jobs        *bool `json:"jobs,omitempty"`
}
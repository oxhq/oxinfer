package config

import (
	"time"
)

type OxinferConfig struct {
	Version    string           `json:"version"`
	Logging    LoggingConfig    `json:"logging"`
	Scoring    ScoringConfig    `json:"scoring"`
	Processing ProcessingConfig `json:"processing"`
	Cache      CacheConfig      `json:"cache"`
	Output     OutputConfig     `json:"output"`
}

type ScoringConfig struct {
	ConfidenceThreshold float64            `json:"confidenceThreshold"`
	FeatureWeights      map[string]float64 `json:"featureWeights"`
	ExcludeFeatures     []string           `json:"excludeFeatures"`
	Penalties           map[string]float64 `json:"penalties"`
	DebugBreakdown      bool               `json:"debugBreakdown"`
}

type LoggingConfig struct {
	Level          string            `json:"level"`
	OutputPath     string            `json:"outputPath"`
	StructuredJSON bool              `json:"structuredJSON"`
	RotateDaily    bool              `json:"rotateDaily"`
	KeepDays       int               `json:"keepDays"`
	Components     map[string]string `json:"components"`
}

type ProcessingConfig struct {
	MaxWorkers     int           `json:"maxWorkers"`
	MaxFiles       int           `json:"maxFiles"`
	Timeout        time.Duration `json:"timeout"`
	VerboseScopes  bool          `json:"verboseScopes"`
	StrictEmission bool          `json:"strictEmission"`
}

type CacheConfig struct {
	Enabled     bool   `json:"enabled"`
	Directory   string `json:"directory"`
	MaxSize     int64  `json:"maxSize"`
	TTLDuration string `json:"ttlDuration"`
}

type OutputConfig struct {
	PrettyPrint       bool   `json:"prettyPrint"`
	IncludeStats      bool   `json:"includeStats"`
	Deterministic     bool   `json:"deterministic"`
	ValidateSchema    bool   `json:"validateSchema"`
	RelativeToProject bool   `json:"relativeToProject"`
	DefaultOutputDir  string `json:"defaultOutputDir"`
}

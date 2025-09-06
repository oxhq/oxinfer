//go:build goexperiment.jsonv2

package config

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ConfigLoader struct {
	basePath string
}

func NewConfigLoader(basePath string) *ConfigLoader {
	return &ConfigLoader{basePath: basePath}
}

func (c *ConfigLoader) Load(configPath string) (*OxinferConfig, error) {
	config := c.getDefaults()
	
	if err := c.loadConfigFile(filepath.Join(c.basePath, ".oxinfer/config/default.json"), config); err != nil {
	}
	
	if err := c.loadConfigFile(filepath.Join(c.basePath, ".oxinfer/config/local.json"), config); err != nil {
	}
	
	if configPath != "" {
		if err := c.loadConfigFile(configPath, config); err != nil {
			return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
		}
	}
	
	c.applyEnvOverrides(config)
	
	if err := c.validate(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	
	return config, nil
}

func (c *ConfigLoader) loadConfigFile(path string, config *OxinferConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, config)
}

func (c *ConfigLoader) getDefaults() *OxinferConfig {
	return &OxinferConfig{
		Version: "1.0",
		Logging: LoggingConfig{
			Level:          "info",
			OutputPath:     ".oxinfer/logs",
			StructuredJSON: true,
			RotateDaily:    true,
			KeepDays:       7,
			Components: map[string]string{
				"parser":   "info",
				"matcher":  "info",
				"registry": "debug",
			},
		},
		Scoring: ScoringConfig{
			ConfidenceThreshold: 0.91,
			FeatureWeights: map[string]float64{
				"httpStatusExplicit": 0.25,
				"resourcesFound":     0.20,
				"requestShapes":      0.15,
				"scopesValid":        0.15,
				"pivotsValid":        0.10,
				"attributesNamed":    0.10,
				"broadcastClean":     0.05,
			},
			ExcludeFeatures: []string{"broadcast"},
			Penalties: map[string]float64{
				"placeholderFound":      -0.15,
				"unknownAttributes":     -0.10,
				"builderScopesDetected": -0.20,
			},
			DebugBreakdown: false,
		},
		Processing: ProcessingConfig{
			MaxWorkers:     8,
			MaxFiles:       1000,
			Timeout:        time.Minute * 5,
			VerboseScopes:  false,
			StrictEmission: true,
		},
		Cache: CacheConfig{
			Enabled:     true,
			Directory:   ".oxinfer/cache",
			MaxSize:     100 * 1024 * 1024,
			TTLDuration: "24h",
		},
		Output: OutputConfig{
			PrettyPrint:       false,
			IncludeStats:      true,
			Deterministic:     true,
			ValidateSchema:    true,
			RelativeToProject: false,
			DefaultOutputDir:  ".",
		},
	}
}

func (c *ConfigLoader) applyEnvOverrides(config *OxinferConfig) {
}

func (c *ConfigLoader) validate(config *OxinferConfig) error {
	return nil
}

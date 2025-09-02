package perf

import (
	"time"

	"github.com/garaekz/oxinfer/internal/bench"
)

// AnalysisResults contains the complete results of performance analysis.
type AnalysisResults struct {
	Timestamp       time.Time               `json:"timestamp"`
	AnalysisDuration time.Duration          `json:"analysisDurationMs"`
	Hotspots        []Hotspot               `json:"hotspots"`
	Recommendations []Recommendation        `json:"recommendations"`
	Regressions     []RegressionResult      `json:"regressions"`
	TargetsMet      bench.TargetResults     `json:"targetsMet"`
	SystemInfo      SystemInfo              `json:"systemInfo"`
	Summary         string                  `json:"summary"`
}

// RegressionResult represents a detected performance regression.
type RegressionResult struct {
	Scenario        string        `json:"scenario"`
	Metric          string        `json:"metric"`
	BaselineValue   float64       `json:"baselineValue"`
	CurrentValue    float64       `json:"currentValue"`
	RegressionRatio float64       `json:"regressionRatio"`
	Severity        SeverityLevel `json:"severity"`
}

// OptimizationTarget defines a specific performance optimization target.
type OptimizationTarget struct {
	Component       string        `json:"component"`
	Phase           string        `json:"phase"`
	Metric          string        `json:"metric"`
	CurrentValue    float64       `json:"currentValue"`
	TargetValue     float64       `json:"targetValue"`
	ImprovementPct  float64       `json:"improvementPct"`
	Priority        Priority      `json:"priority"`
}

// PerformanceTargets defines the MVP performance requirements for the Oxinfer pipeline.
type PerformanceTargets struct {
	ColdRun        time.Duration `json:"coldRunTargetMs"`        // <10s for 200-600 files
	IncrementalRun time.Duration `json:"incrementalRunTargetMs"` // <2s with cache
	MemoryPeak     int64         `json:"memoryPeakTargetMB"`     // <500MB for medium projects
	CPUEfficiency  float64       `json:"cpuEfficiencyTarget"`    // >80% useful CPU time
	
	// Throughput targets
	FilesPerSecond float64       `json:"filesPerSecondTarget"`   // Minimum files processed per second
	PatternsPerSec float64       `json:"patternsPerSecTarget"`   // Minimum patterns matched per second
	
	// Quality targets
	ErrorRate      float64       `json:"errorRateTarget"`        // Maximum acceptable error rate
	CacheHitRate   float64       `json:"cacheHitRateTarget"`     // Minimum cache hit rate for incremental runs
}

// MVPPerformanceTargets returns the performance targets required for MVP.
func MVPPerformanceTargets() *PerformanceTargets {
	return &PerformanceTargets{
		ColdRun:        10 * time.Second,
		IncrementalRun: 2 * time.Second,
		MemoryPeak:     500, // 500MB
		CPUEfficiency:  0.8, // 80%
		FilesPerSecond: 50,  // Minimum 50 files/second for medium projects
		PatternsPerSec: 100, // Minimum 100 patterns/second
		ErrorRate:      0.01, // Maximum 1% error rate
		CacheHitRate:   0.9,  // Minimum 90% cache hit rate for incremental
	}
}

// OptimizationPlan contains a structured plan for implementing performance optimizations.
type OptimizationPlan struct {
	ID               string                `json:"id"`
	CreatedAt        time.Time             `json:"createdAt"`
	Targets          *PerformanceTargets   `json:"targets"`
	Optimizations    []OptimizationTarget  `json:"optimizations"`
	EstimatedImpact  float64               `json:"estimatedImpact"`  // Overall improvement percentage
	ImplementationTime string              `json:"implementationTime"` // Estimated development time
	
	// Phased implementation
	Phases          []OptimizationPhase   `json:"phases"`
	Dependencies    map[string][]string   `json:"dependencies"`     // Optimization dependencies
}

// OptimizationPhase groups related optimizations for phased implementation.
type OptimizationPhase struct {
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Optimizations   []string             `json:"optimizations"`    // References to optimization IDs
	EstimatedImpact float64              `json:"estimatedImpact"`
	Duration        string               `json:"duration"`
	Prerequisites   []string             `json:"prerequisites"`
}

// PerformanceReport provides a comprehensive performance analysis report.
type PerformanceReport struct {
	Metadata        ReportMetadata       `json:"metadata"`
	Executive       ExecutiveSummary     `json:"executive"`
	Analysis        *AnalysisResults     `json:"analysis"`
	Plan            *OptimizationPlan    `json:"optimizationPlan"`
	Implementation  ImplementationGuide  `json:"implementation"`
}

// ReportMetadata contains metadata about the performance report.
type ReportMetadata struct {
	GeneratedAt     time.Time    `json:"generatedAt"`
	Version         string       `json:"version"`
	AnalyzerVersion string       `json:"analyzerVersion"`
	ProjectInfo     ProjectInfo  `json:"projectInfo"`
}

// ProjectInfo contains information about the analyzed project.
type ProjectInfo struct {
	Name        string `json:"name"`
	FileCount   int    `json:"fileCount"`
	ProjectSize string `json:"projectSize"` // small, medium, large
	Complexity  string `json:"complexity"`  // low, medium, high
}

// ExecutiveSummary provides high-level performance analysis results.
type ExecutiveSummary struct {
	OverallScore      float64       `json:"overallScore"`       // 0-100 performance score
	TargetsMet        bool          `json:"targetsMet"`         // Whether MVP targets are met
	CriticalIssues    int           `json:"criticalIssues"`     // Number of critical performance issues
	EstimatedSpeedup  float64       `json:"estimatedSpeedup"`   // Potential speedup from optimizations
	RecommendedActions []string     `json:"recommendedActions"` // Top 3 recommended actions
	
	// MVP-specific status
	MVPReady         bool          `json:"mvpReady"`           // Whether performance meets MVP requirements
	BlockingIssues   []string      `json:"blockingIssues"`     // Issues preventing MVP launch
}

// ImplementationGuide provides detailed guidance for implementing optimizations.
type ImplementationGuide struct {
	QuickWins       []QuickWin        `json:"quickWins"`        // Low-effort, high-impact optimizations
	MajorChanges    []MajorChange     `json:"majorChanges"`     // High-effort optimizations
	Timeline        Timeline          `json:"timeline"`         // Suggested implementation timeline
	RiskAssessment  RiskAssessment    `json:"riskAssessment"`   // Risk analysis for optimizations
}

// QuickWin represents a low-effort, high-impact optimization.
type QuickWin struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	EstimatedHours  int      `json:"estimatedHours"`
	ExpectedImpact  float64  `json:"expectedImpact"`
	CodeChanges     []string `json:"codeChanges"`
	TestingNotes    string   `json:"testingNotes"`
}

// MajorChange represents a high-effort optimization requiring significant changes.
type MajorChange struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	EstimatedDays   int      `json:"estimatedDays"`
	ExpectedImpact  float64  `json:"expectedImpact"`
	Architecture    string   `json:"architecture"`     // Architectural changes required
	RiskLevel       string   `json:"riskLevel"`        // low, medium, high
	Dependencies    []string `json:"dependencies"`     // Other changes this depends on
}

// Timeline provides a suggested implementation timeline.
type Timeline struct {
	Phase1Duration  string   `json:"phase1Duration"`   // Quick wins
	Phase2Duration  string   `json:"phase2Duration"`   // Major changes
	Phase3Duration  string   `json:"phase3Duration"`   // Validation and tuning
	TotalDuration   string   `json:"totalDuration"`
	Milestones      []string `json:"milestones"`
}

// RiskAssessment evaluates the risks associated with optimization implementations.
type RiskAssessment struct {
	OverallRisk     string       `json:"overallRisk"`     // low, medium, high
	TechnicalRisks  []TechRisk   `json:"technicalRisks"`
	BusinessRisks   []BizRisk    `json:"businessRisks"`
	MitigationPlan  []string     `json:"mitigationPlan"`
}

// TechRisk represents a technical risk in optimization implementation.
type TechRisk struct {
	Description string `json:"description"`
	Probability string `json:"probability"` // low, medium, high
	Impact      string `json:"impact"`      // low, medium, high
	Mitigation  string `json:"mitigation"`
}

// BizRisk represents a business risk in optimization implementation.
type BizRisk struct {
	Description string `json:"description"`
	Probability string `json:"probability"` // low, medium, high
	Impact      string `json:"impact"`      // low, medium, high
	Mitigation  string `json:"mitigation"`
}

// PerformanceOptimizer applies identified optimizations to improve pipeline performance.
type PerformanceOptimizer struct {
	config      *OptimizerConfig
	analyzer    *PerformanceAnalyzer
	
	// Optimization state
	appliedOptimizations []string
	optimizationHistory  []OptimizationEvent
}

// OptimizerConfig contains configuration for the performance optimizer.
type OptimizerConfig struct {
	EnableAutoOptimization  bool     `json:"enableAutoOptimization"`  // Automatically apply safe optimizations
	MaxConcurrentWorkers    int      `json:"maxConcurrentWorkers"`    // Maximum worker pool size
	MemoryLimitMB           int64    `json:"memoryLimitMB"`           // Memory usage limit
	OptimizationLevel       string   `json:"optimizationLevel"`       // conservative, balanced, aggressive
	SafetyChecks            bool     `json:"safetyChecks"`            // Enable safety validation
}

// OptimizationEvent tracks the application of a specific optimization.
type OptimizationEvent struct {
	ID              string                   `json:"id"`
	Timestamp       time.Time                `json:"timestamp"`
	Type            RecommendationType       `json:"type"`
	Component       string                   `json:"component"`
	Description     string                   `json:"description"`
	Success         bool                     `json:"success"`
	Error           string                   `json:"error,omitempty"`
	BeforeMetrics   *bench.PerformanceMetrics `json:"beforeMetrics,omitempty"`
	AfterMetrics    *bench.PerformanceMetrics `json:"afterMetrics,omitempty"`
	ImpactMeasured  float64                  `json:"impactMeasured"`   // Actual improvement percentage
}
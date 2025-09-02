// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// This file handles CPU and memory profiling integration with go tool pprof.
package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

// ProfileType represents different types of profiling available.
type ProfileType string

const (
	ProfileCPU       ProfileType = "cpu"
	ProfileMemory    ProfileType = "memory"
	ProfileGoroutine ProfileType = "goroutine"
	ProfileBlock     ProfileType = "block"
	ProfileMutex     ProfileType = "mutex"
	ProfileTrace     ProfileType = "trace"
)

// ProfileConfig contains configuration for profiling sessions.
type ProfileConfig struct {
	Types            []ProfileType `json:"types"`
	OutputDir        string        `json:"outputDir"`
	SampleRate       int           `json:"sampleRate"`       // CPU sampling rate (Hz)
	MemProfileRate   int           `json:"memProfileRate"`   // Memory allocation sampling rate
	BlockProfileRate int           `json:"blockProfileRate"` // Block contention sampling rate
	MutexProfileRate int           `json:"mutexProfileRate"` // Mutex contention sampling rate
	MaxProfileSize   int64         `json:"maxProfileSize"`   // Maximum profile file size in bytes
}

// ProfileSession manages an active profiling session.
type ProfileSession struct {
	config      *ProfileConfig
	sessionID   string
	startTime   time.Time
	active      map[ProfileType]interface{} // Stores active profile files/handles
	mu          sync.RWMutex
	outputPaths map[ProfileType]string
}

// ProfileAnalysis contains analysis results from profiling data.
type ProfileAnalysis struct {
	ProfileType ProfileType   `json:"profileType"`
	FilePath    string        `json:"filePath"`
	FileSize    int64         `json:"fileSize"`
	SampleCount int64         `json:"sampleCount"`
	Duration    time.Duration `json:"durationMs"`

	// Hotspots (top functions by resource usage)
	TopFunctions []FunctionProfile `json:"topFunctions,omitempty"`

	// Memory-specific analysis
	MemoryBreakdown *MemoryBreakdown `json:"memoryBreakdown,omitempty"`

	// CPU-specific analysis
	CPUBreakdown *CPUBreakdown `json:"cpuBreakdown,omitempty"`

	// Goroutine analysis
	GoroutineInfo *GoroutineInfo `json:"goroutineInfo,omitempty"`

	// Performance insights
	Recommendations []string `json:"recommendations,omitempty"`
}

// FunctionProfile represents performance data for a specific function.
type FunctionProfile struct {
	Name           string        `json:"name"`
	Package        string        `json:"package"`
	File           string        `json:"file"`
	Line           int           `json:"line"`
	SelfTime       time.Duration `json:"selfTimeMs"`
	CumulativeTime time.Duration `json:"cumulativeTimeMs"`
	SelfPercent    float64       `json:"selfPercent"`
	CumPercent     float64       `json:"cumPercent"`
	Calls          int64         `json:"calls,omitempty"`
	AllocBytes     int64         `json:"allocBytes,omitempty"`
	AllocObjects   int64         `json:"allocObjects,omitempty"`
}

// MemoryBreakdown provides detailed memory usage analysis.
type MemoryBreakdown struct {
	TotalAllocMB   int64 `json:"totalAllocMB"`
	HeapAllocMB    int64 `json:"heapAllocMB"`
	HeapIdleMB     int64 `json:"heapIdleMB"`
	HeapReleasedMB int64 `json:"heapReleasedMB"`
	StackInuseMB   int64 `json:"stackInuseMB"`

	// Allocation patterns
	LargestAllocs  []AllocationSite `json:"largestAllocs,omitempty"`
	FrequentAllocs []AllocationSite `json:"frequentAllocs,omitempty"`

	// GC analysis
	GCStats GCAnalysis `json:"gcStats"`

	// Memory efficiency metrics
	AllocationRate float64 `json:"allocRateMBPerSec"`
	GCOverhead     float64 `json:"gcOverheadPercent"`
}

// CPUBreakdown provides detailed CPU usage analysis.
type CPUBreakdown struct {
	TotalSamples    int64         `json:"totalSamples"`
	SampleRate      int           `json:"sampleRateHz"`
	ProfileDuration time.Duration `json:"profileDurationMs"`

	// CPU hotspots
	HottestPackages []PackageProfile `json:"hottestPackages,omitempty"`
	SystemTime      float64          `json:"systemTimePercent"`
	UserTime        float64          `json:"userTimePercent"`
	IdleTime        float64          `json:"idleTimePercent"`

	// Performance characteristics
	CPUBoundOps []FunctionProfile `json:"cpuBoundOps,omitempty"`
	IOBoundOps  []FunctionProfile `json:"ioBoundOps,omitempty"`
}

// GoroutineInfo provides goroutine usage analysis.
type GoroutineInfo struct {
	TotalGoroutines   int64 `json:"totalGoroutines"`
	ActiveGoroutines  int64 `json:"activeGoroutines"`
	BlockedGoroutines int64 `json:"blockedGoroutines"`

	// Goroutine states breakdown
	StateBreakdown map[string]int64 `json:"stateBreakdown"`

	// Potential issues
	LeakedGoroutines []GoroutineStack `json:"leakedGoroutines,omitempty"`
	LongRunning      []GoroutineStack `json:"longRunning,omitempty"`
}

// Supporting types
type AllocationSite struct {
	Function string  `json:"function"`
	File     string  `json:"file"`
	Line     int     `json:"line"`
	Bytes    int64   `json:"bytes"`
	Objects  int64   `json:"objects"`
	Percent  float64 `json:"percent"`
}

type GCAnalysis struct {
	NumGC        int64   `json:"numGC"`
	TotalPauseMs int64   `json:"totalPauseMs"`
	MaxPauseMs   int64   `json:"maxPauseMs"`
	AvgPauseMs   float64 `json:"avgPauseMs"`
	GCCPUPercent float64 `json:"gcCpuPercent"`
	NextGCTarget int64   `json:"nextGcTargetMB"`
}

type PackageProfile struct {
	Name        string        `json:"name"`
	SelfTime    time.Duration `json:"selfTimeMs"`
	CumTime     time.Duration `json:"cumTimeMs"`
	SelfPercent float64       `json:"selfPercent"`
	CumPercent  float64       `json:"cumPercent"`
}

type GoroutineStack struct {
	ID         int64         `json:"id"`
	State      string        `json:"state"`
	WaitReason string        `json:"waitReason,omitempty"`
	StackTrace []string      `json:"stackTrace"`
	Duration   time.Duration `json:"durationMs,omitempty"`
}

// ProfileManager handles profiling lifecycle and analysis.
type ProfileManager struct {
	config   *ProfileConfig
	sessions map[string]*ProfileSession
	mu       sync.RWMutex
	baseDir  string
}

// NewProfileManager creates a new profile manager with the specified configuration.
func NewProfileManager(config *ProfileConfig) (*ProfileManager, error) {
	if config == nil {
		return nil, fmt.Errorf("profile config cannot be nil")
	}

	if err := validateProfileConfig(config); err != nil {
		return nil, fmt.Errorf("invalid profile configuration: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create profile output directory: %w", err)
	}

	return &ProfileManager{
		config:   config,
		sessions: make(map[string]*ProfileSession),
		baseDir:  config.OutputDir,
	}, nil
}

// StartProfilingSession begins a new profiling session.
func (pm *ProfileManager) StartProfilingSession(ctx context.Context, sessionID string) (*ProfileSession, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("profiling session '%s' already exists", sessionID)
	}

	session := &ProfileSession{
		config:      pm.config,
		sessionID:   sessionID,
		startTime:   time.Now(),
		active:      make(map[ProfileType]interface{}),
		outputPaths: make(map[ProfileType]string),
	}

	// Configure runtime profiling settings
	if err := pm.configureRuntimeProfiler(); err != nil {
		return nil, fmt.Errorf("failed to configure runtime profiler: %w", err)
	}

	// Start profiling for each requested type
	for _, profileType := range pm.config.Types {
		if err := pm.startProfileType(session, profileType); err != nil {
			// Clean up any started profiles
			pm.stopProfilingSession(session)
			return nil, fmt.Errorf("failed to start %s profiling: %w", profileType, err)
		}
	}

	pm.sessions[sessionID] = session
	return session, nil
}

// StopProfilingSession ends a profiling session and returns analysis.
func (pm *ProfileManager) StopProfilingSession(sessionID string) ([]*ProfileAnalysis, error) {
	pm.mu.Lock()
	session, exists := pm.sessions[sessionID]
	if !exists {
		pm.mu.Unlock()
		return nil, fmt.Errorf("profiling session '%s' not found", sessionID)
	}
	delete(pm.sessions, sessionID)
	pm.mu.Unlock()

	return pm.stopProfilingSession(session)
}

// GetActiveSessionIDs returns IDs of all active profiling sessions.
func (pm *ProfileManager) GetActiveSessionIDs() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	ids := make([]string, 0, len(pm.sessions))
	for id := range pm.sessions {
		ids = append(ids, id)
	}
	return ids
}

// AnalyzeProfile analyzes an existing profile file and returns insights.
func (pm *ProfileManager) AnalyzeProfile(profilePath string, profileType ProfileType) (*ProfileAnalysis, error) {
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("profile file does not exist: %s", profilePath)
	}

	fileInfo, err := os.Stat(profilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile file info: %w", err)
	}

	analysis := &ProfileAnalysis{
		ProfileType: profileType,
		FilePath:    profilePath,
		FileSize:    fileInfo.Size(),
	}

	// Perform type-specific analysis
	switch profileType {
	case ProfileCPU:
		if err := pm.analyzeCPUProfile(analysis); err != nil {
			return nil, fmt.Errorf("CPU profile analysis failed: %w", err)
		}
	case ProfileMemory:
		if err := pm.analyzeMemoryProfile(analysis); err != nil {
			return nil, fmt.Errorf("memory profile analysis failed: %w", err)
		}
	case ProfileGoroutine:
		if err := pm.analyzeGoroutineProfile(analysis); err != nil {
			return nil, fmt.Errorf("goroutine profile analysis failed: %w", err)
		}
	default:
		// Basic analysis for other profile types
		analysis.Recommendations = []string{
			fmt.Sprintf("Profile file generated successfully for %s profiling", profileType),
			"Use 'go tool pprof' for detailed analysis",
		}
	}

	return analysis, nil
}

// Helper methods

// configureRuntimeProfiler sets up runtime profiling parameters.
func (pm *ProfileManager) configureRuntimeProfiler() error {
	// Configure memory profiling rate
	if pm.config.MemProfileRate > 0 {
		runtime.MemProfileRate = pm.config.MemProfileRate
	}

	// Configure block profiling rate
	if pm.config.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(pm.config.BlockProfileRate)
	}

	// Configure mutex profiling rate
	if pm.config.MutexProfileRate > 0 {
		runtime.SetMutexProfileFraction(pm.config.MutexProfileRate)
	}

	return nil
}

// startProfileType starts profiling for a specific type.
func (pm *ProfileManager) startProfileType(session *ProfileSession, profileType ProfileType) error {
	timestamp := session.startTime.Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s_%s.prof", session.sessionID, profileType, timestamp)
	outputPath := filepath.Join(pm.baseDir, filename)

	session.outputPaths[profileType] = outputPath

	switch profileType {
	case ProfileCPU:
		return pm.startCPUProfiling(session, outputPath)
	case ProfileTrace:
		return pm.startTracing(session, outputPath)
	case ProfileMemory, ProfileGoroutine, ProfileBlock, ProfileMutex:
		// These are captured at the end of the session
		return nil
	default:
		return fmt.Errorf("unsupported profile type: %s", profileType)
	}
}

// startCPUProfiling begins CPU profiling.
func (pm *ProfileManager) startCPUProfiling(session *ProfileSession, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		return fmt.Errorf("failed to start CPU profiling: %w", err)
	}

	session.active[ProfileCPU] = file
	return nil
}

// startTracing begins execution tracing.
func (pm *ProfileManager) startTracing(session *ProfileSession, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create trace file: %w", err)
	}

	if err := trace.Start(file); err != nil {
		file.Close()
		return fmt.Errorf("failed to start tracing: %w", err)
	}

	session.active[ProfileTrace] = file
	return nil
}

// stopProfilingSession stops all profiling for a session.
func (pm *ProfileManager) stopProfilingSession(session *ProfileSession) ([]*ProfileAnalysis, error) {
	var analyses []*ProfileAnalysis
	var lastErr error

	session.mu.Lock()
	defer session.mu.Unlock()

	duration := time.Since(session.startTime)

	for profileType := range session.active {
		if err := pm.stopProfileType(session, profileType); err != nil {
			lastErr = fmt.Errorf("failed to stop %s profiling: %w", profileType, err)
			continue
		}
	}

	// Capture heap profiles and other runtime profiles
	for profileType, outputPath := range session.outputPaths {
		if profileType == ProfileCPU || profileType == ProfileTrace {
			continue // Already handled above
		}

		if err := pm.captureRuntimeProfile(profileType, outputPath); err != nil {
			lastErr = fmt.Errorf("failed to capture %s profile: %w", profileType, err)
			continue
		}
	}

	// Analyze all generated profiles
	for profileType, outputPath := range session.outputPaths {
		analysis, err := pm.AnalyzeProfile(outputPath, profileType)
		if err != nil {
			lastErr = fmt.Errorf("failed to analyze %s profile: %w", profileType, err)
			continue
		}

		analysis.Duration = duration
		analyses = append(analyses, analysis)
	}

	return analyses, lastErr
}

// stopProfileType stops profiling for a specific type.
func (pm *ProfileManager) stopProfileType(session *ProfileSession, profileType ProfileType) error {
	switch profileType {
	case ProfileCPU:
		pprof.StopCPUProfile()
		if file, ok := session.active[ProfileCPU].(*os.File); ok {
			return file.Close()
		}
	case ProfileTrace:
		trace.Stop()
		if file, ok := session.active[ProfileTrace].(*os.File); ok {
			return file.Close()
		}
	}
	return nil
}

// captureRuntimeProfile captures runtime profiles (heap, goroutine, etc.).
func (pm *ProfileManager) captureRuntimeProfile(profileType ProfileType, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create profile file: %w", err)
	}
	defer file.Close()

	var profile *pprof.Profile

	switch profileType {
	case ProfileMemory:
		profile = pprof.Lookup("heap")
	case ProfileGoroutine:
		profile = pprof.Lookup("goroutine")
	case ProfileBlock:
		profile = pprof.Lookup("block")
	case ProfileMutex:
		profile = pprof.Lookup("mutex")
	default:
		return fmt.Errorf("unsupported runtime profile type: %s", profileType)
	}

	if profile == nil {
		return fmt.Errorf("profile type %s not available", profileType)
	}

	return profile.WriteTo(file, 0)
}

// Analysis methods (placeholder implementations)

func (pm *ProfileManager) analyzeCPUProfile(analysis *ProfileAnalysis) error {
	// TODO: Implement CPU profile analysis using pprof
	analysis.CPUBreakdown = &CPUBreakdown{
		SampleRate: pm.config.SampleRate,
	}
	analysis.Recommendations = append(analysis.Recommendations,
		"Use 'go tool pprof -top' to see CPU hotspots",
		"Use 'go tool pprof -web' for interactive analysis",
	)
	return nil
}

func (pm *ProfileManager) analyzeMemoryProfile(analysis *ProfileAnalysis) error {
	// TODO: Implement memory profile analysis
	analysis.MemoryBreakdown = &MemoryBreakdown{}
	analysis.Recommendations = append(analysis.Recommendations,
		"Use 'go tool pprof -alloc_space' to see allocation patterns",
		"Check for memory leaks with 'go tool pprof -inuse_space'",
	)
	return nil
}

func (pm *ProfileManager) analyzeGoroutineProfile(analysis *ProfileAnalysis) error {
	// TODO: Implement goroutine profile analysis
	analysis.GoroutineInfo = &GoroutineInfo{}
	analysis.Recommendations = append(analysis.Recommendations,
		"Check for goroutine leaks",
		"Monitor goroutine growth patterns",
	)
	return nil
}

// validateProfileConfig validates profile configuration.
func validateProfileConfig(config *ProfileConfig) error {
	if len(config.Types) == 0 {
		return fmt.Errorf("at least one profile type must be specified")
	}

	if config.OutputDir == "" {
		return fmt.Errorf("output directory cannot be empty")
	}

	if config.SampleRate < 0 {
		return fmt.Errorf("sample rate cannot be negative")
	}

	return nil
}

// DefaultProfileConfig returns a sensible default configuration for profiling.
func DefaultProfileConfig() *ProfileConfig {
	return &ProfileConfig{
		Types:            []ProfileType{ProfileCPU, ProfileMemory},
		OutputDir:        filepath.Join(".", "profiles"),
		SampleRate:       100,  // Hz
		MemProfileRate:   4096, // Sample every 4KB
		BlockProfileRate: 1,
		MutexProfileRate: 1,
		MaxProfileSize:   100 * 1024 * 1024, // 100MB
	}
}

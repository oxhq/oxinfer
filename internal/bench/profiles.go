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
	"strings"
	"sync"
	"time"
	
	"github.com/google/pprof/profile"
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
	// Find the CPU profile file
	cpuProfilePath := filepath.Join(pm.config.OutputDir, "cpu.prof")
	if _, err := os.Stat(cpuProfilePath); os.IsNotExist(err) {
		return fmt.Errorf("CPU profile file not found: %s", cpuProfilePath)
	}
	
	// Open and read the CPU profile
	file, err := os.Open(cpuProfilePath)
	if err != nil {
		return fmt.Errorf("failed to open CPU profile: %w", err)
	}
	defer file.Close()
	
	// Parse the profile using pprof
	prof, err := profile.Parse(file)
	if err != nil {
		return fmt.Errorf("failed to parse CPU profile: %w", err)
	}
	
	// Analyze the profile data
	analysis.CPUBreakdown = &CPUBreakdown{
		SampleRate:      pm.config.SampleRate,
		TotalSamples:    int64(len(prof.Sample)),
		ProfileDuration: time.Duration(prof.TimeNanos),
		HottestPackages: extractHottestPackages(prof, 10),
		CPUBoundOps:     extractTopFunctions(prof, 10),
	}
	
	// Generate recommendations based on analysis
	recommendations := []string{
		"Use 'go tool pprof -top' to see CPU hotspots",
		"Use 'go tool pprof -web' for interactive analysis",
	}
	
	// Add specific recommendations based on profile data
	if analysis.CPUBreakdown.TotalSamples > 1000 {
		recommendations = append(recommendations, "High sample count suggests intensive CPU usage")
	}
	
	if len(analysis.CPUBreakdown.CPUBoundOps) > 0 {
		recommendations = append(recommendations, "Focus optimization on CPU-intensive operations")
	}
	
	analysis.Recommendations = append(analysis.Recommendations, recommendations...)
	return nil
}

func (pm *ProfileManager) analyzeMemoryProfile(analysis *ProfileAnalysis) error {
	// Find the memory profile file
	memProfilePath := filepath.Join(pm.config.OutputDir, "mem.prof")
	if _, err := os.Stat(memProfilePath); os.IsNotExist(err) {
		return fmt.Errorf("memory profile file not found: %s", memProfilePath)
	}
	
	// Open and read the memory profile
	file, err := os.Open(memProfilePath)
	if err != nil {
		return fmt.Errorf("failed to open memory profile: %w", err)
	}
	defer file.Close()
	
	// Parse the profile using pprof
	prof, err := profile.Parse(file)
	if err != nil {
		return fmt.Errorf("failed to parse memory profile: %w", err)
	}
	
	// Extract memory statistics
	var totalAllocated, totalInUse int64
	
	if len(prof.Sample) > 0 {
		// Calculate total allocations and in-use memory
		for _, sample := range prof.Sample {
			if len(sample.Value) >= 2 {
				totalAllocated += sample.Value[0] // alloc_space
				totalInUse += sample.Value[1]     // inuse_space
			}
		}
	}
	
	analysis.MemoryBreakdown = &MemoryBreakdown{
		TotalAllocMB:   totalAllocated / (1024 * 1024),
		HeapAllocMB:    totalInUse / (1024 * 1024),
		LargestAllocs:  extractLargestAllocs(prof, 10),
		AllocationRate: calculateAllocationRate(prof),
	}
	
	// Generate recommendations based on analysis
	recommendations := []string{
		"Use 'go tool pprof -alloc_space' to see allocation patterns",
		"Check for memory leaks with 'go tool pprof -inuse_space'",
	}
	
	// Add specific recommendations based on profile data
	if totalAllocated > 100*1024*1024 { // > 100MB
		recommendations = append(recommendations, "High memory allocation detected - consider optimizing allocations")
	}
	
	if totalInUse > 50*1024*1024 { // > 50MB
		recommendations = append(recommendations, "High in-use memory - check for potential memory leaks")
	}
	
	if len(analysis.MemoryBreakdown.LargestAllocs) > 0 {
		recommendations = append(recommendations, "Focus optimization on top memory allocating functions")
	}
	
	analysis.Recommendations = append(analysis.Recommendations, recommendations...)
	return nil
}

func (pm *ProfileManager) analyzeGoroutineProfile(analysis *ProfileAnalysis) error {
	// Find the goroutine profile file
	goroutineProfilePath := filepath.Join(pm.config.OutputDir, "goroutine.prof")
	if _, err := os.Stat(goroutineProfilePath); os.IsNotExist(err) {
		return fmt.Errorf("goroutine profile file not found: %s", goroutineProfilePath)
	}
	
	// Open and read the goroutine profile
	file, err := os.Open(goroutineProfilePath)
	if err != nil {
		return fmt.Errorf("failed to open goroutine profile: %w", err)
	}
	defer file.Close()
	
	// Parse the profile using pprof
	prof, err := profile.Parse(file)
	if err != nil {
		return fmt.Errorf("failed to parse goroutine profile: %w", err)
	}
	
	// Analyze goroutine data
	var totalGoroutines int64
	var blockedGoroutines int64
	var goroutineStates []string
	
	if len(prof.Sample) > 0 {
		// Count total goroutines and analyze their states
		for _, sample := range prof.Sample {
			if len(sample.Value) > 0 {
				totalGoroutines += sample.Value[0]
			}
			
			// Extract goroutine state information from stack traces
			if len(sample.Location) > 0 && len(sample.Location[0].Line) > 0 {
				line := sample.Location[0].Line[0]
				if line.Function != nil {
					funcName := line.Function.Name
					// Common blocking function patterns
					if isBlockingFunction(funcName) {
						blockedGoroutines += sample.Value[0]
					}
					goroutineStates = append(goroutineStates, funcName)
				}
			}
		}
	}
	
	// Build state breakdown
	stateBreakdown := make(map[string]int64)
	for _, state := range goroutineStates {
		stateBreakdown[state]++
	}
	
	analysis.GoroutineInfo = &GoroutineInfo{
		TotalGoroutines:   totalGoroutines,
		ActiveGoroutines:  totalGoroutines - blockedGoroutines,
		BlockedGoroutines: blockedGoroutines,
		StateBreakdown:    stateBreakdown,
	}
	
	// Generate recommendations based on analysis
	recommendations := []string{
		"Check for goroutine leaks",
		"Monitor goroutine growth patterns",
	}
	
	// Add specific recommendations based on profile data
	if totalGoroutines > 1000 {
		recommendations = append(recommendations, "High goroutine count detected - investigate potential leaks")
	}
	
	if blockedGoroutines > totalGoroutines/2 {
		recommendations = append(recommendations, "Many goroutines are blocked - check for deadlocks or resource contention")
	}
	
	analysis.Recommendations = append(analysis.Recommendations, recommendations...)
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

// extractTopFunctions extracts the top CPU-consuming functions from a profile
func extractTopFunctions(prof *profile.Profile, limit int) []FunctionProfile {
	if prof == nil || len(prof.Sample) == 0 {
		return []FunctionProfile{}
	}
	
	// Aggregate samples by function
	functionData := make(map[string]*FunctionProfile)
	totalSamples := int64(0)
	
	for _, sample := range prof.Sample {
		if len(sample.Location) > 0 && len(sample.Location[0].Line) > 0 {
			loc := sample.Location[0]
			line := loc.Line[0]
			if line.Function != nil {
				funcName := line.Function.Name
				
				if functionData[funcName] == nil {
					functionData[funcName] = &FunctionProfile{
						Name:    funcName,
						Package: extractPackageName(funcName),
						File:    line.Function.Filename,
						Line:    int(line.Line),
					}
				}
				
				for _, value := range sample.Value {
					functionData[funcName].SelfTime += time.Duration(value)
					functionData[funcName].CumulativeTime += time.Duration(value)
					totalSamples += value
				}
			}
		}
	}
	
	// Convert to slice and calculate percentages
	var functions []FunctionProfile
	for _, funcProfile := range functionData {
		if totalSamples > 0 {
			funcProfile.SelfPercent = float64(funcProfile.SelfTime.Nanoseconds()) / float64(totalSamples) * 100
			funcProfile.CumPercent = funcProfile.SelfPercent // Simplified
		}
		functions = append(functions, *funcProfile)
	}
	
	// Simple sort by SelfTime (descending)
	for i := 0; i < len(functions); i++ {
		for j := i + 1; j < len(functions); j++ {
			if functions[j].SelfTime > functions[i].SelfTime {
				functions[i], functions[j] = functions[j], functions[i]
			}
		}
	}
	
	// Return top N functions
	if limit > len(functions) {
		limit = len(functions)
	}
	
	return functions[:limit]
}

// extractHotspots identifies CPU hotspot locations from a profile
func extractHotspots(prof *profile.Profile, limit int) []string {
	if prof == nil || len(prof.Sample) == 0 {
		return []string{}
	}
	
	// Aggregate samples by location
	locationCounts := make(map[string]int64)
	
	for _, sample := range prof.Sample {
		if len(sample.Location) > 0 {
			loc := sample.Location[0]
			if len(loc.Line) > 0 && loc.Line[0].Function != nil {
				line := loc.Line[0]
				location := fmt.Sprintf("%s:%d", line.Function.Name, line.Line)
				for _, value := range sample.Value {
					locationCounts[location] += value
				}
			}
		}
	}
	
	// Sort by count and return top locations
	type locCount struct {
		location string
		count    int64
	}
	
	var locations []locCount
	for location, count := range locationCounts {
		locations = append(locations, locCount{location, count})
	}
	
	// Simple sort by count (descending)
	for i := 0; i < len(locations); i++ {
		for j := i + 1; j < len(locations); j++ {
			if locations[j].count > locations[i].count {
				locations[i], locations[j] = locations[j], locations[i]
			}
		}
	}
	
	// Return top N locations
	result := make([]string, 0, limit)
	for i := 0; i < limit && i < len(locations); i++ {
		result = append(result, locations[i].location)
	}
	
	return result
}

// extractTopMemoryAllocators extracts the top memory allocating functions from a profile
func extractTopMemoryAllocators(prof *profile.Profile, limit int) []string {
	if prof == nil || len(prof.Sample) == 0 {
		return []string{}
	}
	
	// Aggregate allocations by function
	functionAllocations := make(map[string]int64)
	
	for _, sample := range prof.Sample {
		if len(sample.Location) > 0 && len(sample.Location[0].Line) > 0 && len(sample.Value) > 0 {
			line := sample.Location[0].Line[0]
			if line.Function != nil {
				funcName := line.Function.Name
				functionAllocations[funcName] += sample.Value[0] // alloc_space
			}
		}
	}
	
	// Sort by allocation amount and return top functions
	type funcAlloc struct {
		name  string
		alloc int64
	}
	
	var functions []funcAlloc
	for name, alloc := range functionAllocations {
		functions = append(functions, funcAlloc{name, alloc})
	}
	
	// Simple sort by allocation (descending)
	for i := 0; i < len(functions); i++ {
		for j := i + 1; j < len(functions); j++ {
			if functions[j].alloc > functions[i].alloc {
				functions[i], functions[j] = functions[j], functions[i]
			}
		}
	}
	
	// Return top N function names
	result := make([]string, 0, limit)
	for i := 0; i < limit && i < len(functions); i++ {
		result = append(result, functions[i].name)
	}
	
	return result
}

// calculateAllocationRate estimates allocation rate from memory profile
func calculateAllocationRate(prof *profile.Profile) float64 {
	if prof == nil || len(prof.Sample) == 0 || prof.TimeNanos == 0 {
		return 0
	}
	
	var totalAllocated int64
	for _, sample := range prof.Sample {
		if len(sample.Value) > 0 {
			totalAllocated += sample.Value[0]
		}
	}
	
	// Convert nanoseconds to seconds and calculate rate (bytes/sec)
	seconds := float64(prof.TimeNanos) / 1e9
	if seconds > 0 {
		return float64(totalAllocated) / seconds
	}
	
	return 0
}

// extractHottestPackages extracts the top packages by CPU usage from a profile
func extractHottestPackages(prof *profile.Profile, limit int) []PackageProfile {
	if prof == nil || len(prof.Sample) == 0 {
		return []PackageProfile{}
	}
	
	// Aggregate samples by package
	packageData := make(map[string]*PackageProfile)
	totalSamples := int64(0)
	
	for _, sample := range prof.Sample {
		if len(sample.Location) > 0 && len(sample.Location[0].Line) > 0 {
			line := sample.Location[0].Line[0]
			if line.Function != nil {
				pkgName := extractPackageName(line.Function.Name)
				
				if packageData[pkgName] == nil {
					packageData[pkgName] = &PackageProfile{
						Name: pkgName,
					}
				}
				
				for _, value := range sample.Value {
					packageData[pkgName].SelfTime += time.Duration(value)
					packageData[pkgName].CumTime += time.Duration(value)
					totalSamples += value
				}
			}
		}
	}
	
	// Convert to slice and calculate percentages
	var packages []PackageProfile
	for _, pkgProfile := range packageData {
		if totalSamples > 0 {
			pkgProfile.SelfPercent = float64(pkgProfile.SelfTime.Nanoseconds()) / float64(totalSamples) * 100
			pkgProfile.CumPercent = pkgProfile.SelfPercent // Simplified
		}
		packages = append(packages, *pkgProfile)
	}
	
	// Simple sort by SelfTime (descending)
	for i := 0; i < len(packages); i++ {
		for j := i + 1; j < len(packages); j++ {
			if packages[j].SelfTime > packages[i].SelfTime {
				packages[i], packages[j] = packages[j], packages[i]
			}
		}
	}
	
	// Return top N packages
	if limit > len(packages) {
		limit = len(packages)
	}
	
	return packages[:limit]
}

// extractLargestAllocs extracts the largest memory allocation sites from a profile
func extractLargestAllocs(prof *profile.Profile, limit int) []AllocationSite {
	if prof == nil || len(prof.Sample) == 0 {
		return []AllocationSite{}
	}
	
	// Aggregate allocations by function location
	allocSites := make(map[string]*AllocationSite)
	totalAllocated := int64(0)
	
	for _, sample := range prof.Sample {
		if len(sample.Location) > 0 && len(sample.Location[0].Line) > 0 && len(sample.Value) >= 2 {
			loc := sample.Location[0]
			line := loc.Line[0]
			if line.Function != nil {
				key := fmt.Sprintf("%s:%d", line.Function.Name, line.Line)
				
				if allocSites[key] == nil {
					allocSites[key] = &AllocationSite{
						Function: line.Function.Name,
						File:     line.Function.Filename,
						Line:     int(line.Line),
					}
				}
				
				allocSites[key].Bytes += sample.Value[0]    // alloc_space
				allocSites[key].Objects += sample.Value[1]  // alloc_objects
				totalAllocated += sample.Value[0]
			}
		}
	}
	
	// Convert to slice and calculate percentages
	var allocs []AllocationSite
	for _, allocSite := range allocSites {
		if totalAllocated > 0 {
			allocSite.Percent = float64(allocSite.Bytes) / float64(totalAllocated) * 100
		}
		allocs = append(allocs, *allocSite)
	}
	
	// Simple sort by Bytes (descending)
	for i := 0; i < len(allocs); i++ {
		for j := i + 1; j < len(allocs); j++ {
			if allocs[j].Bytes > allocs[i].Bytes {
				allocs[i], allocs[j] = allocs[j], allocs[i]
			}
		}
	}
	
	// Return top N allocations
	if limit > len(allocs) {
		limit = len(allocs)
	}
	
	return allocs[:limit]
}

// extractPackageName extracts package name from a function name
func extractPackageName(funcName string) string {
	if funcName == "" {
		return "unknown"
	}
	
	// Handle method receivers like (*Type).Method or Type.Method
	if idx := strings.LastIndex(funcName, "."); idx != -1 {
		pkgPart := funcName[:idx]
		
		// Remove receiver type information like (*Type)
		if strings.Contains(pkgPart, ")") {
			if parenIdx := strings.LastIndex(pkgPart, ")"); parenIdx != -1 {
				// Look for the package before the receiver
				beforeReceiver := pkgPart[:strings.LastIndex(pkgPart[:parenIdx], "(")]
				if dotIdx := strings.LastIndex(beforeReceiver, "."); dotIdx != -1 {
					return beforeReceiver[:dotIdx]
				}
			}
		}
		
		// Handle regular package.function format
		if lastSlash := strings.LastIndex(pkgPart, "/"); lastSlash != -1 {
			return pkgPart // Return full import path
		}
		
		return pkgPart
	}
	
	return "main" // Default for functions without package prefix
}

// isBlockingFunction determines if a function is commonly associated with blocking goroutines
func isBlockingFunction(funcName string) bool {
	blockingPatterns := []string{
		"runtime.chanrecv",
		"runtime.chansend",
		"runtime.selectgo",
		"runtime.lock",
		"runtime.notesleep",
		"runtime.gopark",
		"sync.(*Mutex).Lock",
		"sync.(*RWMutex).Lock",
		"sync.(*RWMutex).RLock",
		"sync.(*WaitGroup).Wait",
		"time.Sleep",
		"net.(*conn).Read",
		"net.(*conn).Write",
		"os.(*File).Read",
		"os.(*File).Write",
	}
	
	for _, pattern := range blockingPatterns {
		if funcName == pattern {
			return true
		}
	}
	
	return false
}

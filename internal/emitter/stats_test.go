package emitter

import (
	"testing"
)

// MockProcessingStats implements the interfaces needed by NewMetaStatsFromProcessingStats
type MockProcessingStats struct {
	filesProcessed   int64
	filesSkipped     int64
	processingTime   int64
	partial          bool
	phaseStats       map[string]int64
	matchStats       map[string]int
	errorCount       int64
	totalFiles       int64
	startTime        int64
	endTime          int64
	inferenceOps     int64
	propertiesInferred int64
	cacheHits        int64
	cacheMisses      int64
}

func (m *MockProcessingStats) GetFilesProcessed() int64    { return m.filesProcessed }
func (m *MockProcessingStats) GetFilesSkipped() int64      { return m.filesSkipped }
func (m *MockProcessingStats) GetProcessingTime() int64    { return m.processingTime }
func (m *MockProcessingStats) IsPartial() bool             { return m.partial }
func (m *MockProcessingStats) GetPhaseStats() interface{}  { return m.phaseStats }
func (m *MockProcessingStats) GetMatchStats() interface{}  { return m.matchStats }
func (m *MockProcessingStats) GetErrorCount() int64        { return m.errorCount }
func (m *MockProcessingStats) GetTotalFiles() int64        { return m.totalFiles }
func (m *MockProcessingStats) GetStartTime() int64         { return m.startTime }
func (m *MockProcessingStats) GetEndTime() int64           { return m.endTime }
func (m *MockProcessingStats) GetInferenceOps() int64      { return m.inferenceOps }
func (m *MockProcessingStats) GetPropertiesInferred() int64 { return m.propertiesInferred }
func (m *MockProcessingStats) GetCacheHits() int64         { return m.cacheHits }
func (m *MockProcessingStats) GetCacheMisses() int64       { return m.cacheMisses }

func TestNewMetaStatsFromProcessingStats(t *testing.T) {
	// Create a mock stats object
	mockStats := &MockProcessingStats{
		filesProcessed: 100,
		filesSkipped:   5,
		processingTime: 2000,
		partial:        true,
		phaseStats: map[string]int64{
			"indexing": 500,
			"parsing":  1000,
			"matching": 500,
		},
		matchStats: map[string]int{
			"http_status": 25,
			"resource":    15,
		},
		errorCount:         3,
		totalFiles:         105,
		startTime:          1640995200000, // Jan 1, 2022
		endTime:            1640995202000, // Jan 1, 2022 + 2s
		inferenceOps:       50,
		propertiesInferred: 75,
		cacheHits:          10,
		cacheMisses:        2,
	}
	
	// Convert to MetaStats
	metaStats := NewMetaStatsFromProcessingStats(mockStats)
	
	// Verify basic stats
	if metaStats.FilesParsed != 100 {
		t.Errorf("FilesParsed = %d, want 100", metaStats.FilesParsed)
	}
	
	if metaStats.Skipped != 5 {
		t.Errorf("Skipped = %d, want 5", metaStats.Skipped)
	}
	
	if metaStats.DurationMs != 2000 {
		t.Errorf("DurationMs = %d, want 2000", metaStats.DurationMs)
	}
	
	if metaStats.ErrorCount != 3 {
		t.Errorf("ErrorCount = %d, want 3", metaStats.ErrorCount)
	}
	
	// Verify extended stats
	if metaStats.TotalFiles != 105 {
		t.Errorf("TotalFiles = %d, want 105", metaStats.TotalFiles)
	}
	
	if metaStats.StartTime != 1640995200000 {
		t.Errorf("StartTime = %d, want 1640995200000", metaStats.StartTime)
	}
	
	if metaStats.EndTime != 1640995202000 {
		t.Errorf("EndTime = %d, want 1640995202000", metaStats.EndTime)
	}
	
	if metaStats.InferenceOps != 50 {
		t.Errorf("InferenceOps = %d, want 50", metaStats.InferenceOps)
	}
	
	if metaStats.PropertiesInferred != 75 {
		t.Errorf("PropertiesInferred = %d, want 75", metaStats.PropertiesInferred)
	}
	
	if metaStats.CacheHits != 10 {
		t.Errorf("CacheHits = %d, want 10", metaStats.CacheHits)
	}
	
	if metaStats.CacheMisses != 2 {
		t.Errorf("CacheMisses = %d, want 2", metaStats.CacheMisses)
	}
	
	// Verify phase stats
	if metaStats.PhaseStats == nil {
		t.Fatal("PhaseStats is nil")
	}
	
	if len(metaStats.PhaseStats) != 3 {
		t.Errorf("PhaseStats length = %d, want 3", len(metaStats.PhaseStats))
	}
	
	if metaStats.PhaseStats["indexing"] != 500 {
		t.Errorf("PhaseStats[indexing] = %d, want 500", metaStats.PhaseStats["indexing"])
	}
	
	if metaStats.PhaseStats["parsing"] != 1000 {
		t.Errorf("PhaseStats[parsing] = %d, want 1000", metaStats.PhaseStats["parsing"])
	}
	
	if metaStats.PhaseStats["matching"] != 500 {
		t.Errorf("PhaseStats[matching] = %d, want 500", metaStats.PhaseStats["matching"])
	}
	
	// Verify match stats
	if metaStats.MatchStats == nil {
		t.Fatal("MatchStats is nil")
	}
	
	if len(metaStats.MatchStats) != 2 {
		t.Errorf("MatchStats length = %d, want 2", len(metaStats.MatchStats))
	}
	
	if metaStats.MatchStats["http_status"] != 25 {
		t.Errorf("MatchStats[http_status] = %d, want 25", metaStats.MatchStats["http_status"])
	}
	
	if metaStats.MatchStats["resource"] != 15 {
		t.Errorf("MatchStats[resource] = %d, want 15", metaStats.MatchStats["resource"])
	}
}

func TestNewMetaStatsFromProcessingStats_EmptyMaps(t *testing.T) {
	// Test with empty phase and match stats
	mockStats := &MockProcessingStats{
		filesProcessed: 50,
		filesSkipped:   0,
		processingTime: 1000,
		phaseStats:     map[string]int64{}, // empty
		matchStats:     map[string]int{},   // empty
	}
	
	metaStats := NewMetaStatsFromProcessingStats(mockStats)
	
	// Empty maps should be nil in the output
	if metaStats.PhaseStats != nil {
		t.Errorf("PhaseStats should be nil for empty map, got %v", metaStats.PhaseStats)
	}
	
	if metaStats.MatchStats != nil {
		t.Errorf("MatchStats should be nil for empty map, got %v", metaStats.MatchStats)
	}
}

func TestNewMetaStatsFromProcessingStats_ZeroValues(t *testing.T) {
	// Test with maps that have zero values
	mockStats := &MockProcessingStats{
		filesProcessed: 50,
		processingTime: 1000,
		phaseStats: map[string]int64{
			"phase1": 100,
			"phase2": 0, // zero value should be excluded
		},
		matchStats: map[string]int{
			"match1": 5,
			"match2": 0, // zero value should be excluded
		},
	}
	
	metaStats := NewMetaStatsFromProcessingStats(mockStats)
	
	// Only non-zero values should be included
	if len(metaStats.PhaseStats) != 1 {
		t.Errorf("PhaseStats should have 1 entry, got %d", len(metaStats.PhaseStats))
	}
	
	if metaStats.PhaseStats["phase1"] != 100 {
		t.Errorf("PhaseStats[phase1] = %d, want 100", metaStats.PhaseStats["phase1"])
	}
	
	if _, exists := metaStats.PhaseStats["phase2"]; exists {
		t.Error("PhaseStats[phase2] should not exist (zero value)")
	}
	
	if len(metaStats.MatchStats) != 1 {
		t.Errorf("MatchStats should have 1 entry, got %d", len(metaStats.MatchStats))
	}
	
	if metaStats.MatchStats["match1"] != 5 {
		t.Errorf("MatchStats[match1] = %d, want 5", metaStats.MatchStats["match1"])
	}
	
	if _, exists := metaStats.MatchStats["match2"]; exists {
		t.Error("MatchStats[match2] should not exist (zero value)")
	}
}
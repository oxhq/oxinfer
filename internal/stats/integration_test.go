package stats

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
)

// TestStatsToEmitterIntegration verifies that ProcessingStats integrates properly with emitter.MetaStats
func TestStatsToEmitterIntegration(t *testing.T) {
	// Create a stats collector and populate it with data
	collector := NewStatsCollector()
	
	// Simulate some processing activity
	collector.RecordFilesProcessed(150)
	collector.RecordFilesSkipped(5)
	collector.RecordTotalFiles(155)
	collector.RecordProcessingTime(string(PhaseIndexing), 500*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseParsing), 1200*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseMatching), 800*time.Millisecond)
	collector.RecordMatch(string(MatchTypeHTTPStatus), 42)
	collector.RecordMatch(string(MatchTypeResource), 18)
	collector.RecordMatch(string(MatchTypePolymorphic), 7)
	collector.RecordInferenceOperation(string(InferenceOpConsolidation), 25)
	collector.RecordPropertiesInferred(89)
	collector.RecordCacheHit()
	collector.RecordCacheHit()
	collector.RecordCacheMiss()
	collector.SetPartialFlag(true)
	collector.MarkProcessingStart()
	time.Sleep(1 * time.Millisecond) // Small delay to ensure different timestamps
	collector.MarkProcessingEnd()
	
	// Get processing stats
	stats := collector.GetStats()
	
	// Convert to emitter MetaStats format
	metaStats := emitter.NewMetaStatsFromProcessingStats(stats)
	
	// Verify interface conversion is working correctly
	phaseStats := stats.GetPhaseStatsTyped()
	matchStats := stats.GetMatchStatsTyped()
	
	// Ensure we have the expected data
	if len(phaseStats) == 0 {
		t.Fatal("Expected phase stats but got empty map")
	}
	
	if len(matchStats) == 0 {
		t.Fatal("Expected match stats but got empty map")
	}
	
	// Verify the conversion worked correctly
	if metaStats.FilesParsed != 150 {
		t.Errorf("FilesParsed = %d, want 150", metaStats.FilesParsed)
	}
	
	if metaStats.Skipped != 5 {
		t.Errorf("Skipped = %d, want 5", metaStats.Skipped)
	}
	
	if metaStats.TotalFiles != 155 {
		t.Errorf("TotalFiles = %d, want 155", metaStats.TotalFiles)
	}
	
	// Check that total duration is the sum of all phase durations (500+1200+800 = 2500ms)
	if metaStats.DurationMs != 2500 {
		t.Errorf("DurationMs = %d, want 2500", metaStats.DurationMs)
	}
	
	// Check phase stats
	if metaStats.PhaseStats == nil {
		t.Fatal("PhaseStats is nil")
	}
	
	if metaStats.PhaseStats[string(PhaseIndexing)] != 500 {
		t.Errorf("PhaseStats[indexing] = %d, want 500", metaStats.PhaseStats[string(PhaseIndexing)])
	}
	
	if metaStats.PhaseStats[string(PhaseParsing)] != 1200 {
		t.Errorf("PhaseStats[parsing] = %d, want 1200", metaStats.PhaseStats[string(PhaseParsing)])
	}
	
	if metaStats.PhaseStats[string(PhaseMatching)] != 800 {
		t.Errorf("PhaseStats[matching] = %d, want 800", metaStats.PhaseStats[string(PhaseMatching)])
	}
	
	// Check match stats
	if metaStats.MatchStats == nil {
		t.Fatal("MatchStats is nil")
	}
	
	if metaStats.MatchStats[string(MatchTypeHTTPStatus)] != 42 {
		t.Errorf("MatchStats[http_status] = %d, want 42", metaStats.MatchStats[string(MatchTypeHTTPStatus)])
	}
	
	if metaStats.MatchStats[string(MatchTypeResource)] != 18 {
		t.Errorf("MatchStats[resource] = %d, want 18", metaStats.MatchStats[string(MatchTypeResource)])
	}
	
	if metaStats.MatchStats[string(MatchTypePolymorphic)] != 7 {
		t.Errorf("MatchStats[polymorphic] = %d, want 7", metaStats.MatchStats[string(MatchTypePolymorphic)])
	}
	
	// Check inference stats
	if metaStats.InferenceOps != 25 {
		t.Errorf("InferenceOps = %d, want 25", metaStats.InferenceOps)
	}
	
	if metaStats.PropertiesInferred != 89 {
		t.Errorf("PropertiesInferred = %d, want 89", metaStats.PropertiesInferred)
	}
	
	// Check cache stats
	if metaStats.CacheHits != 2 {
		t.Errorf("CacheHits = %d, want 2", metaStats.CacheHits)
	}
	
	if metaStats.CacheMisses != 1 {
		t.Errorf("CacheMisses = %d, want 1", metaStats.CacheMisses)
	}
	
	// Check timestamps are set
	if metaStats.StartTime == 0 {
		t.Error("StartTime should be set")
	}
	
	if metaStats.EndTime == 0 {
		t.Error("EndTime should be set")
	}
	
	if metaStats.EndTime <= metaStats.StartTime {
		t.Errorf("EndTime (%d) should be > StartTime (%d)", metaStats.EndTime, metaStats.StartTime)
	}
}

// TestStatsJSONMarshaling verifies that the stats can be marshaled to JSON deterministically
func TestStatsJSONMarshaling(t *testing.T) {
	collector := NewStatsCollector()
	
	// Add data in a specific order
	collector.RecordMatch("zulu", 10)
	collector.RecordMatch("alpha", 20)
	collector.RecordMatch("bravo", 15)
	
	collector.RecordProcessingTime("zulu_phase", 300*time.Millisecond)
	collector.RecordProcessingTime("alpha_phase", 100*time.Millisecond)
	collector.RecordProcessingTime("bravo_phase", 200*time.Millisecond)
	
	collector.RecordFilesProcessed(100)
	collector.RecordFilesSkipped(3)
	
	stats := collector.GetStats()
	
	// Marshal multiple times to ensure deterministic output
	var results []string
	for i := 0; i < 3; i++ {
		data, err := json.Marshal(stats)
		if err != nil {
			t.Fatalf("JSON marshaling failed: %v", err)
		}
		results = append(results, string(data))
	}
	
	// All results should be identical
	baseline := results[0]
	for i, result := range results[1:] {
		if result != baseline {
			t.Errorf("JSON output %d differs from baseline", i+1)
		}
	}
	
	// Verify key ordering in JSON string
	jsonStr := baseline
	
	// Match stats should be sorted: alpha, bravo, zulu
	alphaPos := findJSONKey(jsonStr, "alpha")
	bravoPos := findJSONKey(jsonStr, "bravo")
	zuluPos := findJSONKey(jsonStr, "zulu")
	
	if alphaPos == -1 || bravoPos == -1 || zuluPos == -1 {
		t.Errorf("Not all match stats keys found in JSON")
	} else if !(alphaPos < bravoPos && bravoPos < zuluPos) {
		t.Errorf("Match stats keys not in sorted order: alpha=%d, bravo=%d, zulu=%d", alphaPos, bravoPos, zuluPos)
	}
	
	// Phase stats should also be sorted: alpha_phase, bravo_phase, zulu_phase
	alphaPhasePos := findJSONKey(jsonStr, "alpha_phase")
	bravoPhasePos := findJSONKey(jsonStr, "bravo_phase")
	zuluPhasePos := findJSONKey(jsonStr, "zulu_phase")
	
	if alphaPhasePos == -1 || bravoPhasePos == -1 || zuluPhasePos == -1 {
		t.Errorf("Not all phase stats keys found in JSON")
	} else if !(alphaPhasePos < bravoPhasePos && bravoPhasePos < zuluPhasePos) {
		t.Errorf("Phase stats keys not in sorted order: alpha_phase=%d, bravo_phase=%d, zulu_phase=%d", alphaPhasePos, bravoPhasePos, zuluPhasePos)
	}
}

// TestEmitterDeltaWithStats tests creating a complete Delta structure with stats
func TestEmitterDeltaWithStats(t *testing.T) {
	collector := NewStatsCollector()
	
	// Simulate processing
	collector.RecordFilesProcessed(50)
	collector.RecordFilesSkipped(2)
	collector.RecordProcessingTime(string(PhaseIndexing), 200*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseParsing), 800*time.Millisecond)
	collector.RecordMatch(string(MatchTypeHTTPStatus), 12)
	collector.RecordMatch(string(MatchTypeResource), 8)
	
	stats := collector.GetStats()
	metaStats := emitter.NewMetaStatsFromProcessingStats(stats)
	
	// Create a Delta structure
	delta := emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats:   metaStats,
		},
		Controllers: []emitter.Controller{},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}
	
	// Marshal to JSON
	data, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("Failed to marshal Delta to JSON: %v", err)
	}
	
	// Unmarshal to verify structure
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal Delta JSON: %v", err)
	}
	
	// Check meta structure
	meta, ok := unmarshaled["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("Meta field not found or wrong type")
	}
	
	statsField, ok := meta["stats"].(map[string]interface{})
	if !ok {
		t.Fatal("Stats field not found or wrong type")
	}
	
	// Check some stats fields
	if filesParsed, ok := statsField["filesParsed"].(float64); !ok || int64(filesParsed) != 50 {
		t.Errorf("filesParsed in JSON = %v, want 50", statsField["filesParsed"])
	}
	
	if skipped, ok := statsField["skipped"].(float64); !ok || int64(skipped) != 2 {
		t.Errorf("skipped in JSON = %v, want 2", statsField["skipped"])
	}
	
	// Check that phase stats and match stats are present
	if _, ok := statsField["phaseStats"]; !ok {
		t.Error("phaseStats not found in JSON")
	}
	
	if _, ok := statsField["matchStats"]; !ok {
		t.Error("matchStats not found in JSON")
	}
}

// Helper function to find a JSON key position in a string
func findJSONKey(jsonStr, key string) int {
	searchStr := `"` + key + `"`
	return indexOf(jsonStr, searchStr)
}

// Helper function to find substring index
func indexOf(str, substr string) int {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
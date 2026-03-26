package optimizations

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/oxhq/oxinfer/internal/matchers"
	"github.com/oxhq/oxinfer/internal/parser"
)

// ParallelPatternMatcher implements aggressive parallel pattern matching per file.
// This replaces the sequential file-by-file pattern matching in the orchestrator
// with a worker pool that processes multiple files concurrently.
type ParallelPatternMatcher struct {
	matcher    matchers.CompositePatternMatcher
	maxWorkers int
}

// NewParallelPatternMatcher creates a new parallel pattern matcher.
func NewParallelPatternMatcher(matcher matchers.CompositePatternMatcher, maxWorkers int) *ParallelPatternMatcher {
	if maxWorkers <= 0 {
		maxWorkers = 8 // Aggressive default
	}

	return &ParallelPatternMatcher{
		matcher:    matcher,
		maxWorkers: maxWorkers,
	}
}

// ParsedFile represents a file that has been parsed and is ready for pattern matching.
type ParsedFile struct {
	FilePath   string
	SyntaxTree *parser.SyntaxTree
}

// FileMatchResult represents the result of pattern matching for a single file.
type FileMatchResult struct {
	FilePath string
	Patterns *matchers.LaravelPatterns
	Error    error
}

// MatchAllFiles performs parallel pattern matching across multiple files.
// This is the core optimization: instead of processing files sequentially,
// we use a worker pool to process multiple files concurrently.
func (ppm *ParallelPatternMatcher) MatchAllFiles(ctx context.Context, parsedFiles []*ParsedFile) (map[string]*matchers.LaravelPatterns, error) {
	if len(parsedFiles) == 0 {
		return make(map[string]*matchers.LaravelPatterns), nil
	}

	// Determine optimal worker count
	workerCount := ppm.maxWorkers
	if workerCount > len(parsedFiles) {
		workerCount = len(parsedFiles) // Don't create more workers than files
	}

	// Channels for aggressive parallel processing
	fileJobs := make(chan *ParsedFile, len(parsedFiles))
	results := make(chan *FileMatchResult, len(parsedFiles))

	// Track processing metrics
	startTime := time.Now()

	// Launch worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for file := range fileJobs {
				select {
				case <-ctx.Done():
					results <- &FileMatchResult{
						FilePath: file.FilePath,
						Error:    ctx.Err(),
					}
					return
				default:
				}

				// Skip files with no syntax tree
				if file.SyntaxTree == nil {
					results <- &FileMatchResult{
						FilePath: file.FilePath,
						Error:    fmt.Errorf("no syntax tree available"),
					}
					continue
				}

				// Perform pattern matching on this file
				patterns, err := ppm.matcher.MatchAll(ctx, file.SyntaxTree, file.FilePath)
				results <- &FileMatchResult{
					FilePath: file.FilePath,
					Patterns: patterns,
					Error:    err,
				}
			}
		}(i)
	}

	// Submit all files for processing
	for _, file := range parsedFiles {
		fileJobs <- file
	}
	close(fileJobs)

	// Close results channel when all workers complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results from all workers
	filePatterns := make(map[string]*matchers.LaravelPatterns)
	errorCount := 0

	for result := range results {
		if result.Error != nil {
			errorCount++
			continue
		}

		if result.Patterns != nil {
			NormalizeLaravelPatterns(result.Patterns)
			filePatterns[result.FilePath] = result.Patterns
		}
	}

	_ = time.Since(startTime) // Performance timing available if needed

	return filePatterns, nil
}

// AggregateResults aggregates pattern matches from multiple files into consolidated results.
// This replaces the manual aggregation loop in the orchestrator.
func (ppm *ParallelPatternMatcher) AggregateResults(filePatterns map[string]*matchers.LaravelPatterns) *AggregatedResults {
	results := &AggregatedResults{
		FilePatterns:        filePatterns,
		HTTPStatusMatches:   make([]*matchers.HTTPStatusMatch, 0),
		RequestUsageMatches: make([]*matchers.RequestUsageMatch, 0),
		ResourceMatches:     make([]*matchers.ResourceMatch, 0),
		PivotMatches:        make([]*matchers.PivotMatch, 0),
		AttributeMatches:    make([]*matchers.AttributeMatch, 0),
		ScopeMatches:        make([]*matchers.ScopeMatch, 0),
		PolymorphicMatches:  make([]*matchers.PolymorphicMatch, 0),
		BroadcastMatches:    make([]*matchers.BroadcastMatch, 0),
		FilesMatched:        int64(len(filePatterns)),
	}

	// Aggregate all matches by type in deterministic file order
	filePaths := make([]string, 0, len(filePatterns))
	for path := range filePatterns {
		filePaths = append(filePaths, path)
	}
	sort.Strings(filePaths)

	for _, path := range filePaths {
		patterns := filePatterns[path]
		NormalizeLaravelPatterns(patterns)
		results.HTTPStatusMatches = append(results.HTTPStatusMatches, patterns.HTTPStatus...)
		results.RequestUsageMatches = append(results.RequestUsageMatches, patterns.RequestUsage...)
		results.ResourceMatches = append(results.ResourceMatches, patterns.Resources...)
		results.PivotMatches = append(results.PivotMatches, patterns.Pivots...)
		results.AttributeMatches = append(results.AttributeMatches, patterns.Attributes...)
		results.ScopeMatches = append(results.ScopeMatches, patterns.Scopes...)
		results.PolymorphicMatches = append(results.PolymorphicMatches, patterns.Polymorphics...)
		results.BroadcastMatches = append(results.BroadcastMatches, patterns.Broadcasts...)
	}

	// Sort aggregated slices for deterministic downstream consumption
	sortHTTPStatusMatches(results.HTTPStatusMatches)
	sortRequestUsageMatches(results.RequestUsageMatches)
	sortResourceMatches(results.ResourceMatches)
	sortPivotMatches(results.PivotMatches)
	sortAttributeMatches(results.AttributeMatches)
	sortScopeMatches(results.ScopeMatches)
	sortPolymorphicMatches(results.PolymorphicMatches)
	sortBroadcastMatches(results.BroadcastMatches)

	// Calculate total matches
	results.TotalMatches = int64(len(results.HTTPStatusMatches) + len(results.RequestUsageMatches) +
		len(results.ResourceMatches) + len(results.PivotMatches) + len(results.AttributeMatches) +
		len(results.ScopeMatches) + len(results.PolymorphicMatches) + len(results.BroadcastMatches))

	return results
}

// NormalizeLaravelPatterns ensures each LaravelPatterns collection is sorted deterministically.
func NormalizeLaravelPatterns(patterns *matchers.LaravelPatterns) {
	if patterns == nil {
		return
	}

	sortHTTPStatusMatches(patterns.HTTPStatus)
	sortRequestUsageMatches(patterns.RequestUsage)
	sortResourceMatches(patterns.Resources)
	sortPivotMatches(patterns.Pivots)
	sortAttributeMatches(patterns.Attributes)
	sortScopeMatches(patterns.Scopes)
	sortPolymorphicMatches(patterns.Polymorphics)
	sortBroadcastMatches(patterns.Broadcasts)
}

func sortHTTPStatusMatches(matches []*matchers.HTTPStatusMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Method != matches[j].Method {
			return matches[i].Method < matches[j].Method
		}
		if matches[i].Status != matches[j].Status {
			return matches[i].Status < matches[j].Status
		}
		if matches[i].Explicit != matches[j].Explicit {
			return !matches[i].Explicit && matches[j].Explicit
		}
		return matches[i].Pattern < matches[j].Pattern
	})
}

func sortRequestUsageMatches(matches []*matchers.RequestUsageMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return stableJSONKey(matches[i]) < stableJSONKey(matches[j])
	})
}

func sortResourceMatches(matches []*matchers.ResourceMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Class != matches[j].Class {
			return matches[i].Class < matches[j].Class
		}
		if matches[i].Collection != matches[j].Collection {
			return !matches[i].Collection && matches[j].Collection
		}
		if matches[i].Method != matches[j].Method {
			return matches[i].Method < matches[j].Method
		}
		return matches[i].Pattern < matches[j].Pattern
	})
}

func sortPivotMatches(matches []*matchers.PivotMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Method != matches[j].Method {
			return matches[i].Method < matches[j].Method
		}
		if matches[i].Relation != matches[j].Relation {
			return matches[i].Relation < matches[j].Relation
		}
		if matches[i].Alias != matches[j].Alias {
			return matches[i].Alias < matches[j].Alias
		}
		return stableJSONKey(matches[i]) < stableJSONKey(matches[j])
	})
}

func sortAttributeMatches(matches []*matchers.AttributeMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Method != matches[j].Method {
			return matches[i].Method < matches[j].Method
		}
		if matches[i].Name != matches[j].Name {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].Pattern < matches[j].Pattern
	})
}

func sortScopeMatches(matches []*matchers.ScopeMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Context != matches[j].Context {
			return matches[i].Context < matches[j].Context
		}
		if matches[i].On != matches[j].On {
			return matches[i].On < matches[j].On
		}
		if matches[i].Name != matches[j].Name {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].Pattern < matches[j].Pattern
	})
}

func sortPolymorphicMatches(matches []*matchers.PolymorphicMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Context != matches[j].Context {
			return matches[i].Context < matches[j].Context
		}
		if matches[i].Relation != matches[j].Relation {
			return matches[i].Relation < matches[j].Relation
		}
		if matches[i].Type != matches[j].Type {
			return matches[i].Type < matches[j].Type
		}
		if matches[i].Model != matches[j].Model {
			return matches[i].Model < matches[j].Model
		}
		return stableJSONKey(matches[i]) < stableJSONKey(matches[j])
	})
}

func sortBroadcastMatches(matches []*matchers.BroadcastMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i] == nil || matches[j] == nil {
			return matches[i] != nil
		}
		if matches[i].Channel != matches[j].Channel {
			return matches[i].Channel < matches[j].Channel
		}
		if matches[i].Method != matches[j].Method {
			return matches[i].Method < matches[j].Method
		}
		return matches[i].Pattern < matches[j].Pattern
	})
}

func stableJSONKey(v any) string {
	if v == nil {
		return ""
	}
	bytes, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return fmt.Sprintf("%p", v)
	}
	return string(bytes)
}

// AggregatedResults represents the consolidated results from parallel pattern matching.
type AggregatedResults struct {
	FilePatterns        map[string]*matchers.LaravelPatterns `json:"filePatterns"`
	HTTPStatusMatches   []*matchers.HTTPStatusMatch          `json:"httpStatusMatches"`
	RequestUsageMatches []*matchers.RequestUsageMatch        `json:"requestUsageMatches"`
	ResourceMatches     []*matchers.ResourceMatch            `json:"resourceMatches"`
	PivotMatches        []*matchers.PivotMatch               `json:"pivotMatches"`
	AttributeMatches    []*matchers.AttributeMatch           `json:"attributeMatches"`
	ScopeMatches        []*matchers.ScopeMatch               `json:"scopeMatches"`
	PolymorphicMatches  []*matchers.PolymorphicMatch         `json:"polymorphicMatches"`
	BroadcastMatches    []*matchers.BroadcastMatch           `json:"broadcastMatches"`
	FilesMatched        int64                                `json:"filesMatched"`
	TotalMatches        int64                                `json:"totalMatches"`
}

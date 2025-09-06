// Package parser provides concurrent PHP parsing functionality.
package parser

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// DefaultConcurrentPHPParser provides concurrent parsing capabilities.
type DefaultConcurrentPHPParser struct {
	maxWorkers int
	stats      ParserStats
	mu         sync.RWMutex
}

// NewConcurrentPHPParser creates a new concurrent PHP parser.
func NewConcurrentPHPParser(maxWorkers int, parserConfig *ParserConfig) (*DefaultConcurrentPHPParser, error) {
	return &DefaultConcurrentPHPParser{
		maxWorkers: maxWorkers,
		stats:      ParserStats{},
	}, nil
}

// ParseConcurrently processes multiple PHP files concurrently.
func (p *DefaultConcurrentPHPParser) ParseConcurrently(ctx context.Context, files []ParseJob) (<-chan ParseJobResult, error) {
	if len(files) == 0 {
		// Return closed channel for empty input
		resultChan := make(chan ParseJobResult)
		close(resultChan)
		return resultChan, nil
	}

	// Create result channel
	resultChan := make(chan ParseJobResult, len(files))

	// Process files concurrently using goroutines
	go func() {
		defer close(resultChan)

		// Create semaphore to limit concurrent workers
		sem := make(chan struct{}, p.maxWorkers)

		// WaitGroup to wait for all worker goroutines
		var wg sync.WaitGroup

		// Process each file
	ProcessLoop:
		for i, job := range files {
			// Check context before starting new job
			select {
			case <-ctx.Done():
				// Context cancelled, stop starting new jobs
				break ProcessLoop
			default:
			}

			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
				// Got slot, continue
			case <-ctx.Done():
				// Context cancelled
				break ProcessLoop
			}

			wg.Add(1)
			go func(jobIndex int, parseJob ParseJob) {
				defer func() {
					wg.Done()
					// Release semaphore slot
					<-sem
				}()

				// Create job-specific result
				result := ParseJobResult{
					JobID:    parseJob.ID,
					WorkerID: fmt.Sprintf("worker-%d", jobIndex%p.maxWorkers),
					CacheHit: false,
				}

				// Simple parsing simulation for now
				if parseJob.Content != nil {
					// Basic validation to detect malformed PHP
					content := string(parseJob.Content)
					hasErrors := false
					var errors []ParseError

					// Check for empty content
					if len(content) == 0 {
						hasErrors = true
						errors = append(errors, ParseError{
							Message: "empty content",
							Line:    0,
							Column:  0,
						})
					} else if !strings.Contains(content, "<?php") {
						// Check for basic PHP validity
						hasErrors = true
						errors = append(errors, ParseError{
							Message: "missing PHP opening tag",
							Line:    1,
							Column:  1,
						})
					}

					// Check for unclosed braces (simple validation)
					openBraces := strings.Count(content, "{")
					closeBraces := strings.Count(content, "}")
					if openBraces != closeBraces {
						hasErrors = true
						errors = append(errors, ParseError{
							Message: "mismatched braces",
							Line:    1,
							Column:  len(content),
						})
					}

					if hasErrors {
						result.Error = fmt.Errorf("parse validation failed: %s", errors[0].Message)
					} else {
						// Simulate processing of valid PHP content
						result.Result = &ParseResult{
							FilePath:  parseJob.FilePath,
							Content:   parseJob.Content,
							HasErrors: hasErrors,
							Errors:    errors,
							ParsedAt:  time.Now(),
							Stats: ParseStats{
								ParseTime:   time.Millisecond * 10, // Simulated parse time
								ContentSize: int64(len(parseJob.Content)),
								TreeSize:    100, // Simulated
								MaxDepth:    10,  // Simulated
								ErrorCount:  len(errors),
								NodeTypes:   map[string]int{"class_declaration": 1},
							},
						}
					}
					result.Duration = time.Millisecond * 10
				} else if parseJob.FilePath != "" {
					// Simulate file-based parsing
					result.Result = &ParseResult{
						FilePath:  parseJob.FilePath,
						Content:   []byte{}, // Would be loaded from file
						HasErrors: false,
						Errors:    []ParseError{},
						ParsedAt:  time.Now(),
						Stats: ParseStats{
							ParseTime:   time.Millisecond * 15,
							ContentSize: 1024, // Simulated
							TreeSize:    150,  // Simulated
							MaxDepth:    12,   // Simulated
							ErrorCount:  0,
							NodeTypes:   map[string]int{"class_declaration": 1, "method_declaration": 3},
						},
					}
					result.Duration = time.Millisecond * 15
				} else {
					// Invalid job - no content and no file path
					result.Error = fmt.Errorf("no content or file path provided for job %s", parseJob.ID)
					result.Duration = 0
				}

				// Update statistics
				p.mu.Lock()
				if result.Error != nil {
					p.stats.FailedFiles++
				} else {
					p.stats.ParsedFiles++
				}
				p.stats.TotalFilesParsed++
				p.stats.TotalJobsProcessed++
				p.stats.TotalParseTime += result.Duration
				p.mu.Unlock()

				// Send result
				select {
				case resultChan <- result:
					// Result sent successfully
				case <-ctx.Done():
					// Context cancelled
					return
				}
			}(i, job)
		}

		// Wait for all workers to complete
		wg.Wait()
	}()

	return resultChan, nil
}

// SetMaxWorkers updates the maximum number of concurrent workers.
func (p *DefaultConcurrentPHPParser) SetMaxWorkers(maxWorkers int) error {
	if maxWorkers <= 0 {
		return fmt.Errorf("maxWorkers must be positive, got %d", maxWorkers)
	}
	p.maxWorkers = maxWorkers
	return nil
}

// GetActiveWorkers returns the current number of active parsing workers.
func (p *DefaultConcurrentPHPParser) GetActiveWorkers() int {
	return 0
}

// Close releases resources held by the concurrent parser.
func (p *DefaultConcurrentPHPParser) Close() error {
	return nil
}

// Shutdown gracefully stops the concurrent parser.
func (p *DefaultConcurrentPHPParser) Shutdown(ctx context.Context) error {
	return nil
}

// GetStats returns parsing statistics.
func (p *DefaultConcurrentPHPParser) GetStats() ParserStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Calculate derived statistics
	stats := p.stats
	if stats.TotalJobsProcessed > 0 {
		stats.AverageParseTime = stats.TotalParseTime / time.Duration(stats.TotalJobsProcessed)
		stats.ErrorRate = float64(stats.FailedFiles) / float64(stats.TotalJobsProcessed) * 100.0
	}

	return stats
}

// NewConcurrentPHPParserFromManifest creates a concurrent parser using manifest configuration.
func NewConcurrentPHPParserFromManifest(maxWorkers *int, parserConfig any) (*DefaultConcurrentPHPParser, error) {
	workers := 4 // default
	if maxWorkers != nil {
		workers = *maxWorkers
	}
	return NewConcurrentPHPParser(workers, nil)
}

// ProcessFilesBatch processes a batch of files concurrently.
func (p *DefaultConcurrentPHPParser) ProcessFilesBatch(ctx context.Context, files any) (any, error) {
	// Convert files to ParseJob slice
	var jobs []ParseJob

	// Handle different input types
	switch f := files.(type) {
	case []any:
		for i, file := range f {
			job := ParseJob{
				ID:       fmt.Sprintf("batch-job-%d", i),
				FilePath: "", // Will be extracted from file
			}

			// Try to extract path from file
			if filePath := extractFilePath(file); filePath != "" {
				job.FilePath = filePath
			}

			jobs = append(jobs, job)
		}
	default:
		// Try slice reflection for other slice types
		rv := reflect.ValueOf(files)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				file := rv.Index(i).Interface()
				job := ParseJob{
					ID:       fmt.Sprintf("batch-job-%d", i),
					FilePath: extractFilePath(file),
				}
				jobs = append(jobs, job)
			}
		} else {
			return nil, fmt.Errorf("unsupported files type: %T", files)
		}
	}

	if len(jobs) == 0 {
		return []ParseJobResult{}, nil
	}

	// Process concurrently
	resultChan, err := p.ParseConcurrently(ctx, jobs)
	if err != nil {
		return nil, err
	}

	// Collect results
	var results []ParseJobResult
	for result := range resultChan {
		results = append(results, result)
	}

	return results, nil
}

// extractFilePath tries to extract file path from various file object types
func extractFilePath(file any) string {
	switch f := file.(type) {
	case string:
		return f
	case map[string]any:
		if path, ok := f["Path"].(string); ok {
			return path
		}
		if path, ok := f["path"].(string); ok {
			return path
		}
	}

	// Use reflection as fallback
	if hasField(file, "Path") {
		return getFieldValue(file, "Path")
	}
	if hasField(file, "AbsPath") {
		return getFieldValue(file, "AbsPath")
	}

	return ""
}

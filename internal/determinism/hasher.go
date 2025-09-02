// Package determinism provides utilities for validating deterministic behavior
// in oxinfer's output generation. This ensures the core promise that
// "same repository → same delta.json, byte-for-byte".
package determinism

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/garaekz/oxinfer/internal/emitter"
)

// HashResult contains hash information and metadata for validation.
type HashResult struct {
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	CanonicalSHA256 string `json:"canonicalSha256"` // Hash without volatile fields
}

// DeltaHasher provides deterministic hashing of Delta structures.
type DeltaHasher struct {
	emitter emitter.DeltaEmitter
}

// NewDeltaHasher creates a new hasher instance.
func NewDeltaHasher() *DeltaHasher {
	return &DeltaHasher{
		emitter: emitter.NewJSONEmitter(),
	}
}

// HashDelta calculates SHA256 hash of a Delta structure.
// It uses the deterministic JSON marshaler to ensure consistent output.
func (h *DeltaHasher) HashDelta(delta *emitter.Delta) (*HashResult, error) {
	if delta == nil {
		return nil, fmt.Errorf("delta cannot be nil")
	}

	// Get deterministic JSON bytes
	jsonBytes, err := h.emitter.MarshalDeterministic(delta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal delta for hashing: %w", err)
	}

	// Calculate main hash
	hash := sha256.Sum256(jsonBytes)
	hashStr := hex.EncodeToString(hash[:])

	// Calculate canonical hash without volatile fields
	canonicalBytes, err := h.emitter.CanonicalBytes(delta)
	if err != nil {
		return nil, fmt.Errorf("failed to get canonical bytes: %w", err)
	}

	canonicalHash := sha256.Sum256(canonicalBytes)
	canonicalHashStr := hex.EncodeToString(canonicalHash[:])

	return &HashResult{
		SHA256:          hashStr,
		Size:            int64(len(jsonBytes)),
		CanonicalSHA256: canonicalHashStr,
	}, nil
}

// HashBytes calculates SHA256 hash of raw JSON bytes.
// This is useful when working with output from CLI runs.
func (h *DeltaHasher) HashBytes(jsonBytes []byte) (*HashResult, error) {
	if len(jsonBytes) == 0 {
		return nil, fmt.Errorf("input bytes cannot be empty")
	}

	// Validate JSON structure first
	var delta emitter.Delta
	if err := json.Unmarshal(jsonBytes, &delta); err != nil {
		return nil, fmt.Errorf("invalid JSON structure: %w", err)
	}

	// Re-marshal to ensure deterministic ordering
	deterministicBytes, err := h.emitter.MarshalDeterministic(&delta)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize JSON for hashing: %w", err)
	}

	// Calculate hash
	hash := sha256.Sum256(deterministicBytes)
	hashStr := hex.EncodeToString(hash[:])

	// Calculate canonical hash
	canonicalBytes, err := h.emitter.CanonicalBytes(&delta)
	if err != nil {
		return nil, fmt.Errorf("failed to get canonical bytes: %w", err)
	}

	canonicalHash := sha256.Sum256(canonicalBytes)
	canonicalHashStr := hex.EncodeToString(canonicalHash[:])

	return &HashResult{
		SHA256:          hashStr,
		Size:            int64(len(deterministicBytes)),
		CanonicalSHA256: canonicalHashStr,
	}, nil
}

// CompareHashes compares two hash results for equality.
// It compares both full hashes and canonical hashes.
func (h *DeltaHasher) CompareHashes(hash1, hash2 *HashResult) *ComparisonResult {
	if hash1 == nil || hash2 == nil {
		return &ComparisonResult{
			Identical: false,
			Error:     "cannot compare nil hash results",
		}
	}

	identical := hash1.SHA256 == hash2.SHA256
	canonicalIdentical := hash1.CanonicalSHA256 == hash2.CanonicalSHA256

	result := &ComparisonResult{
		Identical:          identical,
		CanonicalIdentical: canonicalIdentical,
		SizeDifference:     hash1.Size - hash2.Size,
	}

	// Provide detailed error information if hashes don't match
	if !identical {
		result.Error = fmt.Sprintf("SHA256 mismatch: %s != %s", hash1.SHA256, hash2.SHA256)
	}
	if !canonicalIdentical {
		if result.Error != "" {
			result.Error += "; "
		}
		result.Error += fmt.Sprintf("canonical SHA256 mismatch: %s != %s",
			hash1.CanonicalSHA256, hash2.CanonicalSHA256)
	}

	return result
}

// ComparisonResult contains the results of comparing two hash results.
type ComparisonResult struct {
	Identical          bool   `json:"identical"`
	CanonicalIdentical bool   `json:"canonicalIdentical"`
	SizeDifference     int64  `json:"sizeDifference"`
	Error              string `json:"error,omitempty"`
}

// MultiHashResult contains results from multiple hash calculations.
// This is used for triple-run validation.
type MultiHashResult struct {
	Hashes           []*HashResult `json:"hashes"`
	AllIdentical     bool          `json:"allIdentical"`
	AllCanonical     bool          `json:"allCanonicalIdentical"`
	UniqueHashes     []string      `json:"uniqueHashes"`
	ComparisonMatrix [][]bool      `json:"comparisonMatrix"`
	Errors           []string      `json:"errors,omitempty"`
}

// HashMultiple calculates hashes for multiple Delta instances and compares them.
// This is the core function used by the triple-run validator.
func (h *DeltaHasher) HashMultiple(deltas []*emitter.Delta) (*MultiHashResult, error) {
	if len(deltas) == 0 {
		return nil, fmt.Errorf("no deltas provided")
	}

	result := &MultiHashResult{
		Hashes:           make([]*HashResult, len(deltas)),
		ComparisonMatrix: make([][]bool, len(deltas)),
		Errors:           make([]string, 0),
	}

	// Initialize comparison matrix
	for i := range result.ComparisonMatrix {
		result.ComparisonMatrix[i] = make([]bool, len(deltas))
	}

	// Calculate hashes for all deltas
	uniqueHashes := make(map[string]bool)
	for i, delta := range deltas {
		hash, err := h.HashDelta(delta)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("failed to hash delta %d: %v", i, err))
			continue
		}

		result.Hashes[i] = hash
		uniqueHashes[hash.SHA256] = true
	}

	// Build comparison matrix and check for identical results
	allIdentical := true
	allCanonical := true
	for i := 0; i < len(deltas); i++ {
		for j := 0; j < len(deltas); j++ {
			if i == j {
				result.ComparisonMatrix[i][j] = true
				continue
			}

			if result.Hashes[i] != nil && result.Hashes[j] != nil {
				comparison := h.CompareHashes(result.Hashes[i], result.Hashes[j])
				result.ComparisonMatrix[i][j] = comparison.Identical

				if !comparison.Identical {
					allIdentical = false
				}
				if !comparison.CanonicalIdentical {
					allCanonical = false
				}
			} else {
				result.ComparisonMatrix[i][j] = false
				allIdentical = false
				allCanonical = false
			}
		}
	}

	result.AllIdentical = allIdentical
	result.AllCanonical = allCanonical

	// Extract unique hash values for analysis
	result.UniqueHashes = make([]string, 0, len(uniqueHashes))
	for hash := range uniqueHashes {
		result.UniqueHashes = append(result.UniqueHashes, hash)
	}
	sort.Strings(result.UniqueHashes) // Sort for deterministic output

	return result, nil
}

// ValidationError represents errors found during determinism validation.
type ValidationError struct {
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Details     map[string]string `json:"details,omitempty"`
}

// DeterminismReport contains comprehensive results from determinism validation.
type DeterminismReport struct {
	TestName         string             `json:"testName"`
	RunCount         int                `json:"runCount"`
	AllIdentical     bool               `json:"allIdentical"`
	AllCanonical     bool               `json:"allCanonicalIdentical"`
	UniqueHashCount  int                `json:"uniqueHashCount"`
	FirstHash        *HashResult        `json:"firstHash"`
	ValidationErrors []*ValidationError `json:"validationErrors"`
	ExecutionTime    int64              `json:"executionTimeMs"`
}

// NewDeterminismReport creates a new empty validation report.
func NewDeterminismReport(testName string, runCount int) *DeterminismReport {
	return &DeterminismReport{
		TestName:         testName,
		RunCount:         runCount,
		ValidationErrors: make([]*ValidationError, 0),
	}
}

// AddValidationError adds an error to the validation report.
func (r *DeterminismReport) AddValidationError(errorType, description string, details map[string]string) {
	r.ValidationErrors = append(r.ValidationErrors, &ValidationError{
		Type:        errorType,
		Description: description,
		Details:     details,
	})
}

// IsValid returns true if the validation passed with no errors.
func (r *DeterminismReport) IsValid() bool {
	return r.AllIdentical && len(r.ValidationErrors) == 0
}

// IsCanonicalValid returns true if canonical hashes are identical (ignoring volatile fields).
func (r *DeterminismReport) IsCanonicalValid() bool {
	return r.AllCanonical && len(r.ValidationErrors) == 0
}

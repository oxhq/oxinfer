# Integration Test Suite - T13.2 Implementation

This directory contains the comprehensive end-to-end integration testing system for Oxinfer, validating the complete T1-T12 pipeline with realistic Laravel projects.

## Overview

The integration test suite validates the entire Oxinfer pipeline from manifest validation through delta.json emission using realistic Laravel application fixtures. It includes regression testing, performance validation, and determinism checking.

## Test Structure

### Core Test Files

1. **`e2e_test.go`** - Main end-to-end integration tests
   - Complete pipeline validation with realistic Laravel fixtures
   - Golden file regression testing with SHA256 verification
   - Deterministic output validation across multiple runs
   - Performance threshold checking per fixture complexity

2. **`pipeline_test.go`** - Component integration testing
   - T1-T12 pipeline component integration validation
   - Feature-specific pattern matching verification
   - Performance benchmarking with realistic thresholds
   - Error handling across pipeline stages

3. **`fixtures_test.go`** - Fixture structure validation
   - Laravel project structure validation
   - PHP file syntax and pattern verification
   - Composer.json and manifest.json validation
   - Internal class dependency checking

4. **`end_to_end_test.go`** - Existing CLI validation tests
   - Basic CLI functionality and error handling
   - Manifest validation and schema compliance
   - Output format verification

5. **`performance_test.go`** - Performance benchmarking
   - Manifest processing performance validation
   - Memory usage baseline testing
   - Deterministic output verification

6. **`polymorphic_test.go`** - Advanced pattern testing
   - Polymorphic relationship pattern validation
   - Complex Laravel feature testing

## Test Fixtures

### 1. Minimal Laravel (`test/fixtures/integration/minimal-laravel/`)
**Purpose**: Basic Laravel patterns validation
- Simple UserController with RESTful methods
- Basic User and Post models with relationships
- API routes configuration
- **Expected Patterns**: HTTP status codes, request validation
- **Performance Threshold**: 5 seconds

### 2. API Project (`test/fixtures/integration/api-project/`)
**Purpose**: Advanced API patterns validation
- ProductController with resource transformations
- Form request validation (StoreProductRequest, UpdateProductRequest)
- API resources and collections
- Pivot relationships with additional columns
- Query scopes usage
- **Expected Patterns**: Resources, pivot relationships, scopes, validation
- **Performance Threshold**: 10 seconds

### 3. Complex App (`test/fixtures/integration/complex-app/`)
**Purpose**: Advanced Laravel features validation
- Polymorphic relationships (comments, images, tags)
- Broadcasting channels with parameter extraction
- Complex query scopes and relationship chains
- MorphToMany and MorphedByMany relationships
- **Expected Patterns**: All T5-T11 patterns including polymorphic and broadcasting
- **Performance Threshold**: 15 seconds

## Golden File System

### Location: `test/golden/`

1. **`minimal-laravel.json`** - Expected output for basic patterns
2. **`api-project.json`** - Expected output for API patterns
3. **`complex-app.json`** - Expected output for advanced patterns
4. **`checksums.sha256`** - SHA256 checksums for regression detection

### Golden File Features
- Complete delta.json structure validation
- Deterministic output verification
- Regression detection on schema changes
- Performance metadata validation

## Test Categories

### 1. End-to-End Integration Tests
```go
func TestE2EIntegrationSuite(t *testing.T)
```
- Validates complete manifest → delta.json pipeline
- Tests all three fixture scenarios
- Includes performance and determinism validation
- Verifies output against golden files

### 2. Pipeline Component Tests
```go
func TestPipelineIntegration(t *testing.T)
```
- T1-T4: Manifest validation through PHP parsing
- T5-T6: HTTP status and resource pattern matching
- T7-T8: Pivot relationships and query scopes
- T9-T10: Polymorphic relationships and broadcasting
- T11-T12: Shape inference and JSON emission

### 3. Fixture Validation Tests
```go
func TestFixtureStructure(t *testing.T)
```
- Directory structure validation
- PHP file syntax and namespace checking
- Laravel-specific pattern verification
- Internal dependency validation

### 4. Golden File Integrity Tests
```go
func TestGoldenFileIntegrity(t *testing.T)
```
- JSON structure validation
- SHA256 checksum verification
- Schema compliance checking

### 5. Performance and Determinism Tests
```go
func TestDeterministicOutput(t *testing.T)
```
- Multiple-run consistency validation
- Performance threshold enforcement
- Memory usage monitoring

## Running Tests

### Full Integration Test Suite
```bash
go test ./test/integration/... -v
```

### Specific Test Categories
```bash
# End-to-end tests only
go test ./test/integration/... -v -run TestE2E

# Pipeline integration tests
go test ./test/integration/... -v -run TestPipeline

# Fixture validation tests
go test ./test/integration/... -v -run TestFixture

# Performance tests
go test ./test/integration/... -v -run Performance
```

### With Performance Benchmarking
```bash
go test ./test/integration/... -v -bench=.
```

## Expected Test Behavior

### Current State (T13.2 Complete, T5-T11 Pending)

**✅ Passing Tests:**
- Fixture validation tests - All Laravel fixtures are properly structured
- Golden file integrity - Expected outputs are valid JSON with correct checksums
- Error handling tests - CLI properly handles invalid inputs with structured errors
- Performance validation - Manifest processing meets performance thresholds

**⚠️ Expected Failures (Until T5-T11 Implementation):**
- E2E integration tests fail with "pattern matcher creation not implemented"
- Pipeline component tests fail at pattern matching phase
- This is expected and correct behavior until T5-T11 are implemented

### Post T5-T11 Implementation

Once pattern matchers are implemented, the integration tests will:
1. Validate complete T1-T12 pipeline execution
2. Verify pattern detection accuracy against golden files
3. Ensure deterministic output across multiple runs
4. Confirm performance meets production requirements

## Performance Thresholds

- **Minimal Laravel**: < 5 seconds (5 PHP files)
- **API Project**: < 10 seconds (12 PHP files) 
- **Complex App**: < 15 seconds (10+ PHP files with advanced patterns)

These thresholds ensure the MVP performance goal of <10s for medium projects is achievable.

## Integration with CI/CD

The integration tests are designed for continuous integration:

1. **Deterministic**: Same input always produces identical output
2. **Self-contained**: No external dependencies beyond Go toolchain
3. **Performance-aware**: Fails if execution exceeds thresholds
4. **Regression-safe**: Golden file checksums detect unintended changes

## Future Enhancements

### Phase 1 (Post T5-T11)
- Add integration tests for incremental processing with cache
- Expand fixture complexity with real-world Laravel patterns
- Add memory usage profiling and leak detection

### Phase 2 (Performance Optimization)
- Large project fixtures (500+ files)
- Concurrent processing validation
- Cache effectiveness measurement

### Phase 3 (Production Readiness)
- Edge case fixture coverage
- Error recovery testing
- Integration with external Laravel projects

## Architecture Notes

The integration test suite follows clean architecture principles:

- **Separation of Concerns**: Each test file focuses on specific validation aspects
- **Realistic Testing**: Uses actual Laravel code patterns, not synthetic examples
- **Maintainable**: Clear test structure and comprehensive documentation
- **Extensible**: Easy to add new fixtures and test scenarios

This test suite ensures Oxinfer will work correctly with real Laravel projects when deployed in production environments.
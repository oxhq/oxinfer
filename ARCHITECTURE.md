# Oxinfer Architecture

This document describes the internal architecture, data flow, and module boundaries of Oxinfer.

## High-Level Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                            Oxinfer CLI                              │
├─────────────────────────────────────────────────────────────────────┤
│  Input: manifest.json → Output: delta.json (deterministic)         │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Pipeline Orchestrator                      │
├─────────────────────────────────────────────────────────────────────┤
│  Coordinates: Index → Parse → Match → Infer → Emit                 │
└─────────────────────────────────────────────────────────────────────┘
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        ▼                           ▼                           ▼
┌─────────────┐           ┌─────────────────┐         ┌─────────────────┐
│   Indexer   │           │     Parser      │         │    Matchers     │
├─────────────┤           ├─────────────────┤         ├─────────────────┤
│• File Discovery        │• Tree-sitter    │         │• 8 Pattern Types│
│• Cache Management      │• Concurrent     │         │• Laravel Queries│
│• Worker Pools          │• Memory Pool    │         │• Result Sorting │
└─────────────┘           └─────────────────┘         └─────────────────┘
        │                           │                           │
        └───────────────────────────┼───────────────────────────┘
                                    ▼
        ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
        │ Shape Inferencer │    │   Assembler     │    │    Emitter      │
        ├─────────────────┤    ├─────────────────┤    ├─────────────────┤
        │• Request Shapes │    │• Data Aggregation│    │• JSON Generation│
        │• Content Types  │    │• Cross-references│    │• Deterministic  │
        │• Basic Nesting  │    │• Model Building │    │• Schema Validation│
        └─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Data Flow

### 1. Initialization Phase
```
manifest.json → Validation → Configuration → Component Registry
```

### 2. Indexing Phase
```
Project Root → File Discovery → Cache Check → Worker Pool → File List
```

### 3. Parsing Phase
```
File List → Tree-sitter PHP → Concurrent Processing → Syntax Trees
```

### 4. Pattern Matching Phase
```
Syntax Trees → 8 Pattern Matchers → Laravel Pattern Results
```

### 5. Shape Inference Phase
```
Pattern Results → Request Analysis → Content Type Detection → Shapes
```

### 6. Assembly Phase
```
All Results → Data Aggregation → Cross-referencing → Structured Data
```

### 7. Emission Phase
```
Structured Data → JSON Generation → Schema Validation → delta.json
```

## Module Boundaries

### Core Modules

#### `/cmd/oxinfer` - CLI Entry Point
- **Responsibility**: Command-line interface, flag parsing, orchestration
- **Dependencies**: All internal modules
- **Exports**: Main binary

#### `/internal/cli` - CLI Configuration
- **Responsibility**: Flag parsing, configuration validation, help text
- **Dependencies**: None (pure)
- **Exports**: `CLIConfig`, error types

#### `/internal/manifest` - Manifest Processing
- **Responsibility**: JSON loading, schema validation, defaults
- **Dependencies**: JSON schema validator
- **Exports**: `Manifest`, `ManifestLoader`

#### `/internal/pipeline` - Pipeline Orchestration
- **Responsibility**: Phase coordination, component integration, progress tracking
- **Dependencies**: All processing modules
- **Exports**: `PipelineOrchestrator`, `ComponentRegistry`

### Processing Modules

#### `/internal/psr4` - PSR-4 Resolution
- **Responsibility**: Composer.json parsing, FQCN ↔ file mapping
- **Dependencies**: None (filesystem only)
- **Exports**: `PSR4Resolver`, class mapping

#### `/internal/indexer` - File Indexing
- **Responsibility**: File discovery, caching, worker pool management
- **Dependencies**: PSR-4 resolver
- **Exports**: `FileIndexer`, `FileDiscoverer`, `FileCacher`

#### `/internal/parser` - PHP Parsing
- **Responsibility**: Tree-sitter integration, syntax tree generation
- **Dependencies**: tree-sitter, PHP grammar
- **Exports**: `PHPParser`, `SyntaxTree`, parser pool

#### `/internal/matchers` - Pattern Matching
- **Responsibility**: Laravel pattern detection, query execution
- **Dependencies**: Parser (syntax trees)
- **Exports**: 8 pattern matcher types, `PatternMatcher`

#### `/internal/infer` - Shape Inference
- **Responsibility**: Request shape consolidation, content type detection
- **Dependencies**: Matchers (pattern results)
- **Exports**: `ShapeInferencer`, `RequestInfo`

#### `/internal/emitter` - JSON Emission
- **Responsibility**: Delta JSON generation, deterministic output
- **Dependencies**: All result types
- **Exports**: `DeltaEmitter`, `Delta` types

### Support Modules

#### `/internal/stats` - Statistics Collection
- **Responsibility**: Performance metrics, processing statistics
- **Dependencies**: None (pure)
- **Exports**: `StatsCollector`, metrics types

#### `/internal/bench` - Benchmarking
- **Responsibility**: Performance profiling, memory analysis
- **Dependencies**: Stats collector
- **Exports**: Benchmark runners, profiling tools

#### `/internal/determinism` - Determinism Validation
- **Responsibility**: Hash comparison, output validation
- **Dependencies**: Emitter
- **Exports**: Hash validators, comparison tools

## Component Communication

### Registry Pattern
```go
type ComponentRegistry struct {
    ManifestLoader    manifest.ManifestLoader
    PSR4Resolver      psr4.PSR4Resolver
    FileIndexer       indexer.FileIndexer
    PHPParser         parser.PHPParser
    PatternMatcher    matchers.PatternMatcher
    ShapeInferencer   infer.ShapeInferencer
    DeltaAssembler    pipeline.DeltaAssembler
    DeltaEmitter      emitter.DeltaEmitter
    StatsCollector    stats.StatsCollector
}
```

### Interface-Driven Design
- All major components implement interfaces
- Dependency injection via registry
- Testable via mock implementations
- Clean separation of concerns

### Error Handling
```go
// Structured errors with context
type ProcessingError struct {
    Type    string
    Phase   ProcessingPhase
    Context string
    Cause   error
}
```

## Concurrency Model

### Worker Pools
- **Indexer**: File discovery and cache validation
- **Parser**: Concurrent PHP parsing with tree-sitter
- **Matchers**: Pattern matching across files

### Thread Safety
- **Immutable data**: Results are read-only after creation
- **Atomic operations**: Statistics collection
- **Channel communication**: Worker coordination

### Resource Management
- **Parser pools**: Reuse tree-sitter parsers
- **Memory limits**: Configurable bounds
- **Graceful shutdown**: Context cancellation

## Determinism Architecture

### Sources of Non-Determinism
1. **Map iteration order** (Go randomizes)
2. **Concurrent processing** (race conditions)
3. **Filesystem ordering** (OS-dependent)
4. **Timing fields** (variable execution time)

### Determinism Solutions
1. **Sorted keys**: All map iterations use sorted key slices
2. **Sequential aggregation**: Results collected in deterministic order
3. **File sorting**: Input files processed in lexicographic order
4. **Stable algorithms**: All sorting uses stable comparison functions

### Verification
```go
// Triple-run validation
func VerifyDeterminism(manifest string) bool {
    results := make([][]byte, 3)
    for i := range results {
        results[i] = RunOxinfer(manifest)
    }
    return bytes.Equal(results[0], results[1]) && 
           bytes.Equal(results[1], results[2])
}
```

## Performance Architecture

### Caching Strategy
- **File-level caching**: SHA256+mtime or mtime-only
- **Cache location**: `<project>/.oxinfer/cache/v1/`
- **Cache invalidation**: Automatic on file changes
- **Cache partitioning**: Per-project isolation

### Memory Management
- **Streaming processing**: Large files processed in chunks
- **Pool recycling**: Parser and worker reuse
- **Garbage collection**: Explicit cleanup of large objects
- **Memory monitoring**: Configurable limits with partial results

### Limits & Throttling
```go
type ResourceLimits struct {
    MaxWorkers int // Concurrent processing
    MaxFiles   int // Files before partial=true
    MaxDepth   int // Relationship traversal depth
}
```

## Testing Architecture

### Test Categories
1. **Unit tests**: Per-module with table-driven tests
2. **Integration tests**: End-to-end pipeline validation
3. **Golden tests**: Exact JSON output comparison
4. **Performance tests**: Benchmark validation
5. **Determinism tests**: Triple-run SHA256 verification

### Test Fixtures
```
/test/fixtures/integration/
├── minimal-laravel/     # Basic Laravel structure
├── api-project/         # API-focused project
└── complex-app/         # Full-featured application
```

### Quality Gates
- **Code coverage**: >90% for core modules
- **Determinism**: 100% pass rate for triple runs
- **Performance**: <10s for medium projects
- **Schema compliance**: 100% input/output validation

## Extension Points

### Custom Pattern Matchers
```go
type CustomMatcher struct{}

func (m *CustomMatcher) Match(tree *SyntaxTree) []*MatchResult {
    // Custom Laravel pattern detection
}
```

### Plugin Architecture
- **Interface-based**: Implement `PatternMatcher`
- **Registration**: Add to `ComponentRegistry`
- **Configuration**: Extend manifest schema

### Output Formats
- **Delta JSON**: Primary structured output
- **Custom emitters**: Implement `DeltaEmitter`
- **Format flexibility**: XML, YAML, etc.

This architecture ensures scalability, maintainability, and deterministic behavior while providing clear separation of concerns and testability.
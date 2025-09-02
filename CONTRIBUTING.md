# Contributing to Oxinfer

Thank you for your interest in contributing to Oxinfer! This document provides guidelines for development, testing, and contributing to the project.

## Development Setup

### Prerequisites

- **Go 1.22+**: Required for language features and performance
- **Git**: For version control and contribution workflow
- **tree-sitter**: PHP grammar dependency (handled by Go modules)

### Local Setup

```bash
# Clone the repository
git clone https://github.com/garaekz/oxinfer.git
cd oxinfer

# Install dependencies
go mod download

# Build the CLI
go build -o oxinfer cmd/oxinfer

# Run tests
go test ./...

# Verify installation
./oxinfer --version
```

## Code Style

### Go Standards

- **gofmt**: All code must be formatted with `go fmt`
- **go vet**: Code must pass `go vet` checks
- **golint**: Follow Go lint recommendations
- **Naming conventions**: Use Go idiomatic naming

### Project Conventions

#### File Organization
```
internal/
├── module/           # One directory per module
│   ├── types.go      # Type definitions
│   ├── interfaces.go # Interface definitions  
│   ├── module.go     # Main implementation
│   └── module_test.go # Tests
```

#### Interface Design
```go
// Interfaces should be small and focused
type PatternMatcher interface {
    Match(tree *SyntaxTree) ([]*MatchResult, error)
    GetType() PatternType
}

// Implementation structs follow naming convention
type DefaultPatternMatcher struct {
    // Private fields only
}
```

#### Error Handling
```go
// Use structured errors with context
func processFile(path string) error {
    if _, err := os.Stat(path); err != nil {
        return fmt.Errorf("failed to access file %s: %w", path, err)
    }
    // ...
}

// No panics in production code
// Use typed errors for different failure modes
type ValidationError struct {
    Field   string
    Value   interface{}
    Message string
}
```

### Determinism Requirements

**Critical**: All code must produce deterministic output

```go
// ❌ Wrong: Map iteration is randomized
for key, value := range someMap {
    results = append(results, process(key, value))
}

// ✅ Correct: Sort keys first
keys := make([]string, 0, len(someMap))
for key := range someMap {
    keys = append(keys, key)
}
sort.Strings(keys)
for _, key := range keys {
    results = append(results, process(key, someMap[key]))
}
```

## Testing Strategy

### Test Categories

#### 1. Unit Tests
- **Coverage**: >90% for core modules
- **Pattern**: Table-driven tests
- **Naming**: `TestFunctionName` format

```go
func TestPatternMatcher(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected []PatternMatch
        wantErr  bool
    }{
        {
            name:  "simple_pattern",
            input: "<?php response(201);",
            expected: []PatternMatch{
                {Type: "http_status", Status: 201},
            },
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := matcher.Match(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

#### 2. Integration Tests
- **End-to-end**: Full pipeline validation
- **Golden files**: Exact output comparison
- **Fixtures**: Realistic Laravel projects

```bash
# Run integration tests
go test ./test/integration -v

# Update golden files (after verification)
go test ./test/integration -update-golden
```

#### 3. Performance Tests
- **Benchmarks**: Go benchmark functions
- **Memory profiling**: Check for leaks
- **Determinism**: Triple-run validation

```go
func BenchmarkPatternMatching(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _, err := matcher.Match(largeFile)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

### Test Execution

```bash
# All tests
go test ./...

# Verbose output
go test -v ./...

# With coverage
go test -cover ./...

# Integration tests only
go test ./test/integration/...

# Benchmarks
go test -bench=. ./...

# Determinism validation
go test ./test/determinism/...
```

### Golden File Testing

Golden files store expected output for comparison:

```bash
# Test fixtures structure
test/fixtures/integration/
├── minimal-laravel/
│   ├── manifest.json
│   ├── app/Http/Controllers/...
│   └── expected/delta.json    # Golden file
```

**Updating golden files:**
1. Verify changes are intentional
2. Run tests with `-update-golden`
3. Review diffs carefully
4. Commit both code and golden file changes

## Development Workflow

### Branch Strategy

- **main**: Stable, deployable code
- **feature/**: New features (`feature/add-new-matcher`)
- **fix/**: Bug fixes (`fix/determinism-race-condition`)
- **docs/**: Documentation updates

### Commit Guidelines

#### Commit Message Format
```
<type>: <subject>

<body>

<footer>
```

**Types:**
- `feat`: New features
- `fix`: Bug fixes
- `docs`: Documentation changes
- `test`: Test additions/modifications
- `refactor`: Code refactoring
- `perf`: Performance improvements

#### Examples
```bash
# Good commit messages
feat: add polymorphic relationship pattern matcher
fix: resolve determinism race condition in stats collection
docs: update API documentation with new endpoints
test: add integration tests for broadcast channel detection

# Avoid
fix: bug
add stuff
misc changes
```

### Pull Request Process

1. **Create branch**: `git checkout -b feature/my-feature`
2. **Develop**: Write code + tests
3. **Test**: Ensure all tests pass
4. **Format**: Run `go fmt ./...`
5. **Commit**: Use conventional commit messages
6. **Push**: `git push origin feature/my-feature`
7. **PR**: Create pull request with description

#### PR Template
```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed
- [ ] Golden files updated (if applicable)

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] No breaking changes (or documented)
```

## Adding New Features

### Pattern Matchers

To add a new Laravel pattern matcher:

1. **Create matcher file**: `internal/matchers/my_pattern.go`
2. **Implement interface**: 
```go
type MyPatternMatcher struct{}

func (m *MyPatternMatcher) Match(tree *SyntaxTree) ([]*MatchResult, error) {
    // Implementation
}

func (m *MyPatternMatcher) GetType() PatternType {
    return PatternTypeMyPattern
}
```

3. **Add queries**: Define tree-sitter queries in `queries.go`
4. **Register matcher**: Add to composite matcher
5. **Add tests**: Unit tests + integration tests
6. **Update schema**: Extend delta.schema.json if needed

### CLI Flags

Adding new CLI flags:

1. **Update config**: Add field to `CLIConfig` in `internal/cli/config.go`
2. **Add flag**: Register in flag parser
3. **Add validation**: Validate in config validation
4. **Update help**: Add to help text
5. **Add tests**: Test flag parsing and validation
6. **Update docs**: Document in README.md

## Performance Considerations

### Memory Management
- **Avoid large allocations** in hot paths
- **Reuse objects** where possible (pools)
- **Profile memory usage** with `go tool pprof`
- **Clean up resources** explicitly

### Concurrency
- **Thread-safe data structures** for shared state
- **Channel communication** over shared memory
- **Context cancellation** for graceful shutdown
- **Worker pools** for bounded concurrency

### Caching
- **Cache at appropriate levels** (file, AST, results)
- **Invalidate correctly** on changes
- **Consider memory vs. speed** tradeoffs
- **Profile cache hit rates**

## Debugging

### Debug Builds
```bash
# Build with debug info
go build -gcflags="all=-N -l" -o oxinfer-debug cmd/oxinfer

# Run with detailed logging
./oxinfer-debug --manifest test.json --log-level debug
```

### Profiling
```bash
# CPU profiling
go tool pprof oxinfer cpu.prof

# Memory profiling
go tool pprof oxinfer mem.prof

# Generate profiles during test runs
go test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=.
```

### Tree-sitter Debugging
```bash
# Parse file and dump AST
tree-sitter parse file.php

# Test query against file
tree-sitter query queries.scm file.php
```

## Release Process

### Version Management
- **Semantic versioning**: MAJOR.MINOR.PATCH
- **Tag releases**: `git tag v0.2.0`
- **Update version**: In `cmd/oxinfer/main.go`

### Pre-release Checklist
- [ ] All tests pass
- [ ] Integration tests pass
- [ ] Performance benchmarks stable
- [ ] Documentation updated
- [ ] Breaking changes documented
- [ ] Migration guide (if needed)

## Getting Help

- **Issues**: GitHub issues for bugs and features
- **Discussions**: GitHub discussions for questions
- **Code review**: Pull request comments
- **Documentation**: Check ARCHITECTURE.md for internals

## Recognition

Contributors will be recognized in:
- **CONTRIBUTORS.md**: All contributors listed
- **Release notes**: Major contributors mentioned
- **Code comments**: Attribution for significant contributions

Thank you for contributing to Oxinfer!
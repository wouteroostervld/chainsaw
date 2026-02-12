# Project Skills and Guidelines

This file contains project-specific guidelines that Chainsaw will inject into context when providing code suggestions. It helps maintain consistency across the codebase.

## Coding Standards

### Go Style
- Follow standard Go formatting (gofmt)
- Use meaningful variable names (avoid single-letter except for short-lived loop vars)
- Error handling: Always wrap errors with context using `fmt.Errorf("context: %w", err)`
- Prefer table-driven tests
- Use dependency injection over global state

### Architecture Patterns
- Pure functions where possible (side-effect-free)
- Interfaces for IO operations (testability)
- Context propagation for cancellation and timeouts
- Explicit error handling (no panic in production code)

## Security Requirements

### Path Handling
- Always validate paths against allowed roots using `config.ValidatePathSecurity`
- Use `filepath.Abs` and `filepath.Clean` before comparisons
- Never trust user input for path construction

### SQL Safety
- ALWAYS use `?` placeholders for SQL queries
- Never use `fmt.Sprintf` to construct SQL
- Use prepared statements where possible

### Concurrency
- Protect shared state with mutexes
- Use `sync.RWMutex` for read-heavy scenarios
- Always defer mutex unlocks immediately after lock
- Consider deadlock scenarios in design

## Testing Guidelines

### Test Organization
```go
func TestFeatureName(t *testing.T) {
    tests := []struct {
        name    string
        input   InputType
        want    OutputType
        wantErr bool
    }{
        {name: "descriptive_case_name", ...},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Mock Usage
- Use interfaces for mockable dependencies
- Inject dependencies rather than creating them internally
- Keep mocks simple and focused

### Test Coverage
- Test happy path
- Test error cases
- Test edge cases (nil, empty, boundary conditions)
- Test concurrent access where applicable

## Code Organization

### Package Structure
```
pkg/
├── config/      # Configuration loading and merging
├── filter/      # Filtering logic (pure functions)
├── db/          # Database operations
├── indexer/     # File indexing pipeline
├── ollama/      # Ollama API client
├── search/      # Search query engine
└── watcher/     # Filesystem watching
```

### File Naming
- `types.go` - Data structures
- `<feature>.go` - Main implementation
- `<feature>_test.go` - Tests
- `interfaces.go` - Interface definitions
- `mock_<feature>.go` - Mock implementations

## Configuration Patterns

### Local Config Override
When working with config overrides:
- Local configs are ADDITIVE only
- Daemon uses: exclude + blacklist from local
- CLI uses: include (validated) + exclude + blacklist from local
- Never allow local override of whitelist or model settings

### Filter Precedence
- Directory: `is_in_include AND NOT is_in_exclude`
- File: `NOT matches_blacklist OR matches_whitelist`
- Whitelist is for EXCEPTIONS to blacklist

## Refactoring Guidelines

### When to Extract
- Function > 50 lines → consider breaking up
- Repeated logic (3+ times) → extract to function
- Complex conditional → extract to named function
- Side effects → isolate from pure logic

### Naming Conventions
- Functions: VerbNoun (e.g., `LoadConfig`, `ValidatePath`)
- Interfaces: Noun or Nouner (e.g., `FileSystem`, `ConfigLoader`)
- Tests: `Test<Function>_<Scenario>` or table-driven with descriptive names
- Private helpers: leading lowercase, descriptive

## Common Pitfalls to Avoid

### Don't
- ❌ Use global variables for state
- ❌ Panic in library code (return errors)
- ❌ Ignore errors (always handle or propagate)
- ❌ Create goroutines without cleanup mechanism
- ❌ Use `time.Sleep` in production code for synchronization

### Do
- ✅ Use context for cancellation
- ✅ Close resources in defer
- ✅ Validate all inputs
- ✅ Use channels with proper buffering
- ✅ Test error paths

## Performance Considerations

### Optimization Priorities
1. Correctness first
2. Readability second
3. Performance third (measure before optimizing)

### When to Optimize
- Profile first (don't guess)
- Cache expensive computations
- Batch operations where possible
- Use connection pooling for databases
- Consider memory allocations in hot paths

## Documentation Requirements

### Code Comments
- Package-level doc comment for each package
- Exported types and functions must have doc comments
- Complex logic should have inline comments explaining "why"
- Don't comment obvious code

### Example
```go
// LoadConfig loads the global configuration from the default location.
// It returns an error if the config file is missing, invalid, or if
// the active profile is not found.
func LoadConfig() (*Config, error) {
    // Implementation
}
```

## Review Checklist

Before submitting code:
- [ ] All tests passing
- [ ] No new linter warnings
- [ ] Error paths tested
- [ ] Security implications considered
- [ ] Documentation updated if needed
- [ ] Breaking changes noted
- [ ] Performance impact assessed for hot paths

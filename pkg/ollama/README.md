# Ollama API Client

This package provides a Go client for the Ollama API, enabling local AI capabilities for embeddings and LLM-based code analysis.

## Features

- **Embedding Generation**: Convert text to vector embeddings using models like `nomic-embed-text`
- **Batch Processing**: Generate multiple embeddings in parallel with connection pooling
- **LLM Generation**: Use language models for text completion and analysis
- **Knowledge Graph Extraction**: Extract semantic relations from code
- **Retry Logic**: Automatic retry with exponential backoff for transient failures
- **Context Support**: Full `context.Context` support for cancellation and timeouts
- **Mock Client**: Test-friendly mock implementation for unit testing

## Usage

### Basic Setup

```go
import "github.com/wouteroostervld/chainsaw/pkg/ollama"

// Create client with defaults (localhost:11434)
client := ollama.NewClient(nil)

// Or with custom configuration
client := ollama.NewClient(&ollama.Config{
    BaseURL:    "http://localhost:11434",
    Timeout:    60 * time.Second,
    MaxRetries: 3,
    RetryDelay: time.Second,
})

// Check server health
ctx := context.Background()
if err := client.Ping(ctx); err != nil {
    log.Fatalf("Ollama server not available: %v", err)
}
```

### Generate Embeddings

```go
// Single embedding
embedding, err := client.Embed(ctx, "nomic-embed-text", "function main() {}")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Embedding dimension: %d\n", len(embedding))

// Batch embeddings (parallel with semaphore limiting)
texts := []string{
    "func processData() {}",
    "type User struct {}",
    "const MaxSize = 1024",
}
embeddings, err := client.EmbedBatch(ctx, "nomic-embed-text", texts)
if err != nil {
    log.Fatal(err)
}
```

### Extract Knowledge Graph Edges

```go
code := `
func handleRequest(req *Request) {
    validator.Validate(req)
    db.Save(req.Data)
}
`

edges, err := client.ExtractEdges(ctx, "llama2", code)
if err != nil {
    log.Fatal(err)
}

for _, edge := range edges {
    fmt.Printf("%s -[%s]-> %s (weight: %.2f)\n",
        edge.Source, edge.RelationType, edge.Target, edge.Weight)
}
// Output:
// handleRequest -[calls]-> Validate (weight: 0.80)
// handleRequest -[calls]-> Save (weight: 0.80)
```

### LLM Text Generation

```go
response, err := client.Generate(
    ctx,
    "llama2",
    "Explain this code in one sentence: func add(a, b int) int { return a + b }",
    "You are a helpful code assistant",
)
if err != nil {
    log.Fatal(err)
}
fmt.Println(response)
```

## Testing with MockClient

```go
import "github.com/wouteroostervld/chainsaw/pkg/ollama"

func TestMyIndexer(t *testing.T) {
    mock := ollama.NewMockClient()
    
    // Customize behavior
    mock.EmbedFunc = func(ctx context.Context, model, text string) ([]float32, error) {
        return []float32{0.1, 0.2, 0.3}, nil
    }
    
    // Use in your code
    indexer := NewIndexer(mock)
    indexer.Process("some code")
    
    // Verify calls
    if len(mock.EmbedCalls) != 1 {
        t.Errorf("expected 1 embed call, got %d", len(mock.EmbedCalls))
    }
}

// Test error scenarios
func TestErrorHandling(t *testing.T) {
    mock := ollama.NewMockClient()
    mock.SetError(ollama.ErrMockServerDown)
    
    _, err := mock.Embed(ctx, "model", "text")
    if err == nil {
        t.Error("expected error, got nil")
    }
}
```

## API Reference

### Client Methods

| Method | Description |
|--------|-------------|
| `Embed(ctx, model, text)` | Generate single embedding |
| `EmbedBatch(ctx, model, texts)` | Generate multiple embeddings in parallel |
| `Generate(ctx, model, prompt, system)` | Generate text completion |
| `ExtractEdges(ctx, model, code)` | Extract knowledge graph edges from code |
| `Ping(ctx)` | Check server health |

### Types

```go
type Config struct {
    BaseURL    string        // Default: http://localhost:11434
    Timeout    time.Duration // Default: 60s
    MaxRetries int           // Default: 3
    RetryDelay time.Duration // Default: 1s
}

type Edge struct {
    Source       string  // Source symbol name
    Target       string  // Target symbol name
    RelationType string  // calls, imports, inherits, references, defines
    Weight       float64 // 0.0-1.0 relation strength
}
```

## Integration Points

This client integrates with:
- **pkg/db**: Store embeddings in `vec_chunks` and edges in `graph_edges`
- **Indexer**: Generate embeddings for code chunks during indexing
- **Search Engine**: Use embeddings for similarity search

## Error Handling

The client automatically retries transient failures with exponential backoff:

```go
// Will retry up to MaxRetries times with increasing delays
embedding, err := client.Embed(ctx, model, text)
if err != nil {
    // Permanent failure after all retries exhausted
    log.Printf("Embedding failed: %v", err)
}
```

Use context for timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

embedding, err := client.Embed(ctx, model, text)
// Returns ctx.Err() if timeout exceeded
```

## Performance Considerations

- **Batch Processing**: `EmbedBatch` uses a semaphore to limit concurrent requests (default: 5)
- **Connection Reuse**: HTTP client reuses connections for efficiency
- **Timeout Management**: Configure appropriate timeouts based on model size
- **Retry Strategy**: Exponential backoff prevents overwhelming the server

## Requirements

- Ollama server running locally (default port: 11434)
- Required models pulled: `nomic-embed-text`, `llama2` (or configured alternatives)

## Examples

See `examples/ollama-usage.go` for comprehensive examples.

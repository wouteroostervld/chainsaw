# Indexer Package

The indexer package coordinates the file-to-embedding-to-graph pipeline for Chainsaw.

## Features

- **Content Chunking**: Split files into overlapping chunks for better context
- **Batch Processing**: Process multiple chunks in parallel with configurable concurrency
- **Embedding Generation**: Generate vector embeddings via Ollama
- **Knowledge Graph Extraction**: Extract semantic relations from code (optional)
- **Incremental Updates**: Skip unchanged files based on content hash
- **Filtering Support**: Process only files matching filter criteria

## Usage

```go
import (
    "context"
    "github.com/wouteroostervld/chainsaw/pkg/indexer"
    "github.com/wouteroostervld/chainsaw/pkg/db"
    "github.com/wouteroostervld/chainsaw/pkg/ollama"
)

// Setup
database, _ := db.Open(&db.Config{Path: "chainsaw.db"})
ollamaClient := ollama.NewClient(nil)

cfg := indexer.DefaultConfig()
cfg.ChunkSize = 512
cfg.ChunkOverlap = 64
cfg.EnableGraphMode = true

idx := indexer.New(cfg, database, ollamaClient)

// Index a single file
ctx := context.Background()
if err := idx.IndexFile(ctx, "main.go"); err != nil {
    log.Fatal(err)
}

// Index a directory with filter
result, err := idx.IndexDirectory(ctx, "/project/src", func(path string) bool {
    return filepath.Ext(path) == ".go"
})
fmt.Printf("Indexed %d files in %v\n", result.FilesProcessed, result.Duration)
```

## Configuration

```go
type Config struct {
    ChunkSize       int    // Bytes per chunk (default: 512)
    ChunkOverlap    int    // Overlapping bytes (default: 64)
    EmbedModel      string // Embedding model (default: "nomic-embed-text")
    GraphModel      string // Graph extraction model (default: "llama2")
    BatchSize       int    // Chunks per batch (default: 10)
    MaxConcurrency  int    // Max parallel ops (default: 5)
    EnableGraphMode bool   // Extract edges (default: true)
    MinChunkSize    int    // Skip tiny chunks (default: 10)
    MaxChunkSize    int    // Truncate large chunks (default: 4096)
}
```

## Chunking Strategy

Files are split using a sliding window approach:
- Window size: `ChunkSize` bytes
- Stride: `ChunkSize - ChunkOverlap` bytes  
- Overlap ensures context preservation across boundaries

Example with ChunkSize=20, Overlap=5:
```
Content: "0123456789ABCDEFGHIJKLMNOP"
Chunk 1: "0123456789ABCDEFGHIJ" (0-20)
Chunk 2: "DEFGHIJKLMNOPQRSTUV" (15-35)  <- 5 byte overlap with chunk 1
```

## Integration

This package integrates with:
- **pkg/db**: Stores chunks and edges
- **pkg/ollama**: Generates embeddings and extracts graph
- **pkg/filter**: Applied by daemon for file filtering
- **cmd/chainsawd**: Used in watch loop for incremental indexing

## Testing

Use the provided mock database and Ollama mock client:

```go
func TestMyFeature(t *testing.T) {
    mockDB := indexer.NewMockDatabase()
    mockOllama := ollama.NewMockClient()
    
    idx := indexer.New(indexer.DefaultConfig(), mockDB, mockOllama)
    // ... test logic
}
```

## Performance

Typical performance on modern hardware:
- **Small files** (<10KB): ~50-100 files/sec
- **Medium files** (10-100KB): ~10-20 files/sec  
- **Large files** (>100KB): ~1-5 files/sec

Bottleneck is usually Ollama embedding generation. Use batching to optimize.

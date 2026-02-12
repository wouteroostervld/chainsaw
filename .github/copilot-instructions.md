# Chainsaw - GraphRAG CLI & Daemon

## Project Overview

Chainsaw is a high-performance, local-first GraphRAG system written in Go. It consists of two binaries from a single codebase:

- **`chainsawd`**: Background daemon that watches filesystem events, indexes files, and generates vector embeddings + knowledge graphs via Ollama
- **`chainsaw`**: Read-only CLI for searching, querying, and injecting context into files

**Core Philosophy**: Unix-compliant, security-first, zero-latency search, model-agnostic

## Build & Development Commands

```bash
# Initialize module (first time setup)
go mod init github.com/wouteroostervld/chainsaw
go mod tidy

# Build both binaries
go build -o chainsawd ./cmd/chainsawd
go build -o chainsaw ./cmd/chainsaw

# Install binaries to $GOPATH/bin
go install ./cmd/...

# Run tests
go test ./...

# Run specific test
go test ./pkg/indexer -run TestChunking

# Run with race detector
go test -race ./...

# Lint (requires golangci-lint)
golangci-lint run
```

## Architecture Overview

### Two-Pass Indexing Pipeline

The daemon processes files in two distinct passes:

1. **Pass 1 (Embedding)**: Chunk text → Send to embedding model → Store in `vec_chunks` table
2. **Pass 2 (Graph Extraction)**: Send chunks to generation model → Parse relations → Store in `graph_edges`

This separation allows different models for different tasks (e.g., `nomic-embed-text` for vectors, `llama3` for graph extraction).

### Database Architecture

- **Engine**: SQLite with `sqlite-vec` extension
- **Mode**: WAL (Write-Ahead Logging) for concurrent daemon writes + CLI reads
- **Location**: `~/.chainsaw/chainsaw.db`
- **Security**: File permissions must be `0600` (user R/W only)

#### Schema Highlights

- `files`: Tracks indexed files with `content_hash` (SHA256) to avoid redundant re-indexing
- `vec_chunks`: Virtual table using `sqlite-vec` for vector similarity search
- `graph_edges`: Stores relations between chunks with `created_by_model` tracking
- `meta`: Configuration and metadata key-value store

### Configuration System

#### Global Configuration

- **Location**: `~/.chainsaw/config.yaml`
- **Profiles**: Multiple profiles (e.g., `coding_heavy`, `notes_fast`) with different models/settings
- **Active Profile**: Loaded by `active_profile` key

**Profile Settings:**
- `include`: Directory paths to recursively index (e.g., `["~/go/src/myproject"]`)
- `exclude`: Directory patterns to skip (e.g., `["node_modules", ".git"]`)
- `blacklist`: Regex patterns to reject files (applied first)
- `whitelist`: Regex exceptions to blacklist (allows blacklisted files through)
- Model settings: `embedding_model`, `graph_driver`, `chunk_size`, etc.

#### Local Configuration Override

Projects can provide `.config.yaml` files in their directories to customize indexing behavior:

- **Location**: `.config.yaml` in project root or subdirectories
- **Discovery**: Walk up from file location (daemon) or CWD (CLI) to find closest `.config.yaml`
- **Merge Strategy**: Additive only - local configs make filtering MORE restrictive

**Daemon (indexing) can override:**
- `exclude`: Add additional directories to skip (e.g., `["tmp/", "*.cache"]`)
- `blacklist`: Add additional file patterns to reject (e.g., `[".*\\.tmp$"]`)

**CLI (search) can override:**
- `include`: Add search paths (must be within global `include` scope)
- `exclude`: Add directories to skip
- `blacklist`: Add file patterns to reject

**Cannot override:** `whitelist` (global only), model settings, or remove global restrictions

**Example local config:**
```yaml
# .config.yaml
include: ["./vendor", "../shared-lib"]  # CLI only
exclude: ["tmp/", "build/"]
blacklist: [".*\\.log$"]
```

#### Graph Driver Configuration

Supports **Strategy Pattern** for parsing model outputs:

- `output_format: "json"`: Unmarshal JSON response into `[]Edge` struct
- `output_format: "regex"`: Apply `parsing_regex` with named capture groups (source, target, relation)
- `custom_system_prompt`: Override default prompt per model

#### Filter Evaluation Order

Two orthogonal filtering systems work together:

1. **Directory filtering** (include/exclude):
   - Check `include` first (is path in scope?)
   - Then apply `exclude` (skip excluded paths)
   - Result: `in_include AND NOT in_exclude`

2. **File filtering** (blacklist/whitelist):
   - Apply `blacklist` first (reject patterns)
   - `whitelist` provides exceptions (allows through even if blacklisted)
   - Result: `NOT matches_blacklist OR matches_whitelist`

## Key Conventions

### Security Constraints

1. **Path Traversal Protection**: All file paths must be validated using `filepath.Abs` + `strings.HasPrefix` against allowed roots
2. **SQL Injection Prevention**: ALWAYS use `?` placeholders. Never `fmt.Sprintf` for SQL queries
3. **Input Sanitization**: All CLI args are untrusted input
4. **Ollama Isolation**: Daemon communicates only over `localhost`
5. **Local Config Security**:
   - Local configs are additive only (can only make filtering MORE restrictive)
   - CLI `include` additions must be within global `include` scope
   - Global `whitelist` is immutable (controls blacklist exceptions)
   - Validate all paths in local configs to prevent traversal attacks

### Dependency Injection

- **No global state**: Pass dependencies explicitly
- **Use `context.Context`**: For cancellation and timeouts
- **Error wrapping**: Include stack context in errors

### Logging Strategy

- **Daemon (`chainsawd`)**: Use `zerolog` for structured JSON logging
- **CLI (`chainsaw`)**: Use `pterm` for human-readable output and progress bars

### Filesystem Watching

- **Library**: `fsnotify/fsnotify`
- **Events**: Listen for `IN_CLOSE_WRITE` (Linux) or `FSEvents` (macOS)
- **Debouncing**: Implement 500ms sliding window to group rapid file saves

### CLI Input Modes

The CLI must auto-detect context:

1. **Pipe Mode**: `echo "src/main.go" | chainsaw search "auth"`
2. **Xargs Mode**: `find . -name "*.go" | xargs chainsaw search "auth"`
3. **Interactive Mode**: `chainsaw search "auth"` (scans CWD based on whitelist)

### Context Injection Format

When using `chainsaw search --inject`, prepend this non-destructive block:

```go
/* CHAINSAW_CONTEXT_START
   ID: <uuid>
   Query: "auth logic"
   Related: [session.go (0.89), user.go (0.82)]
   Derived Relations: [AuthService -> calls -> UserRepo]
   Skill: [Refactoring Guidelines]
   CHAINSAW_CONTEXT_END */
```

Use `chainsaw clean` to atomically strip these blocks.

### Chunking Strategy

- Split by **token count** (approx 4 chars/token), not bytes
- Configurable `chunk_size` and `overlap` per profile
- Must preserve code structure where possible

### Ollama Integration

- **Embedding Model**: e.g., `nomic-embed-text`, `all-minilm`
- **Graph Model**: e.g., `llama3`, `phi3:mini`
- **Health Check**: Daemon must validate Ollama availability on startup
- **Batching**: Batch embedding requests where model supports it
- **Keep-Alive**: Implement heartbeat for long-running operations

## Project Structure

```
chainsaw/
├── cmd/
│   ├── chainsawd/     # Daemon entry point
│   └── chainsaw/      # CLI entry point
├── pkg/
│   ├── config/        # Config loader with local override support
│   │   ├── loader.go  # LoadGlobalConfig, FindLocalConfig, MergeConfigFor{Daemon,CLI}
│   │   ├── types.go   # GlobalConfig, LocalConfig, MergedConfig, ProfileConfig
│   │   └── paths.go   # Path resolution, validation, security checks
│   ├── filter/        # Filtering logic for include/exclude/whitelist/blacklist
│   ├── db/            # SQLite schema + WAL setup
│   ├── watcher/       # fsnotify wrapper with debouncing
│   ├── indexer/       # Hashing, chunking, embedding pipeline
│   ├── graph/         # Graph extraction engine + parsers
│   ├── ollama/        # OllamaClient with health checks
│   ├── search/        # Query engine with Recursive CTE
│   └── inject/        # Context injection + clean logic
└── DESIGN.md          # Master specification (authoritative)
```

## Skills System

- **Location**: `~/.chainsaw/skills/` or project root
- **File**: `SKILLS.md`
- **Purpose**: Project-specific guidelines injected with context (e.g., refactoring rules, coding standards)

## Daemon Lifecycle

```bash
# Install as systemd user service
chainsaw daemon install

# Graceful shutdown
# Must handle SIGTERM: close DB WAL, flush queues
```

## Testing Requirements

- Use table-driven tests for chunking strategies
- Mock Ollama API responses for graph extraction tests
- Test path traversal protection with malicious inputs
- Verify WAL mode allows concurrent reads during writes

## Performance Considerations

- **Graph Concurrency**: Controlled by `graph_driver.concurrency` setting
- **Embedding Batch Size**: Maximize throughput where model supports batching
- **Database**: Use prepared statements and connection pooling
- **File Hashing**: Skip re-indexing when `content_hash` matches
- **Config Caching**: Cache merged configs keyed by `.config.yaml` path to avoid re-parsing
- **Config Watching**: Use fsnotify to watch `.config.yaml` files and invalidate cache on modification
- **Auto Re-indexing**: When `.config.yaml` changes, automatically re-index affected files

## Dependencies

- `github.com/spf13/cobra` - CLI framework
- `github.com/mattn/go-sqlite3` - SQLite driver with `sqlite-vec` extension
- `github.com/fsnotify/fsnotify` - Filesystem event watching
- `github.com/rs/zerolog` - Structured logging (daemon)
- `github.com/pterm/pterm` - Terminal UI (CLI)
- `github.com/schollz/progressbar/v3` - Progress bars

# Chainsaw User Manual

Comprehensive guide to using Chainsaw for semantic code search and knowledge graph queries.

## Table of Contents

1. [Installation](#installation)
2. [Getting Started](#getting-started)
3. [Commands Reference](#commands-reference)
4. [Semantic Search](#semantic-search)
5. [Graph Queries](#graph-queries)
6. [Configuration](#configuration)
7. [Advanced Usage](#advanced-usage)
8. [Troubleshooting](#troubleshooting)

## Installation

### Prerequisites

- **Go 1.24.7 or later** - For building from source
- **Ollama** - Running locally for embeddings ([Install Ollama](https://ollama.ai/))
- **Linux/macOS** - Primary supported platforms

### Quick Install

```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make install
```

This builds and installs `chainsaw` to `~/.local/bin/chainsaw`. Ensure `~/.local/bin` is in your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Verify Installation

```bash
chainsaw version
```

## Getting Started

### 1. Initialize Database

Create the database and schema:

```bash
chainsaw init
```

This creates `~/.chainsaw/chainsaw.db` with the necessary tables for vector search and knowledge graphs.

### 2. Pull Embedding Model

Chainsaw uses Ollama for generating embeddings. Pull the default model:

```bash
ollama pull nomic-embed-text
```

### 3. Start the Daemon

The daemon watches for file changes and keeps your index up-to-date:

```bash
chainsaw daemon start
```

Or install as a systemd service (Linux):

```bash
make daemon-install
systemctl --user status chainsawd
```

### 4. Index Your Code

```bash
# Index current directory
cd /path/to/your/project
chainsaw index .

# Or index specific directory
chainsaw index ~/Projects/myapp
```

The daemon processes files in the background, extracting:
- Code chunks with semantic embeddings (vector search)
- Entity relations (function calls, imports, etc.)
- Knowledge graph structure

### 5. Check Indexing Progress

```bash
chainsaw status
```

Example output:
```yaml
database: /home/user/.chainsaw/chainsaw.db
schema_version: 2.4.0
files_indexed: 211
total_chunks: 954
total_entities: 428
total_edges: 156
```

### 6. Search Your Code

```bash
# Semantic search
chainsaw search "error handling patterns"

# Graph query
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name LIMIT 10"
```

## Commands Reference

### `chainsaw init`

Initialize the database schema.

```bash
chainsaw init
```

Creates `~/.chainsaw/chainsaw.db` with tables for files, chunks, embeddings, entities, and graph edges.

### `chainsaw index <path>`

Queue a directory for indexing.

```bash
chainsaw index .                    # Index current directory
chainsaw index ~/Projects/myapp     # Index specific directory
```

The daemon must be running to process the index queue.

### `chainsaw search <query>`

Perform semantic search using vector embeddings.

```bash
chainsaw search "database connection pooling"
chainsaw search "authentication middleware" --limit 5
chainsaw search "error handling" --format yaml
```

**Options:**
- `--limit N` - Limit results to N items (default: 10)
- `--format yaml|json` - Output format (default: yaml)

**Context-aware**: Automatically filters results to current directory and subdirectories.

### `chainsaw graph query <cypher>`

Query the knowledge graph using Cypher-like syntax.

```bash
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"
```

See [Graph Queries](#graph-queries) for detailed syntax.

### `chainsaw daemon start|stop|status`

Manage the background indexing daemon.

```bash
chainsaw daemon start      # Start daemon
chainsaw daemon stop       # Stop daemon
chainsaw daemon status     # Check daemon status
```

The daemon:
- Watches configured directories for changes
- Indexes new/modified files
- Builds the graph from the syntax tree
- Keeps embeddings up-to-date

### `chainsaw status`

Show database statistics and daemon status.

```bash
chainsaw status
```

### `chainsaw version`

Show version information.

```bash
chainsaw version
```

## Semantic Search

Semantic search finds code by **meaning**, not just keywords. It uses vector embeddings to understand the semantic similarity between your query and code chunks.

### Basic Search

```bash
chainsaw search "error handling patterns"
```

### Semantic Chunks and Folded Results

Files in a known syntax are not cut into fixed-size blocks but along their
syntax tree. Every definition is a chunk of its own, and where it sits in the
enclosing chunk only its signature remains, with a reference to the lines of
the chunk that holds the body:

```go
package indexer

func New(cfg *Config, database db.Database) *Indexer // ... chunk: lines 54-65

func (idx *Indexer) IndexFile(ctx context.Context, filePath string) error // ... chunk: lines 80-161
```

This repeats at every level: a file lists its classes, a class its methods, a
method its nested functions. A chunk that is still too large to embed gets its
biggest loops, branches and literals folded the same way; only what cannot be
folded any further is split into blocks.

How much of this a search result shows is set with `--expand`:

```bash
chainsaw search --expand body "error handling"   # Just the definition that matched
chainsaw search --expand file "error handling"   # The folded file, with the matches in full
chainsaw search --expand calls "error handling"  # Also what the matches call (default)
chainsaw search --limit 3 "error handling"       # Match fewer chunks (default 10)
```

- `body` gives the whole definition, even when it had to be embedded in
  several blocks. Nested definitions stay folded.
- `file` shows where the match sits: all matches in one file are one result.
- `calls` also shows in full the definitions the matches call. Those in the
  same file are expanded in place; those in other files follow as related
  chunks. Only `calls` edges are followed, not `may_call`.

Supported: Go, Python, JavaScript, TypeScript/TSX, Rust, Java, C, C++ and
Ruby. Markdown is folded along its sections; HTML and XML only where a chunk
would be too large. Other files are split into overlapping blocks as before.

### Advanced Search Examples

```bash
# Find HTTP server implementations
chainsaw search "HTTP server with middleware"

# Find database migrations
chainsaw search "database schema migrations"

# Find authentication code
chainsaw search "user authentication and session management"

# Find API endpoints
chainsaw search "REST API endpoint handlers"

# Find test utilities
chainsaw search "test helper functions and fixtures"
```

### Search Results

Results are returned in YAML format with snippets and file locations:

```yaml
query: "error handling patterns"
results:
  - index: 0
    file: /home/user/project/pkg/errors/handler.go
    lines: "42-58"
    similarity: 0.8734
    snippet: |
      func HandleError(err error) {
          if err == nil {
              return
          }
          log.Error("error occurred", "err", err)
          // ...
      }
  - index: 1
    file: /home/user/project/pkg/api/middleware.go
    lines: "23-35"
    similarity: 0.8421
    snippet: |
      func ErrorMiddleware(next http.Handler) http.Handler {
          return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
              // ...
          })
      }
total: 2
```

### Context-Aware Filtering

Search results are automatically filtered to your current working directory:

```bash
cd ~/Projects/myapp/api
chainsaw search "error handling"
# Only returns results from ~/Projects/myapp/api and subdirectories
```

## Graph Queries

Query code relationships using a Cypher-like graph query language.

### Basic Syntax

```cypher
MATCH (source:TYPE)-[relation]->(target)
RETURN source.property, target.property
```

### Where the Graph Comes From

For the languages listed under [Semantic Chunks](#semantic-chunks-and-folded-results)
the graph is read from the syntax tree: every definition is an entity, and
every call and import in the source is an edge. No LLM is involved, and
files in other languages are searchable but have no entities or edges.

**Go** files are also run through the Go type checker, so every call has
its exact target: the function or method it refers to, in the file that
declares it, whatever variable, field or package it is reached through. A
call of an interface method is a `calls` edge to that method of the
interface, plus a `may_call` edge to the method of each type that implements
the interface, and each such type has an `implements` edge to the interface.
Calls of function values (callbacks, closures in variables) have no target
and are left out. The type checker looks at the types in the package and
what it imports, so an implementation that is only wired in elsewhere is
found through `implements`, not through `may_call`. Dependencies are located
with the `go` command; if it is not available, calls into them fall back to
the rules below.

For the **other languages** the syntax tree gives names, not types. An edge
is `calls` when the target follows from the source itself: a function in the
same directory, a name reached through an import, a method on `self`/`this`
or on a named type. When only the name is known, as with a method call on a
variable, the edge is `may_call` and goes to every definition in the project
with that name.

### Entity Types

Supported entity types:
- `FUNCTION` - Functions
- `METHOD` - Methods; named `Type.method`
- `CLASS` - Classes and modules
- `STRUCT` - Struct definitions
- `INTERFACE` - Interfaces and traits
- `TYPE` - Other type definitions
- `PACKAGE` - A source file's package or module, and imported packages
- `EXTERNAL` - Something that is called but not defined in the index, such as `fmt.Println`

### Relation Types

Supported relationships:
- `calls` - Function/method calls with a known target
- `may_call` - Calls that may end up there: dispatched through an interface (Go), or matched by name only
- `implements` - A type implements an interface (Go)
- `imports` - Package imports
- `defines` - A package defines its functions and types, a class its methods

### Query Examples

#### Find Function Calls

```bash
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"
```

#### Find Interface Implementations

```bash
chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s:STRUCT) RETURN i.name, s.name"
```

#### Get Code Snippets

```bash
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name, t.snippet, t.file"
```

#### Multi-Hop Queries

Find indirect relationships using `*min..max` syntax:

```bash
# Find functions called 2 hops away
chainsaw graph query "MATCH (f:FUNCTION)-[:calls*2]->(t) RETURN f.name, t.name"

# Find call chains up to 3 levels deep
chainsaw graph query "MATCH (a:FUNCTION)-[:calls*1..3]->(b) RETURN a.name, b.name"

# Find all transitive imports
chainsaw graph query "MATCH (p:PACKAGE)-[:imports*]->(t) RETURN p.name, t.name"
```

#### Aggregation Queries

Count, group, and sort results:

```bash
# Most-called functions
chainsaw graph query "
  MATCH (a)-[:calls]->(b)
  RETURN b.name, COUNT(a) AS callers
  GROUP BY b.name
  ORDER BY callers DESC
  LIMIT 10
"

# Fan-out (functions that call many others)
chainsaw graph query "
  MATCH (a:FUNCTION)-[:calls]->(b)
  RETURN a.name, COUNT(b) AS targets
  GROUP BY a.name
  ORDER BY targets DESC
  LIMIT 10
"

# Entity type distribution
chainsaw graph query "
  MATCH (n)
  RETURN n.entity_type, COUNT(n) AS count
  GROUP BY n.entity_type
  ORDER BY count DESC
"
```

### Return Properties

Available properties for return values:
- `name` - Entity name
- `entity_type` - Entity type (FUNCTION, STRUCT, etc.)
- `snippet` - Full code snippet
- `file` - Absolute file path
- `lines` - Line range (e.g., "42-58")

### Query Result Format

Results are returned in YAML:

```yaml
query: "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"
results:
  - index: 0
    f_name: main
    t_name: initialize
  - index: 1
    f_name: handleSearch
    t_name: SearchSimilar
total: 2
```

## Configuration

Configuration file: `~/.chainsaw/config.yaml`

### Basic Configuration

```yaml
version: "2.0"
active_profile: "default"

profiles:
  default:
    # Directories to watch and index
    include:
      - ~/Projects/myapp
      - ~/Projects/mylib

    # Directories to skip
    exclude:
      - node_modules
      - .git
      - vendor
      - target
      - build

    # File patterns to skip
    blacklist:
      - "**/*.min.js"
      - "**/*.test.js"
      - "**/test_*.go"

    # File patterns to include (whitelist)
    whitelist:
      - "**/*.go"
      - "**/*.js"
      - "**/*.ts"
      - "**/*.py"
      - "**/*.java"

    # Embedding settings
    embedding_model: "nomic-embed-text"
    chunk_size: 512
    overlap: 64
```

Older versions extracted the graph with an LLM. The settings for that
(`graph_driver`, `llm_provider`, `llm_base_url`, `llm_api_key`) are ignored
and can be removed.

### Advanced Configuration

#### Multiple Profiles

```yaml
version: "2.0"
active_profile: "work"

profiles:
  work:
    include:
      - ~/work/projects
    embedding_model: "nomic-embed-text"

  personal:
    include:
      - ~/personal/projects
    embedding_model: "nomic-embed-text"
```

Switch profiles by editing `active_profile` and restarting the daemon.

### Local Configuration Override

Create a `.chainsaw.yaml` file in any project directory to override settings:

```yaml
# Project-specific overrides
exclude:
  - generated
  - .next
  - dist

blacklist:
  - "**/*.generated.go"

chunk_size: 256  # Smaller chunks for this project
```

Local configs can only make filtering MORE restrictive (additive security).

## Advanced Usage

### Indexing Large Codebases

For large projects (1000+ files), consider:

1. **Monitor progress**:
   ```bash
   watch -n 5 chainsaw status
   ```

2. **Check daemon logs**:
   ```bash
   journalctl --user -u chainsawd -f
   ```

### Output Formats

Both `search` and `graph query` support YAML and JSON:

```bash
chainsaw search "query" --format json
chainsaw graph query "MATCH ..." --format json
```

YAML is default for human readability; JSON for programmatic consumption.

## Troubleshooting

### Daemon Not Starting

```bash
# Check status
chainsaw daemon status

# View logs
journalctl --user -u chainsawd -f

# Restart daemon
systemctl --user restart chainsawd
```

### No Search Results

1. **Check indexing status**:
   ```bash
   chainsaw status
   ```

2. **Verify Ollama is running**:
   ```bash
   ollama list
   ollama pull nomic-embed-text
   ```

3. **Check current directory**:
   Search is scoped to CWD. Try from project root.

### Database Errors

If you encounter database corruption:

```bash
# Backup current database
cp ~/.chainsaw/chainsaw.db ~/.chainsaw/chainsaw.db.backup

# Reinitialize
chainsaw init

# Reindex
cd /path/to/project
chainsaw index .
```

### Slow Indexing

Indexing time is spent on embeddings. Check that Ollama responds quickly, and
exclude directories that are not worth searching (caches, build output,
coverage reports).

### Permission Denied

Ensure database directory is writable:

```bash
ls -la ~/.chainsaw/
chmod 700 ~/.chainsaw/
chmod 600 ~/.chainsaw/chainsaw.db
```

## Getting Help

- **GitHub Issues**: [Report bugs or request features](https://github.com/wouteroostervld/chainsaw/issues)
- **Check logs**: `journalctl --user -u chainsawd -f`
- **Verbose mode**: Set `LOG_LEVEL=debug` in environment

## Next Steps

- Explore [Graph Queries](#graph-queries) for advanced code analysis
- Configure [Multiple Profiles](#multiple-profiles) for different projects

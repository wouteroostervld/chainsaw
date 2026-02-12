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
- Extracts graph relations
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

### Entity Types

Supported entity types:
- `FUNCTION` - Functions and top-level functions
- `METHOD` - Methods on structs/classes
- `TYPE` - Type definitions
- `INTERFACE` - Interface definitions
- `STRUCT` - Struct definitions
- `PACKAGE` - Package/module imports
- `VARIABLE` - Variables
- `CONSTANT` - Constants

### Relation Types

Supported relationships:
- `calls` - Function/method calls
- `uses` - Uses/references
- `imports` - Package imports
- `implements` - Interface implementations
- `extends` - Type extensions/embedding
- `defines` - Definitions
- `has_field` - Struct field relationships

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

    # Graph extraction settings
    graph_driver:
      model: "qwen2.5:3b"
      temperature: 0.1
      concurrency: 5
      batch_size: 100
```

### Advanced Configuration

#### Using OpenRouter for Graph Extraction

You can use cloud LLM providers like OpenRouter while keeping embeddings local:

```yaml
profiles:
  default:
    embedding_model: "nomic-embed-text"  # Local Ollama

    # Cloud LLM for graph extraction
    llm_provider: "openai"
    llm_base_url: "https://openrouter.ai/api/v1"
    llm_api_key: "${OPENROUTER_API_KEY}"

    graph_driver:
      model: "anthropic/claude-3.5-haiku"
      batch_size: 100
```

Set your API key:
```bash
export OPENROUTER_API_KEY="sk-or-v1-..."
```

#### Multiple Profiles

```yaml
version: "2.0"
active_profile: "work"

profiles:
  work:
    include:
      - ~/work/projects
    embedding_model: "nomic-embed-text"
    graph_driver:
      model: "qwen2.5:7b"

  personal:
    include:
      - ~/personal/projects
    embedding_model: "nomic-embed-text"
    graph_driver:
      model: "anthropic/claude-3.5-haiku"
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

1. **Adjust batch size** for your LLM:
   ```yaml
   graph_driver:
     batch_size: 150  # Increase for models with large context windows
   ```

2. **Monitor progress**:
   ```bash
   watch -n 5 chainsaw status
   ```

3. **Check daemon logs**:
   ```bash
   journalctl --user -u chainsawd -f
   ```

### Graph Extraction Performance

The graph worker processes chunks in batches for efficiency:
- Default batch size: 100 chunks per LLM call
- Typical processing: ~30 seconds for 1000 chunks
- Configurable via `graph_driver.batch_size`

### Custom LLM Models

Use any Ollama-compatible model:

```yaml
graph_driver:
  model: "deepseek-r1:7b"  # Or any model you've pulled
  temperature: 0.0         # Lower for more deterministic extraction
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

Graph extraction can be slow on first run. Optimizations:

1. **Use faster model**:
   ```yaml
   graph_driver:
     model: "qwen2.5:3b"  # Smaller, faster
   ```

2. **Increase batch size**:
   ```yaml
   graph_driver:
     batch_size: 150  # More chunks per call
   ```

3. **Check LLM performance**:
   ```bash
   ollama run qwen2.5:3b "test"  # Should respond quickly
   ```

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
- Set up [OpenRouter](#using-openrouter-for-graph-extraction) for cloud LLM integration

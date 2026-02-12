# Chainsaw - Semantic Code Search and Knowledge Graph

## Description

Chainsaw is a GraphRAG-powered code search tool that indexes codebases into a searchable knowledge graph with vector embeddings. It enables semantic search (find code by meaning) and graph queries (explore relationships between functions, types, and packages).

**Key capabilities:**
- Semantic search using natural language queries
- Graph queries with Cypher-like syntax
- Multi-hop relationship traversal
- Aggregation queries (COUNT, GROUP BY, ORDER BY)
- Context-aware filtering (scoped to current directory)
- Background indexing daemon

## When to Use

Use Chainsaw when you need to:

- **Find code by meaning, not keywords** - Search for "error handling patterns" instead of grepping for specific function names
- **Explore code relationships** - Find what calls what, what implements what, package dependencies
- **Analyze codebase structure** - Count most-called functions, find fan-out, visualize dependencies
- **Navigate unfamiliar codebases** - Discover relevant code without knowing exact names or locations
- **Answer architectural questions** - "What implements this interface?", "What are the call chains?"

**Do NOT use** when:
- You know the exact file/function name (use grep/find instead)
- You need to modify code (Chainsaw is read-only)
- Working with non-code files (images, binaries, etc.)

## Prerequisites

```bash
# Install Ollama (for embeddings)
curl https://ollama.ai/install.sh | sh

# Pull embedding model
ollama pull nomic-embed-text

# Initialize database
chainsaw init

# Start daemon
chainsaw daemon start
```

## Installation

```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make install
```

## Basic Usage

### 1. Index Your Codebase

```bash
# Index current directory
cd /path/to/your/project
chainsaw index .

# Check status
chainsaw status
```

The daemon processes files in the background, extracting:
- Code chunks with semantic embeddings
- Entity relations (function calls, imports, type definitions)
- Knowledge graph structure

### 2. Semantic Search

Find code by **meaning**, not just keywords:

```bash
# Find error handling code
chainsaw search "error handling patterns"

# Find database queries
chainsaw search "SQL database connection pooling"

# Find authentication logic
chainsaw search "user authentication and session management"

# Find specific patterns
chainsaw search "HTTP middleware with error handling"
```

**Context-aware:** Results are automatically filtered to the current directory and subdirectories.

### 3. Graph Queries

Query code relationships using Cypher-like syntax:

```bash
# Find function calls
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"

# Find interface implementations
chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s) RETURN i.name, s.name"

# Multi-hop: Find call chains (up to 3 levels)
chainsaw graph query "MATCH (a:FUNCTION)-[:calls*1..3]->(b) RETURN a.name, b.name"

# Aggregation: Most-called functions
chainsaw graph query "
  MATCH (a)-[:calls]->(b)
  RETURN b.name, COUNT(a) AS callers
  GROUP BY b.name
  ORDER BY callers DESC
  LIMIT 10
"

# Get code snippets with file paths
chainsaw graph query "
  MATCH (f:FUNCTION)-[:calls]->(t)
  RETURN f.name, t.name, t.snippet, t.file, t.lines
"
```

## Advanced Usage

### Entity Types

- `FUNCTION` - Functions and top-level functions
- `METHOD` - Methods on structs/classes
- `TYPE` - Type definitions
- `INTERFACE` - Interface definitions
- `STRUCT` - Struct/class definitions
- `PACKAGE` - Package/module imports
- `VARIABLE` - Variables
- `CONSTANT` - Constants

### Relation Types

- `calls` - Function/method invocations
- `uses` - References and usage
- `imports` - Package imports
- `implements` - Interface implementations
- `extends` - Type extensions/embedding
- `defines` - Definitions
- `has_field` - Struct field relationships

### Multi-Hop Queries

Use `*min..max` syntax for recursive traversal:

```bash
# Exactly 2 hops
chainsaw graph query "MATCH (a)-[:calls*2]->(b) RETURN a.name, b.name"

# 1 to 3 hops
chainsaw graph query "MATCH (a)-[:calls*1..3]->(b) RETURN a.name, b.name"

# Unlimited hops (use carefully!)
chainsaw graph query "MATCH (a)-[:calls*]->(b) RETURN a.name, b.name"
```

### Aggregation Queries

```bash
# Count by entity type
chainsaw graph query "
  MATCH (n)
  RETURN n.entity_type, COUNT(n) AS count
  GROUP BY n.entity_type
  ORDER BY count DESC
"

# Find fan-out (functions that call many others)
chainsaw graph query "
  MATCH (a:FUNCTION)-[:calls]->(b)
  RETURN a.name, COUNT(b) AS targets
  GROUP BY a.name
  ORDER BY targets DESC
  LIMIT 10
"
```

### Output Formats

Both search and graph queries support YAML and JSON:

```bash
chainsaw search "query" --format json
chainsaw graph query "..." --format json | jq '.results[].f_name'
```

## Configuration

Configuration file: `~/.chainsaw/config.yaml`

```yaml
version: "2.0"
active_profile: "default"

profiles:
  default:
    # Directories to watch
    include:
      - ~/Projects/myproject
    
    # Directories to skip
    exclude:
      - node_modules
      - .git
      - vendor
    
    # File patterns
    whitelist:
      - "**/*.go"
      - "**/*.py"
      - "**/*.js"
      - "**/*.ts"
    
    # Embedding settings
    embedding_model: "nomic-embed-text"
    chunk_size: 512
    overlap: 64
    
    # Graph extraction
    graph_driver:
      model: "qwen2.5:3b"
      batch_size: 100
```

### Using Cloud LLMs for Graph Extraction

Keep embeddings local, use cloud for graph:

```yaml
profiles:
  default:
    embedding_model: "nomic-embed-text"  # Local
    
    # Cloud LLM for graph extraction
    llm_provider: "openai"
    llm_base_url: "https://openrouter.ai/api/v1"
    llm_api_key: "${OPENROUTER_API_KEY}"
    
    graph_driver:
      model: "anthropic/claude-3.5-haiku"
      batch_size: 100
```

## Tips and Best Practices

### Search Tips

1. **Use natural language** - "error handling" works better than "try catch"
2. **Be specific** - "HTTP middleware with authentication" is better than just "middleware"
3. **Check context** - Search is scoped to current directory
4. **Increase limit** - Default is 10 results, use `--limit 20` for more

### Graph Query Tips

1. **Start simple** - Begin with single-hop queries, then add complexity
2. **Use LIMIT** - Always limit results to avoid overwhelming output
3. **Return snippets** - Use `t.snippet` to see actual code in results
4. **Return file paths** - Use `t.file` and `t.lines` to locate code
5. **Watch depth** - Multi-hop queries can explode; start with `*1..2`

### Performance Tips

1. **Index incrementally** - Daemon watches for changes automatically
2. **Exclude noise** - Add `node_modules`, `vendor`, `.git` to exclude list
3. **Use whitelist** - Specify file patterns to index only relevant files
4. **Monitor status** - Run `chainsaw status` to check indexing progress
5. **Check daemon logs** - `journalctl --user -u chainsawd -f`

### Troubleshooting

```bash
# Check daemon status
systemctl --user status chainsawd

# View daemon logs
journalctl --user -u chainsawd -f

# Verify database
chainsaw status

# Check Ollama
ollama list
ollama pull nomic-embed-text

# Rebuild binary
cd ~/Projects/chainsaw
make install
hash -r  # Clear shell cache
```

## Common Patterns

### Finding Code Entry Points

```bash
chainsaw search "main function application entry"
chainsaw graph query "MATCH (m:FUNCTION) WHERE m.name = 'main' RETURN m.snippet, m.file"
```

### Exploring Dependencies

```bash
# What does this package import?
chainsaw graph query "MATCH (p:PACKAGE)-[:imports]->(t) WHERE p.name = 'api' RETURN t.name"

# What imports this package?
chainsaw graph query "MATCH (p)-[:imports]->(t:PACKAGE) WHERE t.name = 'utils' RETURN p.name"
```

### Finding Implementations

```bash
# What implements this interface?
chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s) WHERE i.name = 'Handler' RETURN s.name, s.snippet"
```

### Analyzing Call Graphs

```bash
# Direct calls
chainsaw graph query "MATCH (a:FUNCTION)-[:calls]->(b) WHERE a.name = 'processRequest' RETURN b.name"

# Call chains
chainsaw graph query "MATCH (a:FUNCTION)-[:calls*1..3]->(b) WHERE a.name = 'main' RETURN b.name, b.file"
```

## Integration with AI Agents

Chainsaw's YAML output is designed for AI agent consumption:

```bash
# Search for relevant code
CONTEXT=$(chainsaw search "database schema migrations" --format yaml)

# Query relationships
GRAPH=$(chainsaw graph query "MATCH (m:FUNCTION)-[:calls]->(d) WHERE m.name =~ '.*migrate.*' RETURN m.name, d.name" --format yaml)

# Feed to LLM
echo "$CONTEXT" | llm "Explain these database migration patterns"
```

## Example Workflows

### Understanding a New Codebase

```bash
# 1. Index the codebase
cd /path/to/project
chainsaw index .

# 2. Find entry points
chainsaw search "main application entry point"

# 3. Explore main dependencies
chainsaw graph query "MATCH (m:FUNCTION)-[:calls]->(t) WHERE m.name = 'main' RETURN t.name"

# 4. Find key interfaces
chainsaw graph query "MATCH (i:INTERFACE) RETURN i.name, i.snippet LIMIT 10"

# 5. See what implements them
chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s) RETURN i.name, s.name"
```

### Refactoring Analysis

```bash
# Find all callers of a function
chainsaw graph query "MATCH (a)-[:calls]->(t:FUNCTION) WHERE t.name = 'oldFunction' RETURN a.name, a.file, a.lines"

# Find transitive dependencies
chainsaw graph query "MATCH (a:FUNCTION)-[:calls*]->(b) WHERE a.name = 'criticalFunction' RETURN b.name"

# Find most-coupled components
chainsaw graph query "
  MATCH (a:FUNCTION)-[:calls]->(b)
  RETURN a.name, COUNT(b) AS dependencies
  GROUP BY a.name
  ORDER BY dependencies DESC
  LIMIT 20
"
```

## Limitations

- **Read-only** - Cannot modify code
- **No cross-project queries** - Search scoped to current directory tree
- **Language-agnostic extraction** - Graph quality depends on LLM model
- **Index lag** - New changes take a few seconds to index
- **No version control** - Searches current state only, not history

## Resources

- **Manual**: See `MANUAL.md` for complete reference
- **Developer Guide**: See `DEVELOPER.md` for architecture
- **Examples**: Check `examples/` directory
- **Issues**: https://github.com/wouteroostervld/chainsaw/issues

## Version

Compatible with Chainsaw v0.1.0+ (Schema version 2.4.0)

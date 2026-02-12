# 🪚 Chainsaw

**GraphRAG-powered semantic code search and knowledge graph for your codebase**

Chainsaw indexes your code into a searchable knowledge graph with vector embeddings. Find code by meaning with semantic search, or explore relationships between functions, types, and packages using graph queries.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Nightly Release](https://github.com/wouteroostervld/chainsaw/actions/workflows/nightly-release.yml/badge.svg)](https://github.com/wouteroostervld/chainsaw/actions/workflows/nightly-release.yml)
## ✨ Features

- **🔍 Semantic Search** - Find code by meaning, not just keywords
- **📊 Knowledge Graph** - Query relations between functions, types, and packages
- **🔄 Multi-Hop Queries** - Explore transitive relationships (e.g., `*1..3` for call chains)
- **📈 Aggregation** - COUNT, GROUP BY, ORDER BY for code analytics
- **🎯 Context-Aware** - Automatically scopes to current directory
- **⚡ Background Indexing** - Daemon watches for changes and keeps index fresh
- **🤖 AI-Ready** - YAML/JSON output for LLM consumption
- **🔗 Cypher Queries** - Familiar graph query syntax

## 🚀 Quick Start

### Installation

```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make install
```

### Prerequisites

- [Ollama](https://ollama.ai/) - for embeddings (free, runs locally)
- Go 1.24.7+ - for building from source

### Setup

```bash
# Pull embedding model
ollama pull nomic-embed-text

# Initialize database
chainsaw init

# Start daemon
chainsaw daemon start

# Index your code
cd /path/to/your/project
chainsaw index .
```

### Search

```bash
# Semantic search
chainsaw search "error handling patterns"

# Graph query  
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name LIMIT 10"

# Check status
chainsaw status
```

## 📖 Documentation

- **[User Manual](MANUAL.md)** - Complete usage guide with examples
- **[Developer Guide](DEVELOPER.md)** - Architecture, design, and contribution guide

## 💡 Examples

### Semantic Search

```bash
# Find database code
chainsaw search "database connection pooling"

# Find auth logic
chainsaw search "authentication middleware"

# Find error handling
chainsaw search "error handling patterns" --limit 5
```

Results automatically filtered to current directory and subdirectories.

### Graph Queries

```bash
# Find function calls
chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"

# Find interface implementations
chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s) RETURN i.name, s.name"

# Multi-hop: Find call chains up to 3 levels deep
chainsaw graph query "MATCH (a)-[:calls*1..3]->(b) RETURN a.name, b.name"

# Most-called functions
chainsaw graph query "
  MATCH (a)-[:calls]->(b)
  RETURN b.name, COUNT(a) AS callers
  GROUP BY b.name
  ORDER BY callers DESC
  LIMIT 10
"
```

## 🏗️ Architecture

```
┌─────────────┐
│   CLI       │  Query interface (read-only)
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────┐
│   SQLite + sqlite-vec (WAL)     │  Vector search + graph storage
└─────────────────────────────────┘
       ▲
       │
┌──────┴──────┐
│   Daemon    │  Background indexer + graph extractor
└─────────────┘
       │
       ▼
┌─────────────┐
│   Ollama    │  Embeddings + graph extraction (local)
└─────────────┘
```

**Key components:**
- **Database**: SQLite with `sqlite-vec` extension for vector similarity search
- **Daemon**: Background worker that indexes files and extracts graph relations
- **CLI**: Read-only query interface for search and graph queries
- **LLM**: Ollama (local) or OpenRouter/OpenAI (cloud) for embeddings and graph extraction

## ⚙️ Configuration

Default config: `~/.chainsaw/config.yaml`

```yaml
version: "2.0"
active_profile: "default"

profiles:
  default:
    include:
      - ~/Projects/myproject
    exclude:
      - node_modules
      - .git
    whitelist:
      - "**/*.go"
      - "**/*.js"
      - "**/*.py"
    embedding_model: "nomic-embed-text"
    chunk_size: 512
    overlap: 64
    graph_driver:
      model: "qwen2.5:3b"
      batch_size: 100
```

See [Manual](MANUAL.md#configuration) for advanced configuration including cloud LLM providers.

## 🛠️ Development

```bash
# Build locally
make build

# Install
make install

# Run tests
make test

# Development loop (build + restart daemon)
make dev
```

See [Developer Guide](DEVELOPER.md) for architecture details and contribution guidelines.

## 🔧 Commands

| Command | Description |
|---------|-------------|
| `chainsaw init` | Initialize database |
| `chainsaw index <path>` | Index directory |
| `chainsaw search <query>` | Semantic search |
| `chainsaw graph query <cypher>` | Graph query |
| `chainsaw daemon start/stop` | Manage daemon |
| `chainsaw status` | Show statistics |
| `chainsaw version` | Show version |

## 📊 Performance

- **Graph extraction**: ~30 seconds for 1000 chunks (batched)
- **Search latency**: Sub-second semantic search
- **Scalability**: Tested with 200+ files, 1000+ chunks

## 🤝 Contributing

Contributions welcome! See [Developer Guide](DEVELOPER.md) for:
- Architecture overview
- Design principles
- Development workflow
- Testing guidelines

## 📝 License

MIT License - see LICENSE file for details

## 🙏 Credits

Built with:
- [sqlite-vec](https://github.com/asg017/sqlite-vec) - Vector search extension
- [Ollama](https://ollama.ai/) - Local LLM runtime
- [fsnotify](https://github.com/fsnotify/fsnotify) - File system watching

## 🔗 Links

- **Documentation**: [MANUAL.md](MANUAL.md)
- **Development**: [DEVELOPER.md](DEVELOPER.md)
- **Issues**: [GitHub Issues](https://github.com/wouteroostervld/chainsaw/issues)

---

**Status**: Production-ready. Actively maintained.

**Version**: 2.4.0 (Schema version)

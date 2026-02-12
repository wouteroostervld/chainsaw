# Development Guide

## Quick Start for Developers

### One-time Setup
```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make daemon-install  # Builds, installs, and starts daemon
```

### Daily Development Workflow

```bash
# Make your changes to the code...

# Build and restart daemon with new version
make dev

# Or just install without restarting
make install

# Run tests
make test
```

### Makefile Tasks

| Task | Description |
|------|-------------|
| `make install` | Build and install to ~/.local/bin |
| `make update` | Alias for install (update existing) |
| `make build` | Build locally (creates ./chainsaw) |
| `make daemon-install` | Install daemon systemd service |
| `make daemon-restart` | Restart daemon with new binary |
| `make dev` | Build + restart daemon (quick dev loop) |
| `make test` | Run all tests |
| `make clean` | Remove build artifacts |
| `make version` | Show current version |

### Manual Installation

If you prefer not to use Make:

```bash
# Build
go build -o chainsaw ./cmd/chainsaw

# Install
cp chainsaw ~/.local/bin/

# Restart daemon
systemctl --user restart chainsawd
```

### Testing

```bash
# Run all tests
go test ./...

# Test specific package
go test ./pkg/db

# Verbose output
go test -v ./pkg/indexer
```

### Debugging

```bash
# Check daemon status
systemctl --user status chainsawd

# View daemon logs
journalctl --user -u chainsawd -f

# Database location
~/.chainsaw/chainsaw.db

# Inspect database
sqlite3 ~/.chainsaw/chainsaw.db
```

## Project Structure

```
chainsaw/
├── cmd/
│   └── chainsaw/          # Main binary
├── pkg/
│   ├── config/            # Configuration loading
│   ├── cypher/            # Cypher query transpiler
│   ├── db/                # Database layer (SQLite + vec)
│   ├── filter/            # File/directory filtering
│   ├── indexer/           # Indexing engine + graph extraction
│   ├── llm/               # LLM clients (Ollama, OpenAI)
│   └── watcher/           # File system watcher
├── examples/              # Example configs and scripts
├── Makefile              # Build automation
└── README.md
```

## Common Development Tasks

### Adding a New Command

1. Add handler in `cmd/chainsaw/main.go`
2. Update command list in usage message
3. Add tests in `cmd/chainsaw/main_test.go`
4. Document in README

### Modifying Database Schema

1. Update schema in `pkg/db/schema.go`
2. Increment `SchemaVersion`
3. Add migration logic if needed
4. Update interface in `pkg/db/interface.go`
5. Test with fresh database

### Adding LLM Provider

1. Create client in `pkg/llm/<provider>/client.go`
2. Implement `GraphExtractor` and `EmbeddingProvider` interfaces
3. Add provider detection in `cmd/chainsaw/main.go`
4. Document in configuration examples

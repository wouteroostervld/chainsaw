# 🪚 Chainsaw

**A code graph and context-aware search for your codebase, built from the syntax tree**

Chainsaw parses your source files and answers three kinds of question
without reading whole files: *where is this text, and what is it inside of*,
*what does this file look like*, and *what calls what*. No embeddings, no
model, no GPU: indexing is parsing, and takes seconds.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Commands

```bash
# Search with a regular expression (PCRE); each match shows the definition it is in
chainsaw grep 'IS_EXT2_SB' fs/ext4

# A file folded to its signatures, each with the lines of its body
chainsaw outline pkg/indexer/indexer.go

# The code graph: who calls this, what implements that
chainsaw query 'MATCH (f)-[:calls]->(t {name: "Open"}) RETURN f.name, f.file, f.lines'
```

`grep` and `outline` work on the files as they are on disk and need no index.
`query` needs one:

```bash
chainsaw index .          # parse the source files under a path
chainsaw daemon start     # optional: keep the index current as files change
```

### What a result looks like

```
$ chainsaw grep -s 'IS_EXT[23]_SB' fs/ext4
fs/ext4/super.c
  (outside any definition): 3
  FUNCTION ext4_check_opt_consistency  (lines 2773-2853): 2
  FUNCTION _ext4_show_options  (lines 2960-3089): 1
  FUNCTION ext4_set_def_opts  (lines 4388-4443): 1
  FUNCTION ext4_check_feature_compatibility  (lines 4713-4815): 2
```

Both `grep` and `query` can show the code around a result with `--expand`:

| | |
|---|---|
| `--expand body` | the whole definition around each result |
| `--expand file` | the file folded to signatures, with those definitions in full |
| `--expand calls` | as `file`, and also what those definitions call |

## Languages

Go, Python, JavaScript, TypeScript/TSX, Rust, Java, C, C++ and Ruby are
parsed for definitions, calls and imports. Go calls are resolved exactly with
the Go type checker, including calls through interfaces. Markdown, HTML and
XML fold into an outline but define no symbols.

## Installation

```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make install
```

Needs Go 1.24.7+ and a C compiler (for SQLite and the tree-sitter grammars).
The `go` command must be on the PATH for exact Go call resolution into
dependencies.

## Documentation

- **[Manual](MANUAL.md)**: commands, the query language, the graph, configuration
- **[Agent skill](share/skills/chainsaw/Skill.md)**: how an AI coding agent should use it

## How it works

```
source files ── tree-sitter ──► definitions, calls, imports ──► SQLite graph ──► chainsaw query
      │              (+ go/types for Go)
      └────────────────────────────────────────────────────────► chainsaw grep / outline
```

The index lives in `~/.chainsaw/graph.db`. It holds no source of truth: delete
it and run `chainsaw index` again at any time.

## License

MIT, see [LICENSE](LICENSE).

## Credits

Built with [tree-sitter](https://tree-sitter.github.io/), SQLite, PCRE2 and
[fsnotify](https://github.com/fsnotify/fsnotify).

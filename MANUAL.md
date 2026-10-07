# Chainsaw Manual

Chainsaw looks at code through its syntax tree. It has three ways of looking,
`grep`, `outline` and `query`, and one way of showing what it found in more
or less context, `--expand`.

## Contents

- [Installation](#installation)
- [grep](#grep): search, with the definition around each match
- [outline](#outline): a file folded to its signatures
- [query](#query): the code graph
- [Showing code: --expand](#showing-code---expand)
- [The index](#the-index)
- [The graph](#the-graph)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)

## Installation

```bash
git clone https://github.com/wouteroostervld/chainsaw.git
cd chainsaw
make install          # builds to ~/.local/bin/chainsaw
```

Needs Go 1.24.7+, a C compiler and the PCRE2 development files
(`pcre2-devel` on Fedora, `libpcre2-dev` on Debian and Ubuntu). Nothing else:
there is no model to pull and no service to run. `grep` and `outline` work straight away; `query`
needs `chainsaw index <path>` first.

## grep

```bash
chainsaw grep [options] <pattern> [path...]
```

Searches files with a Perl-compatible regular expression (PCRE2: lookaround,
backreferences, `\b`, `(?i)`, and so on) and groups the matching lines by the
definition they are in.

```
$ chainsaw grep 'relation_type\s+(IN|=)' pkg/db
pkg/db/chunks.go
  METHOD DB.Callees  (lines 95-123)
    102: JOIN graph_edges g ON g.source_entity_id = sd.entity_id AND g.relation_type = ?
pkg/db/symbols.go
  FUNCTION resolveSymbols  (lines 197-285)
    233: WHERE source_entity_id = ? AND chunk_id = ? AND relation_type IN (?, ?)
```

Each group names the innermost definition around the lines: its type
(`FUNCTION`, `METHOD`, `STRUCT`, `INTERFACE`, `CLASS`, `TYPE`), its qualified
name (`Type.method`), and where it starts and ends. Lines outside any
definition, and matches in files that cannot be parsed, are listed without
one.

| Option | |
|---|---|
| `-i` | Ignore case |
| `-F` | The pattern is a literal string |
| `-w` | Match whole words only |
| `-s` | Only list the definitions that contain a match, with a count |
| `--expand body\|file\|calls` | Show the code, see [below](#showing-code---expand) |

Without a path the current directory is searched. Hidden directories and the
`exclude` directories of your configuration are skipped; a file named
explicitly is always searched. `^` and `$` match at line ends. No index is
needed, so results are always current.

`-s` is the quick way to answer "which functions touch this": it is the list
to work through when something has to change everywhere.

## outline

```bash
chainsaw outline [options] <file>
```

Prints a file folded to its skeleton. Every definition of five lines or more
is reduced to its signature and a reference to the lines of its body:

```go
package indexer

func New(cfg *Config, database db.Database) *Indexer // ... chunk: lines 44-54

func (idx *Indexer) IndexFile(ctx context.Context, filePath string) error // ... chunk: lines 56-109
```

This repeats inside: a class lists its methods, a method its nested
functions. Markdown folds along its headings.

| Option | |
|---|---|
| `--expand NAME` | Show this definition in full: a qualified name, a bare name, or a line number. Repeatable. |
| `--symbols` | List the definitions instead: type, name, lines, signature |

## query

```bash
chainsaw query [options] '<cypher>'
```

Queries the graph of definitions and how they relate, in a subset of Cypher.

```cypher
// Who calls this function?
MATCH (f)-[:calls]->(t) WHERE t.name = "ext4_orphan_add" RETURN f.name, f.file, f.lines

// What does this one call, up to three levels deep?
MATCH (a {name: "handleIndex"})-[:calls*1..3]->(b) RETURN b.name, b.signature

// Which types implement an interface, and what do calls through it reach?
MATCH (s)-[:implements]->(i {name: "Database"}) RETURN s.name
MATCH (f)-[:may_call]->(t) WHERE t.name ENDS WITH ".StoreFileSymbols" RETURN f.name, t.name

// The functions of a directory, with their signatures
MATCH (n:FUNCTION|METHOD) WHERE n.file CONTAINS "/indexer/" RETURN n.signature, n.lines

// Most-called functions
MATCH (f)-[:calls]->(t) RETURN t.name, count(f) AS callers ORDER BY callers DESC LIMIT 10

// What is called but not defined in the index
MATCH (f)-[:calls]->(t:EXTERNAL) RETURN t.name, count(f) AS n ORDER BY n DESC LIMIT 20
```

### Patterns

| | |
|---|---|
| `(v)` `(v:LABEL)` `(v:A\|B)` | A node; labels are entity types, not case-sensitive |
| `(v {name: "x"})` | A node with a property equal to a value |
| `-[:type]->` `<-[:type]-` `-[:type]-` | An edge, forward, backward or either way |
| `-[:a\|b]->` | Any of several relation types |
| `-[r:type]->` | Names the edge so its properties can be used |
| `-[:calls*1..3]->` `-[:calls*2]->` `-[*]->` | A path of several edges; up to 5 when no limit is given |
| `-->` `<--` `--` | An edge of any type |

A query matches one path: `(a)-[...]->(b)-[...]->(c)`, of any length, or a
single node.

### Properties

| Node | |
|---|---|
| `name` | Qualified name: `Open`, `Store.Close` |
| `type` | Entity type |
| `file` | Path of the file it is defined in; null for `EXTERNAL` |
| `line`, `end_line`, `lines` | Where the definition starts and ends; `lines` is `"12-40"` |
| `signature` | The definition up to its body |
| `container`, `short_name` | The two halves of a method's name |
| `snippet` | The code of the definition, read from the file as it is now |

| Edge | |
|---|---|
| `type` | Relation type |
| `weight` | 1.0 for an exact edge, 0.5 for one matched by name |
| `depth` | Length of a path of several edges |

### Clauses

- `WHERE` with `=` `<>` `<` `>` `<=` `>=`, `CONTAINS`, `STARTS WITH`,
  `ENDS WITH`, `=~` (regular expression), `IN [...]`, `IS NULL`,
  `IS NOT NULL`, combined with `AND`, `OR`, `NOT` and parentheses
- `RETURN` a node (`v` gives name, type, file and lines), a property, or
  `count(*)`, `count(v)`, `count(DISTINCT v.prop)`; `AS` to name a column;
  `DISTINCT`. With a count, the other columns are what is grouped by.
- `ORDER BY ... [DESC]`, `SKIP n`, `LIMIT n`

### Options

| Option | |
|---|---|
| `--all` | Do not limit results to the current directory |
| `--limit N` | Rows to return when the query has no `LIMIT` (default 100) |
| `--expand body\|file\|calls` | Also show the code of the returned nodes |
| `--no-index` | Query the index as it is, without first indexing what changed in the current directory |
| `--sql` | Print the SQL the query is translated to |

By default a result must touch the current directory: at least one node of
the matched path is defined under it. `chainsaw graph query` is accepted as
an older spelling of `chainsaw query`.

## Showing code: --expand

`grep` and `query` find places. `--expand` shows the code at those places,
in one of three amounts:

| | |
|---|---|
| `body` | The whole definition around each place. Nested definitions stay folded. |
| `file` | The file folded as by `outline`, with those definitions in full. All places in one file share one view. |
| `calls` | As `file`, and also what those definitions call: in place when it is in the same file, after it when it is in another. Uses the index, and follows `calls` edges only. |

```bash
chainsaw grep --expand body 'filepath\.SkipDir' cmd
chainsaw query --expand file 'MATCH (f)-[:calls]->(t {name: "nearest"}) RETURN f'
```

`body` is the smallest and usually enough. `file` shows where the code sits.
`calls` is for following the code without opening more files.

## The index

```bash
chainsaw index <path>...      # parse and record files, or the files under directories
chainsaw index --force <path> # also files that have not changed
chainsaw status               # what is in the index
chainsaw daemon start         # watch the configured directories for changes
```

**You rarely need any of these.** Before `chainsaw query` runs, and before
`--expand calls` looks up what is called, the source files under the current
directory that are new or modified are indexed and deleted ones are removed.
A first query on a project that was never indexed just takes a moment longer
(this repository: about two seconds) and says so on standard error. Results
never come from stale files. `--no-index` skips the step.

A file counts as modified when its modification time differs from when it
was indexed. When more than 1,000 files need indexing the step is skipped
with a note, because it would take too long to do in passing; run
`chainsaw index .` once for a tree that size.

`chainsaw index` does the same by hand, with no limit and for paths outside
the current directory. It takes seconds: the Linux kernel's `fs/ext4` (70,000
lines) in about two. It accepts any number of files and directories, and
reads more from standard input when given `-`, so it fits in a pipeline:

```bash
chainsaw index pkg cmd/chainsaw/main.go
git diff --name-only | chainsaw index -
find . -name '*.go' -newer Makefile -print0 | xargs -0 chainsaw index
find . -name '*.py' -print0 | chainsaw index -0 -
```

A file named directly is indexed whatever the configuration excludes; one
that is not in a parseable language is passed over, so a pipeline does not
have to filter. A path that does not exist is reported, the rest is still
indexed, and the exit status is 1.

The daemon is optional as well. It watches the `include` directories of your
configuration and re-indexes files as they change.

The index is `~/.chainsaw/graph.db`. It holds names, locations and edges, and
no source text: the code a query shows (`snippet`, `--expand`) is read from
the files when it is asked for. It can be deleted at any time.

## The graph

Every definition is an entity; every call and import in the source is an
edge.

**Entity types:** `FUNCTION`, `METHOD` (named `Type.method`), `CLASS`,
`STRUCT`, `INTERFACE`, `TYPE`, `PACKAGE` (a file's package or module, and
what it imports), and `EXTERNAL` for something that is called but not defined
in the index, such as `fmt.Println`.

**Relation types:**

- `calls`: a call whose target is known
- `may_call`: a call that may end up there
- `implements`: a type implements an interface (Go)
- `imports`: a file imports a package
- `defines`: a package defines its functions and types, a class its methods

How exact `calls` is depends on the language.

**Go** files are run through the Go type checker. Every call has its exact
target, whatever variable, field or package it is reached through. A call of
an interface method is a `calls` edge to that method of the interface, plus a
`may_call` edge to the method of each type that implements it. So to find
everything that can reach an implementation, ask for `[:calls|may_call]`.
Calls of function values have no target and are left out. Implementations
are looked for in the package and what it imports; the `implements` edges
find the rest. Dependencies are located with the `go` command; without it,
calls into them fall back to the rules below.

**The other languages** have names, not types. An edge is `calls` when the
target follows from the source: a function in the same directory, a name
reached through an import, a method on `self`/`this` or on a named type.
When only the name is known, as with a method call on a variable, the edge is
`may_call` and goes to every definition in the same repository with that
name.

A graph built from syntax cannot see calls made through reflection, or
strings that name a function. `chainsaw grep` can.

## Configuration

`~/.chainsaw/config.yaml` is optional. Chainsaw uses these settings of the
active profile:

```yaml
version: "2.0"
active_profile: "default"

profiles:
  default:
    # Directories the daemon watches
    include:
      - ~/Projects
    # Directory names skipped by grep, index and the daemon
    exclude:
      - node_modules
      - vendor
      - .venv
      - __pycache__
    # Files never indexed (regular expressions on the path)
    blacklist:
      - '\.min\.js$'
```

Write directory names without a trailing slash. Settings from older versions
(`embedding_model`, `chunk_size`, `overlap`, `graph_driver`, `llm_*`) are
ignored.

## Troubleshooting

**`query` returns nothing.** Results are limited to the current directory;
try `--all`. If a note says there were too many files to index in passing,
run `chainsaw index .` once.

**A call is missing from the graph.** In Go, check that the package compiles
and that `go` is on the PATH. In other languages, a method call on a variable
is a `may_call` edge, not `calls`. Ruby calls written without parentheses are
not recognised.

**The index is from another version.** Chainsaw says so and stops. Remove
`~/.chainsaw/graph.db` and index again.

**Daemon logs.** `journalctl --user -u chainsawd -f`

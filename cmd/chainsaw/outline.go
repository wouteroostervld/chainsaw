package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
)

const outlineUsage = `Usage: chainsaw outline [options] <file>

Shows a source file folded to its skeleton: every definition is reduced to
its signature and the lines of its body. Works on the file as it is on disk;
the index is not used.

Options:
  --expand NAME    Show this definition in full; a qualified name (Type.method),
                   a bare name, or a line number. May be given more than once.
  --symbols        List the definitions instead: type, name, lines, signature

Examples:
  chainsaw outline pkg/indexer/indexer.go
  chainsaw outline --expand Indexer.IndexFile pkg/indexer/indexer.go`

// names collects a flag that may be repeated
type names []string

func (n *names) String() string     { return strings.Join(*n, ",") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

func handleOutline(args []string) {
	flags := flag.NewFlagSet("outline", flag.ExitOnError)
	flags.Usage = func() { fmt.Println(outlineUsage) }
	var expand names
	flags.Var(&expand, "expand", "")
	symbols := flags.Bool("symbols", false, "")
	flags.Parse(args)

	if flags.NArg() != 1 {
		fmt.Println(outlineUsage)
		os.Exit(1)
	}
	path, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		fail("%v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	f, ok := chunker.Parse(path, content, 0)
	if !ok {
		fail("%s is not in a language chainsaw can parse", flags.Arg(0))
	}

	if *symbols {
		for _, s := range f.Symbols {
			fmt.Printf("%-9s %-40s %d-%d  %s\n", s.Type, s.QualifiedName(), s.StartLine, s.EndLine, s.Signature)
		}
		return
	}

	expanded := make(map[int]bool)
	for _, name := range expand {
		found := false
		if line, err := strconv.Atoi(name); err == nil {
			expanded[f.ChunkAt(line)] = true
			continue
		}
		for _, s := range f.Symbols {
			if s.QualifiedName() == name || s.Name == name {
				expanded[f.ChunkAt(s.StartLine)] = true
				found = true
			}
		}
		// Sections of a document have a title but are not symbols
		for i, c := range f.Chunks {
			if c.Title == name || strings.HasSuffix(c.Title, " > "+name) {
				expanded[i] = true
				found = true
			}
		}
		if !found {
			fail("no definition named %s in %s", name, flags.Arg(0))
		}
	}
	fmt.Print(f.Render(expanded))
}

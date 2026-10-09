package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/grep"
	"github.com/wouteroostervld/chainsaw/pkg/view"
)

const grepUsage = `Usage: chainsaw grep [options] <pattern> [path...]

Searches files with a Perl-compatible regular expression and shows, for each
matching line, the definition it is in: the qualified name of the function,
method, struct or interface. Searches the current directory when no path is
given. Hidden directories and the excluded directories of your configuration
are skipped.

Options:
  -i               Ignore case
  -F               The pattern is a literal string
  -w               Match whole words only
  -s               Only list the definitions that contain a match, with a count
  --expand body    Show the whole definition around each match
  --expand file    Show each file folded to signatures, with those definitions in full
  --expand calls   As file, and also what those definitions call. This uses the
                   index, which is first brought up to date for the current directory
  --no-index       With --expand calls, use the index as it is

Examples:
  chainsaw grep 'IS_EXT2_SB' fs/ext4
  chainsaw grep -s 'ctx\.Done\(\)'
  chainsaw grep --expand body '(?<=func )handle\w+'`

func handleGrep(args []string) {
	flags := flag.NewFlagSet("grep", flag.ExitOnError)
	flags.Usage = func() { fmt.Println(grepUsage) }
	ignoreCase := flags.Bool("i", false, "")
	fixed := flags.Bool("F", false, "")
	word := flags.Bool("w", false, "")
	symbolsOnly := flags.Bool("s", false, "")
	expand := flags.String("expand", "", "")
	noIndex := flags.Bool("no-index", false, "")
	flags.Parse(args)

	if flags.NArg() < 1 {
		fmt.Println(grepUsage)
		os.Exit(1)
	}
	paths := flags.Args()[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			fail("%v", err)
		}
		paths[i] = abs
	}

	files, err := grep.Search(flags.Arg(0), paths, grep.Options{
		IgnoreCase: *ignoreCase,
		Fixed:      *fixed,
		Word:       *word,
		Exclude:    activeProfile().Exclude,
	})
	if err != nil {
		fail("%v", err)
	}
	if len(files) == 0 {
		fmt.Println("No matches")
		os.Exit(1)
	}

	cwd := workingDir()
	if *expand != "" {
		mode, err := view.ParseMode(*expand)
		if err != nil {
			fail("%v", err)
		}
		var hits []view.Hit
		for _, f := range files {
			for _, g := range f.Groups {
				for _, l := range g.Lines {
					hits = append(hits, view.Hit{Path: f.Path, Line: l.Number})
				}
			}
		}
		renderHits(hits, mode, cwd, *noIndex)
		return
	}

	matches := 0
	for _, f := range files {
		fmt.Println(displayPath(f.Path, cwd))
		for _, g := range f.Groups {
			matches += len(g.Lines)
			header := "(outside any definition)"
			if g.Symbol != "" {
				header = fmt.Sprintf("%s %s  (lines %d-%d)", g.Type, g.Symbol, g.StartLine, g.EndLine)
			}
			if *symbolsOnly {
				fmt.Printf("  %s: %d\n", header, len(g.Lines))
				continue
			}
			fmt.Printf("  %s\n", header)
			for _, l := range g.Lines {
				fmt.Printf("    %d: %s\n", l.Number, strings.TrimSpace(l.Text))
			}
		}
	}
	fmt.Printf("\n%d matching lines in %d files\n", matches, len(files))
}

// renderHits shows hits in their context. The index is only opened when the
// view needs to know what the hits call, and is then brought up to date
// unless asked not to.
func renderHits(hits []view.Hit, mode view.Mode, cwd string, noIndex bool) {
	r := &view.Renderer{Out: os.Stdout, Mode: mode, Base: cwd}
	if mode == view.ModeCalls {
		database := openDB()
		defer database.Close()
		if !noIndex {
			autoIndex(database, cwd, config.IndexGraph)
		}
		r.Finder = database
	}
	if err := r.Render(hits); err != nil {
		fail("%v", err)
	}
}

// displayPath shows a path relative to the current directory when it is under it
func displayPath(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

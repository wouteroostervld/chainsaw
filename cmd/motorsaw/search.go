package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/wouteroostervld/motorsaw/pkg/config"
	"github.com/wouteroostervld/motorsaw/pkg/embed"
	"github.com/wouteroostervld/motorsaw/pkg/view"
)

const searchUsage = `Usage: motorsaw search [options] <words>...

Searches by meaning: finds the definitions and sections whose content is
closest to what you describe, whatever words they use. Results are limited
to the current directory unless --all is given.

This uses the embeddings index, which is off by default and needs a model;
"motorsaw status" shows whether it is on here. To find a name or a string,
"motorsaw grep" is exact and needs nothing.

Options:
  --all            Do not limit results to the current directory
  --limit N        Results to return (default 10)
  --expand body    Also show the definition around each result
  --expand file    Also show its file folded to signatures, with the definition in full
  --expand calls   As file, and also what those definitions call
  --no-index       Search the index as it is, without first embedding what changed here
  --oneline        One result on a line: path:lines, distance and what it is
                   in, separated by tabs, and nothing else
  --width N        Cut lines to N characters

Options may stand before or after the words.

Before a search runs, source files under the current directory that are new
or modified are embedded, when embeddings are switched on for them.

Examples:
  motorsaw search "where are retries handled"
  motorsaw search --expand body --limit 3 parse a config file`

// autoEmbedLimit is the most files a search will embed by itself. Each one is
// a request to a model, so the limit is far lower than for parsing.
const autoEmbedLimit = 100

func handleSearch(args []string) {
	flags := flag.NewFlagSet("search", flag.ExitOnError)
	flags.Usage = func() { fmt.Println(searchUsage) }
	all := flags.Bool("all", false, "")
	limit := flags.Int("limit", 10, "")
	expand := flags.String("expand", "", "")
	noIndex := flags.Bool("no-index", false, "")
	oneline := flags.Bool("oneline", false, "")
	width := flags.Int("width", 0, "")
	given := parse(flags, args)

	if len(given) < 1 {
		fmt.Println(searchUsage)
		os.Exit(1)
	}
	if *limit < 1 {
		fail("--limit must be at least 1")
	}
	query := strings.Join(given, " ")
	out, done := output(*width)
	defer done()

	var mode view.Mode
	if *expand != "" {
		var err error
		if mode, err = view.ParseMode(*expand); err != nil {
			fail("%v", err)
		}
	}

	st, err := loadSettings()
	if err != nil {
		fail("%v", err)
	}
	client, err := st.embedder()
	if err != nil {
		fail("%v", err)
	}
	if client == nil {
		fail("no embedding model is configured.\n\n%s", embeddingsHelp)
	}

	cwd := workingDir()
	database := openDB()
	defer database.Close()
	if !*noIndex {
		autoIndex(database, cwd, config.IndexEmbeddings)
	}

	have, _, err := database.EmbeddingModel()
	if err != nil {
		fail("%v", err)
	}
	if have != "" && have != client.Model() {
		fail("the index holds embeddings made with %s, and %s is configured; run \"motorsaw index .\" to make them again", have, client.Model())
	}

	root := cwd
	if *all {
		root = ""
	}
	if !*oneline {
		fmt.Fprintf(out, "search: %q\n", query)
	}
	var hits []view.Hit
	if have != "" {
		vectors, err := client.Embed(context.Background(), []string{embed.QueryText(client.Model(), query)})
		if err != nil {
			fail("%v", err)
		}
		found, err := database.SearchEmbeddings(vectors[0], *limit, root)
		if err != nil {
			fail("%v", err)
		}
		for i, h := range found {
			hits = append(hits, view.Hit{Path: h.Path, Line: h.StartLine})
			if *oneline {
				fmt.Fprintf(out, "%s:%d-%d\t%.3f\t%s\n", displayPath(h.Path, cwd), h.StartLine, h.EndLine, h.Distance, oneLine(h.Title))
				continue
			}
			if i == 0 {
				fmt.Fprintln(out, "results:")
			}
			printValue(out, "  - ", "file", displayPath(h.Path, cwd))
			printValue(out, "    ", "lines", fmt.Sprintf("%d-%d", h.StartLine, h.EndLine))
			if h.Title != "" {
				printValue(out, "    ", "in", h.Title)
			}
			fmt.Fprintf(out, "    distance: %.3f\n", h.Distance)
		}
	}

	if len(hits) == 0 {
		if !*oneline {
			fmt.Fprintln(out, "results: []")
		}
		done()
		// Nothing found is most often nothing embedded
		if kinds, err := st.kinds.ForDir(cwd); err == nil && !kinds[config.IndexEmbeddings] {
			fmt.Fprintf(os.Stderr, "note: the embeddings index is not switched on for this directory.\n\n%s\n", embeddingsHelp)
		}
		return
	}
	if !*oneline {
		fmt.Fprintf(out, "total: %d\n", len(hits))
	}

	if mode != "" {
		fmt.Fprintln(out)
		renderHits(out, hits, mode, cwd, *noIndex)
	}
}

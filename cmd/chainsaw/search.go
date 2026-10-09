package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/embed"
	"github.com/wouteroostervld/chainsaw/pkg/view"
)

const searchUsage = `Usage: chainsaw search [options] <words>...

Searches by meaning: finds the definitions and sections whose content is
closest to what you describe, whatever words they use. Results are limited
to the current directory unless --all is given.

This uses the embeddings index, which is off by default and needs a model;
"chainsaw status" shows whether it is on here. To find a name or a string,
"chainsaw grep" is exact and needs nothing.

Options:
  --all            Do not limit results to the current directory
  --limit N        Results to return (default 10)
  --expand body    Also show the definition around each result
  --expand file    Also show its file folded to signatures, with the definition in full
  --expand calls   As file, and also what those definitions call
  --no-index       Search the index as it is, without first embedding what changed here

Before a search runs, source files under the current directory that are new
or modified are embedded, when embeddings are switched on for them.

Examples:
  chainsaw search "where are retries handled"
  chainsaw search --expand body --limit 3 parse a config file`

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
	flags.Parse(args)

	if flags.NArg() < 1 {
		fmt.Println(searchUsage)
		os.Exit(1)
	}
	if *limit < 1 {
		fail("--limit must be at least 1")
	}
	query := strings.Join(flags.Args(), " ")

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
		fail("the index holds embeddings made with %s, and %s is configured; run \"chainsaw index .\" to make them again", have, client.Model())
	}

	root := cwd
	if *all {
		root = ""
	}
	fmt.Printf("search: %q\n", query)
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
			if i == 0 {
				fmt.Println("results:")
			}
			printValue("  - ", "file", displayPath(h.Path, cwd))
			printValue("    ", "lines", fmt.Sprintf("%d-%d", h.StartLine, h.EndLine))
			if h.Title != "" {
				printValue("    ", "in", h.Title)
			}
			fmt.Printf("    distance: %.3f\n", h.Distance)
			hits = append(hits, view.Hit{Path: h.Path, Line: h.StartLine})
		}
	}

	if len(hits) == 0 {
		fmt.Println("results: []")
		// Nothing found is most often nothing embedded
		if kinds, err := st.kinds.ForDir(cwd); err == nil && !kinds[config.IndexEmbeddings] {
			fmt.Fprintf(os.Stderr, "note: the embeddings index is not switched on for this directory.\n\n%s\n", embeddingsHelp)
		}
		return
	}
	fmt.Printf("total: %d\n", len(hits))

	if mode != "" {
		fmt.Println()
		renderHits(hits, mode, cwd, *noIndex)
	}
}

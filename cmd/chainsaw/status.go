package main

import (
	"fmt"
	"sort"

	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/db"
)

func handleStatus() {
	database := openDB()
	defer database.Close()

	counts, err := database.Count()
	if err != nil {
		fail("%v", err)
	}
	statusCounts, _ := database.CountFilesByStatus()
	version, _ := database.GetMeta("schema_version")

	fmt.Println("Chainsaw Status")
	fmt.Println("===============")
	fmt.Printf("Index:        %s (schema %s)\n\n", database.Path(), version)

	printKinds(database)

	fmt.Printf("Files:        %d\n", counts.Files)
	for _, status := range []string{"pending", "processing", "failed"} {
		if n := statusCounts[status]; n > 0 {
			fmt.Printf("  %-11s %d\n", status+":", n)
		}
	}
	fmt.Printf("Definitions:  %d\n", counts.Definitions)
	fmt.Printf("External:     %d (referenced, not in the index)\n", counts.Externals)

	fmt.Println("Edges:")
	relations := make([]string, 0, len(counts.Edges))
	for relation := range counts.Edges {
		relations = append(relations, relation)
	}
	sort.Strings(relations)
	for _, relation := range relations {
		fmt.Printf("  %-11s %d\n", relation+":", counts.Edges[relation])
	}
}

// printKinds shows the kinds of index: whether each is on in the current
// directory, and how much it holds
func printKinds(database *db.DB) {
	held, _ := database.CountFilesByModule()

	var on map[string]bool
	st, err := loadSettings()
	if err == nil {
		on, err = st.kinds.ForDir(workingDir())
	}
	if err != nil {
		fmt.Printf("Configuration cannot be used: %v\n\n", err)
	}

	fmt.Println("Kinds of index (here means the current directory):")
	for _, kind := range []string{config.IndexGraph, config.IndexEmbeddings} {
		state := "off here"
		switch {
		case on == nil:
			state = "unknown"
		case on[kind]:
			state = "on here"
		}
		fmt.Printf("  %-11s %-9s %d files\n", kind+":", state+",", held[kind])
	}

	model, dim, _ := database.EmbeddingModel()
	chunks, _ := database.CountEmbeddings()
	configured := ""
	if st != nil {
		configured = st.profile.EmbeddingSettings().Model
	}
	switch {
	case model != "":
		fmt.Printf("  embeddings made with %s: %d chunks, vectors of length %d\n", model, chunks, dim)
		if configured != "" && configured != model {
			fmt.Printf("  %s is configured now; \"chainsaw index\" will make them again\n", configured)
		}
	case configured != "":
		fmt.Printf("  embedding model: %s\n", configured)
	default:
		fmt.Println("  no embedding model is configured")
	}
	fmt.Println()
}

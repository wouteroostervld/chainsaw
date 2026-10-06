package llm

import "strings"

// Some embedding models are trained with a task prefix and retrieve poorly
// without it: what is stored and what is asked must be marked as such.

// DocumentText returns the text to embed for content that is stored in the index
func DocumentText(model, text string) string {
	if usesTaskPrefix(model) {
		return "search_document: " + text
	}
	return text
}

// QueryText returns the text to embed for a search query
func QueryText(model, text string) string {
	if usesTaskPrefix(model) {
		return "search_query: " + text
	}
	return text
}

func usesTaskPrefix(model string) bool {
	return strings.Contains(strings.ToLower(model), "nomic-embed")
}

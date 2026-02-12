package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChunkMetadata tracks both chunk ID and file ID for a chunk number
type ChunkMetadata struct {
	ChunkID int64
	FileID  int64
}

// BuildMarkdownPrompt creates a markdown-formatted prompt with multiple code chunks
func BuildMarkdownPrompt(chunks []ChunkInput) (prompt string, chunkMapping map[int]ChunkMetadata) {
	var sb strings.Builder
	chunkMapping = make(map[int]ChunkMetadata)

	// Instructions
	sb.WriteString(`You are analyzing Go source code. Extract relationships between code entities from the chunks below.

For each relationship found, output ONE JSON line with:
- chunk: which chunk number (1, 2, 3, etc.)
- source: entity name
- source_type: FUNCTION|METHOD|TYPE|STRUCT|INTERFACE|VARIABLE|CONSTANT
- target: entity name  
- target_type: FUNCTION|METHOD|TYPE|STRUCT|INTERFACE|VARIABLE|CONSTANT
- relation_type: calls|uses|implements|extends|creates|returns|accepts|has_field

Focus on meaningful relationships. Ignore trivial built-ins (int, string, error).

Output ONLY JSONL format (one JSON object per line). No explanations, no markdown wrappers.

---

`)

	// Add each chunk
	for i, chunk := range chunks {
		chunkNum := i + 1
		chunkMapping[chunkNum] = ChunkMetadata{
			ChunkID: chunk.ChunkID,
			FileID:  chunk.FileID,
		}

		sb.WriteString(fmt.Sprintf("# Chunk %d\n", chunkNum))
		if chunk.FilePath != "" {
			sb.WriteString(fmt.Sprintf("File: `%s`\n\n", chunk.FilePath))
		}
		sb.WriteString("```go\n")
		sb.WriteString(chunk.Content)
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("---\n\nOutput:\n")

	return sb.String(), chunkMapping
}

// ParseJSONL parses JSONL response and maps chunk numbers to chunk IDs
func ParseJSONL(response string, chunkMapping map[int]ChunkMetadata) ([]EdgeWithMetadata, error) {
	// Strip markdown code fence if present
	response = stripMarkdownCodeFence(response)

	lines := strings.Split(response, "\n")
	var edges []EdgeWithMetadata
	var errors []string

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip lines that are just backticks or markdown artifacts
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "`") || line == "```json" || line == "```" {
			continue
		}

		// Skip lines that don't look like JSON (don't start with {)
		if !strings.HasPrefix(line, "{") {
			continue
		}

		// Parse the JSON line
		var parsed struct {
			Chunk        int    `json:"chunk"`
			Source       string `json:"source"`
			SourceType   string `json:"source_type"`
			Target       string `json:"target"`
			TargetType   string `json:"target_type"`
			RelationType string `json:"relation_type"`
		}

		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			errors = append(errors, fmt.Sprintf("line %d: %v", lineNum+1, err))
			continue
		}

		// Map chunk number to chunk metadata (chunk ID + file ID)
		metadata, ok := chunkMapping[parsed.Chunk]
		if !ok {
			errors = append(errors, fmt.Sprintf("line %d: invalid chunk number %d", lineNum+1, parsed.Chunk))
			continue
		}

		// Create edge with metadata
		edge := EdgeWithMetadata{
			Edge: Edge{
				Source:       parsed.Source,
				SourceType:   parsed.SourceType,
				Target:       parsed.Target,
				TargetType:   parsed.TargetType,
				RelationType: parsed.RelationType,
			},
			ChunkID: metadata.ChunkID,
			FileID:  metadata.FileID,
		}

		edges = append(edges, edge)
	}

	// Return partial success if we got some edges (don't error on minor issues)
	if len(edges) > 0 {
		if len(errors) > 0 {
			// Got edges but had some errors - this is OK, just log it
			fmt.Printf("  (Parsed %d edges, skipped %d malformed lines)\n", len(edges), len(errors))
		}
		return edges, nil
	}

	// No edges at all - this is an error
	if len(errors) > 0 {
		return nil, fmt.Errorf("failed to parse JSONL: %s", strings.Join(errors[:min(5, len(errors))], "; "))
	}

	return edges, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stripMarkdownCodeFence removes markdown code fences from responses
// Handles: ```json\n...\n``` or ```\n...\n```
func stripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)

	// Check for opening fence
	if strings.HasPrefix(s, "```") {
		// Find the end of the opening fence line
		firstNewline := strings.Index(s, "\n")
		if firstNewline == -1 {
			return s // Malformed, return as-is
		}

		// Remove opening fence
		s = s[firstNewline+1:]

		// Check for closing fence
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}

		s = strings.TrimSpace(s)
	}

	return s
}

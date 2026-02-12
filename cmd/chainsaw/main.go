package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/asg017/sqlite-vec-go-bindings/cgo" // Load sqlite-vec extension
	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/cypher"
	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/filter"
	"github.com/wouteroostervld/chainsaw/pkg/indexer"
	"github.com/wouteroostervld/chainsaw/pkg/llm"
	"github.com/wouteroostervld/chainsaw/pkg/llm/ollama"
	"github.com/wouteroostervld/chainsaw/pkg/llm/openai"
	"github.com/wouteroostervld/chainsaw/pkg/watcher"
	"github.com/wouteroostervld/chainsaw/pkg/worker"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: chainsaw [init|index|search|graph|daemon|status|version]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		handleInit()
	case "index":
		handleIndex()
	case "search":
		handleSearch()
	case "graph":
		handleGraph()
	case "daemon":
		handleDaemon()
	case "status":
		handleStatus()
	case "version":
		fmt.Printf("chainsaw version %s\n", version)
	default:
		fmt.Println("Unknown command:", os.Args[1])
		os.Exit(1)
	}
}

func handleInit() {
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")
	os.MkdirAll(filepath.Dir(dbPath), 0700)

	if _, err := os.Stat(dbPath); err == nil {
		fmt.Println("Database already exists")
		return
	}

	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	database.SetMeta("created_at", time.Now().UTC().Format(time.RFC3339))
	fmt.Println("✓ Database initialized at", dbPath)
	fmt.Printf("✓ Embedding dimension: %d\n", 768)
}

func handleStatus() {
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")
	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	files, _ := database.CountFiles()
	chunks, _ := database.CountChunks()
	edges, _ := database.CountEdges()
	statusCounts, _ := database.CountFilesByStatus()

	fmt.Println("Chainsaw Status")
	fmt.Println("===============")
	fmt.Printf("Database:       %s\n", dbPath)
	fmt.Println()

	// File statistics with status breakdown
	fmt.Println("Files:")
	fmt.Printf("  Total:      %d\n", files)
	if indexed, ok := statusCounts["indexed"]; ok && indexed > 0 {
		fmt.Printf("  Indexed:    %d\n", indexed)
	}
	if pending, ok := statusCounts["pending"]; ok && pending > 0 {
		fmt.Printf("  Pending:    %d\n", pending)
	}
	if processing, ok := statusCounts["processing"]; ok && processing > 0 {
		fmt.Printf("  Processing: %d\n", processing)
	}
	if failed, ok := statusCounts["failed"]; ok && failed > 0 {
		fmt.Printf("  Failed:     %d\n", failed)
	}

	fmt.Println()
	fmt.Printf("Chunks:         %d\n", chunks)
	fmt.Printf("Graph edges:    %d\n", edges)
	fmt.Printf("Embedding dim:  %d\n", 768)
}

func handleIndex() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: chainsaw index <path>")
		os.Exit(1)
	}

	path := os.Args[2]
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")

	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Load config and create file filter (same as daemon)
	configPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "config.yaml")
	fs := &config.RealFileSystem{}
	globalCfg, err := config.LoadGlobalConfigFromPath(configPath, fs)
	if err != nil {
		slog.Debug("Config not found, using defaults", "error", err)
		globalCfg = createDefaultConfig()
	}

	fileFilter := createFileFilter(globalCfg)

	fmt.Printf("📇 Queueing for indexing: %s\n", path)
	start := time.Now()

	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	queuedCount := 0
	errorCount := 0
	skippedCount := 0

	if info.IsDir() {
		// Walk directory and queue all files
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				errorCount++
				return nil
			}

			if info.IsDir() {
				return nil
			}

			// Apply file filter (same as daemon)
			absPath, err := filepath.Abs(filePath)
			if err != nil {
				absPath = filePath
			}

			if !fileFilter(absPath) {
				skippedCount++
				return nil
			}

			if err := queueFile(database, filePath); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to queue %s: %v\n", filePath, err)
				errorCount++
			} else {
				queuedCount++
			}
			return nil
		})

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\n✓ Queued %d files in %v\n", queuedCount, time.Since(start))
		if skippedCount > 0 {
			fmt.Printf("⏭️  Skipped %d files (filtered)\n", skippedCount)
		}
		if errorCount > 0 {
			fmt.Printf("⚠️  %d errors\n", errorCount)
		}
	} else {
		// Single file
		if err := queueFile(database, path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		queuedCount = 1
		fmt.Printf("✓ Queued in %v\n", time.Since(start))
	}

	fmt.Println("\n💡 Files queued for indexing. The daemon will process them in the background.")
	fmt.Println("   Check status with: chainsaw status")
}

// queueFile queues a single file for indexing
func queueFile(database db.Database, path string) error {
	// Ensure absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	modTime := info.ModTime().Unix()

	// Queue for indexing - let the worker decide if it needs re-indexing
	return database.MarkFilePending(absPath, modTime, hash)
}

func indentLines(text, indent string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" { // Don't indent empty lines
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func handleSearch() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: chainsaw search <query>")
		fmt.Println()
		fmt.Println("Searches indexed code using semantic similarity.")
		fmt.Println("Results are automatically scoped to the current directory and subdirectories.")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  cd /project/pkg/db")
		fmt.Println("  chainsaw search \"database connection\"")
		fmt.Println("  chainsaw search \"error handling patterns\"")
		os.Exit(1)
	}

	query := os.Args[2]

	// Get current working directory for path filtering
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get current directory: %v\n", err)
		cwd = "" // No filtering
	}

	// Convert to absolute path
	var pathFilter string
	if cwd != "" {
		absCwd, err := filepath.Abs(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not get absolute path: %v\n", err)
			pathFilter = ""
		} else {
			// Add trailing slash and wildcard for subdirectory matching
			pathFilter = absCwd
			if !strings.HasSuffix(pathFilter, "/") {
				pathFilter += "/"
			}
			pathFilter += "%"
		}
	}

	// Open database
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")
	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Create Ollama client
	ollamaClient := ollama.NewClient(&ollama.Config{
		BaseURL: "http://localhost:11434",
	})

	// Generate embedding for query
	fmt.Printf("🔍 Searching for: %s\n\n", query)
	ctx := context.Background()
	embeddings, err := ollamaClient.Embed(ctx, "nomic-embed-text", []string{query}, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating embedding: %v\n", err)
		os.Exit(1)
	}
	if len(embeddings) == 0 {
		fmt.Fprintf(os.Stderr, "No embedding generated\n")
		os.Exit(1)
	}
	embedding := embeddings[0]

	// Search with relations (depth=1 for direct connections) WITH path filtering
	results, err := database.SearchWithRelations(embedding, 10, 1, pathFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No results found")
		return
	}

	// Display results in GitHub markdown format
	fmt.Printf("# Search Results\n\n")
	fmt.Printf("**Query:** `%s`\n\n", query)
	fmt.Printf("**Found %d results**\n\n", len(results))

	for i, result := range results {
		// Get file info
		file, err := database.GetFileByID(result.Chunk.FileID)
		if err != nil || file == nil {
			slog.Warn("Failed to get file for chunk", "file_id", result.Chunk.FileID, "error", err)
			continue
		}

		// Detect language from file extension
		ext := filepath.Ext(file.Path)
		lang := getLanguage(ext)

		fmt.Printf("---\n\n")
		fmt.Printf("## %d/%d. %s\n\n", i+1, len(results), filepath.Base(file.Path))
		fmt.Printf("**File:** `%s` | **Lines:** %d-%d | **Distance:** %.4f\n\n",
			file.Path,
			result.Chunk.StartLine,
			result.Chunk.EndLine,
			result.Distance,
		)

		// Ensure content ends with newline for proper markdown fence
		content := result.Chunk.ContentSnippet
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		fmt.Printf("```%s\n%s```\n\n", lang, content)

		// Show related chunks
		if len(result.RelatedChunks) > 0 {
			fmt.Printf("### Related Chunks\n\n")
			totalRelated := len(result.RelatedChunks)
			for i, related := range result.RelatedChunks {
				relFile, err := database.GetFileByID(related.Chunk.FileID)
				if err != nil || relFile == nil {
					slog.Warn("Failed to get file for related chunk", "file_id", related.Chunk.FileID, "error", err)
					continue
				}

				relExt := filepath.Ext(relFile.Path)
				relLang := getLanguage(relExt)

				fmt.Printf("#### %d/%d: %s (%s) - Lines %d-%d\n\n",
					i+1,
					totalRelated,
					relFile.Path,
					related.RelationType,
					related.Chunk.StartLine,
					related.Chunk.EndLine,
				)

				// Show full snippet (no truncation - needed for editing)
				snippet := related.Chunk.ContentSnippet
				// Ensure snippet ends with newline for proper markdown fence
				if !strings.HasSuffix(snippet, "\n") {
					snippet += "\n"
				}
				fmt.Printf("```%s\n%s```\n\n", relLang, snippet)
			}
		}
	}
}

// getLanguage returns the GitHub markdown language identifier from file extension
func getLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}

func createDefaultConfig() *config.GlobalConfig {
	return &config.GlobalConfig{
		Version:       "2.0",
		ActiveProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Include:        []string{"."},
				Exclude:        []string{},           // Empty = don't exclude directories
				Blacklist:      []string{"/\\.git/"}, // Block .git directories and contents
				Whitelist:      []string{},           // Empty = accept all (blacklist only)
				EmbeddingModel: "nomic-embed-text",
				ChunkSize:      512,
				Overlap:        64,
			},
		},
	}
}

func createFileFilter(cfg *config.GlobalConfig) func(string) bool {
	if cfg == nil || len(cfg.Profiles) == 0 {
		return func(string) bool { return true }
	}

	profile := cfg.Profiles[cfg.ActiveProfile]
	if profile == nil {
		return func(string) bool { return true }
	}

	return func(path string) bool {
		// Only check file blacklist/whitelist, skip directory check
		// (Directory Include/Exclude doesn't work with absolute paths during indexing)
		fileOk, _ := filter.ShouldIndexFile(path, profile.Blacklist, profile.Whitelist)
		return fileOk
	}
}

func handleGraph() {
	if len(os.Args) < 3 {
		printGraphUsage()
		os.Exit(1)
	}

	subcommand := os.Args[2]

	switch subcommand {
	case "query":
		handleGraphQuery()
	default:
		fmt.Printf("Unknown graph subcommand: %s\n", subcommand)
		printGraphUsage()
		os.Exit(1)
	}
}

func printGraphUsage() {
	fmt.Println(`Usage: chainsaw graph <subcommand>

Subcommands:
  query <cypher>    Query the knowledge graph using Cypher syntax

Examples:
  # Find what functions call other functions
  chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name"

  # Find what packages import types with code snippets
  chainsaw graph query "MATCH (p:PACKAGE)-[:imports]->(t:TYPE) RETURN p.name, t.name, t.snippet"

  # Find function calls with file paths and line numbers
  chainsaw graph query "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name, t.file, t.lines"

  # Find what implements interfaces (reverse relation)
  chainsaw graph query "MATCH (i:INTERFACE)<-[:implements]-(s) RETURN i.name, s.name, s.snippet"

Supported patterns:
  (var:LABEL)       Node with label (entity type)
  (var)             Node without label (any type)
  -[:type]->        Forward relation
  <-[:type]-        Backward relation

RETURN properties:
  var.name          Entity name
  var.entity_type   Entity type (FUNCTION, METHOD, etc.)
  var.snippet       Code snippet where entity is defined
  var.file          File path where entity found
  var.lines         Line range (e.g. "42-58")

Entity types: FUNCTION, METHOD, TYPE, INTERFACE, STRUCT, PACKAGE, VARIABLE, etc.
Relation types: calls, uses, imports, implements, extends, etc.`)
}

func handleGraphQuery() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: chainsaw graph query <cypher>")
		fmt.Println("Example: chainsaw graph query \"MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name\"")
		os.Exit(1)
	}

	cypherQuery := os.Args[3]

	// Open database
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")
	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Get CWD for path filtering
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "" // Fall back to no filtering
	} else {
		// Convert to absolute path
		cwd, err = filepath.Abs(cwd)
		if err != nil {
			cwd = ""
		}
	}

	// Transpile Cypher to SQL with CWD filtering
	result, err := cypher.Transpile(cypherQuery, cypher.TranspileOptions{
		CWD: cwd,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing Cypher query: %v\n", err)
		os.Exit(1)
	}

	// Execute the generated SQL
	rows, err := database.RawQuery(result.SQL, result.Args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing query: %v\n", err)
		fmt.Fprintf(os.Stderr, "Generated SQL:\n%s\n", result.SQL)
		fmt.Fprintf(os.Stderr, "Args: %v\n", result.Args)
		os.Exit(1)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting columns: %v\n", err)
		os.Exit(1)
	}

	// Scan all rows first
	var allRows [][]string
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		err := rows.Scan(valuePtrs...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning row: %v\n", err)
			continue
		}

		rowData := make([]string, len(columns))
		for i, val := range values {
			if val == nil {
				rowData[i] = "NULL"
			} else {
				switch v := val.(type) {
				case []byte:
					rowData[i] = string(v)
				default:
					rowData[i] = fmt.Sprintf("%v", v)
				}
			}
		}
		allRows = append(allRows, rowData)
	}

	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error iterating rows: %v\n", err)
		os.Exit(1)
	}

	// Print results in YAML format (better for LLM consumption)
	fmt.Printf("query: %q\n", cypherQuery)

	if len(allRows) == 0 {
		fmt.Println("results: []")
		return
	}

	fmt.Println("results:")
	for idx, row := range allRows {
		fmt.Printf("  - index: %d\n", idx)
		for i, col := range columns {
			val := row[i]
			// Check if value contains newlines (like code snippets)
			if strings.Contains(val, "\n") {
				// Multi-line YAML literal block
				fmt.Printf("    %s: |\n", col)
				for _, line := range strings.Split(val, "\n") {
					fmt.Printf("      %s\n", line)
				}
			} else {
				// Single line value
				if strings.Contains(val, "\"") || strings.Contains(val, "'") {
					fmt.Printf("    %s: %q\n", col, val)
				} else {
					fmt.Printf("    %s: %s\n", col, val)
				}
			}
		}
	}
	fmt.Printf("\ntotal: %d\n", len(allRows))
}

// ============================================================================
// Daemon commands (merged from chainsawd)
// ============================================================================

func handleDaemon() {
	if len(os.Args) < 3 {
		printDaemonUsage()
		os.Exit(1)
	}

	subcommand := os.Args[2]

	switch subcommand {
	case "start":
		handleDaemonStart()
	case "status":
		handleDaemonStatus()
	case "help", "-h":
		printDaemonUsage()
	default:
		fmt.Printf("Unknown daemon subcommand: %s\n\n", subcommand)
		printDaemonUsage()
		os.Exit(1)
	}
}

func printDaemonUsage() {
	fmt.Println("chainsaw daemon - Background indexing daemon")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  chainsaw daemon start [-debug] [-loglevel level]")
	fmt.Println("                        Start the daemon")
	fmt.Println("  chainsaw daemon status")
	fmt.Println("                        Show daemon status")
	fmt.Println()
	fmt.Println("Flags for 'start' command:")
	fmt.Println("  -debug          Enable debug logging")
	fmt.Println("  -loglevel LEVEL Set log level: debug, info, warn, error (default: info)")
}

func handleDaemonStart() {
	startFlags := flag.NewFlagSet("daemon-start", flag.ExitOnError)
	debug := startFlags.Bool("debug", false, "Enable debug logging")
	logLevelStr := startFlags.String("loglevel", "info", "Log level (debug, info, warn, error)")

	startFlags.Parse(os.Args[3:])

	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	} else {
		switch *logLevelStr {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{Level: logLevel}
	handler := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(handler))
	fmt.Println("🚀 Starting Chainsaw daemon...")

	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")
	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.HealthCheck(); err != nil {
		slog.Error("Database health check failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Database ready at %s\n", dbPath)

	configPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "config.yaml")
	fs := &config.RealFileSystem{}
	globalCfg, err := config.LoadGlobalConfigFromPath(configPath, fs)
	if err != nil {
		slog.Warn("Config not found, using defaults", "error", err)
		globalCfg = createDefaultDaemonConfig()
	} else {
		fmt.Printf("✓ Config loaded from %s\n", configPath)
	}

	// Get active profile for LLM settings
	profile := globalCfg.Profiles[globalCfg.ActiveProfile]
	if profile == nil {
		profile = &config.Profile{}
	}

	// Create embedding client (always local Ollama)
	embeddingClient := ollama.NewClient(&ollama.Config{
		BaseURL: "http://localhost:11434",
		Timeout: 5 * time.Minute,
	})

	// Create graph extraction client (based on config)
	var graphClient llm.GraphExtractor

	// Determine provider: explicit config > URL detection > default to ollama
	provider := strings.ToLower(profile.LLMProvider)
	if provider == "" && profile.LLMBaseURL != "" {
		// Auto-detect from URL
		if strings.Contains(profile.LLMBaseURL, "openrouter") || strings.Contains(profile.LLMBaseURL, "openai") {
			provider = "openai"
		}
	}

	if provider == "openai" {
		// Use OpenAI-compatible client (OpenRouter, Azure OpenAI, etc.)
		graphClient = openai.NewClient(&openai.Config{
			BaseURL: profile.LLMBaseURL,
			APIKey:  profile.LLMAPIKey,
			Timeout: 5 * time.Minute,
		})
		slog.Info("Using OpenAI-compatible API for graph extraction", "base_url", profile.LLMBaseURL)
	} else {
		// Use local Ollama for graph extraction (default)
		graphClient = ollama.NewClient(&ollama.Config{
			BaseURL: func() string {
				if profile.LLMBaseURL != "" {
					return profile.LLMBaseURL
				}
				return "http://localhost:11434"
			}(),
			Timeout: 5 * time.Minute,
		})
		slog.Info("Using Ollama for graph extraction")
	}

	ctx := context.Background()
	if err := embeddingClient.Ping(ctx); err != nil {
		slog.Warn("Ollama not available", "error", err)
		fmt.Println("   Run: ollama serve")
		fmt.Println("   The daemon will continue but indexing won't work.")
	} else {
		fmt.Println("✓ Ollama connected")
	}

	indexerCfg := indexer.DefaultConfig()
	if globalCfg != nil && len(globalCfg.Profiles) > 0 && globalCfg.ActiveProfile != "" {
		if profile, ok := globalCfg.Profiles[globalCfg.ActiveProfile]; ok && profile != nil {
			indexerCfg.EmbedModel = profile.EmbeddingModel
			indexerCfg.ChunkSize = profile.ChunkSize
			indexerCfg.ChunkOverlap = profile.Overlap
			// Set graph model from config if provided
			if profile.GraphDriver != nil {
				if profile.GraphDriver.Model != "" {
					indexerCfg.GraphModel = profile.GraphDriver.Model
				}
				// Set graph batch size from config if provided
				if profile.GraphDriver.BatchSize > 0 {
					indexerCfg.GraphBatchSize = profile.GraphDriver.BatchSize
				}
			}
		}
	}
	idx := indexer.NewWithSeparateClients(indexerCfg, database, embeddingClient, graphClient)
	slog.Info("Indexer initialized", "model", indexerCfg.EmbedModel, "chunk_size", indexerCfg.ChunkSize, "graph_batch_size", indexerCfg.GraphBatchSize)

	indexWorker := worker.NewIndexWorker(&worker.Config{
		DB:           database,
		Indexer:      idx,
		PollInterval: 5 * time.Second,
		BatchSize:    5,
		MaxRetries:   3,
	})

	if err := database.ResetStuckProcessing(); err != nil {
		slog.Warn("Failed to reset stuck processing state", "error", err)
	}

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	go func() {
		if err := indexWorker.Start(workerCtx); err != nil && err != context.Canceled {
			slog.Error("Index worker error", "error", err)
		}
	}()

	// Start graph extraction worker
	if indexerCfg.EnableGraphMode {
		graphWorker := indexer.NewGraphWorker(idx, indexer.GraphWorkerConfig{
			PollInterval: 5 * time.Second,
			BatchSize:    150, // Fetch up to 150 chunks to process
			Concurrency:  1,   // Process 1 batch at a time (can be configured later)
		})

		go func() {
			graphWorker.Start(workerCtx)
		}()
	}

	fileFilter := createDaemonFileFilter(globalCfg)

	fw, err := watcher.New(&watcher.Config{
		DebounceDelay: time.Second,
		OnChange: func(path string) {
			if !fileFilter(path) {
				return
			}

			content, err := os.ReadFile(path)
			if err != nil {
				slog.Debug("Failed to read file for queueing", "path", path, "error", err)
				return
			}

			info, err := os.Stat(path)
			if err != nil {
				slog.Debug("Failed to stat file for queueing", "path", path, "error", err)
				return
			}

			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			modTime := info.ModTime().Unix()

			if err := database.MarkFilePending(path, modTime, hash); err != nil {
				slog.Error("Failed to queue file", "path", path, "error", err)
				return
			}
			slog.Debug("Queued file for indexing", "path", path)
		},
	})
	if err != nil {
		slog.Error("Failed to create watcher", "error", err)
		os.Exit(1)
	}
	defer fw.Close()

	watchedCount := 0
	if globalCfg != nil && len(globalCfg.Profiles) > 0 && globalCfg.ActiveProfile != "" {
		if profile, ok := globalCfg.Profiles[globalCfg.ActiveProfile]; ok && profile != nil {
			for _, includePath := range profile.Include {
				absPath, err := filepath.Abs(includePath)
				if err != nil {
					slog.Warn("Skipping invalid path", "path", includePath, "error", err)
					continue
				}

				 filepath.Walk(includePath, func(absolutePath, d fs.DirEntry) {
					info, err := d.Info()
					if err != nil {
						slog.Warm("Could not stat","path", absolutePath, "error", err)
					} else if d.IsDir() and absolutePath {
						if err := fw.Watch(absPath); err != nil {
							slog.Warn("Failed to watch directory", "path", absPath, "error", err)
						} else {
							watchedCount++
							fmt.Printf("👁️  Watching: %s\n", absPath)
						}
					}
				}
			}
		}
	}

	if watchedCount == 0 {
		slog.Warn("⚠️  No directories being watched. Update config.yaml with include paths.")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	go func() {
		if err := fw.Start(watchCtx); err != nil && err != context.Canceled {
			slog.Error("Watcher error", "error", err)
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	fmt.Println()
	fmt.Println("✅ Daemon running. Press Ctrl+C to stop.")
	fmt.Println()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n🛑 Shutting down gracefully...")
			watchCancel()
			return

		case <-ticker.C:
			fileCount, _ := database.CountFiles()
			chunkCount, err := database.CountChunks()
			if err != nil {
				slog.Error("Failed to count chunks", "error", err)
			}
			edgeCount, err := database.CountEdges()
			if err != nil {
				slog.Error("Failed to count edges", "error", err)
			}
			statusCounts, _ := database.CountFilesByStatus()

			indexed := statusCounts["indexed"]
			pending := statusCounts["pending"]
			processing := statusCounts["processing"]
			failed := statusCounts["failed"]

			fmt.Printf("📊 Status: %d files (%d indexed, %d pending", fileCount, indexed, pending)
			if processing > 0 {
				fmt.Printf(", %d processing", processing)
			}
			if failed > 0 {
				fmt.Printf(", %d failed", failed)
			}
			fmt.Printf("), %d chunks, %d edges\n", chunkCount, edgeCount)
		}
	}
}

func handleDaemonStatus() {
	dbPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "chainsaw.db")

	database, err := db.Open(db.Config{
		Path:         dbPath,
		SkipVecTable: false,
		EmbeddingDim: 768,
	})
	if err != nil {
		fmt.Printf("❌ Database not available: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.HealthCheck(); err != nil {
		fmt.Printf("❌ Health check failed: %v\n", err)
		os.Exit(1)
	}

	fileCount, _ := database.CountFiles()
	chunkCount, _ := database.CountChunks()
	edgeCount, _ := database.CountEdges()
	statusCounts, _ := database.CountFilesByStatus()

	fmt.Println("Chainsaw Daemon Status")
	fmt.Println("======================")
	fmt.Printf("Database: %s\n", dbPath)
	fmt.Println()

	// File statistics with status breakdown
	fmt.Println("Files:")
	fmt.Printf("  Total:      %d\n", fileCount)
	if indexed, ok := statusCounts["indexed"]; ok && indexed > 0 {
		fmt.Printf("  Indexed:    %d\n", indexed)
	}
	if pending, ok := statusCounts["pending"]; ok && pending > 0 {
		fmt.Printf("  Pending:    %d\n", pending)
	}
	if processing, ok := statusCounts["processing"]; ok && processing > 0 {
		fmt.Printf("  Processing: %d\n", processing)
	}
	if failed, ok := statusCounts["failed"]; ok && failed > 0 {
		fmt.Printf("  Failed:     %d\n", failed)
	}

	fmt.Println()
	fmt.Printf("Chunks:       %d\n", chunkCount)
	fmt.Printf("Graph edges:  %d\n", edgeCount)
	fmt.Println()
	fmt.Printf("Embedding dim:  %d\n", database.EmbeddingDim())

	schemaVer, _ := database.GetMeta("schema_version")
	fmt.Printf("Schema version: %s\n", schemaVer)
}

func createDefaultDaemonConfig() *config.GlobalConfig {
	return &config.GlobalConfig{
		Version:       "2.0",
		ActiveProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Include:        []string{"."},
				Exclude:        []string{"node_modules", ".git", "vendor"},
				Whitelist:      []string{"**/*.go", "**/*.js", "**/*.py"},
				EmbeddingModel: "nomic-embed-text",
				ChunkSize:      512,
				Overlap:        64,
			},
		},
	}
}

func createDaemonFileFilter(cfg *config.GlobalConfig) func(string) bool {
	if cfg == nil || len(cfg.Profiles) == 0 {
		return func(string) bool { return true }
	}

	profile := cfg.Profiles[cfg.ActiveProfile]
	if profile == nil {
		return func(string) bool { return true }
	}

	slog.Debug("Filter configured", "blacklist_count", len(profile.Blacklist), "whitelist_count", len(profile.Whitelist))
	for i, pattern := range profile.Blacklist {
		slog.Debug("Blacklist pattern", "index", i, "pattern", pattern)
	}

	return func(path string) bool {
		dirOk, _ := filter.ShouldIndexDirectory(filepath.Dir(path), profile.Include, profile.Exclude)
		if !dirOk {
			return false
		}

		fileOk, _ := filter.ShouldIndexFile(path, profile.Blacklist, profile.Whitelist)
		if !fileOk {
			slog.Debug("Filtered out file", "path", path, "reason", "blacklist matched")
		}
		return fileOk
	}
}

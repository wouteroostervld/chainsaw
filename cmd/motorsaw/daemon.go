package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wouteroostervld/motorsaw/pkg/chunker"
	"github.com/wouteroostervld/motorsaw/pkg/config"
	"github.com/wouteroostervld/motorsaw/pkg/db"
	"github.com/wouteroostervld/motorsaw/pkg/filter"
	"github.com/wouteroostervld/motorsaw/pkg/indexer"
	"github.com/wouteroostervld/motorsaw/pkg/watcher"
	"github.com/wouteroostervld/motorsaw/pkg/worker"
)

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
	fmt.Println("motorsaw daemon - Background indexing daemon")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  motorsaw daemon start [-debug] [-loglevel level] [-scan=false] [-scan-kinds kinds]")
	fmt.Println("                        Start the daemon")
	fmt.Println("  motorsaw daemon status")
	fmt.Println("                        Show daemon status")
	fmt.Println()
	fmt.Println("Flags for 'start' command:")
	fmt.Println("  -debug          Enable debug logging")
	fmt.Println("  -loglevel LEVEL Set log level: debug, info, warn, error (default: info)")
	fmt.Println("  -scan=false     Do not bring the index up to date with the watched")
	fmt.Println("                  directories at startup; only index what changes from now on")
	fmt.Println("  -scan-kinds K   The kinds of index the startup scan fills, comma-separated")
	fmt.Println("                  (graph, embeddings; default: graph)")
	fmt.Println()
	fmt.Println("The scan flags override daemon.scan and daemon.scan_kinds of the configuration.")
}

func handleDaemonStart() {
	startFlags := flag.NewFlagSet("daemon-start", flag.ExitOnError)
	debug := startFlags.Bool("debug", false, "Enable debug logging")
	logLevelStr := startFlags.String("loglevel", "info", "Log level (debug, info, warn, error)")

	scan := startFlags.Bool("scan", true, "Bring the index up to date at startup")
	scanKinds := startFlags.String("scan-kinds", "", "Kinds of index the startup scan fills")

	startFlags.Parse(os.Args[3:])

	level := slog.LevelInfo
	switch {
	case *debug:
		level = slog.LevelDebug
	case *logLevelStr == "debug":
		level = slog.LevelDebug
	case *logLevelStr == "warn" || *logLevelStr == "warning":
		level = slog.LevelWarn
	case *logLevelStr == "error":
		level = slog.LevelError
	case *logLevelStr != "info":
		fmt.Fprintf(os.Stderr, "Unknown log level: %s (known: debug, info, warn, error)\n", *logLevelStr)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	dbPath := db.DefaultPath()
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	configPath := filepath.Join(os.Getenv("HOME"), ".motorsaw", "config.yaml")
	fs := &config.RealFileSystem{}
	globalCfg, err := config.LoadGlobalConfigFromPath(configPath, fs)
	if err != nil {
		slog.Warn("Configuration not loaded, using defaults", "config", configPath, "error", err)
		globalCfg = createDefaultDaemonConfig()
	}
	slog.Info("Daemon starting", "database", dbPath, "config", configPath)

	// Every kind of index that is switched on for a file is kept for it
	st, err := loadSettings()
	if err != nil {
		slog.Error("Configuration cannot be used", "error", err)
		os.Exit(1)
	}
	idx, err := st.indexer(database)
	if err != nil {
		slog.Error("Configuration cannot be used", "error", err)
		os.Exit(1)
	}
	if err := idx.Ready([]string{config.IndexEmbeddings}); err != nil {
		slog.Info("Not making embeddings, no model is configured")
	}

	// What to scan for at startup: the configuration, unless a flag says otherwise
	scanFor, err := st.profile.DaemonScan()
	if err != nil {
		slog.Error("Configuration cannot be used", "setting", "daemon.scan_kinds", "error", err)
		os.Exit(1)
	}
	startFlags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "scan":
			if !*scan {
				scanFor = nil
			} else if scanFor == nil {
				scanFor = []string{config.IndexGraph}
			}
		case "scan-kinds":
			if scanFor, err = config.CheckIndexKinds(strings.Split(*scanKinds, ",")); err != nil {
				slog.Error("Flag cannot be used", "flag", "-scan-kinds", "error", err)
				os.Exit(1)
			}
		}
	})
	if !*scan {
		scanFor = nil
	}

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

	fileFilter := createDaemonFileFilter(globalCfg)

	fw, err := watcher.New(&watcher.Config{
		DebounceDelay: time.Second,
		OnChange: func(path string) {
			// A project may have switched a kind of index on or off
			if filepath.Base(path) == ".config.yaml" {
				st.kinds.Reset()
				return
			}

			// Only what can be parsed has anything to index
			if !chunker.Supported(path) || !fileFilter(path) {
				return
			}

			content, err := os.ReadFile(path)
			if err != nil {
				slog.Debug("Failed to read file for queueing", "file", path, "error", err)
				return
			}

			info, err := os.Stat(path)
			if err != nil {
				slog.Debug("Failed to stat file for queueing", "file", path, "error", err)
				return
			}

			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			modTime := info.ModTime().UnixNano()

			if err := database.MarkFilePending(path, modTime, hash); err != nil {
				slog.Error("Failed to queue file", "file", path, "error", err)
				return
			}
			slog.Debug("Queued file for indexing", "file", path)
		},
	})
	if err != nil {
		slog.Error("Failed to create watcher", "error", err)
		os.Exit(1)
	}
	defer fw.Close()

	watchedCount, excludedCount, unwatchedCount := 0, 0, 0
	var roots []string // The included directories that are there
	if globalCfg != nil && len(globalCfg.Profiles) > 0 && globalCfg.ActiveProfile != "" {
		if profile, ok := globalCfg.Profiles[globalCfg.ActiveProfile]; ok && profile != nil {
			for _, includePath := range profile.Include {
				absPath, err := config.NormalizePath(includePath)
				if err != nil {
					slog.Warn("Skipping invalid include path", "dir", includePath, "error", err)
					continue
				}
				if info, err := os.Stat(absPath); err == nil && info.IsDir() {
					roots = append(roots, absPath)
				}
				err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						slog.Warn("Failed to read directory", "dir", path, "error", err)
						return err
					}
					if d.IsDir() {
						for _, excludePath := range profile.Exclude {
							currentBase := filepath.Dir(path)
							if !filepath.IsAbs(excludePath) {
								excludePath = filepath.Join(currentBase, excludePath)
							}
							if path == excludePath {
								excludedCount++
								slog.Debug("Excluding directory", "dir", excludePath)
								return filepath.SkipDir
							}
						}
						if err := fw.Watch(path); err != nil {
							unwatchedCount++
							slog.Warn("Failed to watch directory", "dir", path, "error", err)
						} else {
							watchedCount++
							slog.Debug("Watching directory", "dir", path)
						}
					}
					return err
				})
			}
		}
	}

	if watchedCount == 0 {
		slog.Warn("No directories are watched, name them under include in the configuration", "config", configPath)
	} else {
		slog.Info("Watching directories", "include", roots, "watched", watchedCount, "excluded", excludedCount, "failed", unwatchedCount)
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

	// Watching only shows what changes from now on. What changed while the
	// daemon was away is found by looking, once the watches are set so that
	// nothing falls between the two.
	scanCtx, scanCancel := context.WithCancel(context.Background())
	defer scanCancel()
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		if len(scanFor) > 0 && len(roots) > 0 {
			startupScan(scanCtx, database, st, roots, scanFor)
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	slog.Info("Daemon running")

	var lastStatus string // To say it again only when it is news

	for {
		select {
		case <-sigChan:
			slog.Info("Daemon stopping")
			watchCancel()
			scanCancel()
			<-scanDone
			slog.Info("Daemon stopped")
			return

		case <-ticker.C:
			counts, err := database.Count()
			if err != nil {
				slog.Error("Failed to count what is indexed", "error", err)
				continue
			}
			var edgeCount int64
			for _, n := range counts.Edges {
				edgeCount += n
			}
			byStatus, _ := database.CountFilesByStatus()
			status := []any{
				"files", counts.Files, "indexed", byStatus["indexed"], "pending", byStatus["pending"],
				"processing", byStatus["processing"], "failed", byStatus["failed"],
				"definitions", counts.Definitions, "edges", edgeCount,
			}
			// An idle daemon has nothing new to say
			level := slog.LevelDebug
			if now := fmt.Sprint(status...); now != lastStatus {
				level, lastStatus = slog.LevelInfo, now
			}
			slog.Log(context.Background(), level, "Index status", status...)
		}
	}
}

// startupScan brings the index up to date with the included directories, as
// "motorsaw index" would: new and modified files are indexed and the index
// forgets files that are gone. It works beside the watcher and stops between
// two files when the daemon does.
func startupScan(ctx context.Context, database *db.DB, st *settings, roots []string, kinds []string) {
	slog.Info("Startup scan started", "kinds", kinds, "dirs", roots)
	start := time.Now()
	stats, err := syncIndex(database, st, roots, syncOptions{ctx: ctx, only: kinds, log: slog.Default()})
	result := []any{
		"took", time.Since(start).Round(time.Millisecond), "indexed", stats.indexed, "unchanged", stats.unchanged,
		"removed", stats.removed, "failed", stats.failed,
	}
	switch {
	case errors.Is(err, context.Canceled):
		slog.Info("Startup scan stopped", append(result, "to_go", stats.pending)...)
	case errors.Is(err, indexer.ErrNoEmbedder):
		slog.Warn("Startup scan made no embeddings, no model is configured")
	case err != nil:
		slog.Error("Startup scan failed", append(result, "error", err)...)
	default:
		slog.Info("Startup scan done", result...)
	}
}

func handleDaemonStatus() {
	handleStatus()
}

func createDefaultDaemonConfig() *config.GlobalConfig {
	return &config.GlobalConfig{
		Version:       "2.0",
		ActiveProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Include: []string{"."},
				Exclude: []string{"node_modules", ".git", "vendor"},
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
			slog.Debug("Filtered out file", "file", path, "reason", "blacklist matched")
		}
		return fileOk
	}

}

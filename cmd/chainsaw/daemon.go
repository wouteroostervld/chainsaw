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
	"syscall"
	"time"

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/db"
	"github.com/wouteroostervld/chainsaw/pkg/filter"
	"github.com/wouteroostervld/chainsaw/pkg/watcher"
	"github.com/wouteroostervld/chainsaw/pkg/worker"
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

	logLevel := new(slog.LevelVar)
	logLevel.Set(slog.LevelError)
	if *debug {
		logLevel.Set(slog.LevelDebug)
	} else {
		logLevel.Set(slog.LevelError)
		if logLevelStr != nil {
			switch {
			case *logLevelStr == "debug":
				logLevel.Set(slog.LevelDebug)
			case *logLevelStr == "info":
				logLevel.Set(slog.LevelInfo)
			case *logLevelStr == "warn" || *logLevelStr == "warning":
				logLevel.Set(slog.LevelWarn)
			}
		}
	}

	opts := &slog.HandlerOptions{Level: logLevel}
	handler := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(handler))
	fmt.Println("🚀 Starting Chainsaw daemon...")

	dbPath := db.DefaultPath()
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

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
		slog.Info("Not making embeddings: no model is configured")
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
				slog.Debug("Failed to read file for queueing", "path", path, "error", err)
				return
			}

			info, err := os.Stat(path)
			if err != nil {
				slog.Debug("Failed to stat file for queueing", "path", path, "error", err)
				return
			}

			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			modTime := info.ModTime().UnixNano()

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
				err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						slog.Warn("Could not stat", "path", path, "error", err)
						return err
					}
					if d.IsDir() {
						for _, excludePath := range profile.Exclude {
							currentBase := filepath.Dir(path)
							if !filepath.IsAbs(excludePath) {
								excludePath = filepath.Join(currentBase, excludePath)
							}
							if path == excludePath {
								slog.Info("Excluding directory", "path", excludePath)
								return filepath.SkipDir
							}
						}
						if err := fw.Watch(path); err != nil {
							slog.Warn("Failed to watch directory", "path", path, "error", err)
						} else {
							watchedCount++
							slog.Info("👁️  Watching:", "path", path)
						}
					}
					return err
				})
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
			counts, err := database.Count()
			if err != nil {
				slog.Error("Failed to count", "error", err)
				continue
			}
			var edgeCount int64
			for _, n := range counts.Edges {
				edgeCount += n
			}
			fileCount := counts.Files
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
			fmt.Printf("), %d definitions, %d edges\n", counts.Definitions, edgeCount)
		}
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
			slog.Debug("Filtered out file", "path", path, "reason", "blacklist matched")
		}
		return fileOk
	}

}

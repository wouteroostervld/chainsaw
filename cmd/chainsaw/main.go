package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wouteroostervld/chainsaw/pkg/config"
	"github.com/wouteroostervld/chainsaw/pkg/db"
)

const version = "0.2.0"

const usage = `Usage: chainsaw <command>

Looking at code:
  grep <pattern> [path...]   Search with a regular expression; shows the definition around each match
  outline <file>             Show a file folded to its signatures
  query <cypher>             Query the code graph: definitions, calls, imports

Keeping the index:
  index <path>               Index the source files under a path
  status                     Show what is in the index
  daemon start               Watch for changes and keep the index current

Run a command with --help for its options.`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "grep":
		handleGrep(os.Args[2:])
	case "outline":
		handleOutline(os.Args[2:])
	case "query":
		handleQuery(os.Args[2:])
	case "graph":
		// "graph query" is the older spelling of "query"
		if len(os.Args) > 2 && os.Args[2] == "query" {
			handleQuery(os.Args[3:])
		} else {
			handleQuery([]string{"--help"})
		}
	case "index":
		handleIndex(os.Args[2:])
	case "status":
		handleStatus()
	case "daemon":
		handleDaemon()
	case "version":
		fmt.Printf("chainsaw version %s\n", version)
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n%s\n", os.Args[1], usage)
		os.Exit(1)
	}
}

// fail prints an error and exits
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

// openDB opens the index
func openDB() *db.DB {
	database, err := db.Open(db.Config{Path: db.DefaultPath()})
	if err != nil {
		fail("opening the index: %v", err)
	}
	return database
}

// workingDir returns the absolute current directory, or an empty string
func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	return abs
}

// activeProfile returns the active profile of the user's configuration, or
// defaults when there is none
func activeProfile() *config.Profile {
	configPath := filepath.Join(os.Getenv("HOME"), ".chainsaw", "config.yaml")
	cfg, err := config.LoadGlobalConfigFromPath(configPath, &config.RealFileSystem{})
	if err == nil {
		if profile := cfg.Profiles[cfg.ActiveProfile]; profile != nil {
			return profile
		}
	}
	return &config.Profile{Exclude: []string{"node_modules", "vendor", "__pycache__"}}
}

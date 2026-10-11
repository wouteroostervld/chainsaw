package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The kinds of index
const (
	// IndexGraph is the code graph: definitions, calls and imports, read from
	// the syntax tree
	IndexGraph = "graph"
	// IndexEmbeddings is a vector per chunk of code, for search by meaning.
	// It needs a model and is slow to build.
	IndexEmbeddings = "embeddings"
)

// DefaultIndex returns which kinds of index are kept when nothing is
// configured: everything that costs nothing but parsing
func DefaultIndex() map[string]bool {
	return map[string]bool{
		IndexGraph:      true,
		IndexEmbeddings: false,
	}
}

// EmbeddingSettings returns where embeddings come from, with defaults filled
// in. The model is empty when none is configured.
func (p *Profile) EmbeddingSettings() EmbeddingsConfig {
	var e EmbeddingsConfig
	if p.Embeddings != nil {
		e = *p.Embeddings
	}
	if e.Model == "" {
		e.Model = p.EmbeddingModel
	}
	if e.Provider == "" {
		e.Provider = "ollama"
		if e.APIKey != "" || strings.HasSuffix(strings.TrimRight(e.BaseURL, "/"), "/v1") {
			e.Provider = "openai"
		}
	}
	if e.BaseURL == "" && e.Provider == "ollama" {
		e.BaseURL = "http://localhost:11434"
	}
	return e
}

// DaemonScan returns the kinds of index the daemon brings up to date when it
// starts, and nil when it is not to scan at all
func (p *Profile) DaemonScan() ([]string, error) {
	d := p.Daemon
	if d == nil {
		d = &DaemonConfig{}
	}
	if d.Scan != nil && !*d.Scan {
		return nil, nil
	}
	if len(d.ScanKinds) == 0 {
		return []string{IndexGraph}, nil
	}
	return CheckIndexKinds(d.ScanKinds)
}

// CheckIndexKinds returns the given kinds of index, or an error naming the
// first that does not exist
func CheckIndexKinds(kinds []string) ([]string, error) {
	checked := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		kind = strings.TrimSpace(kind)
		if _, known := DefaultIndex()[kind]; !known {
			return nil, fmt.Errorf("unknown kind of index %q (known: %s, %s)", kind, IndexGraph, IndexEmbeddings)
		}
		checked = append(checked, kind)
	}
	return checked, nil
}

// IndexResolver tells which kinds of index are on for a file: the defaults,
// as changed by the profile, as changed by the nearest .config.yaml above
// the file.
type IndexResolver struct {
	fs   FileSystem
	base map[string]bool

	mu   sync.Mutex
	dirs map[string]map[string]bool
}

// NewIndexResolver creates a resolver for a profile
func NewIndexResolver(profile *Profile, fs FileSystem) (*IndexResolver, error) {
	base, err := overlayIndex(DefaultIndex(), profile.Index)
	if err != nil {
		return nil, err
	}
	return &IndexResolver{fs: fs, base: base, dirs: make(map[string]map[string]bool)}, nil
}

// For returns the kinds of index that are on for a file. The result is
// shared and must not be changed.
func (r *IndexResolver) For(path string) (map[string]bool, error) {
	return r.ForDir(filepath.Dir(path))
}

// ForDir returns the kinds of index that are on for the files in a directory
func (r *IndexResolver) ForDir(dir string) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resolve(dir)
}

// Reset forgets what was read from .config.yaml files, so that they are read
// again
func (r *IndexResolver) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = make(map[string]map[string]bool)
}

func (r *IndexResolver) resolve(dir string) (map[string]bool, error) {
	if kinds, ok := r.dirs[dir]; ok {
		return kinds, nil
	}

	var kinds map[string]bool
	localPath := filepath.Join(dir, ".config.yaml")
	if _, err := r.fs.Stat(localPath); err == nil {
		local, err := LoadLocalConfigWithFS(localPath, r.fs)
		if err != nil {
			return nil, err
		}
		if kinds, err = overlayIndex(r.base, local.Index); err != nil {
			return nil, fmt.Errorf("%s: %w", localPath, err)
		}
	} else if parent := filepath.Dir(dir); parent != dir {
		if kinds, err = r.resolve(parent); err != nil {
			return nil, err
		}
	} else {
		kinds = r.base
	}

	r.dirs[dir] = kinds
	return kinds, nil
}

// overlayIndex returns base with the given switches applied
func overlayIndex(base, switches map[string]bool) (map[string]bool, error) {
	if len(switches) == 0 {
		return base, nil
	}
	kinds := make(map[string]bool, len(base))
	for name, on := range base {
		kinds[name] = on
	}
	for name, on := range switches {
		if _, ok := base[name]; !ok {
			known := make([]string, 0, len(base))
			for name := range base {
				known = append(known, name)
			}
			sort.Strings(known)
			return nil, fmt.Errorf("unknown kind of index %q (known: %s)", name, strings.Join(known, ", "))
		}
		kinds[name] = on
	}
	return kinds, nil
}

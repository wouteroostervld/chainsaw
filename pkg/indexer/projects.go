package indexer

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wouteroostervld/chainsaw/pkg/config"
)

// Projects tells which project a file is in, and where the references of a
// project may lead outside it. A reference is matched with the definitions of
// its own project; an import can also lead into the directories the project
// follows.
type Projects struct {
	// The included directories. A directory right below one is a project
	// when nothing else marks one.
	Include []string
	// The follow setting of the project in a directory; nil for none
	Follow func(root string) ([]string, error)

	goDirs sync.Once
	goEnv  []string
}

// Root returns the directory of the project a file is in: the nearest one
// above it that holds a repository or a Python virtual environment, which is
// a project of its own wherever it is. A file in neither is in the directory
// right below the included directory it is under, or else in its own. A
// project is never above an included directory: a home directory that is
// itself in version control does not make one project of all that is in it.
func (p *Projects) Root(filePath string) string {
	include, rel := p.included(filePath)
	for dir := filepath.Dir(filePath); ; {
		for _, marker := range []string{"pyvenv.cfg", ".git", ".hg", ".svn"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if dir == include || parent == dir {
			break
		}
		dir = parent
	}

	if include == "" {
		return filepath.Dir(filePath)
	}
	if first, _, below := strings.Cut(rel, string(filepath.Separator)); below {
		return filepath.Join(include, first)
	}
	return include
}

// included returns the included directory a file is under, and the path of
// the file from there. Both are empty for a file under none.
func (p *Projects) included(filePath string) (include, rel string) {
	if p == nil {
		return "", ""
	}
	for _, dir := range p.Include {
		dir = filepath.Clean(dir)
		rel, err := filepath.Rel(dir, filePath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return dir, rel
		}
	}
	return "", ""
}

// Follows returns the directories the project in root follows its imports
// into, sorted. Places that are not there are left out.
func (p *Projects) Follows(root string) ([]string, error) {
	if p == nil || p.Follow == nil {
		return nil, nil
	}
	follow, err := p.Follow(root)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var dirs []string
	add := func(dir string) {
		if dir == "" || dir == root || seen[dir] {
			return
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	for _, entry := range follow {
		switch entry {
		case config.FollowVenv:
			add(venv(root))
		case config.FollowGo:
			p.goDirs.Do(func() { p.goEnv = goLibraries() })
			for _, dir := range p.goEnv {
				add(dir)
			}
		case config.FollowHeaders:
			add("/usr/include")
			add("/usr/local/include")
		default:
			if rest, ok := strings.CutPrefix(entry, "~/"); ok {
				entry = filepath.Join(os.Getenv("HOME"), rest)
			}
			add(filepath.Clean(entry))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// venv returns the Python virtual environment of a project: the one in its
// directory, or else the one that is active.
func venv(root string) string {
	for _, name := range []string{".venv", "venv", "env"} {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, "pyvenv.cfg")); err == nil {
			return dir
		}
	}
	return os.Getenv("VIRTUAL_ENV")
}

// goLibraries returns where Go keeps the standard library and the modules a
// build depends on
func goLibraries() []string {
	out, err := exec.Command("go", "env", "GOROOT", "GOMODCACHE").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var dirs []string
	if len(lines) > 0 && lines[0] != "" {
		dirs = append(dirs, filepath.Join(lines[0], "src"))
	}
	if len(lines) > 1 && lines[1] != "" {
		dirs = append(dirs, lines[1])
	}
	return dirs
}

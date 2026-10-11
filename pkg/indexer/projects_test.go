package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wouteroostervld/chainsaw/pkg/config"
)

// tree creates the named files, empty, below a new directory
func tree(t *testing.T, files ...string) string {
	t.Helper()
	base := t.TempDir()
	for _, file := range files {
		path := filepath.Join(base, file)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}
	return base
}

func TestProjects_Root(t *testing.T) {
	base := tree(t,
		"src/repo/.git/HEAD",
		"src/repo/pkg/a.go",
		"src/repo/.venv/pyvenv.cfg",
		"src/repo/.venv/lib/site-packages/requests/api.py",
		"src/tarball/lib/b.c",
		"src/loose.py",
		"elsewhere/dir/c.py",
		".svn/entries", // Above the included directory: not what makes a project
	)
	projects := &Projects{Include: []string{filepath.Join(base, "src")}}

	tests := []struct {
		name string
		file string
		want string
	}{
		{"in a repository", "src/repo/pkg/a.go", "src/repo"},
		{"a virtual environment is a project of its own", "src/repo/.venv/lib/site-packages/requests/api.py", "src/repo/.venv"},
		{"no repository: the directory below the included one", "src/tarball/lib/b.c", "src/tarball"},
		{"right in the included directory", "src/loose.py", "src"},
		{"not included: the repository it is in", "elsewhere/dir/c.py", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := projects.Root(filepath.Join(base, tt.file)), filepath.Join(base, tt.want); got != want {
				t.Errorf("Root = %s, want %s", got, want)
			}
		})
	}

	// Without settings a file is still in a project: its directory, when no
	// repository is above it
	loose := tree(t, "dir/sub/d.py")
	var none *Projects
	if got, want := none.Root(filepath.Join(loose, "dir/sub/d.py")), filepath.Join(loose, "dir/sub"); got != want {
		t.Errorf("Root without settings = %s, want %s", got, want)
	}
}

func TestProjects_Follows(t *testing.T) {
	base := tree(t,
		"repo/.git/HEAD",
		"repo/venv/pyvenv.cfg",
		"plain/main.py",
		"active/pyvenv.cfg",
		"libs/x.py",
	)
	repo, plain := filepath.Join(base, "repo"), filepath.Join(base, "plain")
	t.Setenv("VIRTUAL_ENV", filepath.Join(base, "active"))

	follow := []string{config.FollowVenv, filepath.Join(base, "libs"), filepath.Join(base, "missing")}
	projects := &Projects{Follow: func(string) ([]string, error) { return follow, nil }}

	// The environment in the project comes before the one that is active,
	// and a directory that is not there is not followed
	got, err := projects.Follows(repo)
	if err != nil {
		t.Fatalf("Follows failed: %v", err)
	}
	if want := []string{filepath.Join(base, "libs"), filepath.Join(repo, "venv")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Follows = %v, want %v", got, want)
	}

	got, err = projects.Follows(plain)
	if err != nil {
		t.Fatalf("Follows failed: %v", err)
	}
	if want := []string{filepath.Join(base, "active"), filepath.Join(base, "libs")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Follows without an environment of its own = %v, want %v", got, want)
	}

	if got, _ := (&Projects{}).Follows(repo); got != nil {
		t.Errorf("Follows with nothing set = %v, want none", got)
	}
}

package view

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wouteroostervld/chainsaw/pkg/db"
)

const source = `import lib


def run(items):
    for item in items:
        helper(item)
        lib.load(item)
    return len(items)


def helper(item):
    value = item.strip()
    value = value.lower()
    value = value + "!"
    return value


def unused():
    a = 1
    b = 2
    c = a + b
    return c
`

const libSource = `def load(item):
    data = open(item)
    text = data.read()
    data.close()
    return text
`

// finder says run calls helper in the same file and load in another
type finder struct{ dir string }

func (f finder) Callees(path string, startLine, endLine int) ([]db.Location, error) {
	if startLine != 4 {
		return nil, nil
	}
	return []db.Location{
		{Name: "helper", Type: "FUNCTION", Path: path, StartLine: 11, EndLine: 15},
		{Name: "load", Type: "FUNCTION", Path: filepath.Join(f.dir, "lib.py"), StartLine: 1, EndLine: 5},
	}, nil
}

func render(t *testing.T, mode Mode) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"main.py": source, "lib.py": libSource} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	r := &Renderer{Out: &out, Mode: mode, Finder: finder{dir}, Base: dir}
	// Two hits in run, both on lines inside it
	if err := r.Render([]Hit{{filepath.Join(dir, "main.py"), 6}, {filepath.Join(dir, "main.py"), 7}}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestRender_Body(t *testing.T) {
	got := render(t, ModeBody)
	want := "## main.py:4-8 run\n\n```python\ndef run(items):\n    for item in items:\n        helper(item)\n        lib.load(item)\n    return len(items)\n```\n\n"
	if got != want {
		t.Errorf("body view =\n%s\nwant\n%s", got, want)
	}
}

func TestRender_File(t *testing.T) {
	got := render(t, ModeFile)
	for _, want := range []string{
		"## main.py\n\n**Lines:** 4-8\n",
		"import lib\n",
		"    return len(items)\n",                      // The hit, in full
		"def helper(item): # ... chunk: lines 11-15\n", // The rest, folded
		"def unused(): # ... chunk: lines 18-22\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("file view does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "value.lower()") || strings.Contains(got, "lib.py") {
		t.Errorf("file view shows more than the hit:\n%s", got)
	}
}

func TestRender_Calls(t *testing.T) {
	got := render(t, ModeCalls)
	for _, want := range []string{
		"    return len(items)\n",     // The hit
		"    value = value.lower()\n", // What it calls in this file, in place
		"def unused(): # ... chunk: lines 18-22\n",
		"### calls load\n\n## lib.py:1-5 load\n", // What it calls elsewhere, after
		"    text = data.read()\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("calls view does not contain %q:\n%s", want, got)
		}
	}
}

func TestRender_Unparseable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (&Renderer{Out: &out, Mode: ModeFile}).Render([]Hit{{path, 2}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Lines 2 (not a file that can be folded)") {
		t.Errorf("unexpected output for a text file:\n%s", out.String())
	}
}

func TestParseMode(t *testing.T) {
	if m, err := ParseMode("calls"); err != nil || m != ModeCalls {
		t.Errorf("ParseMode(calls) = %v, %v", m, err)
	}
	if _, err := ParseMode("everything"); err == nil {
		t.Error("ParseMode accepted an unknown view")
	}
}

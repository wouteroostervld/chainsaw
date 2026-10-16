// Package view shows places in source files in their context. A place is a
// line of a file; how much of the code around it is shown is the mode:
//
//	body   the definition the line is in
//	file   the whole file folded to signatures, with those definitions in full
//	calls  as file, and also what the definitions call: in place when it is in
//	       the same file, after it when it is in another
package view

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wouteroostervld/motorsaw/pkg/chunker"
	"github.com/wouteroostervld/motorsaw/pkg/db"
)

// Mode selects how much context is shown around a hit
type Mode string

const (
	ModeBody  Mode = "body"
	ModeFile  Mode = "file"
	ModeCalls Mode = "calls"
)

// ParseMode returns the mode with the given name
func ParseMode(name string) (Mode, error) {
	switch m := Mode(name); m {
	case ModeBody, ModeFile, ModeCalls:
		return m, nil
	}
	return "", fmt.Errorf("unknown view %q: use body, file or calls", name)
}

// Hit is a line of a file that is of interest
type Hit struct {
	Path string
	Line int // 1-indexed
}

// CalleeFinder looks up what the definitions in a range of lines call
type CalleeFinder interface {
	Callees(path string, startLine, endLine int) ([]db.Location, error)
}

// Renderer writes hits in their context
type Renderer struct {
	Out    io.Writer
	Mode   Mode
	Finder CalleeFinder // Needed for ModeCalls; nil shows no callees
	Base   string       // Paths are shown relative to this directory when set

	parsed map[string]*chunker.File
}

// source is a parsed file, or nil when it cannot be read or parsed
func (r *Renderer) source(path string) *chunker.File {
	if f, ok := r.parsed[path]; ok {
		return f
	}
	if r.parsed == nil {
		r.parsed = make(map[string]*chunker.File)
	}
	var f *chunker.File
	if content, err := os.ReadFile(path); err == nil {
		f, _ = chunker.Parse(path, content, 0)
	}
	r.parsed[path] = f
	return f
}

func (r *Renderer) display(path string) string {
	if r.Base != "" {
		if rel, err := filepath.Rel(r.Base, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// Render writes the hits, grouped by file in the order the files first appear
func (r *Renderer) Render(hits []Hit) error {
	var paths []string
	byPath := make(map[string][]int)
	for _, h := range hits {
		if _, seen := byPath[h.Path]; !seen {
			paths = append(paths, h.Path)
		}
		byPath[h.Path] = append(byPath[h.Path], h.Line)
	}

	for _, path := range paths {
		f := r.source(path)
		if f == nil {
			fmt.Fprintf(r.Out, "## %s\n\nLines %s (not a file that can be folded)\n\n", r.display(path), joinInts(byPath[path]))
			continue
		}

		// The chunks the hits are in, in file order
		chunks := make(map[int]bool)
		for _, line := range byPath[path] {
			chunks[f.ChunkAt(line)] = true
		}
		order := sortedKeys(chunks)

		if r.Mode == ModeBody {
			for _, i := range order {
				r.section(path, f, i)
			}
			continue
		}

		// Elsewhere is what is called in other files, shown after this one
		var elsewhere []db.Location
		expanded := make(map[int]bool)
		for _, i := range order {
			expanded[i] = true
			if r.Mode != ModeCalls || r.Finder == nil || i == 0 {
				continue
			}
			callees, err := r.Finder.Callees(path, f.Chunks[i].StartLine, f.Chunks[i].EndLine)
			if err != nil {
				return err
			}
			for _, c := range callees {
				if c.Path == path {
					expanded[f.ChunkAt(c.StartLine)] = true
				} else {
					elsewhere = append(elsewhere, c)
				}
			}
		}

		var ranges []string
		for _, i := range order {
			ranges = append(ranges, fmt.Sprintf("%d-%d", f.Chunks[i].StartLine, f.Chunks[i].EndLine))
		}
		fmt.Fprintf(r.Out, "## %s\n\n**Lines:** %s\n\n", r.display(path), strings.Join(ranges, ", "))
		r.code(f.Language, f.Render(expanded))

		seen := make(map[string]bool)
		for _, c := range elsewhere {
			cf := r.source(c.Path)
			key := fmt.Sprintf("%s:%d", c.Path, c.StartLine)
			if cf == nil || seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(r.Out, "### calls %s\n\n", c.Name)
			r.section(c.Path, cf, cf.ChunkAt(c.StartLine))
		}
	}
	return nil
}

// section writes one chunk of a file under its own heading
func (r *Renderer) section(path string, f *chunker.File, chunk int) {
	c := f.Chunks[chunk]
	title := c.Title
	if title != "" {
		title = " " + title
	}
	fmt.Fprintf(r.Out, "## %s:%d-%d%s\n\n", r.display(path), c.StartLine, c.EndLine, title)
	r.code(f.Language, c.Content)
}

func (r *Renderer) code(language, content string) {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	fmt.Fprintf(r.Out, "```%s\n%s```\n\n", language, content)
}

func sortedKeys(set map[int]bool) []int {
	keys := make([]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}

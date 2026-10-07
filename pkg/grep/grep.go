// Package grep searches files for a regular expression and reports each
// matching line together with the definition it is in: the qualified name of
// the function, method, struct or interface around it.
//
// Patterns are Perl-compatible regular expressions (PCRE2).
package grep

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.elara.ws/pcre"

	"github.com/wouteroostervld/chainsaw/pkg/chunker"
)

// maxFileSize is the largest file that is searched
const maxFileSize = 5 << 20

// Options control a search
type Options struct {
	IgnoreCase bool
	Fixed      bool     // The pattern is a literal string, not a regular expression
	Word       bool     // The pattern must match whole words
	Exclude    []string // Names of directories to skip, besides hidden ones
}

// Line is a line that matched
type Line struct {
	Number int // 1-indexed
	Text   string
}

// Group holds the matching lines that share an enclosing definition
type Group struct {
	Symbol    string // Qualified name; empty for lines outside any definition
	Type      string // Entity type of the definition
	StartLine int    // Extent of the definition
	EndLine   int
	Lines     []Line
}

// File holds the matches in one file, grouped by definition in file order
type File struct {
	Path   string
	Groups []Group
}

// Search looks for the pattern in the given files and directories
func Search(pattern string, paths []string, opts Options) ([]File, error) {
	if opts.Fixed {
		pattern = regexp.QuoteMeta(pattern)
	}
	if opts.Word {
		pattern = `\b(?:` + pattern + `)\b`
	}
	// ^ and $ match at line ends, as grep users expect
	flags := pcre.Multiline
	if opts.IgnoreCase {
		flags |= pcre.Caseless
	}
	// The library frees a pattern when it is garbage collected; closing it
	// here as well would free it twice
	re, err := pcre.CompileOpts(pattern, flags)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	exclude := make(map[string]bool, len(opts.Exclude))
	for _, name := range opts.Exclude {
		exclude[strings.TrimSuffix(name, "/")] = true
	}

	var files []File
	search := func(path string) {
		if f, ok := searchFile(re, path); ok {
			files = append(files, f)
		}
	}

	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			// A file named on the command line is searched whatever it is
			search(root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if path != root && (strings.HasPrefix(name, ".") || exclude[name]) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() {
				search(path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// searchFile returns the matches in a file; false when there are none or the
// file is not text
func searchFile(re *pcre.Regexp, path string) (File, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxFileSize || info.Size() == 0 {
		return File{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(content[:min(len(content), 8000)], 0) >= 0 {
		return File{}, false
	}

	matches := re.FindAllIndex(content, -1)
	if len(matches) == 0 {
		return File{}, false
	}

	// The lines the matches start on
	var numbers []int
	line, offset := 1, 0
	for _, m := range matches {
		line += bytes.Count(content[offset:m[0]], []byte{'\n'})
		offset = m[0]
		if len(numbers) == 0 || numbers[len(numbers)-1] != line {
			numbers = append(numbers, line)
		}
	}
	lines := bytes.Split(content, []byte{'\n'})

	parsed, _ := chunker.Parse(path, content, 0)

	file := File{Path: path}
	groups := make(map[int]int) // Symbol index -> index into file.Groups
	for _, n := range numbers {
		symbol := -1
		if parsed != nil {
			symbol = parsed.SymbolAt(n)
		}
		g, ok := groups[symbol]
		if !ok {
			g = len(file.Groups)
			groups[symbol] = g
			group := Group{}
			if symbol >= 0 {
				s := parsed.Symbols[symbol]
				group = Group{Symbol: s.QualifiedName(), Type: s.Type, StartLine: s.StartLine, EndLine: s.EndLine}
			}
			file.Groups = append(file.Groups, group)
		}
		text := strings.TrimRight(string(lines[n-1]), "\r")
		file.Groups[g].Lines = append(file.Groups[g].Lines, Line{Number: n, Text: text})
	}

	sort.SliceStable(file.Groups, func(i, j int) bool {
		return file.Groups[i].Lines[0].Number < file.Groups[j].Lines[0].Number
	})
	return file, true
}

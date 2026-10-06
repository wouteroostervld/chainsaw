package chunker

import (
	"regexp"
	"strings"
)

var (
	atxHeading      = regexp.MustCompile(`^ {0,3}(#{1,6})(\s|$)`)
	setextUnderline = regexp.MustCompile(`^ {0,3}(=+|-+)\s*$`)
	codeFence       = regexp.MustCompile("^ {0,3}(```|~~~)")
)

// mdLine is a line of a Markdown document
type mdLine struct {
	start, end uint // Byte range, without the line break
	text       string
	level      int // Heading level when a section starts here, else 0
}

// markdownSections builds the fold tree of a Markdown document: a section
// runs from its heading to the next heading of the same or a higher level,
// and holds its subsections.
func markdownSections(b *builder) *fold {
	lines := markdownLines(b.src)

	root := &fold{end: uint(len(b.src))}
	if len(lines) > 0 {
		root.endRow = uint(len(lines) - 1)
	}

	type open struct {
		fold  *fold
		level int
	}
	stack := []open{{fold: root}}

	// closeTo ends the open sections at or below a level, at the last line
	// with text before the given row
	closeTo := func(level int, row int) {
		last := row - 1
		for last > 0 && strings.TrimSpace(lines[last].text) == "" {
			last--
		}
		for len(stack) > 1 && stack[len(stack)-1].level >= level {
			f := stack[len(stack)-1].fold
			stack = stack[:len(stack)-1]
			f.end, f.endRow = lines[last].end, uint(last)
			if f.endRow-f.startRow+1 >= minFoldLines {
				parent := stack[len(stack)-1].fold
				parent.children = append(parent.children, f)
			}
		}
	}

	for row, line := range lines {
		if line.level == 0 {
			continue
		}
		closeTo(line.level, row)
		title := strings.TrimSpace(strings.Trim(strings.TrimSpace(line.text), "#"))
		if parent := stack[len(stack)-1].fold; parent.title != "" {
			title = parent.title + " > " + title
		}
		stack = append(stack, open{
			level: line.level,
			fold: &fold{
				title:    title,
				start:    line.start,
				startRow: uint(row),
				header:   strings.TrimRight(line.text, " \t\r"),
			},
		})
	}
	closeTo(1, len(lines))

	return root
}

// markdownLines splits a document into lines and marks the headings
func markdownLines(src []byte) []mdLine {
	var lines []mdLine
	offset := uint(0)
	for _, text := range strings.SplitAfter(string(src), "\n") {
		if text == "" {
			continue
		}
		trimmed := strings.TrimRight(text, "\r\n")
		lines = append(lines, mdLine{start: offset, end: offset + uint(len(trimmed)), text: trimmed})
		offset += uint(len(text))
	}

	// Front matter is not content
	first := 0
	if len(lines) > 0 && lines[0].text == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i].text == "---" || lines[i].text == "..." {
				first = i + 1
				break
			}
		}
	}

	fence := ""
	for i := first; i < len(lines); i++ {
		text := lines[i].text
		if m := codeFence.FindStringSubmatch(text); m != nil {
			if fence == "" {
				fence = m[1]
			} else if fence == m[1] {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}

		if m := atxHeading.FindStringSubmatch(text); m != nil {
			lines[i].level = len(m[1])
			continue
		}

		// A setext heading is a single line of text, underlined
		if m := setextUnderline.FindStringSubmatch(text); m != nil && i > first {
			title := lines[i-1]
			above := i-1 == first || strings.TrimSpace(lines[i-2].text) == ""
			if above && title.level == 0 && strings.TrimSpace(title.text) != "" && !setextUnderline.MatchString(title.text) {
				lines[i-1].level = 2
				if m[1][0] == '=' {
					lines[i-1].level = 1
				}
			}
		}
	}
	return lines
}

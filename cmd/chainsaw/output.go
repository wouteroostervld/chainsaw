package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// parse reads the options of a command wherever they stand, before or after
// what the command is given, and returns what is not an option. After "--"
// nothing is an option.
func parse(flags *flag.FlagSet, args []string) []string {
	var given []string
	for {
		flags.Parse(args)
		rest := flags.Args()
		if len(rest) == 0 {
			return given
		}
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(given, rest...)
		}
		given = append(given, rest[0])
		args = rest[1:]
	}
}

// output is where a command writes what it found: standard output, with
// lines cut to a width when one is given. The function it returns writes
// what is left of the last line, and must be called before the command ends.
func output(width int) (io.Writer, func()) {
	if width <= 0 {
		return os.Stdout, func() {}
	}
	n := &narrow{out: os.Stdout, width: width}
	return n, n.flush
}

// narrow cuts the lines written through it to a width
type narrow struct {
	out   io.Writer
	width int
	line  []byte // The line that is not yet complete
}

func (n *narrow) Write(p []byte) (int, error) {
	n.line = append(n.line, p...)
	for {
		i := bytes.IndexByte(n.line, '\n')
		if i < 0 {
			return len(p), nil
		}
		if _, err := fmt.Fprintln(n.out, cut(string(n.line[:i]), n.width)); err != nil {
			return 0, err
		}
		n.line = n.line[i+1:]
	}
}

func (n *narrow) flush() {
	if len(n.line) > 0 {
		fmt.Fprint(n.out, cut(string(n.line), n.width))
		n.line = nil
	}
}

// cut shortens a line to a number of characters; the last one is then an
// ellipsis, to show that there was more
func cut(line string, width int) string {
	if utf8.RuneCountInString(line) <= width {
		return line
	}
	runes := []rune(line)
	return string(runes[:width-1]) + "…"
}

// oneLine makes a value fit on a line of its own
func oneLine(s string) string {
	return strings.NewReplacer("\n", `\n`, "\t", " ").Replace(strings.TrimRight(s, "\n"))
}

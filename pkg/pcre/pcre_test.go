package pcre

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// found returns the text of every match
func found(t *testing.T, pattern string, flags Flags, subject string) []string {
	t.Helper()
	re, err := Compile(pattern, flags)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}
	defer re.Close()
	matches, err := re.FindAllIndex([]byte(subject))
	if err != nil {
		t.Fatalf("FindAllIndex failed: %v", err)
	}
	var out []string
	for _, m := range matches {
		out = append(out, subject[m[0]:m[1]])
	}
	return out
}

func TestFindAllIndex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		flags   Flags
		subject string
		want    []string
	}{
		{"literal", `foo`, 0, "foo bar foo", []string{"foo", "foo"}},
		{"no match", `xyz`, 0, "foo bar", nil},
		{"lookbehind", `(?<=func )\w+`, 0, "func foo() and func bar()", []string{"foo", "bar"}},
		{"negative lookahead", `\bfoo(?!bar)\w*`, 0, "foobar foobaz foo", []string{"foobaz", "foo"}},
		{"backreference", `(\w)\1`, 0, "abccdeef", []string{"cc", "ee"}},
		{"possessive quantifier", `a++b`, 0, "aaab aac", []string{"aaab"}},
		{"named group", `(?<year>\d{4})-(?<month>\d\d)`, 0, "on 2026-10-07", []string{"2026-10"}},
		{"case-sensitive by default", `Foo`, 0, "foo Foo FOO", []string{"Foo"}},
		{"caseless", `foo`, Caseless, "foo Foo FOO", []string{"foo", "Foo", "FOO"}},
		{"inline flag", `(?i)foo`, 0, "Foo", []string{"Foo"}},
		{"anchors match the whole subject by default", `^\w+$`, 0, "one\ntwo", nil},
		{"multiline anchors", `^\w+$`, Multiline, "one\ntwo\nthree four", []string{"one", "two"}},
		{"word boundary", `\bopen\b`, 0, "open reopen open_file opens open", []string{"open", "open"}},
		{"unicode letters", `\p{L}+`, 0, "naïve café 42", []string{"naïve", "café"}},
		{"empty matches move on by a character", `x*`, 0, "aé", []string{"", "", ""}},
		{"empty subject", `^$`, 0, "", []string{""}},
		{"empty pattern", ``, 0, "ab", []string{"", "", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := found(t, tt.pattern, tt.flags, tt.subject); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%q on %q: got %q, want %q", tt.pattern, tt.subject, got, tt.want)
			}
		})
	}
}

func TestFindAllIndex_Offsets(t *testing.T) {
	re, err := Compile(`b+`, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	got, err := re.FindAllIndex([]byte("abbcb"))
	if want := [][2]int{{1, 3}, {4, 5}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v (%v), want %v", got, err, want)
	}
}

func TestFindAllIndex_InvalidUTF8(t *testing.T) {
	// Source files are not always valid UTF-8; that must not stop a search
	subject := "needle \xff\xfe needle \xc3"
	if got := found(t, `needle`, 0, subject); len(got) != 2 {
		t.Errorf("got %d matches around invalid bytes, want 2", len(got))
	}
}

func TestFindAllIndex_Many(t *testing.T) {
	// More matches than the first allocation holds
	subject := strings.Repeat("ab ", 5000)
	if got := found(t, `ab`, 0, subject); len(got) != 5000 {
		t.Errorf("got %d matches, want 5000", len(got))
	}
}

func TestCompile_Error(t *testing.T) {
	_, err := Compile(`(unclosed`, 0)
	if err == nil {
		t.Fatal("Compile accepted an invalid pattern")
	}
	if !strings.Contains(err.Error(), "offset 9") {
		t.Errorf("error does not say where: %v", err)
	}
}

func TestClose(t *testing.T) {
	re, err := Compile(`a`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !re.Match([]byte("cat")) {
		t.Error("Match(cat) = false")
	}
	re.Close()
	re.Close() // Closing twice is harmless
	if _, err := re.FindAllIndex([]byte("cat")); err == nil {
		t.Error("a closed pattern still matched")
	}

	// Patterns that are closed, and patterns that are dropped, are each
	// freed exactly once
	for i := 0; i < 2000; i++ {
		re, err := Compile(`\w+`, Multiline)
		if err != nil {
			t.Fatal(err)
		}
		re.Match([]byte("some text"))
		if i%2 == 0 {
			re.Close()
		}
	}
	runtime.GC()
	runtime.GC()
}

func TestVersion(t *testing.T) {
	if v := Version(); !strings.HasPrefix(v, "10.") {
		t.Errorf("Version() = %q, want a PCRE2 10.x version", v)
	}
}

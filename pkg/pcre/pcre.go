// Package pcre binds the system's PCRE2 library: Perl-compatible regular
// expressions, with lookaround, backreferences and the rest of what Go's own
// regexp package leaves out.
//
// Building needs the PCRE2 development files (pcre2-devel on Fedora,
// libpcre2-dev on Debian and Ubuntu).
package pcre

/*
#cgo pkg-config: libpcre2-8
#define PCRE2_CODE_UNIT_WIDTH 8
#include <pcre2.h>
#include <stdlib.h>

// find_all runs the pattern over the subject and collects the start and end
// offset of every match. Doing the loop here keeps it to one call from Go
// per subject. The caller frees *out.
static int find_all(pcre2_code *code, pcre2_match_data *md,
                    const unsigned char *subject, size_t length,
                    size_t **out, size_t *count) {
	size_t cap = 32, n = 0, offset = 0;
	size_t *res = malloc(cap * 2 * sizeof(size_t));
	if (res == NULL) {
		return PCRE2_ERROR_NOMEMORY;
	}

	while (offset <= length) {
		int rc = pcre2_match(code, subject, length, offset, 0, md, NULL);
		if (rc == PCRE2_ERROR_NOMATCH) {
			break;
		}
		if (rc < 0) {
			free(res);
			return rc;
		}

		PCRE2_SIZE *ovector = pcre2_get_ovector_pointer(md);
		size_t start = ovector[0], end = ovector[1];
		if (end < start) {
			// \K in a lookaround can put the start after the end
			end = start;
		}

		if (n == cap) {
			cap *= 2;
			size_t *grown = realloc(res, cap * 2 * sizeof(size_t));
			if (grown == NULL) {
				free(res);
				return PCRE2_ERROR_NOMEMORY;
			}
			res = grown;
		}
		res[2 * n] = start;
		res[2 * n + 1] = end;
		n++;

		if (end > start) {
			offset = end;
		} else {
			// After an empty match, move on by one character, not into
			// the middle of one
			offset = end + 1;
			while (offset < length && (subject[offset] & 0xC0) == 0x80) {
				offset++;
			}
		}
	}

	*out = res;
	*count = n;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// Flags change how a pattern is compiled
type Flags uint32

const (
	// Caseless makes letters match both upper and lower case
	Caseless Flags = C.PCRE2_CASELESS
	// Multiline makes ^ and $ match at the start and end of every line
	Multiline Flags = C.PCRE2_MULTILINE
)

// Regexp is a compiled pattern. It is not safe for use by several goroutines
// at once.
type Regexp struct {
	code *C.pcre2_code
	data *C.pcre2_match_data
}

// errorMessage returns PCRE2's text for an error code
func errorMessage(code C.int) string {
	var buf [256]C.PCRE2_UCHAR
	C.pcre2_get_error_message(code, &buf[0], C.PCRE2_SIZE(len(buf)))
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}

// Compile compiles a pattern. Subjects are read as UTF-8; bytes that are not
// valid UTF-8 never match but do not stop a search.
func Compile(pattern string, flags Flags) (*Regexp, error) {
	// An empty Go string has no bytes to point at
	p := []byte(pattern + "\x00")

	var errCode C.int
	var errOffset C.PCRE2_SIZE
	options := C.uint32_t(flags) | C.PCRE2_UTF | C.PCRE2_MATCH_INVALID_UTF
	code := C.pcre2_compile((*C.PCRE2_UCHAR)(unsafe.Pointer(&p[0])), C.PCRE2_SIZE(len(pattern)),
		options, &errCode, &errOffset, nil)
	if code == nil {
		return nil, fmt.Errorf("%s at offset %d", errorMessage(errCode), int(errOffset))
	}

	// Compiling to machine code is an optimisation; without it the pattern
	// is interpreted
	C.pcre2_jit_compile(code, C.PCRE2_JIT_COMPLETE)

	data := C.pcre2_match_data_create_from_pattern(code, nil)
	if data == nil {
		C.pcre2_code_free(code)
		return nil, fmt.Errorf("out of memory")
	}

	re := &Regexp{code: code, data: data}
	runtime.SetFinalizer(re, (*Regexp).Close)
	return re, nil
}

// Close frees the pattern. It may be called more than once; a pattern that is
// never closed is freed when it is garbage collected.
func (re *Regexp) Close() {
	if re.code == nil {
		return
	}
	C.pcre2_match_data_free(re.data)
	C.pcre2_code_free(re.code)
	re.code, re.data = nil, nil
	runtime.SetFinalizer(re, nil)
}

// FindAllIndex returns the start and end offset of every match in b, in
// order. Matches do not overlap.
func (re *Regexp) FindAllIndex(b []byte) ([][2]int, error) {
	if re.code == nil {
		return nil, fmt.Errorf("pattern is closed")
	}

	// C needs a pointer even when there is nothing to read
	subject := (*C.uchar)(unsafe.Pointer(&[1]byte{}))
	if len(b) > 0 {
		subject = (*C.uchar)(unsafe.Pointer(&b[0]))
	}

	var out *C.size_t
	var count C.size_t
	if rc := C.find_all(re.code, re.data, subject, C.size_t(len(b)), &out, &count); rc != 0 {
		return nil, fmt.Errorf("%s", errorMessage(rc))
	}
	defer C.free(unsafe.Pointer(out))
	runtime.KeepAlive(b)

	offsets := unsafe.Slice((*C.size_t)(out), 2*int(count))
	matches := make([][2]int, int(count))
	for i := range matches {
		matches[i] = [2]int{int(offsets[2*i]), int(offsets[2*i+1])}
	}
	return matches, nil
}

// Match reports whether the pattern matches anywhere in b
func (re *Regexp) Match(b []byte) bool {
	matches, err := re.FindAllIndex(b)
	return err == nil && len(matches) > 0
}

// Version returns the version of the PCRE2 library in use
func Version() string {
	var buf [64]C.PCRE2_UCHAR
	C.pcre2_config(C.PCRE2_CONFIG_VERSION, unsafe.Pointer(&buf[0]))
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}

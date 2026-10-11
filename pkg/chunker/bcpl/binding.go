// Package bcpl holds the tree-sitter grammar for BCPL.
//
// The grammar is tree-sitter-bcpl, which is kept in the kick_reverse
// repository and has no Go binding of its own: parser.c, scanner.c and the
// headers in tree_sitter are copied from its src directory, unchanged. To
// update, copy them again.
package bcpl

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}
// const struct TSLanguage *tree_sitter_bcpl(void);
import "C"

import "unsafe"

// Language returns the tree-sitter language for BCPL
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_bcpl())
}

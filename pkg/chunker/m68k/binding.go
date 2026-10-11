// Package m68k holds the tree-sitter grammar for Motorola 68000 assembly.
//
// The grammar is github.com/grahambates/tree-sitter-m68k, which has no Go
// binding of its own: parser.c and tree_sitter/parser.h are copied from its
// src directory at commit ab9f2ad96db56619d05a6474a52e3dbdd705a637 (v0.3.2
// and later fixes), unchanged. To update, copy the two files again.
package m68k

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}
// const struct TSLanguage *tree_sitter_m68k(void);
import "C"

import "unsafe"

// Language returns the tree-sitter language for m68k assembly
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_m68k())
}

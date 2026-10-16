// Package cobol holds the tree-sitter grammar for IBM Enterprise COBOL, with
// EXEC CICS and EXEC SQL parsed as statements.
//
// The grammar is github.com/Spantree/tree-sitter-cobol-enterprise, which has
// no Go binding of its own and does not commit its generated parser. Here
// parser.c and tree_sitter/ are made from commit
// 86a2c479cb7e299e8dc3bb9db7a346502335d21e with "tree-sitter generate --abi 14"
// (tree-sitter 0.26.11, about 15 minutes and 6 GB), and scanner.c is copied
// unchanged. To update, generate again and copy the files.
package cobol

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}
// const struct TSLanguage *tree_sitter_cobol(void);
import "C"

import "unsafe"

// Language returns the tree-sitter language for COBOL
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_cobol())
}

#include "tree_sitter/alloc.h"
#include "tree_sitter/parser.h"

#include <stdbool.h>
#include <string.h>

enum TokenType {
  BLOCK_COMMENT,
  STRING_CONTENT,
  SECTION_OPEN,
  SECTION_CLOSE,
  IMPLICIT_SECTION_CLOSE,
  SL_APPLY,
  SL_VEC,
  SL_MUL,
  SL_ADD,
  SL_REL,
  SL_SHIFT,
  SL_AND,
  SL_OR,
  SL_EQV,
  SL_COND,
  ERROR_SENTINEL,
};

#define NO_GUARD ERROR_SENTINEL

#define MAX_TAG 31
#define STACK_SIZE (TREE_SITTER_SERIALIZATION_BUFFER_SIZE - 2)

// The tags of the open section brackets, innermost last. Each entry is the tag
// text followed by its length, so the top can be popped without a scan.
// Sections that no longer fit are only counted.
typedef struct {
  uint16_t length;
  uint16_t overflow;
  char tags[STACK_SIZE];
} Scanner;

void *tree_sitter_bcpl_external_scanner_create(void) { return ts_calloc(1, sizeof(Scanner)); }

void tree_sitter_bcpl_external_scanner_destroy(void *payload) { ts_free(payload); }

unsigned tree_sitter_bcpl_external_scanner_serialize(void *payload, char *buffer) {
  Scanner *scanner = payload;
  buffer[0] = (char)(scanner->overflow & 0xff);
  buffer[1] = (char)(scanner->overflow >> 8);
  memcpy(buffer + 2, scanner->tags, scanner->length);
  return scanner->length + 2;
}

void tree_sitter_bcpl_external_scanner_deserialize(void *payload, const char *buffer, unsigned length) {
  Scanner *scanner = payload;
  scanner->length = 0;
  scanner->overflow = 0;
  if (length < 2) return;
  scanner->overflow = (uint16_t)((uint8_t)buffer[0] | ((uint8_t)buffer[1] << 8));
  scanner->length = (uint16_t)(length - 2);
  memcpy(scanner->tags, buffer + 2, scanner->length);
}

static void push_tag(Scanner *scanner, const char *tag, unsigned length) {
  if (scanner->overflow > 0 || scanner->length + length + 1 > STACK_SIZE) {
    scanner->overflow++;
    return;
  }
  memcpy(scanner->tags + scanner->length, tag, length);
  scanner->length += length;
  scanner->tags[scanner->length++] = (char)length;
}

static void pop_tag(Scanner *scanner) {
  if (scanner->overflow > 0) scanner->overflow--;
  else if (scanner->length > 0) scanner->length -= (uint16_t)(scanner->tags[scanner->length - 1] + 1);
}

// Whether `tag` names an open section, and if so whether it is the innermost.
static bool find_tag(const Scanner *scanner, const char *tag, unsigned length, bool *innermost) {
  if (scanner->overflow > 0) return false;
  unsigned end = scanner->length;
  *innermost = true;
  while (end > 0) {
    unsigned entry = (unsigned char)scanner->tags[end - 1];
    if (entry == length && !memcmp(scanner->tags + end - 1 - entry, tag, length)) return true;
    end -= entry + 1;
    *innermost = false;
  }
  return false;
}

static inline void advance(TSLexer *lexer) { lexer->advance(lexer, false); }
static inline void skip(TSLexer *lexer) { lexer->advance(lexer, true); }

static inline bool is_tag_char(int32_t c) {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_';
}

static unsigned read_tag(TSLexer *lexer, char *tag) {
  unsigned length = 0;
  while (is_tag_char(lexer->lookahead)) {
    if (length < MAX_TAG) tag[length++] = (char)lexer->lookahead;
    advance(lexer);
  }
  return length;
}

// The lookahead is `{`, `}`, or the `(` or `)` after a `$`.
static bool scan_section_bracket(Scanner *scanner, TSLexer *lexer, const bool *valid_symbols) {
  char tag[MAX_TAG];
  bool dollar = lexer->lookahead == '(' || lexer->lookahead == ')';
  bool open = lexer->lookahead == '(' || lexer->lookahead == '{';
  advance(lexer);
  unsigned length = dollar ? read_tag(lexer, tag) : 0;

  if (open) {
    if (!valid_symbols[SECTION_OPEN]) return false;
    push_tag(scanner, tag, length);
    lexer->mark_end(lexer);
    lexer->result_symbol = SECTION_OPEN;
    return true;
  }

  bool innermost;
  if (length > 0 && find_tag(scanner, tag, length, &innermost) && !innermost) {
    if (!valid_symbols[IMPLICIT_SECTION_CLOSE]) return false;
    pop_tag(scanner);
    lexer->result_symbol = IMPLICIT_SECTION_CLOSE;
    return true;
  }
  if (!valid_symbols[SECTION_CLOSE]) return false;
  pop_tag(scanner);
  lexer->mark_end(lexer);
  lexer->result_symbol = SECTION_CLOSE;
  return true;
}

// Everything in a string up to the closing quote or the next `*` escape. Lexed
// here so that `//` and `/*` inside a string are not taken for comments.
static bool scan_string_content(TSLexer *lexer) {
  bool has_content = false;
  while (!lexer->eof(lexer) && lexer->lookahead != '"' && lexer->lookahead != '*' && lexer->lookahead != '\n') {
    advance(lexer);
    has_content = true;
  }
  lexer->result_symbol = STRING_CONTENT;
  return has_content;
}

// The opening `/*` has been consumed. Block comments nest.
static void scan_block_comment(TSLexer *lexer) {
  unsigned depth = 1;
  while (depth > 0 && !lexer->eof(lexer)) {
    if (lexer->lookahead == '*') {
      advance(lexer);
      if (lexer->lookahead == '/') {
        advance(lexer);
        depth--;
      }
    } else if (lexer->lookahead == '/') {
      advance(lexer);
      if (lexer->lookahead == '*') {
        advance(lexer);
        depth++;
      }
    } else {
      advance(lexer);
    }
  }
}

// An operator directly followed by `:=` is an assignment operator, not infix.
static enum TokenType unless_assign(TSLexer *lexer, enum TokenType guard) {
  if (lexer->lookahead != ':') return guard;
  advance(lexer);
  return lexer->lookahead == '=' ? NO_GUARD : guard;
}

static enum TokenType word_operator(TSLexer *lexer, bool flt) {
  char word[8];
  unsigned length = 0;
  while (is_tag_char(lexer->lookahead)) {
    if (length == sizeof(word) - 1) return NO_GUARD;
    word[length++] = (char)lexer->lookahead;
    advance(lexer);
  }
  word[length] = 0;

  if (!strcmp(word, "MOD")) return unless_assign(lexer, SL_MUL);
  if (flt) return NO_GUARD;
  if (!strcmp(word, "REM")) return unless_assign(lexer, SL_MUL);
  if (!strcmp(word, "OF")) return SL_VEC;
  if (!strcmp(word, "EQ") || !strcmp(word, "NE") || !strcmp(word, "LS") ||
      !strcmp(word, "LE") || !strcmp(word, "GR") || !strcmp(word, "GE")) return SL_REL;
  if (!strcmp(word, "LSHIFT") || !strcmp(word, "RSHIFT")) return unless_assign(lexer, SL_SHIFT);
  if (!strcmp(word, "LOGAND")) return unless_assign(lexer, SL_AND);
  if (!strcmp(word, "LOGOR")) return unless_assign(lexer, SL_OR);
  if (!strcmp(word, "EQV") || !strcmp(word, "XOR") || !strcmp(word, "NEQV")) return unless_assign(lexer, SL_EQV);
  return NO_GUARD;
}

// Which precedence level the infix operator at the lookahead belongs to. `flt`
// is set once a `#` prefix (floating point operator) has been consumed.
static enum TokenType infix_operator(TSLexer *lexer, bool flt) {
  switch (lexer->lookahead) {
    case '*':
      advance(lexer);
      return unless_assign(lexer, SL_MUL);
    case '/':
      advance(lexer);
      return unless_assign(lexer, SL_MUL);
    case '+':
      advance(lexer);
      return unless_assign(lexer, SL_ADD);
    case '-':
      advance(lexer);
      if (lexer->lookahead == '>') return SL_COND;
      return unless_assign(lexer, SL_ADD);
    case '=':
      advance(lexer);
      return lexer->lookahead == '>' ? NO_GUARD : SL_REL;
    case '~':
      advance(lexer);
      return lexer->lookahead == '=' ? SL_REL : NO_GUARD;
    case '<':
      advance(lexer);
      if (lexer->lookahead == '>') return NO_GUARD;
      if (lexer->lookahead == '<' && !flt) {
        advance(lexer);
        return unless_assign(lexer, SL_SHIFT);
      }
      return SL_REL;
    case '>':
      advance(lexer);
      if (lexer->lookahead == '>' && !flt) {
        advance(lexer);
        return unless_assign(lexer, SL_SHIFT);
      }
      return SL_REL;
    default:
      break;
  }
  if (lexer->lookahead >= 'A' && lexer->lookahead <= 'Z') return word_operator(lexer, flt);
  if (flt) return NO_GUARD;

  switch (lexer->lookahead) {
    case '(':
    case '[':
      return SL_APPLY;
    case '%':
      return SL_VEC;
    case '!':
      advance(lexer);
      return unless_assign(lexer, SL_VEC);
    case ':':
      advance(lexer);
      return lexer->lookahead == ':' ? SL_VEC : NO_GUARD;
    case '&':
      advance(lexer);
      return unless_assign(lexer, SL_AND);
    case '|':
      advance(lexer);
      if (lexer->lookahead == '|') return NO_GUARD;
      return unless_assign(lexer, SL_OR);
    case '\\':
      advance(lexer);
      if (lexer->lookahead == '=') return SL_REL;
      if (lexer->lookahead == '/') {
        advance(lexer);
        return unless_assign(lexer, SL_OR);
      }
      return NO_GUARD;
    case '#':
      advance(lexer);
      if (lexer->lookahead == '(') return SL_APPLY;
      return infix_operator(lexer, true);
    default:
      return NO_GUARD;
  }
}

bool tree_sitter_bcpl_external_scanner_scan(void *payload, TSLexer *lexer, const bool *valid_symbols) {
  bool recovering = valid_symbols[ERROR_SENTINEL];

  if (valid_symbols[STRING_CONTENT] && !recovering) return scan_string_content(lexer);

  bool newline = false;
  for (;;) {
    int32_t c = lexer->lookahead;
    if (c == '\n') newline = true;
    else if (c != ' ' && c != '\t' && c != '\r' && c != '\f') break;
    skip(lexer);
  }
  // Anything consumed past this point is lookahead only, unless it turns out to
  // be a block comment.
  lexer->mark_end(lexer);

  if (lexer->lookahead == '{' || lexer->lookahead == '}') return scan_section_bracket(payload, lexer, valid_symbols);
  if (lexer->lookahead == '$') {
    advance(lexer);
    if (lexer->lookahead != '(' && lexer->lookahead != ')') return false;
    return scan_section_bracket(payload, lexer, valid_symbols);
  }

  enum TokenType guard = NO_GUARD;
  if (lexer->lookahead == '/') {
    advance(lexer);
    if (lexer->lookahead == '*') {
      if (!valid_symbols[BLOCK_COMMENT]) return false;
      advance(lexer);
      scan_block_comment(lexer);
      lexer->mark_end(lexer);
      lexer->result_symbol = BLOCK_COMMENT;
      return true;
    }
    if (lexer->lookahead == '/') return false;
    if (lexer->lookahead == '\\') {
      advance(lexer);
      guard = unless_assign(lexer, SL_AND);
    } else {
      guard = unless_assign(lexer, SL_MUL);
    }
  } else if (!newline && !recovering) {
    guard = infix_operator(lexer, false);
  }

  // An infix operator only continues an expression on the line it started on.
  // Known gap: a newline in front of a block comment is forgotten by the time
  // the token after that comment is scanned.
  if (guard == NO_GUARD || newline || recovering || !valid_symbols[guard]) return false;
  lexer->result_symbol = guard;
  return true;
}

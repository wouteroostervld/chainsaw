package cypher

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// defaultMaxHops bounds a variable-length edge that gives no upper limit
const defaultMaxHops = 5

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokInt
	tokPunct
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

// lex splits a query into tokens
func lex(src string) ([]token, error) {
	var tokens []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case unicode.IsSpace(rune(c)):
			i++
		case c == '"' || c == '\'':
			var sb strings.Builder
			j := i + 1
			for ; j < len(src) && src[j] != c; j++ {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				sb.WriteByte(src[j])
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string at position %d", i)
			}
			tokens = append(tokens, token{tokString, sb.String(), i})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			tokens = append(tokens, token{tokInt, src[i:j], i})
			i = j
		case c == '_' || unicode.IsLetter(rune(c)):
			j := i
			for j < len(src) && (src[j] == '_' || unicode.IsLetter(rune(src[j])) || unicode.IsDigit(rune(src[j]))) {
				j++
			}
			tokens = append(tokens, token{tokIdent, src[i:j], i})
			i = j
		default:
			text := string(c)
			if i+1 < len(src) {
				switch two := src[i : i+2]; two {
				case "<>", "<=", ">=", "=~", "..", "!=":
					text = two
				}
			}
			if !strings.Contains("()[]{}:,.|*-<>=", text[:1]) && text != "!=" {
				return nil, fmt.Errorf("unexpected character %q at position %d", c, i)
			}
			tokens = append(tokens, token{tokPunct, text, i})
			i += len(text)
		}
	}
	return append(tokens, token{tokEOF, "", len(src)}), nil
}

type parser struct {
	tokens []token
	pos    int
}

// Parse parses a query
func Parse(src string) (*Query, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	q, err := p.query()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, p.errorf("unexpected %q", t.text)
	}
	return q, nil
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) next() token {
	t := p.tokens[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s at position %d", fmt.Sprintf(format, args...), p.peek().pos)
}

// punct consumes a punctuation token if it is next
func (p *parser) punct(text string) bool {
	if t := p.peek(); t.kind == tokPunct && t.text == text {
		p.pos++
		return true
	}
	return false
}

// keyword consumes a keyword if it is next; keywords are not case-sensitive
func (p *parser) keyword(word string) bool {
	if t := p.peek(); t.kind == tokIdent && strings.EqualFold(t.text, word) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) isKeyword(word string) bool {
	t := p.peek()
	return t.kind == tokIdent && strings.EqualFold(t.text, word)
}

func (p *parser) expectPunct(text string) error {
	if !p.punct(text) {
		return p.errorf("expected %q, found %q", text, p.peek().text)
	}
	return nil
}

func (p *parser) ident() (string, error) {
	if t := p.peek(); t.kind == tokIdent {
		p.pos++
		return t.text, nil
	}
	return "", p.errorf("expected a name, found %q", p.peek().text)
}

func (p *parser) integer() (int, error) {
	if t := p.peek(); t.kind == tokInt {
		p.pos++
		return strconv.Atoi(t.text)
	}
	return 0, p.errorf("expected a number, found %q", p.peek().text)
}

func (p *parser) query() (*Query, error) {
	q := &Query{Skip: -1, Limit: -1}
	if !p.keyword("MATCH") {
		return nil, p.errorf("a query starts with MATCH")
	}

	node, err := p.node()
	if err != nil {
		return nil, err
	}
	q.Nodes = append(q.Nodes, node)
	for p.isEdgeStart() {
		edge, err := p.edge()
		if err != nil {
			return nil, err
		}
		if node, err = p.node(); err != nil {
			return nil, err
		}
		q.Edges = append(q.Edges, edge)
		q.Nodes = append(q.Nodes, node)
	}

	if p.keyword("WHERE") {
		if q.Where, err = p.or(); err != nil {
			return nil, err
		}
	}

	if !p.keyword("RETURN") {
		return nil, p.errorf("expected RETURN, found %q", p.peek().text)
	}
	q.Distinct = p.keyword("DISTINCT")
	for {
		item := &ReturnItem{}
		if item.Expr, err = p.value(); err != nil {
			return nil, err
		}
		if p.keyword("AS") {
			if item.Alias, err = p.ident(); err != nil {
				return nil, err
			}
		}
		q.Return = append(q.Return, item)
		if !p.punct(",") {
			break
		}
	}

	// Grouping follows from the aggregates, as in Cypher; an explicit GROUP BY
	// is accepted for queries written for older versions
	if p.keyword("GROUP") {
		if !p.keyword("BY") {
			return nil, p.errorf("expected BY after GROUP")
		}
		for {
			if _, err := p.value(); err != nil {
				return nil, err
			}
			if !p.punct(",") {
				break
			}
		}
	}

	if p.keyword("ORDER") {
		if !p.keyword("BY") {
			return nil, p.errorf("expected BY after ORDER")
		}
		for {
			item := &OrderItem{}
			if item.Expr, err = p.value(); err != nil {
				return nil, err
			}
			if p.keyword("DESC") {
				item.Desc = true
			} else {
				p.keyword("ASC")
			}
			q.OrderBy = append(q.OrderBy, item)
			if !p.punct(",") {
				break
			}
		}
	}
	if p.keyword("SKIP") {
		if q.Skip, err = p.integer(); err != nil {
			return nil, err
		}
	}
	if p.keyword("LIMIT") {
		if q.Limit, err = p.integer(); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// node parses (v:LABEL|LABEL {key: value})
func (p *parser) node() (*Node, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	n := &Node{}
	if p.peek().kind == tokIdent {
		n.Var = p.next().text
	}
	if p.punct(":") {
		for {
			label, err := p.ident()
			if err != nil {
				return nil, err
			}
			n.Labels = append(n.Labels, strings.ToUpper(label))
			if !p.punct("|") {
				break
			}
		}
	}
	if p.punct("{") {
		for {
			key, err := p.ident()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(":"); err != nil {
				return nil, err
			}
			value, err := p.literal()
			if err != nil {
				return nil, err
			}
			n.Props = append(n.Props, PropEq{Key: key, Value: value})
			if !p.punct(",") {
				break
			}
		}
		if err := p.expectPunct("}"); err != nil {
			return nil, err
		}
	}
	return n, p.expectPunct(")")
}

func (p *parser) isEdgeStart() bool {
	t := p.peek()
	return t.kind == tokPunct && (t.text == "-" || t.text == "<")
}

// edge parses -[v:type|type*1..3]->, <-[...]-, -[...]-, and the bare forms
// -->, <-- and --
func (p *parser) edge() (*Edge, error) {
	e := &Edge{MinHops: 1, MaxHops: 1}
	backward := p.punct("<")
	if err := p.expectPunct("-"); err != nil {
		return nil, err
	}

	if p.punct("[") {
		if p.peek().kind == tokIdent {
			e.Var = p.next().text
		}
		if p.punct(":") {
			for {
				relation, err := p.ident()
				if err != nil {
					return nil, err
				}
				e.Types = append(e.Types, strings.ToLower(relation))
				if !p.punct("|") {
					break
				}
			}
		}
		if p.punct("*") {
			e.VarLength = true
			e.MaxHops = defaultMaxHops
			if p.peek().kind == tokInt {
				e.MinHops, _ = p.integer()
				e.MaxHops = e.MinHops
			}
			if p.punct("..") {
				e.MaxHops = defaultMaxHops
				if p.peek().kind == tokInt {
					e.MaxHops, _ = p.integer()
				}
			}
			if e.MinHops < 1 || e.MaxHops < e.MinHops {
				return nil, p.errorf("invalid hop range %d..%d", e.MinHops, e.MaxHops)
			}
		}
		if err := p.expectPunct("]"); err != nil {
			return nil, err
		}
	}

	if err := p.expectPunct("-"); err != nil {
		return nil, err
	}
	forward := p.punct(">")
	switch {
	case backward && forward:
		return nil, p.errorf("an edge cannot point both ways")
	case backward:
		e.Direction = Backward
	case forward:
		e.Direction = Forward
	}
	return e, nil
}

func (p *parser) or() (Expr, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.keyword("OR") {
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		left = &Logical{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) and() (Expr, error) {
	left, err := p.not()
	if err != nil {
		return nil, err
	}
	for p.keyword("AND") {
		right, err := p.not()
		if err != nil {
			return nil, err
		}
		left = &Logical{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) not() (Expr, error) {
	if p.keyword("NOT") {
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return &Not{X: x}, nil
	}
	if p.punct("(") {
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		return x, p.expectPunct(")")
	}
	return p.comparison()
}

func (p *parser) comparison() (Expr, error) {
	left, err := p.operand()
	if err != nil {
		return nil, err
	}

	if p.keyword("IS") {
		prop, ok := left.(*PropRef)
		if !ok {
			return nil, p.errorf("IS NULL needs a property")
		}
		notNull := p.keyword("NOT")
		if !p.keyword("NULL") {
			return nil, p.errorf("expected NULL")
		}
		return &NullCheck{Prop: prop, NotNull: notNull}, nil
	}

	c := &Comparison{Left: left}
	switch t := p.next(); {
	case t.kind == tokPunct && strings.Contains("= <> != < > <= >= =~", t.text) && t.text != "":
		c.Op = t.text
		if c.Op == "!=" {
			c.Op = "<>"
		}
	case t.kind == tokIdent && strings.EqualFold(t.text, "CONTAINS"):
		c.Op = "CONTAINS"
	case t.kind == tokIdent && (strings.EqualFold(t.text, "STARTS") || strings.EqualFold(t.text, "ENDS")):
		c.Op = strings.ToUpper(t.text)
		if !p.keyword("WITH") {
			return nil, p.errorf("expected WITH after %s", c.Op)
		}
	case t.kind == tokIdent && strings.EqualFold(t.text, "IN"):
		c.Op = "IN"
		if err := p.expectPunct("["); err != nil {
			return nil, err
		}
		for {
			value, err := p.literal()
			if err != nil {
				return nil, err
			}
			c.List = append(c.List, value)
			if !p.punct(",") {
				break
			}
		}
		return c, p.expectPunct("]")
	default:
		p.pos--
		return nil, p.errorf("expected a comparison, found %q", t.text)
	}

	if c.Right, err = p.operand(); err != nil {
		return nil, err
	}
	return c, nil
}

// operand parses v.prop or a constant
func (p *parser) operand() (Operand, error) {
	if t := p.peek(); t.kind == tokString || t.kind == tokInt {
		value, err := p.literal()
		return &Literal{Value: value}, err
	}
	return p.propRef()
}

func (p *parser) propRef() (*PropRef, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	ref := &PropRef{Var: name}
	if p.punct(".") {
		if ref.Prop, err = p.ident(); err != nil {
			return nil, err
		}
	}
	return ref, nil
}

func (p *parser) literal() (any, error) {
	switch t := p.next(); t.kind {
	case tokString:
		return t.text, nil
	case tokInt:
		n, err := strconv.ParseInt(t.text, 10, 64)
		return n, err
	default:
		p.pos--
		return nil, p.errorf("expected a string or number, found %q", t.text)
	}
}

// value parses what can be returned or sorted on: v, v.prop or count(...)
func (p *parser) value() (any, error) {
	if p.isKeyword("COUNT") && p.tokens[p.pos+1].text == "(" {
		p.pos += 2
		c := &Count{}
		if !p.punct("*") {
			c.Distinct = p.keyword("DISTINCT")
			arg, err := p.propRef()
			if err != nil {
				return nil, err
			}
			c.Arg = arg
		}
		return c, p.expectPunct(")")
	}
	return p.propRef()
}

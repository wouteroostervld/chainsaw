// Package cypher implements the subset of the Cypher query language that
// chainsaw uses to query its code graph, and translates it to SQL.
//
//	MATCH (f:FUNCTION)-[:calls]->(t)
//	WHERE t.name = "Open" AND f.file CONTAINS "/indexer/"
//	RETURN f.name, f.file, f.lines
//	ORDER BY f.name LIMIT 20
package cypher

// Query is a parsed query: one path pattern, an optional filter, and what to
// return
type Query struct {
	Nodes    []*Node // The nodes of the path, in order
	Edges    []*Edge // Edges[i] connects Nodes[i] and Nodes[i+1]
	Where    Expr    // nil when there is no WHERE clause
	Distinct bool
	Return   []*ReturnItem
	OrderBy  []*OrderItem
	Skip     int // -1 when not given
	Limit    int // -1 when not given
}

// Node is a node pattern: (v:LABEL|LABEL {key: value})
type Node struct {
	Var    string
	Labels []string // Entity types, any of which may match
	Props  []PropEq
}

// PropEq is a property that must equal a value
type PropEq struct {
	Key   string
	Value any // string or int64
}

// Direction of an edge pattern
const (
	Forward  = 1
	Backward = -1
	Either   = 0
)

// Edge is an edge pattern: -[v:type|type*min..max]->
type Edge struct {
	Var       string
	Types     []string // Relation types, any of which may match
	Direction int
	VarLength bool // Matches a path of MinHops to MaxHops edges
	MinHops   int
	MaxHops   int
}

// Expr is a boolean expression in a WHERE clause: *Logical, *Not,
// *Comparison or *NullCheck
type Expr interface{}

// Logical combines two expressions with AND or OR
type Logical struct {
	Op          string
	Left, Right Expr
}

// Not negates an expression
type Not struct {
	X Expr
}

// Comparison compares two operands. Op is one of = <> < > <= >= CONTAINS
// STARTS ENDS =~ IN
type Comparison struct {
	Op    string
	Left  Operand
	Right Operand
	List  []any // The values of an IN list
}

// NullCheck tests a property with IS NULL or IS NOT NULL
type NullCheck struct {
	Prop    *PropRef
	NotNull bool
}

// Operand is a value in a comparison: *PropRef or *Literal
type Operand interface{}

// PropRef refers to a property of a pattern variable; Prop is empty for the
// variable itself
type PropRef struct {
	Var  string
	Prop string
}

// Literal is a string or integer constant
type Literal struct {
	Value any
}

// Count is the aggregate count(*), count(v), count(v.p) or count(DISTINCT ...)
type Count struct {
	Distinct bool
	Arg      *PropRef // nil for count(*)
}

// ReturnItem is one column of the result: a *PropRef or a *Count
type ReturnItem struct {
	Expr  any
	Alias string
}

// OrderItem is one sort key: a *PropRef (which may name a RETURN alias) or a *Count
type OrderItem struct {
	Expr any
	Desc bool
}

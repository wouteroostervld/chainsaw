package cypher

import (
	"fmt"
	"strings"
)

// Options control how a query is translated
type Options struct {
	CWD          string // Only return paths that touch a file under this directory (empty = no filtering)
	DefaultLimit int    // LIMIT to apply when the query has none (0 = unlimited)
	Locations    bool   // Also select where each returned node is defined, for showing its code
}

// Result is a query translated to SQL
type Result struct {
	SQL     string
	Args    []any
	Columns []string // Names of the columns the query asked for
	// Locations names, for each node variable in RETURN, the three hidden
	// columns (path, start line, end line) that follow the query's own
	Locations []string
	Limited   bool // DefaultLimit was applied
}

// Transpile parses a query and translates it to SQL over the code graph
func Transpile(query string, opts Options) (*Result, error) {
	q, err := Parse(query)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	t := &transpiler{q: q, opts: opts, nodes: make(map[string]int), edges: make(map[string]int)}
	return t.run()
}

// nodeProps maps the properties of a node to SQL; %[1]d is the node's index
var nodeProps = map[string]string{
	"id":          "n%[1]d.id",
	"name":        "n%[1]d.name",
	"type":        "n%[1]d.entity_type",
	"entity_type": "n%[1]d.entity_type",
	"kind":        "n%[1]d.entity_type",
	"file":        "f%[1]d.path",
	"path":        "f%[1]d.path",
	"line":        "d%[1]d.start_line",
	"start_line":  "d%[1]d.start_line",
	"end_line":    "d%[1]d.end_line",
	"lines":       "(d%[1]d.start_line || '-' || d%[1]d.end_line)",
	"signature":   "d%[1]d.signature",
	"container":   "d%[1]d.container",
	"short_name":  "d%[1]d.name",
	"snippet":     "c%[1]d.content_snippet",
	"code":        "c%[1]d.content_snippet",
}

// summaryProps are what a bare node variable in RETURN stands for
var summaryProps = []string{"name", "type", "file", "lines"}

type transpiler struct {
	q     *Query
	opts  Options
	nodes map[string]int // Variable -> node index
	edges map[string]int // Variable -> edge index
	args  []any
}

func (t *transpiler) run() (*Result, error) {
	q := t.q
	for i, n := range q.Nodes {
		if n.Var == "" {
			continue
		}
		if _, dup := t.nodes[n.Var]; dup {
			return nil, fmt.Errorf("variable %s is used for more than one node", n.Var)
		}
		t.nodes[n.Var] = i
	}
	for i, e := range q.Edges {
		if e.Var == "" {
			continue
		}
		if _, dup := t.nodes[e.Var]; dup {
			return nil, fmt.Errorf("variable %s is used for a node and an edge", e.Var)
		}
		t.edges[e.Var] = i
	}

	// The clauses are built in the order their parameters appear in the SQL
	with, err := t.paths()
	if err != nil {
		return nil, err
	}

	result := &Result{}
	selects, groupBy, err := t.returns(result)
	if err != nil {
		return nil, err
	}
	from := t.from()
	where, err := t.where()
	if err != nil {
		return nil, err
	}
	orderBy, err := t.orderBy()
	if err != nil {
		return nil, err
	}

	var sql strings.Builder
	if len(with) > 0 {
		sql.WriteString("WITH RECURSIVE " + strings.Join(with, ",\n") + "\n")
	}
	sql.WriteString("SELECT ")
	// A path of several hops reaches the same node in more than one way
	if q.Distinct || (t.hasVarLength() && groupBy == nil) {
		sql.WriteString("DISTINCT ")
	}
	sql.WriteString(strings.Join(selects, ", "))
	sql.WriteString("\n" + from)
	if len(where) > 0 {
		sql.WriteString("\nWHERE " + strings.Join(where, "\n  AND "))
	}
	if len(groupBy) > 0 {
		sql.WriteString("\nGROUP BY " + strings.Join(groupBy, ", "))
	}
	if len(orderBy) > 0 {
		sql.WriteString("\nORDER BY " + strings.Join(orderBy, ", "))
	}

	limit := q.Limit
	if limit < 0 && t.opts.DefaultLimit > 0 {
		limit = t.opts.DefaultLimit
		result.Limited = true
	}
	if limit >= 0 || q.Skip >= 0 {
		sql.WriteString(fmt.Sprintf("\nLIMIT %d", limit))
		if q.Skip >= 0 {
			sql.WriteString(fmt.Sprintf(" OFFSET %d", q.Skip))
		}
	}

	result.SQL = sql.String()
	result.Args = t.args
	return result, nil
}

func (t *transpiler) hasVarLength() bool {
	for _, e := range t.q.Edges {
		if e.VarLength {
			return true
		}
	}
	return false
}

func (t *transpiler) arg(v any) string {
	t.args = append(t.args, v)
	return "?"
}

// in renders "column IN (?, ?)", or "column = ?" for a single value
func (t *transpiler) in(column string, values []string) string {
	if len(values) == 1 {
		return column + " = " + t.arg(values[0])
	}
	marks := make([]string, len(values))
	for i, v := range values {
		marks[i] = t.arg(v)
	}
	return column + " IN (" + strings.Join(marks, ", ") + ")"
}

// nodeConstraints renders what the pattern itself says about node i: its
// labels and inline properties
func (t *transpiler) nodeConstraints(i int) ([]string, error) {
	var conds []string
	n := t.q.Nodes[i]
	if len(n.Labels) > 0 {
		conds = append(conds, t.in(fmt.Sprintf("n%d.entity_type", i), n.Labels))
	}
	for _, p := range n.Props {
		column, ok := nodeProps[strings.ToLower(p.Key)]
		if !ok {
			return nil, fmt.Errorf("unknown property %s", p.Key)
		}
		conds = append(conds, fmt.Sprintf(column, i)+" = "+t.arg(p.Value))
	}
	return conds, nil
}

// nodeJoins joins what is known about node i beyond the entity itself
func nodeJoins(i int) string {
	return fmt.Sprintf(`LEFT JOIN symbol_defs d%[1]d ON d%[1]d.entity_id = n%[1]d.id
LEFT JOIN files f%[1]d ON f%[1]d.id = d%[1]d.file_id
LEFT JOIN chunks c%[1]d ON c%[1]d.chunk_id = n%[1]d.chunk_id AND n%[1]d.chunk_id != 0`, i)
}

// paths builds a recursive table for each variable-length edge: the pairs of
// entities connected by a path of edges, and its length
func (t *transpiler) paths() ([]string, error) {
	var with []string
	for i, e := range t.q.Edges {
		if !e.VarLength {
			continue
		}
		if e.Direction == Either {
			return nil, fmt.Errorf("a variable-length edge needs a direction")
		}

		// src is where the edges of the path start, whichever way the
		// pattern is written
		src, dst := i, i+1
		if e.Direction == Backward {
			src, dst = dst, src
		}

		// Walk from the end the pattern pins down, so the walk does not
		// start from every entity in the index
		anchor, column, next, far := src, "source_entity_id", "g.source_entity_id = p.dst", "p.src, g.target_entity_id"
		if !t.constrained(src) && t.constrained(dst) {
			anchor, column, next, far = dst, "target_entity_id", "g.target_entity_id = p.src", "g.source_entity_id, p.dst"
		}

		// Parameters are added in the order they appear in the SQL
		relation := ""
		if len(e.Types) > 0 {
			relation = " AND " + t.in("relation_type", e.Types)
		}

		seed := ""
		if t.constrained(anchor) {
			conds, err := t.nodeConstraints(anchor)
			if err != nil {
				return nil, err
			}
			seed = fmt.Sprintf(" AND %s IN (SELECT n%[2]d.id FROM entities n%[2]d\n%[3]s\nWHERE %[4]s)",
				column, anchor, nodeJoins(anchor), strings.Join(conds, " AND "))
		}

		stepRelation := ""
		if len(e.Types) > 0 {
			stepRelation = " AND " + t.in("g.relation_type", e.Types)
		}

		with = append(with, fmt.Sprintf(`p%[1]d(src, dst, depth) AS (
  SELECT source_entity_id, target_entity_id, 1 FROM graph_edges WHERE 1=1%[2]s%[3]s
  UNION
  SELECT %[4]s, p.depth + 1 FROM p%[1]d p JOIN graph_edges g ON %[5]s
  WHERE p.depth < %[6]d%[7]s
)`, i, relation, seed, far, next, e.MaxHops, stepRelation))
	}
	return with, nil
}

// constrained reports whether the pattern narrows node i by itself
func (t *transpiler) constrained(i int) bool {
	return len(t.q.Nodes[i].Labels) > 0 || len(t.q.Nodes[i].Props) > 0
}

func (t *transpiler) from() string {
	var sb strings.Builder
	sb.WriteString("FROM entities n0\n" + nodeJoins(0))
	for i, e := range t.q.Edges {
		j := i + 1
		switch {
		case e.VarLength:
			near, far := "src", "dst"
			if e.Direction == Backward {
				near, far = far, near
			}
			fmt.Fprintf(&sb, "\nJOIN p%[1]d ON p%[1]d.%[3]s = n%[1]d.id AND p%[1]d.depth >= %[5]d\nJOIN entities n%[2]d ON n%[2]d.id = p%[1]d.%[4]s",
				i, j, near, far, e.MinHops)
		case e.Direction == Either:
			fmt.Fprintf(&sb, "\nJOIN graph_edges g%[1]d ON (g%[1]d.source_entity_id = n%[1]d.id OR g%[1]d.target_entity_id = n%[1]d.id)\nJOIN entities n%[2]d ON n%[2]d.id = CASE WHEN g%[1]d.source_entity_id = n%[1]d.id THEN g%[1]d.target_entity_id ELSE g%[1]d.source_entity_id END",
				i, j)
		default:
			near, far := "source_entity_id", "target_entity_id"
			if e.Direction == Backward {
				near, far = far, near
			}
			fmt.Fprintf(&sb, "\nJOIN graph_edges g%[1]d ON g%[1]d.%[3]s = n%[1]d.id\nJOIN entities n%[2]d ON n%[2]d.id = g%[1]d.%[4]s",
				i, j, near, far)
		}
		sb.WriteString("\n" + nodeJoins(j))
	}
	return sb.String()
}

func (t *transpiler) where() ([]string, error) {
	var conds []string
	for i := range t.q.Nodes {
		c, err := t.nodeConstraints(i)
		if err != nil {
			return nil, err
		}
		conds = append(conds, c...)
	}
	for i, e := range t.q.Edges {
		if !e.VarLength && len(e.Types) > 0 {
			conds = append(conds, t.in(fmt.Sprintf("g%d.relation_type", i), e.Types))
		}
	}

	if t.q.Where != nil {
		c, err := t.expr(t.q.Where)
		if err != nil {
			return nil, err
		}
		conds = append(conds, c)
	}

	// Keep to the current directory: at least one node of the path is in it
	if t.opts.CWD != "" {
		pattern := strings.TrimSuffix(t.opts.CWD, "/") + "/%"
		scope := make([]string, len(t.q.Nodes))
		for i := range t.q.Nodes {
			scope[i] = fmt.Sprintf("f%d.path LIKE %s", i, t.arg(pattern))
		}
		conds = append(conds, "("+strings.Join(scope, " OR ")+")")
	}
	return conds, nil
}

// property renders a reference to a property of a node or an edge
func (t *transpiler) property(ref *PropRef) (string, error) {
	prop := strings.ToLower(ref.Prop)
	if i, ok := t.nodes[ref.Var]; ok {
		if prop == "" {
			prop = "name"
		}
		column, ok := nodeProps[prop]
		if !ok {
			return "", fmt.Errorf("unknown property %s.%s", ref.Var, ref.Prop)
		}
		return fmt.Sprintf(column, i), nil
	}
	if i, ok := t.edges[ref.Var]; ok {
		e := t.q.Edges[i]
		switch {
		case e.VarLength && (prop == "depth" || prop == "hops"):
			return fmt.Sprintf("p%d.depth", i), nil
		case !e.VarLength && (prop == "type" || prop == ""):
			return fmt.Sprintf("g%d.relation_type", i), nil
		case !e.VarLength && prop == "weight":
			return fmt.Sprintf("g%d.weight", i), nil
		}
		return "", fmt.Errorf("unknown property %s.%s", ref.Var, ref.Prop)
	}
	return "", fmt.Errorf("unknown variable %s", ref.Var)
}

func (t *transpiler) operand(o Operand) (string, error) {
	switch o := o.(type) {
	case *PropRef:
		return t.property(o)
	case *Literal:
		return t.arg(o.Value), nil
	}
	return "", fmt.Errorf("unexpected operand %T", o)
}

func (t *transpiler) expr(e Expr) (string, error) {
	switch e := e.(type) {
	case *Logical:
		left, err := t.expr(e.Left)
		if err != nil {
			return "", err
		}
		right, err := t.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + left + " " + e.Op + " " + right + ")", nil

	case *Not:
		x, err := t.expr(e.X)
		if err != nil {
			return "", err
		}
		return "NOT " + x, nil

	case *NullCheck:
		column, err := t.property(e.Prop)
		if err != nil {
			return "", err
		}
		if e.NotNull {
			return column + " IS NOT NULL", nil
		}
		return column + " IS NULL", nil

	case *Comparison:
		left, err := t.operand(e.Left)
		if err != nil {
			return "", err
		}
		if e.Op == "IN" {
			marks := make([]string, len(e.List))
			for i, v := range e.List {
				marks[i] = t.arg(v)
			}
			return left + " IN (" + strings.Join(marks, ", ") + ")", nil
		}
		right, err := t.operand(e.Right)
		if err != nil {
			return "", err
		}
		switch e.Op {
		case "CONTAINS":
			return fmt.Sprintf("instr(%s, %s) > 0", left, right), nil
		case "STARTS", "ENDS":
			// The value is needed twice: once to measure, once to compare
			if lit, ok := e.Right.(*Literal); ok {
				again := t.arg(lit.Value)
				if e.Op == "STARTS" {
					return fmt.Sprintf("substr(%s, 1, length(%s)) = %s", left, right, again), nil
				}
				return fmt.Sprintf("substr(%s, -length(%s)) = %s", left, right, again), nil
			}
			if e.Op == "STARTS" {
				return fmt.Sprintf("substr(%[1]s, 1, length(%[2]s)) = %[2]s", left, right), nil
			}
			return fmt.Sprintf("substr(%[1]s, -length(%[2]s)) = %[2]s", left, right), nil
		case "=~":
			return left + " REGEXP " + right, nil
		default:
			return left + " " + e.Op + " " + right, nil
		}
	}
	return "", fmt.Errorf("unexpected expression %T", e)
}

func (t *transpiler) count(c *Count) (string, error) {
	if c.Arg == nil {
		return "COUNT(*)", nil
	}
	ref := c.Arg
	if _, isNode := t.nodes[ref.Var]; isNode && ref.Prop == "" {
		ref = &PropRef{Var: ref.Var, Prop: "id"}
	}
	column, err := t.property(ref)
	if err != nil {
		return "", err
	}
	if c.Distinct {
		return "COUNT(DISTINCT " + column + ")", nil
	}
	return "COUNT(" + column + ")", nil
}

// returns builds the SELECT list, and the GROUP BY list when the query
// aggregates: everything returned that is not an aggregate is a group key
func (t *transpiler) returns(result *Result) (selects, groupBy []string, err error) {
	aggregates := false
	var keys []string
	var locations []string

	add := func(expr, name string) {
		selects = append(selects, fmt.Sprintf("%s AS %q", expr, name))
		result.Columns = append(result.Columns, name)
	}

	for _, item := range t.q.Return {
		switch e := item.Expr.(type) {
		case *Count:
			aggregates = true
			expr, err := t.count(e)
			if err != nil {
				return nil, nil, err
			}
			name := item.Alias
			if name == "" {
				name = "count"
			}
			add(expr, name)

		case *PropRef:
			i, isNode := t.nodes[e.Var]
			if isNode && e.Prop == "" {
				// A bare node stands for what identifies it
				for _, prop := range summaryProps {
					expr := fmt.Sprintf(nodeProps[prop], i)
					name := e.Var + "_" + prop
					if item.Alias != "" {
						name = item.Alias + "_" + prop
					}
					add(expr, name)
					keys = append(keys, expr)
				}
				locations = append(locations, e.Var)
				continue
			}
			expr, err := t.property(e)
			if err != nil {
				return nil, nil, err
			}
			name := item.Alias
			if name == "" {
				name = e.Var
				if e.Prop != "" {
					name += "_" + e.Prop
				}
			}
			add(expr, name)
			keys = append(keys, expr)
			if isNode && !contains(locations, e.Var) {
				locations = append(locations, e.Var)
			}
		}
	}

	if aggregates {
		return selects, keys, nil
	}

	if t.opts.Locations {
		for _, v := range locations {
			i := t.nodes[v]
			selects = append(selects,
				fmt.Sprintf("f%d.path", i), fmt.Sprintf("d%d.start_line", i), fmt.Sprintf("d%d.end_line", i))
			result.Locations = append(result.Locations, v)
		}
	}
	return selects, nil, nil
}

func (t *transpiler) orderBy() ([]string, error) {
	aliases := make(map[string]bool)
	for _, item := range t.q.Return {
		if item.Alias != "" {
			aliases[item.Alias] = true
		}
	}

	var keys []string
	for _, item := range t.q.OrderBy {
		var key string
		var err error
		switch e := item.Expr.(type) {
		case *Count:
			key, err = t.count(e)
		case *PropRef:
			if e.Prop == "" && aliases[e.Var] {
				key = fmt.Sprintf("%q", e.Var)
			} else {
				key, err = t.property(e)
			}
		}
		if err != nil {
			return nil, err
		}
		if item.Desc {
			key += " DESC"
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

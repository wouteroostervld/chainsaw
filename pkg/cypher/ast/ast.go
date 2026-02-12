package ast

import (
	"fmt"
	"github.com/wouteroostervld/chainsaw/pkg/cypher/token"
)

// Attrib is the interface for all AST nodes
type Attrib interface{}

// Query represents a complete Cypher query
type Query struct {
	Match   *MatchClause
	Return  *ReturnClause
	GroupBy *GroupByClause
	OrderBy *OrderByClause
	Limit   *LimitClause
}

// MatchClause represents the MATCH part
type MatchClause struct {
	Pattern *PathPattern
}

// PathPattern represents a node-edge-node pattern
type PathPattern struct {
	SourceNode *Node
	Edge       *Edge
	TargetNode *Node
}

// Node represents a node in the pattern
type Node struct {
	Variable string
	Label    string // entity_type filter
}

// Edge represents an edge in the pattern
type Edge struct {
	Type      string // relation_type filter
	Direction string // "->", "<-", "-"
	MinHops   int    // Minimum hops for multi-hop (0 = not specified)
	MaxHops   int    // Maximum hops for multi-hop (0 = not specified)
}

// ReturnClause represents the RETURN part
type ReturnClause struct {
	Items []ReturnItem
}

// ReturnItem represents what to return
type ReturnItem struct {
	Variable  string
	Property  string             // empty if returning whole variable
	Aggregate *AggregateFunction // non-nil if this is an aggregate
	Alias     string             // AS alias (empty if none)
}

// AggregateFunction represents COUNT, SUM, etc.
type AggregateFunction struct {
	Function string // "COUNT", "SUM", "AVG", "MIN", "MAX"
	Variable string // variable being aggregated
	Property string // property being aggregated (empty for whole variable)
}

// GroupByClause represents GROUP BY
type GroupByClause struct {
	Items []GroupByItem
}

// GroupByItem represents a single GROUP BY expression
type GroupByItem struct {
	Variable string
	Property string
}

// OrderByClause represents ORDER BY
type OrderByClause struct {
	Items []OrderByItem
}

// OrderByItem represents a single ORDER BY expression
type OrderByItem struct {
	Expression string // variable.property or alias
	Ascending  bool   // true for ASC, false for DESC
}

// LimitClause represents LIMIT
type LimitClause struct {
	Count int
}

// Constructor functions for gocc

func NewQuery(match, ret Attrib) (*Query, error) {
	return &Query{
		Match:   match.(*MatchClause),
		Return:  ret.(*ReturnClause),
		GroupBy: nil,
		OrderBy: nil,
		Limit:   nil,
	}, nil
}

func NewQueryWithClauses(match, ret, groupBy, orderBy, limit Attrib) (*Query, error) {
	q := &Query{
		Match:  match.(*MatchClause),
		Return: ret.(*ReturnClause),
	}

	if groupBy != nil {
		q.GroupBy = groupBy.(*GroupByClause)
	}
	if orderBy != nil {
		q.OrderBy = orderBy.(*OrderByClause)
	}
	if limit != nil {
		q.Limit = limit.(*LimitClause)
	}

	return q, nil
}

func NewMatchClause(pattern Attrib) (*MatchClause, error) {
	return &MatchClause{
		Pattern: pattern.(*PathPattern),
	}, nil
}

func NewPathPattern(source, edge, target Attrib) (*PathPattern, error) {
	return &PathPattern{
		SourceNode: source.(*Node),
		Edge:       edge.(*Edge),
		TargetNode: target.(*Node),
	}, nil
}

func NewNodeLabeled(varTok, labelTok Attrib) (*Node, error) {
	return &Node{
		Variable: string(varTok.(*token.Token).Lit),
		Label:    string(labelTok.(*token.Token).Lit),
	}, nil
}

func NewNodeVar(varTok Attrib) (*Node, error) {
	return &Node{
		Variable: string(varTok.(*token.Token).Lit),
		Label:    "",
	}, nil
}

func NewNodeAnon() (*Node, error) {
	return &Node{
		Variable: "",
		Label:    "",
	}, nil
}

func NewEdgeForward(typeTok Attrib) (*Edge, error) {
	return &Edge{
		Type:      string(typeTok.(*token.Token).Lit),
		Direction: "->",
		MinHops:   0,
		MaxHops:   0,
	}, nil
}

func NewEdgeBackward(typeTok Attrib) (*Edge, error) {
	return &Edge{
		Type:      string(typeTok.(*token.Token).Lit),
		Direction: "<-",
		MinHops:   0,
		MaxHops:   0,
	}, nil
}

func NewEdgeAnyForward() (*Edge, error) {
	return &Edge{
		Type:      "",
		Direction: "->",
		MinHops:   0,
		MaxHops:   0,
	}, nil
}

func NewEdgeAny() (*Edge, error) {
	return &Edge{
		Type:      "",
		Direction: "-",
		MinHops:   0,
		MaxHops:   0,
	}, nil
}

func NewEdgeMultiHopForward(typeTok, minTok, maxTok Attrib) (*Edge, error) {
	minHops := 1
	maxHops := 10 // Default max

	if minTok != nil {
		if tok, ok := minTok.(*token.Token); ok {
			fmt.Sscanf(string(tok.Lit), "%d", &minHops)
		}
	}

	if maxTok != nil {
		if tok, ok := maxTok.(*token.Token); ok {
			fmt.Sscanf(string(tok.Lit), "%d", &maxHops)
		}
	}

	typeStr := ""
	if typeTok != nil {
		if tok, ok := typeTok.(*token.Token); ok {
			typeStr = string(tok.Lit)
		}
	}

	return &Edge{
		Type:      typeStr,
		Direction: "->",
		MinHops:   minHops,
		MaxHops:   maxHops,
	}, nil
}

func NewEdgeMultiHopBackward(typeTok, minTok, maxTok Attrib) (*Edge, error) {
	minHops := 1
	maxHops := 10

	if minTok != nil {
		if tok, ok := minTok.(*token.Token); ok {
			fmt.Sscanf(string(tok.Lit), "%d", &minHops)
		}
	}

	if maxTok != nil {
		if tok, ok := maxTok.(*token.Token); ok {
			fmt.Sscanf(string(tok.Lit), "%d", &maxHops)
		}
	}

	typeStr := ""
	if typeTok != nil {
		if tok, ok := typeTok.(*token.Token); ok {
			typeStr = string(tok.Lit)
		}
	}

	return &Edge{
		Type:      typeStr,
		Direction: "<-",
		MinHops:   minHops,
		MaxHops:   maxHops,
	}, nil
}

func NewReturnClause(items Attrib) (*ReturnClause, error) {
	return &ReturnClause{
		Items: items.([]ReturnItem),
	}, nil
}

func NewReturnItems(item Attrib) ([]ReturnItem, error) {
	return []ReturnItem{item.(ReturnItem)}, nil
}

func AppendReturnItem(list, item Attrib) ([]ReturnItem, error) {
	items := list.([]ReturnItem)
	return append(items, item.(ReturnItem)), nil
}

func NewReturnProp(varTok, propTok Attrib) (ReturnItem, error) {
	return ReturnItem{
		Variable:  string(varTok.(*token.Token).Lit),
		Property:  string(propTok.(*token.Token).Lit),
		Aggregate: nil,
		Alias:     "",
	}, nil
}

func NewReturnVar(varTok Attrib) (ReturnItem, error) {
	return ReturnItem{
		Variable:  string(varTok.(*token.Token).Lit),
		Property:  "",
		Aggregate: nil,
		Alias:     "",
	}, nil
}

func NewReturnPropWithAlias(varTok, propTok, aliasTok Attrib) (ReturnItem, error) {
	return ReturnItem{
		Variable:  string(varTok.(*token.Token).Lit),
		Property:  string(propTok.(*token.Token).Lit),
		Aggregate: nil,
		Alias:     string(aliasTok.(*token.Token).Lit),
	}, nil
}

func NewReturnAggregate(funcTok, varTok Attrib) (ReturnItem, error) {
	return ReturnItem{
		Variable: "",
		Property: "",
		Aggregate: &AggregateFunction{
			Function: string(funcTok.(*token.Token).Lit),
			Variable: string(varTok.(*token.Token).Lit),
			Property: "",
		},
		Alias: "",
	}, nil
}

func NewReturnAggregateWithAlias(funcTok, varTok, aliasTok Attrib) (ReturnItem, error) {
	return ReturnItem{
		Variable: "",
		Property: "",
		Aggregate: &AggregateFunction{
			Function: string(funcTok.(*token.Token).Lit),
			Variable: string(varTok.(*token.Token).Lit),
			Property: "",
		},
		Alias: string(aliasTok.(*token.Token).Lit),
	}, nil
}

// GroupBy constructors

func NewGroupByClause(items Attrib) (*GroupByClause, error) {
	return &GroupByClause{
		Items: items.([]GroupByItem),
	}, nil
}

func NewGroupByItems(item Attrib) ([]GroupByItem, error) {
	return []GroupByItem{item.(GroupByItem)}, nil
}

func AppendGroupByItem(list, item Attrib) ([]GroupByItem, error) {
	items := list.([]GroupByItem)
	return append(items, item.(GroupByItem)), nil
}

func NewGroupByItem(varTok, propTok Attrib) (GroupByItem, error) {
	return GroupByItem{
		Variable: string(varTok.(*token.Token).Lit),
		Property: string(propTok.(*token.Token).Lit),
	}, nil
}

// OrderBy constructors

func NewOrderByClause(items Attrib) (*OrderByClause, error) {
	return &OrderByClause{
		Items: items.([]OrderByItem),
	}, nil
}

func NewOrderByItems(item Attrib) ([]OrderByItem, error) {
	return []OrderByItem{item.(OrderByItem)}, nil
}

func AppendOrderByItem(list, item Attrib) ([]OrderByItem, error) {
	items := list.([]OrderByItem)
	return append(items, item.(OrderByItem)), nil
}

func NewOrderByItemAsc(exprTok Attrib) (OrderByItem, error) {
	return OrderByItem{
		Expression: string(exprTok.(*token.Token).Lit),
		Ascending:  true,
	}, nil
}

func NewOrderByItemDesc(exprTok Attrib) (OrderByItem, error) {
	return OrderByItem{
		Expression: string(exprTok.(*token.Token).Lit),
		Ascending:  false,
	}, nil
}

func NewOrderByItem(exprTok Attrib) (OrderByItem, error) {
	return OrderByItem{
		Expression: string(exprTok.(*token.Token).Lit),
		Ascending:  true, // Default to ASC
	}, nil
}

// Limit constructor

func NewLimitClause(countTok Attrib) (*LimitClause, error) {
	var count int
	fmt.Sscanf(string(countTok.(*token.Token).Lit), "%d", &count)
	return &LimitClause{
		Count: count,
	}, nil
}

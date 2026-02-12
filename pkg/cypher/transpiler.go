package cypher

import (
	"fmt"
	"strings"

	"github.com/wouteroostervld/chainsaw/pkg/cypher/ast"
	"github.com/wouteroostervld/chainsaw/pkg/cypher/lexer"
	"github.com/wouteroostervld/chainsaw/pkg/cypher/parser"
)

// TranspileOptions contains options for transpiling Cypher to SQL
type TranspileOptions struct {
	CWD string // Current working directory for path filtering (empty = no filtering)
}

// TranspileResult contains the generated SQL and bind parameters
type TranspileResult struct {
	SQL  string
	Args []interface{}
}

// Transpile converts a Cypher query to SQL with prepared statement placeholders
func Transpile(query string, opts TranspileOptions) (*TranspileResult, error) {
	// Parse Cypher using generated parser
	lex := lexer.NewLexer([]byte(query))
	parsedQuery, err := parser.NewParser().Parse(lex)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Type assert to our Query AST
	q, ok := parsedQuery.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("unexpected parse result type: %T", parsedQuery)
	}

	// Generate SQL from AST
	return generateSQL(q, opts)
}

// generateSQL walks the AST and builds SQL with placeholders
func generateSQL(q *ast.Query, opts TranspileOptions) (*TranspileResult, error) {
	// Extract components from AST
	edge := q.Match.Pattern.Edge

	// Check if this is a multi-hop query
	if edge.MinHops > 0 || edge.MaxHops > 0 {
		return generateMultiHopSQL(q, opts)
	}

	// Single-hop query (existing logic)
	return generateSingleHopSQL(q, opts)
}

// generateSingleHopSQL generates SQL for single-hop pattern matching
func generateSingleHopSQL(q *ast.Query, opts TranspileOptions) (*TranspileResult, error) {
	var sql strings.Builder
	var args []interface{}

	// Extract components from AST
	pattern := q.Match.Pattern
	sourceNode := pattern.SourceNode
	edge := pattern.Edge
	targetNode := pattern.TargetNode

	// Handle direction - determines SQL JOIN but vars stay with their visual nodes
	var e1Var, e2Var string     // Which variable maps to e1/e2 in SQL
	var e1Label, e2Label string // Labels for SQL e1/e2

	if edge.Direction == "->" {
		// Forward: source (left) → target (right)
		// SQL: e1 is source, e2 is target
		e1Var = sourceNode.Variable
		e1Label = sourceNode.Label
		e2Var = targetNode.Variable
		e2Label = targetNode.Label
	} else if edge.Direction == "<-" {
		// Backward: target (left) ← source (right)
		// In SQL, e1 is always source_entity_id, e2 is target_entity_id
		// But visually: left is target, right is source
		e1Var = targetNode.Variable // Right node (source) maps to e1
		e1Label = targetNode.Label
		e2Var = sourceNode.Variable // Left node (target) maps to e2
		e2Label = sourceNode.Label
	} else {
		// Undirected - not yet supported
		return nil, fmt.Errorf("undirected edges not yet supported")
	}

	// Build SELECT clause
	sql.WriteString("SELECT ")
	selectItems := []string{}
	for _, item := range q.Return.Items {
		// Handle aggregate functions
		if item.Aggregate != nil {
			// Map aggregate variable to table alias
			var tableAlias string
			if item.Aggregate.Variable == e1Var {
				tableAlias = "e1"
			} else if item.Aggregate.Variable == e2Var {
				tableAlias = "e2"
			} else {
				return nil, fmt.Errorf("unknown variable in aggregate: %s", item.Aggregate.Variable)
			}

			// Build aggregate expression
			var aggExpr string
			switch item.Aggregate.Function {
			case "COUNT":
				aggExpr = fmt.Sprintf("COUNT(DISTINCT %s.id)", tableAlias)
			case "SUM", "AVG", "MIN", "MAX":
				return nil, fmt.Errorf("aggregate function %s not yet supported", item.Aggregate.Function)
			default:
				return nil, fmt.Errorf("unknown aggregate function: %s", item.Aggregate.Function)
			}

			// Add alias if specified
			if item.Alias != "" {
				aggExpr += " AS " + item.Alias
			}
			selectItems = append(selectItems, aggExpr)
			continue
		}

		// Map variable to correct table alias
		var tableAlias string
		if item.Variable == e1Var {
			tableAlias = "e1"
		} else if item.Variable == e2Var {
			tableAlias = "e2"
		} else if item.Variable == "" {
			// Empty variable in non-aggregate item - skip it
			continue
		} else {
			return nil, fmt.Errorf("unknown variable in RETURN: %s", item.Variable)
		}

		// Build column reference
		var colExpr string
		if item.Property != "" {
			// Specific property requested
			if item.Property == "snippet" {
				// Special case: get the code snippet from chunks
				colExpr = fmt.Sprintf("c%s.content_snippet AS %s_snippet",
					tableAlias[1:], item.Variable) // e1 -> c1, e2 -> c2
			} else if item.Property == "file" {
				// Special case: get the file path
				colExpr = fmt.Sprintf("f%s.path AS %s_file",
					tableAlias[1:], item.Variable)
			} else if item.Property == "lines" {
				// Special case: get line range
				colExpr = fmt.Sprintf("(c%s.start_line || '-' || c%s.end_line) AS %s_lines",
					tableAlias[1:], tableAlias[1:], item.Variable)
			} else {
				// Regular entity property
				colExpr = fmt.Sprintf("%s.%s AS %s_%s",
					tableAlias, item.Property, item.Variable, item.Property)
			}
		} else {
			// Return all entity columns
			colExpr = fmt.Sprintf("%s.*", tableAlias)
		}

		selectItems = append(selectItems, colExpr)
	}
	sql.WriteString(strings.Join(selectItems, ", "))

	// FROM clause with JOINs - include chunks and files for snippet access
	sql.WriteString("\nFROM entities e1")
	sql.WriteString("\nJOIN graph_edges g ON g.source_entity_id = e1.id")
	sql.WriteString("\nJOIN entities e2 ON g.target_entity_id = e2.id")

	// JOIN chunks for both entities to enable snippet/file queries
	sql.WriteString("\nLEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id")
	sql.WriteString("\nLEFT JOIN files f1 ON c1.file_id = f1.id")
	sql.WriteString("\nLEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id")
	sql.WriteString("\nLEFT JOIN files f2 ON c2.file_id = f2.id")

	// WHERE clause - add filters for labels and edge type
	whereConditions := []string{}

	// Source node label filter
	if e1Label != "" {
		whereConditions = append(whereConditions, "e1.entity_type = ?")
		args = append(args, e1Label)
	}

	// Edge type filter
	if edge.Type != "" {
		whereConditions = append(whereConditions, "g.relation_type = ?")
		args = append(args, edge.Type)
	}

	// Target node label filter
	if e2Label != "" {
		whereConditions = append(whereConditions, "e2.entity_type = ?")
		args = append(args, e2Label)
	}

	if len(whereConditions) > 0 {
		sql.WriteString("\nWHERE ")
		sql.WriteString(strings.Join(whereConditions, "\n  AND "))
	}

	// GROUP BY clause
	if q.GroupBy != nil {
		sql.WriteString("\nGROUP BY ")
		groupItems := []string{}
		for _, item := range q.GroupBy.Items {
			// Map variable to table alias
			var tableAlias string
			if item.Variable == e1Var {
				tableAlias = "e1"
			} else if item.Variable == e2Var {
				tableAlias = "e2"
			} else {
				return nil, fmt.Errorf("unknown variable in GROUP BY: %s", item.Variable)
			}
			groupItems = append(groupItems, fmt.Sprintf("%s.%s", tableAlias, item.Property))
		}
		sql.WriteString(strings.Join(groupItems, ", "))
	}

	// ORDER BY clause
	if q.OrderBy != nil {
		sql.WriteString("\nORDER BY ")
		orderItems := []string{}
		for _, item := range q.OrderBy.Items {
			direction := "ASC"
			if !item.Ascending {
				direction = "DESC"
			}
			// Expression can be an alias or a column reference
			orderItems = append(orderItems, fmt.Sprintf("%s %s", item.Expression, direction))
		}
		sql.WriteString(strings.Join(orderItems, ", "))
	}

	// LIMIT clause
	if q.Limit != nil {
		sql.WriteString(fmt.Sprintf("\nLIMIT %d", q.Limit.Count))
	}

	return &TranspileResult{
		SQL:  sql.String(),
		Args: args,
	}, nil
}

// generateMultiHopSQL generates SQL with recursive CTE for multi-hop pattern matching
func generateMultiHopSQL(q *ast.Query, opts TranspileOptions) (*TranspileResult, error) {
	var sql strings.Builder
	var args []interface{}

	pattern := q.Match.Pattern
	sourceNode := pattern.SourceNode
	edge := pattern.Edge
	targetNode := pattern.TargetNode

	// Determine direction
	var e1Var, e2Var string
	var e1Label, e2Label string

	if edge.Direction == "->" {
		e1Var = sourceNode.Variable
		e1Label = sourceNode.Label
		e2Var = targetNode.Variable
		e2Label = targetNode.Label
	} else if edge.Direction == "<-" {
		e1Var = targetNode.Variable
		e1Label = targetNode.Label
		e2Var = sourceNode.Variable
		e2Label = sourceNode.Label
	} else {
		return nil, fmt.Errorf("undirected edges not supported in multi-hop")
	}

	// Build recursive CTE
	sql.WriteString("WITH RECURSIVE paths(source_id, target_id, depth) AS (\n")

	// Base case: direct edges (depth 1)
	sql.WriteString("  SELECT g.source_entity_id, g.target_entity_id, 1\n")
	sql.WriteString("  FROM graph_edges g\n")
	sql.WriteString("  JOIN entities e1 ON g.source_entity_id = e1.id\n")
	sql.WriteString("  JOIN entities e2 ON g.target_entity_id = e2.id\n")
	sql.WriteString("  WHERE 1=1\n")

	// Add base case filters
	if e1Label != "" {
		sql.WriteString("    AND e1.entity_type = ?\n")
		args = append(args, e1Label)
	}
	if edge.Type != "" {
		sql.WriteString("    AND g.relation_type = ?\n")
		args = append(args, edge.Type)
	}
	if e2Label != "" {
		sql.WriteString("    AND e2.entity_type = ?\n")
		args = append(args, e2Label)
	}

	sql.WriteString("\n  UNION ALL\n\n")

	// Recursive case: extend paths
	sql.WriteString("  SELECT p.source_id, g.target_entity_id, p.depth + 1\n")
	sql.WriteString("  FROM paths p\n")
	sql.WriteString("  JOIN graph_edges g ON p.target_id = g.source_entity_id\n")

	// Add edge type filter for recursive step
	if edge.Type != "" {
		sql.WriteString("  WHERE g.relation_type = ?\n")
		args = append(args, edge.Type)
	}

	// Add max depth constraint
	maxDepth := edge.MaxHops
	if maxDepth == 0 {
		maxDepth = 10 // Default
	}
	if edge.Type != "" {
		sql.WriteString("    AND p.depth < ?\n")
	} else {
		sql.WriteString("  WHERE p.depth < ?\n")
	}
	args = append(args, maxDepth)

	sql.WriteString(")\n")

	// Main SELECT from the CTE
	sql.WriteString("SELECT DISTINCT ")
	selectItems := []string{}
	for _, item := range q.Return.Items {
		var tableAlias string
		if item.Variable == e1Var {
			tableAlias = "e1"
		} else if item.Variable == e2Var {
			tableAlias = "e2"
		} else {
			return nil, fmt.Errorf("unknown variable in RETURN: %s", item.Variable)
		}

		if item.Property != "" {
			if item.Property == "snippet" {
				selectItems = append(selectItems, fmt.Sprintf("c%s.content_snippet AS %s_snippet",
					tableAlias[1:], item.Variable))
			} else if item.Property == "file" {
				selectItems = append(selectItems, fmt.Sprintf("f%s.path AS %s_file",
					tableAlias[1:], item.Variable))
			} else if item.Property == "lines" {
				selectItems = append(selectItems, fmt.Sprintf("(c%s.start_line || '-' || c%s.end_line) AS %s_lines",
					tableAlias[1:], tableAlias[1:], item.Variable))
			} else {
				selectItems = append(selectItems, fmt.Sprintf("%s.%s AS %s_%s",
					tableAlias, item.Property, item.Variable, item.Property))
			}
		} else {
			selectItems = append(selectItems, fmt.Sprintf("%s.*", tableAlias))
		}
	}
	sql.WriteString(strings.Join(selectItems, ", "))

	// FROM the CTE result joined with entities
	sql.WriteString("\nFROM paths p")
	sql.WriteString("\nJOIN entities e1 ON p.source_id = e1.id")
	sql.WriteString("\nJOIN entities e2 ON p.target_id = e2.id")
	sql.WriteString("\nLEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id")
	sql.WriteString("\nLEFT JOIN files f1 ON c1.file_id = f1.id")
	sql.WriteString("\nLEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id")
	sql.WriteString("\nLEFT JOIN files f2 ON c2.file_id = f2.id")

	// Add min depth constraint if specified
	if edge.MinHops > 0 {
		sql.WriteString("\nWHERE p.depth >= ?")
		args = append(args, edge.MinHops)
	}

	return &TranspileResult{
		SQL:  sql.String(),
		Args: args,
	}, nil
}

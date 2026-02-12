package cypher

import (
	"strings"
	"testing"
)

func TestTranspileBasic(t *testing.T) {
	tests := []struct {
		name     string
		cypher   string
		wantSQL  string
		wantArgs []interface{}
		wantErr  bool
	}{
		{
			name:   "basic forward relation",
			cypher: "MATCH (f:FUNCTION)-[:calls]->(t:FUNCTION) RETURN f.name, t.name",
			wantSQL: `SELECT e1.name AS f_name, e2.name AS t_name
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE e1.entity_type = ?
  AND g.relation_type = ?
  AND e2.entity_type = ?`,
			wantArgs: []interface{}{"FUNCTION", "calls", "FUNCTION"},
			wantErr:  false,
		},
		{
			name:   "wildcard target",
			cypher: "MATCH (p:PACKAGE)-[:imports]->(t) RETURN p.name, t.name",
			wantSQL: `SELECT e1.name AS p_name, e2.name AS t_name
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE e1.entity_type = ?
  AND g.relation_type = ?`,
			wantArgs: []interface{}{"PACKAGE", "imports"},
			wantErr:  false,
		},
		{
			name:   "backward relation",
			cypher: "MATCH (i:INTERFACE)<-[:implements]-(s:STRUCT) RETURN i.name, s.name",
			wantSQL: `SELECT e2.name AS i_name, e1.name AS s_name
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE e1.entity_type = ?
  AND g.relation_type = ?
  AND e2.entity_type = ?`,
			wantArgs: []interface{}{"STRUCT", "implements", "INTERFACE"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Transpile(tt.cypher, TranspileOptions{})

			if (err != nil) != tt.wantErr {
				t.Errorf("Transpile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			if result.SQL != tt.wantSQL {
				t.Errorf("Transpile() SQL mismatch:\nGot:\n%s\n\nWant:\n%s", result.SQL, tt.wantSQL)
			}

			if len(result.Args) != len(tt.wantArgs) {
				t.Errorf("Transpile() args length = %d, want %d", len(result.Args), len(tt.wantArgs))
				return
			}

			for i := range result.Args {
				if result.Args[i] != tt.wantArgs[i] {
					t.Errorf("Transpile() args[%d] = %v, want %v", i, result.Args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestTranspileWithCWD(t *testing.T) {
	tests := []struct {
		name     string
		cypher   string
		cwd      string
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:   "filter by CWD",
			cypher: "MATCH (f:FUNCTION)-[:calls]->(t) RETURN f.name, t.name",
			cwd:    "/home/user/project/pkg/db",
			wantSQL: `SELECT e1.name AS f_name, e2.name AS t_name
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE (f1.path LIKE ? OR f2.path LIKE ?)
  AND e1.entity_type = ?
  AND g.relation_type = ?`,
			wantArgs: []interface{}{"/home/user/project/pkg/db/%", "/home/user/project/pkg/db/%", "FUNCTION", "calls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := TranspileOptions{CWD: tt.cwd}
			result, err := Transpile(tt.cypher, opts)
			if err != nil {
				t.Fatalf("Transpile() error = %v", err)
			}

			if result.SQL != tt.wantSQL {
				t.Errorf("SQL mismatch:\nGot:\n%s\n\nWant:\n%s", result.SQL, tt.wantSQL)
			}

			if len(result.Args) != len(tt.wantArgs) {
				t.Errorf("Args length = %d, want %d", len(result.Args), len(tt.wantArgs))
				return
			}

			for i, arg := range result.Args {
				if arg != tt.wantArgs[i] {
					t.Errorf("Arg[%d] = %v, want %v", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestMultiHopTranspile(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantHops bool
		minHops  int
		maxHops  int
	}{
		{
			name:     "1 to 3 hops",
			query:    `MATCH (a:FUNCTION)-[:calls*1..3]->(b) RETURN a.name, b.name`,
			wantHops: true,
			minHops:  1,
			maxHops:  3,
		},
		{
			name:     "2 to 2 hops (single value)",
			query:    `MATCH (a)-[:uses*2]->(b:TYPE) RETURN a.name`,
			wantHops: true,
			minHops:  2,
			maxHops:  2,
		},
		{
			name:     "any relation multihop",
			query:    `MATCH (a)-[*1..5]->(b) RETURN a.name, b.name`,
			wantHops: true,
			minHops:  1,
			maxHops:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Transpile(tt.query, TranspileOptions{})
			if err != nil {
				t.Fatalf("Transpile failed: %v", err)
			}

			if tt.wantHops {
				// Should contain recursive CTE
				if !strings.Contains(result.SQL, "WITH RECURSIVE") {
					t.Errorf("Expected recursive CTE, got:\n%s", result.SQL)
				}
				if !strings.Contains(result.SQL, "paths") {
					t.Errorf("Expected paths CTE, got:\n%s", result.SQL)
				}
			}

			t.Logf("Generated SQL:\n%s", result.SQL)
			t.Logf("Args: %v", result.Args)
		})
	}
}

func TestAggregateTranspile(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:  "count with group by",
			query: "MATCH (a)-[:calls]->(b) RETURN b.name, COUNT(a) AS callers GROUP BY b.name",
			wantSQL: `SELECT e2.name AS b_name, COUNT(DISTINCT e1.id) AS callers
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE g.relation_type = ?
GROUP BY e2.name`,
			wantArgs: []interface{}{"calls"},
		},
		{
			name:  "count with order by and limit",
			query: "MATCH (a)-[:calls]->(b) RETURN b.name, COUNT(a) AS callers GROUP BY b.name ORDER BY callers DESC LIMIT 10",
			wantSQL: `SELECT e2.name AS b_name, COUNT(DISTINCT e1.id) AS callers
FROM entities e1
JOIN graph_edges g ON g.source_entity_id = e1.id
JOIN entities e2 ON g.target_entity_id = e2.id
LEFT JOIN vec_chunks c1 ON e1.chunk_id = c1.chunk_id
LEFT JOIN files f1 ON c1.file_id = f1.id
LEFT JOIN vec_chunks c2 ON e2.chunk_id = c2.chunk_id
LEFT JOIN files f2 ON c2.file_id = f2.id
WHERE g.relation_type = ?
GROUP BY e2.name
ORDER BY callers DESC
LIMIT 10`,
			wantArgs: []interface{}{"calls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Transpile(tt.query, TranspileOptions{})
			if err != nil {
				t.Fatalf("Transpile() error = %v", err)
			}

			// Normalize whitespace for comparison
			gotSQL := strings.TrimSpace(result.SQL)
			wantSQL := strings.TrimSpace(tt.wantSQL)

			if gotSQL != wantSQL {
				t.Errorf("SQL mismatch:\nGot:\n%s\n\nWant:\n%s", gotSQL, wantSQL)
			}

			if len(result.Args) != len(tt.wantArgs) {
				t.Errorf("Args length = %d, want %d", len(result.Args), len(tt.wantArgs))
			}
		})
	}
}

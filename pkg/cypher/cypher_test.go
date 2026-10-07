package cypher

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wouteroostervld/chainsaw/pkg/db"
)

// projDir is where the files of the test graph are; results show it as /proj
var projDir string

// graph builds a small index:
//
//	/proj/app/main.go: main (package), Run -> Load, Run -> Server.Start, Server.Start -> log
//	/proj/lib/load.go: lib (package), Load -> parse, parse -> fmt.Println (not indexed)
func graph(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// The files exist, so their code can be read back
	dir := t.TempDir()
	add := func(path string, defs []db.SymbolDef, refs []db.SymbolRef) {
		path = strings.Replace(path, "/proj", dir, 1)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		var lines []string
		for i := 1; i <= 40; i++ {
			lines = append(lines, fmt.Sprintf("// %s line %d", filepath.Base(path), i))
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		fileID, err := database.UpsertFile(path, 1, "hash")
		if err != nil {
			t.Fatal(err)
		}
		chunkID, err := database.InsertChunk(fileID, 1, 40)
		if err != nil {
			t.Fatal(err)
		}
		for i := range defs {
			defs[i].ChunkID = chunkID
		}
		if err := database.StoreFileSymbols(fileID, dir, defs, refs); err != nil {
			t.Fatal(err)
		}
	}
	projDir = dir

	add("/proj/lib/load.go", []db.SymbolDef{
		{Name: "lib", EntityType: "PACKAGE", Parent: -1, StartLine: 1, EndLine: 40},
		{Name: "Load", EntityType: "FUNCTION", Parent: 0, Signature: "func Load(path string) error", StartLine: 5, EndLine: 12},
		{Name: "parse", EntityType: "FUNCTION", Parent: 0, Signature: "func parse(b []byte) error", StartLine: 14, EndLine: 20},
	}, []db.SymbolRef{
		{Source: 1, RelationType: "calls", Name: "parse", ExternalName: "parse"},
		{Source: 2, RelationType: "calls", Name: "Println", ImportBase: "fmt", ExternalName: "fmt.Println", Member: true},
	})
	add("/proj/app/main.go", []db.SymbolDef{
		{Name: "main", EntityType: "PACKAGE", Parent: -1, StartLine: 1, EndLine: 40},
		{Name: "Run", EntityType: "FUNCTION", Parent: 0, Signature: "func Run() error", StartLine: 8, EndLine: 15},
		{Name: "Server", EntityType: "STRUCT", Parent: 0, Signature: "type Server struct", StartLine: 17, EndLine: 20},
		{Name: "Start", Container: "Server", EntityType: "METHOD", Parent: 0, Signature: "func (s *Server) Start() error", StartLine: 22, EndLine: 30},
		{Name: "log", EntityType: "FUNCTION", Parent: 0, Signature: "func log(msg string)", StartLine: 32, EndLine: 34},
	}, []db.SymbolRef{
		{Source: 0, RelationType: "imports", Name: "lib", ImportBase: "lib", ExternalName: "example.com/proj/lib"},
		{Source: 1, RelationType: "calls", Name: "Load", ImportBase: "lib", ExternalName: "lib.Load", Member: true},
		{Source: 1, RelationType: "calls", Name: "Start", Container: "Server", ExternalName: "Start", Member: true},
		{Source: 3, RelationType: "calls", Name: "log", ExternalName: "log"},
	})
	return database
}

// run executes a query and returns its rows, each joined with spaces, sorted
// unless the query orders them
func run(t *testing.T, database *db.DB, query string, opts Options) []string {
	t.Helper()
	result, err := Transpile(query, opts)
	if err != nil {
		t.Fatalf("Transpile(%s) failed: %v", query, err)
	}
	rows, err := database.RawQuery(result.SQL, result.Args...)
	if err != nil {
		t.Fatalf("query failed: %v\n%s\n%v", err, result.SQL, result.Args)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		values := make([]any, len(result.Columns)+3*len(result.Locations))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(values))
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			parts[i] = fmt.Sprint(v)
		}
		out = append(out, strings.ReplaceAll(strings.Join(parts, " "), projDir, "/proj"))
	}
	if !strings.Contains(strings.ToUpper(query), "ORDER BY") {
		sort.Strings(out)
	}
	return out
}

func TestQueries(t *testing.T) {
	database := graph(t)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "who calls a function",
			query: `MATCH (f)-[:calls]->(t) WHERE t.name = "parse" RETURN f.name, f.file, f.lines`,
			want:  []string{"Load /proj/lib/load.go 5-12"},
		},
		{
			name:  "what a function calls, across files and outside the index",
			query: `MATCH (f {name: "Run"})-[:calls]->(t) RETURN t.name, t.type`,
			want:  []string{"Load FUNCTION", "Server.Start METHOD"},
		},
		{
			name:  "single node with a label and a filter",
			query: `MATCH (n:FUNCTION|METHOD) WHERE n.file CONTAINS "/app/" RETURN n.name, n.signature`,
			want:  []string{"Run func Run() error", "Server.Start func (s *Server) Start() error", "log func log(msg string)"},
		},
		{
			name:  "labels and relation types are not case-sensitive",
			query: `MATCH (n:struct) RETURN n.name`,
			want:  []string{"Server"},
		},
		{
			name:  "backward edge",
			query: `MATCH (t)<-[:calls]-(f) WHERE t.name = "log" RETURN f.name`,
			want:  []string{"Server.Start"},
		},
		{
			name:  "edge in either direction",
			query: `MATCH (a {name: "Load"})-[:calls]-(b) RETURN b.name`,
			want:  []string{"Run", "parse"},
		},
		{
			name:  "path of two edges",
			query: `MATCH (a)-[:calls]->(b)-[:calls]->(c) RETURN a.name, b.name, c.name`,
			want:  []string{"Load parse fmt.Println", "Run Load parse", "Run Server.Start log"},
		},
		{
			name:  "everything reachable within three calls",
			query: `MATCH (a {name: "Run"})-[:calls*1..3]->(b) RETURN b.name`,
			want:  []string{"Load", "Server.Start", "fmt.Println", "log", "parse"},
		},
		{
			name:  "exactly two calls away",
			query: `MATCH (a {name: "Run"})-[:calls*2]->(b) RETURN b.name`,
			want:  []string{"log", "parse"},
		},
		{
			name:  "everything that reaches a function, walking backward from it",
			query: `MATCH (a)-[:calls*]->(b {name: "fmt.Println"}) RETURN a.name`,
			want:  []string{"Load", "Run", "parse"},
		},
		{
			name:  "several relation types and the edge's own type",
			query: `MATCH (a {name: "main"})-[r:imports|defines]->(b) RETURN r.type, b.name`,
			want:  []string{"defines Run", "defines Server", "defines Server.Start", "defines log", "imports lib"},
		},
		{
			name:  "string predicates and boolean logic",
			query: `MATCH (n) WHERE (n.name STARTS WITH "Ser" OR n.name ENDS WITH "se") AND NOT n.type = "STRUCT" RETURN n.name`,
			want:  []string{"Server.Start", "parse"},
		},
		{
			name:  "regular expression and IN",
			query: `MATCH (n) WHERE n.name =~ "^[A-Z][a-z]+$" AND n.type IN ["FUNCTION", "STRUCT"] RETURN n.name`,
			want:  []string{"Load", "Run", "Server"},
		},
		{
			name:  "what is not in the index has no file",
			query: `MATCH (f)-[:calls]->(t) WHERE t.file IS NULL RETURN f.name, t.name, t.type`,
			want:  []string{"parse fmt.Println EXTERNAL"},
		},
		{
			name:  "counting groups by what else is returned",
			query: `MATCH (f)-[:calls]->(t) RETURN f.name, count(t) AS n ORDER BY n DESC, f.name LIMIT 2`,
			want:  []string{"Run 2", "Load 1"},
		},
		{
			name:  "count of everything",
			query: `MATCH (n:FUNCTION) RETURN count(*)`,
			want:  []string{"4"},
		},
		{
			name:  "a bare node returns what identifies it",
			query: `MATCH (n {name: "log"}) RETURN n`,
			want:  []string{"log FUNCTION /proj/app/main.go 32-34"},
		},
		{
			name:  "code is read from the file, not the index",
			query: `MATCH (n {name: "log"}) RETURN n.snippet`,
			want:  []string{"// main.go line 32\n// main.go line 33\n// main.go line 34"},
		},
		{
			name:  "and can be searched",
			query: `MATCH (n:FUNCTION) WHERE n.snippet CONTAINS "load.go line 14" RETURN n.name`,
			want:  []string{"parse"},
		},
		{
			name:  "order, skip and limit",
			query: `MATCH (n:FUNCTION) RETURN n.name ORDER BY n.name SKIP 1 LIMIT 2`,
			want:  []string{"Run", "log"},
		},
		{
			name:  "numeric comparison on lines",
			query: `MATCH (n) WHERE n.line > 20 AND n.end_line < 40 RETURN n.name`,
			want:  []string{"Server.Start", "log"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(t, database, tt.query, Options{}); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s\n got %q\nwant %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	database := graph(t)

	// Scoped to a directory: paths that touch it, from either end
	got := run(t, database, `MATCH (f)-[:calls]->(t) RETURN f.name, t.name`, Options{CWD: projDir + "/lib"})
	want := []string{"Load parse", "Run Load", "parse fmt.Println"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scoped to /proj/lib: got %q, want %q", got, want)
	}

	// Without a LIMIT the default applies, and says so
	result, err := Transpile(`MATCH (n) RETURN n.name`, Options{DefaultLimit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Limited || !strings.HasSuffix(result.SQL, "LIMIT 3") {
		t.Errorf("default limit not applied: limited=%v\n%s", result.Limited, result.SQL)
	}
	if result, _ = Transpile(`MATCH (n) RETURN n.name LIMIT 50`, Options{DefaultLimit: 3}); result.Limited {
		t.Error("default limit applied over the query's own")
	}

	// Locations follow the columns, three per returned node
	got = run(t, database, `MATCH (f)-[:calls]->(t {name: "log"}) RETURN f.name, t`, Options{Locations: true})
	want = []string{"Server.Start log FUNCTION /proj/app/main.go 32-34 /proj/app/main.go 22 30 /proj/app/main.go 32 34"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with locations: got %q, want %q", got, want)
	}
}

func TestErrors(t *testing.T) {
	for _, query := range []string{
		``,
		`RETURN n`,
		`MATCH (n)`,
		`MATCH (n RETURN n`,
		`MATCH (n) RETURN m.name`,
		`MATCH (n) RETURN n.colour`,
		`MATCH (n) WHERE n.name = RETURN n`,
		`MATCH (n) WHERE n.name ~ "x" RETURN n`,
		`MATCH (a)<-[:calls]->(b) RETURN a`,
		`MATCH (a)-[:calls*]-(b) RETURN a`,
		`MATCH (a)-[:calls*3..1]->(b) RETURN a`,
		`MATCH (a)-[a:calls]->(b) RETURN b`,
		`MATCH (n) RETURN n.name "unterminated`,
		`MATCH (n) RETURN n.name extra`,
	} {
		if _, err := Transpile(query, Options{}); err == nil {
			t.Errorf("Transpile(%q) did not fail", query)
		}
	}
}

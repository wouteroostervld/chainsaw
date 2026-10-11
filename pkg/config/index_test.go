package config

import (
	"strings"
	"testing"
)

func TestIndexResolver(t *testing.T) {
	fs := NewMockFileSystem()
	fs.AddFile("/src/semantic/.config.yaml", []byte("index:\n  embeddings: true\n"))
	fs.AddFile("/src/semantic/quiet/.config.yaml", []byte("index:\n  graph: false\n"))
	fs.AddFile("/src/filters/.config.yaml", []byte("exclude: [build]\n"))

	r, err := NewIndexResolver(&Profile{}, fs)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		path              string
		graph, embeddings bool
	}{
		// Nothing configured: the graph, and no embeddings
		{"/src/plain/a.go", true, false},
		// A project switches embeddings on, for everything below it
		{"/src/semantic/a.go", true, true},
		{"/src/semantic/deep/er/a.go", true, true},
		// The nearest .config.yaml is the one that counts
		{"/src/semantic/quiet/a.go", false, false},
		// One that says nothing about the index changes nothing
		{"/src/filters/a.go", true, false},
	} {
		kinds, err := r.For(tt.path)
		if err != nil {
			t.Fatalf("%s: %v", tt.path, err)
		}
		if kinds[IndexGraph] != tt.graph || kinds[IndexEmbeddings] != tt.embeddings {
			t.Errorf("%s: graph=%v embeddings=%v, want %v and %v", tt.path,
				kinds[IndexGraph], kinds[IndexEmbeddings], tt.graph, tt.embeddings)
		}
	}
}

func TestIndexResolver_Profile(t *testing.T) {
	fs := NewMockFileSystem()
	fs.AddFile("/src/off/.config.yaml", []byte("index:\n  embeddings: false\n"))

	r, err := NewIndexResolver(&Profile{Index: map[string]bool{IndexEmbeddings: true}}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if kinds, _ := r.For("/src/a.go"); !kinds[IndexGraph] || !kinds[IndexEmbeddings] {
		t.Errorf("profile switch not applied: %v", kinds)
	}
	if kinds, _ := r.For("/src/off/a.go"); !kinds[IndexGraph] || kinds[IndexEmbeddings] {
		t.Errorf("a project could not switch embeddings off: %v", kinds)
	}
}

func TestIndexResolver_Reset(t *testing.T) {
	fs := NewMockFileSystem()
	r, err := NewIndexResolver(&Profile{}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if kinds, _ := r.For("/src/p/a.go"); kinds[IndexEmbeddings] {
		t.Fatal("embeddings on by default")
	}

	fs.AddFile("/src/p/.config.yaml", []byte("index:\n  embeddings: true\n"))
	if kinds, _ := r.For("/src/p/a.go"); kinds[IndexEmbeddings] {
		t.Error("a new .config.yaml was read without a reset")
	}
	r.Reset()
	if kinds, _ := r.For("/src/p/a.go"); !kinds[IndexEmbeddings] {
		t.Error("a new .config.yaml was not read after a reset")
	}
}

func TestIndexResolver_UnknownKind(t *testing.T) {
	fs := NewMockFileSystem()
	if _, err := NewIndexResolver(&Profile{Index: map[string]bool{"vibes": true}}, fs); err == nil || !strings.Contains(err.Error(), "vibes") {
		t.Errorf("unknown kind in the profile: err = %v", err)
	}

	fs.AddFile("/src/p/.config.yaml", []byte("index:\n  vibes: true\n"))
	r, _ := NewIndexResolver(&Profile{}, fs)
	if _, err := r.For("/src/p/a.go"); err == nil || !strings.Contains(err.Error(), "/src/p/.config.yaml") {
		t.Errorf("unknown kind in a project: err = %v", err)
	}
}

func TestEmbeddingSettings(t *testing.T) {
	for _, tt := range []struct {
		name    string
		profile Profile
		want    EmbeddingsConfig
	}{
		{"nothing configured", Profile{},
			EmbeddingsConfig{Provider: "ollama", BaseURL: "http://localhost:11434"}},
		{"model of an older configuration", Profile{EmbeddingModel: "nomic-embed-text"},
			EmbeddingsConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "nomic-embed-text"}},
		{"an API key means the OpenAI protocol", Profile{Embeddings: &EmbeddingsConfig{BaseURL: "https://openrouter.ai/api/v1", APIKey: "k", Model: "m"}},
			EmbeddingsConfig{Provider: "openai", BaseURL: "https://openrouter.ai/api/v1", APIKey: "k", Model: "m"}},
		{"so does a /v1 address", Profile{Embeddings: &EmbeddingsConfig{BaseURL: "http://localhost:8080/v1/", Model: "m"}},
			EmbeddingsConfig{Provider: "openai", BaseURL: "http://localhost:8080/v1/", Model: "m"}},
		{"unless the provider is named", Profile{Embeddings: &EmbeddingsConfig{Provider: "ollama", APIKey: "k", Model: "m"}},
			EmbeddingsConfig{Provider: "ollama", BaseURL: "http://localhost:11434", APIKey: "k", Model: "m"}},
	} {
		if got := tt.profile.EmbeddingSettings(); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestDaemonScan(t *testing.T) {
	on, off := true, false
	for _, tt := range []struct {
		name   string
		daemon *DaemonConfig
		want   string // Kinds, comma-separated; empty for no scan
	}{
		// Nothing configured: scan, and only for what costs nothing but parsing
		{"default", nil, "graph"},
		{"switched on", &DaemonConfig{Scan: &on}, "graph"},
		{"switched off", &DaemonConfig{Scan: &off}, ""},
		{"kinds named", &DaemonConfig{ScanKinds: []string{"graph", "embeddings"}}, "graph,embeddings"},
		// Off stays off whatever kinds are named
		{"off with kinds", &DaemonConfig{Scan: &off, ScanKinds: []string{"graph"}}, ""},
	} {
		kinds, err := (&Profile{Daemon: tt.daemon}).DaemonScan()
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := strings.Join(kinds, ","); got != tt.want {
			t.Errorf("%s: scan for %q, want %q", tt.name, got, tt.want)
		}
	}

	// A kind that does not exist is a mistake, not something to pass over
	if _, err := (&Profile{Daemon: &DaemonConfig{ScanKinds: []string{"grahp"}}}).DaemonScan(); err == nil {
		t.Error("an unknown kind of index was accepted")
	}
}

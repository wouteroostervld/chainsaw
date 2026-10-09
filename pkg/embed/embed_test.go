package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type request struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

func TestEmbed_Ollama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/embed" || req.Model != "m" || r.Header.Get("Authorization") != "" {
			t.Errorf("request to %s for model %q with authorization %q", r.URL.Path, req.Model, r.Header.Get("Authorization"))
		}
		vectors := make([][]float32, len(req.Input))
		for i, text := range req.Input {
			vectors[i] = []float32{float32(len(text)), 1}
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
	}))
	defer server.Close()

	client, err := New(Config{Provider: ProviderOllama, BaseURL: server.URL + "/", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), []string{"a", "bcd"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Errorf("vectors = %v", vectors)
	}
}

func TestEmbed_OpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("request to %s with authorization %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		// Answers out of order
		var data []map[string]any
		for i := len(req.Input) - 1; i >= 0; i-- {
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(len(req.Input[i]))}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	client, err := New(Config{Provider: ProviderOpenAI, BaseURL: server.URL + "/v1", APIKey: "key", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), []string{"a", "bcd"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Errorf("vectors = %v", vectors)
	}
}

func TestEmbed_Errors(t *testing.T) {
	answer := `{"error":"model \"m\" not found"}`
	status := http.StatusNotFound
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	defer server.Close()

	client, _ := New(Config{Provider: ProviderOllama, BaseURL: server.URL, Model: "m"})

	// What the server says is passed on
	if _, err := client.Embed(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown model: err = %v", err)
	}

	// Fewer vectors than texts is not quietly accepted
	status, answer = http.StatusOK, `{"embeddings":[[1,2]]}`
	if _, err := client.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Error("one embedding for two texts was accepted")
	}

	// Nothing to embed asks nothing
	if vectors, err := client.Embed(context.Background(), nil); err != nil || vectors != nil {
		t.Errorf("no texts: %v, %v", vectors, err)
	}
}

func TestNew_Invalid(t *testing.T) {
	for _, cfg := range []Config{
		{Provider: ProviderOllama, BaseURL: "http://localhost"},
		{Provider: "telepathy", BaseURL: "http://localhost", Model: "m"},
		{Provider: ProviderOpenAI, Model: "m"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v) succeeded", cfg)
		}
	}
}

func TestTaskPrefix(t *testing.T) {
	if got := DocumentText("nomic-embed-text:latest", "x"); got != "search_document: x" {
		t.Errorf("document text = %q", got)
	}
	if got := QueryText("nomic-embed-text", "x"); got != "search_query: x" {
		t.Errorf("query text = %q", got)
	}
	if got := QueryText("mxbai-embed-large", "x"); got != "x" {
		t.Errorf("query text for a model without prefixes = %q", got)
	}
}

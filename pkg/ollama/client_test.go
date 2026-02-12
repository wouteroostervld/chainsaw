package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   *Config
	}{
		{
			name:   "nil config uses defaults",
			config: nil,
			want: &Config{
				BaseURL:    "http://localhost:11434",
				Timeout:    60 * time.Second,
				MaxRetries: 3,
				RetryDelay: time.Second,
			},
		},
		{
			name: "partial config fills defaults",
			config: &Config{
				BaseURL: "http://custom:8080",
			},
			want: &Config{
				BaseURL:    "http://custom:8080",
				Timeout:    60 * time.Second,
				MaxRetries: 3,
				RetryDelay: time.Second,
			},
		},
		{
			name: "custom config respected",
			config: &Config{
				BaseURL:    "http://custom:8080",
				Timeout:    30 * time.Second,
				MaxRetries: 5,
				RetryDelay: 2 * time.Second,
			},
			want: &Config{
				BaseURL:    "http://custom:8080",
				Timeout:    30 * time.Second,
				MaxRetries: 5,
				RetryDelay: 2 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			if client.baseURL != tt.want.BaseURL {
				t.Errorf("baseURL = %v, want %v", client.baseURL, tt.want.BaseURL)
			}
			if client.httpClient.Timeout != tt.want.Timeout {
				t.Errorf("timeout = %v, want %v", client.httpClient.Timeout, tt.want.Timeout)
			}
			if client.maxRetries != tt.want.MaxRetries {
				t.Errorf("maxRetries = %v, want %v", client.maxRetries, tt.want.MaxRetries)
			}
			if client.retryDelay != tt.want.RetryDelay {
				t.Errorf("retryDelay = %v, want %v", client.retryDelay, tt.want.RetryDelay)
			}
		})
	}
}

func TestEmbed(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3, 0.4}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		resp := EmbeddingResponse{Embedding: expectedEmbedding}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	embedding, err := client.Embed(ctx, "nomic-embed-text", "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != len(expectedEmbedding) {
		t.Errorf("embedding length = %d, want %d", len(embedding), len(expectedEmbedding))
	}

	for i, v := range embedding {
		if v != expectedEmbedding[i] {
			t.Errorf("embedding[%d] = %f, want %f", i, v, expectedEmbedding[i])
		}
	}
}

func TestEmbed_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbeddingResponse{Embedding: []float32{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	_, err := client.Embed(ctx, "nomic-embed-text", "test text")
	if err == nil {
		t.Error("expected error for empty embedding, got nil")
	}
}

func TestEmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req EmbeddingRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Return different embeddings based on prompt
		embedding := []float32{0.1, 0.2, 0.3}
		if req.Prompt == "text2" {
			embedding = []float32{0.4, 0.5, 0.6}
		} else if req.Prompt == "text3" {
			embedding = []float32{0.7, 0.8, 0.9}
		}

		resp := EmbeddingResponse{Embedding: embedding}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	texts := []string{"text1", "text2", "text3"}
	embeddings, err := client.EmbedBatch(ctx, "nomic-embed-text", texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(embeddings) != 3 {
		t.Errorf("got %d embeddings, want 3", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 3 {
			t.Errorf("embedding %d has length %d, want 3", i, len(emb))
		}
	}
}

func TestGenerate(t *testing.T) {
	expectedResponse := "This is a generated response"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Stream {
			t.Error("stream should be false")
		}

		resp := GenerateResponse{
			Response: expectedResponse,
			Done:     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	response, err := client.Generate(ctx, "llama2", "test prompt", "test system")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if response != expectedResponse {
		t.Errorf("response = %q, want %q", response, expectedResponse)
	}
}

func TestExtractEdges(t *testing.T) {
	edgesJSON := `[
{"source": "funcA", "target": "funcB", "relation_type": "calls", "weight": 0.8},
{"source": "ClassX", "target": "ClassY", "relation_type": "inherits", "weight": 0.9}
]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GenerateResponse{
			Response: edgesJSON,
			Done:     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	edges, err := client.ExtractEdges(ctx, "llama2", "func funcA() { funcB() }")
	if err != nil {
		t.Fatalf("ExtractEdges failed: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("got %d edges, want 2", len(edges))
	}

	if edges[0].Source != "funcA" || edges[0].Target != "funcB" {
		t.Errorf("edge 0 mismatch: got %s->%s, want funcA->funcB", edges[0].Source, edges[0].Target)
	}

	if edges[0].RelationType != "calls" {
		t.Errorf("edge 0 relation = %s, want calls", edges[0].RelationType)
	}

	if edges[1].RelationType != "inherits" {
		t.Errorf("edge 1 relation = %s, want inherits", edges[1].RelationType)
	}
}

func TestExtractEdges_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GenerateResponse{
			Response: "This is not JSON",
			Done:     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	_, err := client.ExtractEdges(ctx, "llama2", "code")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx := context.Background()

	if err := client.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestPing_ServerDown(t *testing.T) {
	client := NewClient(&Config{
		BaseURL:    "http://localhost:99999",
		MaxRetries: 0,
		Timeout:    100 * time.Millisecond,
	})
	ctx := context.Background()

	if err := client.Ping(ctx); err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestRetryLogic(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := EmbeddingResponse{Embedding: []float32{0.1, 0.2}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL:    server.URL,
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	})
	ctx := context.Background()

	_, err := client.Embed(ctx, "nomic-embed-text", "test")
	if err != nil {
		t.Errorf("expected success after retries, got error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := EmbeddingResponse{Embedding: []float32{0.1}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, MaxRetries: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Embed(ctx, "nomic-embed-text", "test")
	if err == nil {
		t.Error("expected context timeout error, got nil")
	}
}

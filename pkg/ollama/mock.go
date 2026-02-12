package ollama

import (
	"context"
	"fmt"
	"sync"
)

// MockClient is a mock implementation of OllamaClient for testing
type MockClient struct {
	mu sync.Mutex

	// Configurable responses
	EmbedFunc        func(ctx context.Context, model, text string) ([]float32, error)
	EmbedBatchFunc   func(ctx context.Context, model string, texts []string) ([][]float32, error)
	GenerateFunc     func(ctx context.Context, model, prompt, system string) (string, error)
	ExtractEdgesFunc func(ctx context.Context, model, code string) ([]Edge, error)
	PingFunc         func(ctx context.Context) error

	// Call tracking
	EmbedCalls        []EmbedCall
	EmbedBatchCalls   []EmbedBatchCall
	GenerateCalls     []GenerateCall
	ExtractEdgesCalls []ExtractEdgesCall
	PingCalls         int
}

type EmbedCall struct {
	Model string
	Text  string
}

type EmbedBatchCall struct {
	Model string
	Texts []string
}

type GenerateCall struct {
	Model  string
	Prompt string
	System string
}

type ExtractEdgesCall struct {
	Model string
	Code  string
}

// NewMockClient creates a new mock client with default behaviors
func NewMockClient() *MockClient {
	return &MockClient{
		EmbedFunc: func(ctx context.Context, model, text string) ([]float32, error) {
			// Return dummy embedding based on text length
			dim := 384
			if len(text) < dim {
				dim = len(text)
			}
			embedding := make([]float32, dim)
			for i := range embedding {
				embedding[i] = float32(i) / float32(dim)
			}
			return embedding, nil
		},
		EmbedBatchFunc: func(ctx context.Context, model string, texts []string) ([][]float32, error) {
			embeddings := make([][]float32, len(texts))
			for i := range texts {
				embedding := make([]float32, 384)
				for j := range embedding {
					embedding[j] = float32(i+j) / 384.0
				}
				embeddings[i] = embedding
			}
			return embeddings, nil
		},
		GenerateFunc: func(ctx context.Context, model, prompt, system string) (string, error) {
			return "Generated response for: " + prompt, nil
		},
		ExtractEdgesFunc: func(ctx context.Context, model, code string) ([]Edge, error) {
			return []Edge{
				{Source: "funcA", Target: "funcB", RelationType: "calls", Weight: 0.8},
			}, nil
		},
		PingFunc: func(ctx context.Context) error {
			return nil
		},
	}
}

// Embed implements OllamaClient
func (m *MockClient) Embed(ctx context.Context, model, text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EmbedCalls = append(m.EmbedCalls, EmbedCall{Model: model, Text: text})
	return m.EmbedFunc(ctx, model, text)
}

// EmbedBatch implements OllamaClient
func (m *MockClient) EmbedBatch(ctx context.Context, model string, texts []string) ([][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EmbedBatchCalls = append(m.EmbedBatchCalls, EmbedBatchCall{Model: model, Texts: texts})
	return m.EmbedBatchFunc(ctx, model, texts)
}

// Generate implements OllamaClient
func (m *MockClient) Generate(ctx context.Context, model, prompt, system string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GenerateCalls = append(m.GenerateCalls, GenerateCall{Model: model, Prompt: prompt, System: system})
	return m.GenerateFunc(ctx, model, prompt, system)
}

// ExtractEdges implements OllamaClient
func (m *MockClient) ExtractEdges(ctx context.Context, model, code string) ([]Edge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ExtractEdgesCalls = append(m.ExtractEdgesCalls, ExtractEdgesCall{Model: model, Code: code})
	return m.ExtractEdgesFunc(ctx, model, code)
}

// Ping implements OllamaClient
func (m *MockClient) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PingCalls++
	return m.PingFunc(ctx)
}

// Reset clears all call tracking
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EmbedCalls = nil
	m.EmbedBatchCalls = nil
	m.GenerateCalls = nil
	m.ExtractEdgesCalls = nil
	m.PingCalls = 0
}

// SetError configures the mock to return an error for all operations
func (m *MockClient) SetError(err error) {
	m.EmbedFunc = func(ctx context.Context, model, text string) ([]float32, error) {
		return nil, err
	}
	m.EmbedBatchFunc = func(ctx context.Context, model string, texts []string) ([][]float32, error) {
		return nil, err
	}
	m.GenerateFunc = func(ctx context.Context, model, prompt, system string) (string, error) {
		return "", err
	}
	m.ExtractEdgesFunc = func(ctx context.Context, model, code string) ([]Edge, error) {
		return nil, err
	}
	m.PingFunc = func(ctx context.Context) error {
		return err
	}
}

// Verify mock implements interface

// Helper for common test errors
var (
	ErrMockServerDown   = fmt.Errorf("mock: server unreachable")
	ErrMockTimeout      = fmt.Errorf("mock: request timeout")
	ErrMockInvalidModel = fmt.Errorf("mock: invalid model")
)

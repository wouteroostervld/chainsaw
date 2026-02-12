package config

import (
	"fmt"
	"io/fs"
	"os"
	"time"
)

// MockFileSystem is a mock implementation of FileSystem for testing
type MockFileSystem struct {
	Files   map[string][]byte // path -> content
	HomeDir string
}

// NewMockFileSystem creates a new mock filesystem
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files:   make(map[string][]byte),
		HomeDir: "/home/testuser",
	}
}

func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	data, ok := m.Files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

func (m *MockFileSystem) Stat(path string) (fs.FileInfo, error) {
	if _, ok := m.Files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return &mockFileInfo{name: path}, nil
}

func (m *MockFileSystem) Abs(path string) (string, error) {
	// Simple implementation - just return the path as-is
	// In reality, filepath.Abs does more
	return path, nil
}

func (m *MockFileSystem) UserHomeDir() (string, error) {
	return m.HomeDir, nil
}

// AddFile adds a file to the mock filesystem
func (m *MockFileSystem) AddFile(path string, content []byte) {
	m.Files[path] = content
}

// mockFileInfo implements fs.FileInfo for testing
type mockFileInfo struct {
	name string
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

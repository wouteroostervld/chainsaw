package config

import (
	"sync"
	"testing"
	"time"
)

// MockWatcher is a mock implementation of ConfigWatcher for testing
type MockWatcher struct {
	mu       sync.Mutex
	events   chan WatchEvent
	watching map[string]bool
}

func NewMockWatcher() *MockWatcher {
	return &MockWatcher{
		events:   make(chan WatchEvent, 10),
		watching: make(map[string]bool),
	}
}

func (m *MockWatcher) Watch(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watching[path] = true
	return nil
}

func (m *MockWatcher) Unwatch(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.watching, path)
	return nil
}

func (m *MockWatcher) Events() <-chan WatchEvent {
	return m.events
}

func (m *MockWatcher) Close() error {
	close(m.events)
	return nil
}

// SendEvent simulates a file change event
func (m *MockWatcher) SendEvent(path, op string) {
	m.events <- WatchEvent{Path: path, Operation: op}
}

// IsWatching checks if a path is being watched
func (m *MockWatcher) IsWatching(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.watching[path]
}

func TestWatchedConfigLoader(t *testing.T) {
	// Setup mock filesystem
	mockFS := NewMockFileSystem()
	mockFS.HomeDir = "/home/testuser"

	globalConfig := `
version: "2.0"
active_profile: "coding"
profiles:
  coding:
    include: ["/home/testuser/project"]
    exclude: ["node_modules"]
    blacklist: []
    whitelist: []
`
	mockFS.AddFile("/home/testuser/.chainsaw/config.yaml", []byte(globalConfig))

	localConfig := `
exclude: ["tmp/"]
`
	mockFS.AddFile("/home/testuser/project/.config.yaml", []byte(localConfig))

	// Create loader stack
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)
	mockWatcher := NewMockWatcher()
	watchedLoader := NewWatchedConfigLoader(cachedLoader, mockWatcher)
	defer watchedLoader.Close()

	// Load global config
	global, err := loader.LoadGlobalFromPath("/home/testuser/.chainsaw/config.yaml")
	if err != nil {
		t.Fatalf("Failed to load global config: %v", err)
	}

	// Get config for a file
	_, err = watchedLoader.GetForFile("/home/testuser/project/main.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed: %v", err)
	}

	// Verify local config is being watched
	if !mockWatcher.IsWatching("/home/testuser/project/.config.yaml") {
		t.Error("Expected local config to be watched")
	}

	// Verify config is cached
	cachedConfig, ok := cache.Get("/home/testuser/project/.config.yaml")
	if !ok {
		t.Error("Expected config to be cached")
	}

	if len(cachedConfig.Exclude) != 2 { // node_modules + tmp/
		t.Errorf("Expected 2 exclude entries, got %d", len(cachedConfig.Exclude))
	}

	// Simulate config file modification
	mockWatcher.SendEvent("/home/testuser/project/.config.yaml", "modified")

	// Give event handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify cache was invalidated
	_, ok = cache.Get("/home/testuser/project/.config.yaml")
	if ok {
		t.Error("Expected cache to be invalidated after config modification")
	}
}

func TestWatchedConfigLoaderReindexCallback(t *testing.T) {
	mockFS := NewMockFileSystem()
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)
	mockWatcher := NewMockWatcher()
	watchedLoader := NewWatchedConfigLoader(cachedLoader, mockWatcher)
	defer watchedLoader.Close()

	// Set up callback tracking
	var reindexedPaths []string
	var mu sync.Mutex

	watchedLoader.SetReindexCallback(func(configPath string) {
		mu.Lock()
		defer mu.Unlock()
		reindexedPaths = append(reindexedPaths, configPath)
	})

	// Simulate config changes
	mockWatcher.SendEvent("/home/user/project/.config.yaml", "modified")
	mockWatcher.SendEvent("/home/user/other/.config.yaml", "created")

	// Give callbacks time to execute
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(reindexedPaths) != 2 {
		t.Errorf("Expected 2 reindex callbacks, got %d", len(reindexedPaths))
	}

	if reindexedPaths[0] != "/home/user/project/.config.yaml" {
		t.Errorf("Expected first path to be /home/user/project/.config.yaml, got %s", reindexedPaths[0])
	}
}

func TestWatchedConfigLoaderDeletion(t *testing.T) {
	mockFS := NewMockFileSystem()
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)
	mockWatcher := NewMockWatcher()
	watchedLoader := NewWatchedConfigLoader(cachedLoader, mockWatcher)
	defer watchedLoader.Close()

	configPath := "/home/user/project/.config.yaml"

	// Add to cache
	cache.Set(configPath, &MergedConfig{ProfileName: "test"})

	// Start watching
	mockWatcher.Watch(configPath)

	// Simulate deletion
	mockWatcher.SendEvent(configPath, "deleted")

	// Give event handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify cache was invalidated
	_, ok := cache.Get(configPath)
	if ok {
		t.Error("Expected cache to be invalidated after config deletion")
	}

	// Verify file is no longer watched
	if mockWatcher.IsWatching(configPath) {
		t.Error("Expected file to be unwatched after deletion")
	}
}

func TestWatchedConfigLoaderNoLocalConfig(t *testing.T) {
	// Setup mock filesystem with no local config
	mockFS := NewMockFileSystem()
	mockFS.HomeDir = "/home/testuser"

	globalConfig := `
version: "2.0"
active_profile: "coding"
profiles:
  coding:
    include: ["/home/testuser/project"]
    exclude: []
    blacklist: []
    whitelist: []
`
	mockFS.AddFile("/home/testuser/.chainsaw/config.yaml", []byte(globalConfig))
	// No local config file

	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)
	mockWatcher := NewMockWatcher()
	watchedLoader := NewWatchedConfigLoader(cachedLoader, mockWatcher)
	defer watchedLoader.Close()

	global, err := loader.LoadGlobalFromPath("/home/testuser/.chainsaw/config.yaml")
	if err != nil {
		t.Fatalf("Failed to load global config: %v", err)
	}

	// Get config for a file (no local config)
	config, err := watchedLoader.GetForFile("/home/testuser/project/main.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed: %v", err)
	}

	// Verify no local config path
	if config.LocalConfigPath != "" {
		t.Error("Expected no local config path")
	}

	// Verify nothing is being watched (no local config to watch)
	// The mock watcher should have no entries
	if len(mockWatcher.watching) > 0 {
		t.Error("Expected no files to be watched when no local config exists")
	}
}

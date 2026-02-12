package config

import (
	"sync"
	"testing"
)

func TestMemoryCache(t *testing.T) {
	cache := NewMemoryCache()

	// Test empty cache
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected cache miss for non-existent key")
	}

	// Test set and get
	config1 := &MergedConfig{
		Include:     []string{"/home/user/project"},
		ProfileName: "test",
	}
	cache.Set("key1", config1)

	retrieved, ok := cache.Get("key1")
	if !ok {
		t.Error("Expected cache hit after set")
	}
	if retrieved.ProfileName != "test" {
		t.Errorf("Retrieved config ProfileName = %s, want test", retrieved.ProfileName)
	}

	// Test multiple entries
	config2 := &MergedConfig{
		Include:     []string{"/home/user/other"},
		ProfileName: "test2",
	}
	cache.Set("key2", config2)

	retrieved1, ok1 := cache.Get("key1")
	retrieved2, ok2 := cache.Get("key2")

	if !ok1 || !ok2 {
		t.Error("Expected both cache entries to exist")
	}
	if retrieved1.ProfileName != "test" || retrieved2.ProfileName != "test2" {
		t.Error("Cache entries not correctly stored")
	}

	// Test invalidate
	cache.Invalidate("key1")
	_, ok = cache.Get("key1")
	if ok {
		t.Error("Expected cache miss after invalidate")
	}
	// key2 should still exist
	_, ok = cache.Get("key2")
	if !ok {
		t.Error("Expected key2 to still exist after invalidating key1")
	}

	// Test clear
	cache.Clear()
	_, ok1 = cache.Get("key1")
	_, ok2 = cache.Get("key2")
	if ok1 || ok2 {
		t.Error("Expected all entries to be cleared")
	}
}

func TestMemoryCacheConcurrency(t *testing.T) {
	cache := NewMemoryCache()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			config := &MergedConfig{
				ProfileName: string(rune('a' + n%26)),
			}
			cache.Set(string(rune('a'+n%26)), config)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cache.Get(string(rune('a' + n%26)))
		}(i)
	}

	wg.Wait()
	// If we get here without race conditions, test passes
}

func TestCachedLoader(t *testing.T) {
	// Create mock filesystem
	mockFS := NewMockFileSystem()
	mockFS.HomeDir = "/home/testuser"

	// Add global config
	globalConfig := `
version: "2.0"
active_profile: "coding"
profiles:
  coding:
    include: ["/home/testuser/project"]
    exclude: ["node_modules"]
    blacklist: [".*\\.secret$"]
    whitelist: []
`
	mockFS.AddFile("/home/testuser/.chainsaw/config.yaml", []byte(globalConfig))

	// Add local config
	localConfig := `
exclude: ["tmp/"]
blacklist: [".*\\.tmp$"]
`
	mockFS.AddFile("/home/testuser/project/.config.yaml", []byte(localConfig))

	// Create loader and cached loader
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)

	// Load global config
	global, err := loader.LoadGlobalFromPath("/home/testuser/.chainsaw/config.yaml")
	if err != nil {
		t.Fatalf("Failed to load global config: %v", err)
	}

	// First call - should miss cache and load
	config1, err := cachedLoader.GetForFile("/home/testuser/project/main.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed: %v", err)
	}

	// Check that config was merged correctly
	if len(config1.Exclude) != 2 { // node_modules + tmp/
		t.Errorf("Expected 2 exclude entries, got %d", len(config1.Exclude))
	}

	// Second call - should hit cache
	config2, err := cachedLoader.GetForFile("/home/testuser/project/other.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed on second call: %v", err)
	}

	// Should be the same cached instance
	if config1 != config2 {
		t.Error("Expected cached config to be returned")
	}

	// Invalidate cache
	cachedLoader.InvalidateLocalConfig("/home/testuser/project/.config.yaml")

	// Third call - should miss cache again
	config3, err := cachedLoader.GetForFile("/home/testuser/project/third.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed after invalidation: %v", err)
	}

	// Should be a different instance (newly loaded)
	if config1 == config3 {
		t.Error("Expected new config instance after cache invalidation")
	}
}

func TestCachedLoaderNoLocalConfig(t *testing.T) {
	// Create mock filesystem
	mockFS := NewMockFileSystem()
	mockFS.HomeDir = "/home/testuser"

	// Add global config only (no local config)
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

	// Create loader and cached loader
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)

	// Load global config
	global, err := loader.LoadGlobalFromPath("/home/testuser/.chainsaw/config.yaml")
	if err != nil {
		t.Fatalf("Failed to load global config: %v", err)
	}

	// Call for daemon
	config1, err := cachedLoader.GetForFile("/home/testuser/project/main.go", global, true)
	if err != nil {
		t.Fatalf("GetForFile failed: %v", err)
	}

	// Call for CLI with same file - should use different cache key
	config2, err := cachedLoader.GetForFile("/home/testuser/project/main.go", global, false)
	if err != nil {
		t.Fatalf("GetForFile failed: %v", err)
	}

	// Should be different instances (different cache keys)
	if config1 == config2 {
		t.Error("Expected different cache entries for daemon vs CLI")
	}

	// Verify both are cached by calling again
	config1b, _ := cachedLoader.GetForFile("/home/testuser/project/other.go", global, true)
	config2b, _ := cachedLoader.GetForFile("/home/testuser/project/other.go", global, false)

	if config1 != config1b {
		t.Error("Expected cached daemon config")
	}
	if config2 != config2b {
		t.Error("Expected cached CLI config")
	}
}

func TestClearCache(t *testing.T) {
	mockFS := NewMockFileSystem()
	loader := NewLoader(mockFS)
	cache := NewMemoryCache()
	cachedLoader := NewCachedLoader(loader, cache)

	// Add some entries to cache
	cache.Set("key1", &MergedConfig{ProfileName: "test1"})
	cache.Set("key2", &MergedConfig{ProfileName: "test2"})

	// Clear cache
	cachedLoader.ClearCache()

	// Verify cache is empty
	_, ok1 := cache.Get("key1")
	_, ok2 := cache.Get("key2")

	if ok1 || ok2 {
		t.Error("Expected cache to be cleared")
	}
}

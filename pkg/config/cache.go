package config

import (
	"sync"
)

// ConfigCache defines the interface for caching merged configurations
type ConfigCache interface {
	Get(key string) (*MergedConfig, bool)
	Set(key string, config *MergedConfig)
	Invalidate(key string)
	Clear()
}

// MemoryCache is a thread-safe in-memory implementation of ConfigCache
type MemoryCache struct {
	mu    sync.RWMutex
	cache map[string]*MergedConfig
}

// NewMemoryCache creates a new in-memory config cache
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		cache: make(map[string]*MergedConfig),
	}
}

// Get retrieves a cached config by key
func (c *MemoryCache) Get(key string) (*MergedConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	config, ok := c.cache[key]
	return config, ok
}

// Set stores a config in the cache
func (c *MemoryCache) Set(key string, config *MergedConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[key] = config
}

// Invalidate removes a config from the cache
func (c *MemoryCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, key)
}

// Clear removes all entries from the cache
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*MergedConfig)
}

// CachedLoader wraps a Loader with caching capabilities
type CachedLoader struct {
	loader *Loader
	cache  ConfigCache
}

// NewCachedLoader creates a new cached loader
func NewCachedLoader(loader *Loader, cache ConfigCache) *CachedLoader {
	return &CachedLoader{
		loader: loader,
		cache:  cache,
	}
}

// GetForFile gets config for a file with caching
// Cache key is the local config path (or empty string if no local config)
func (cl *CachedLoader) GetForFile(filePath string, global *GlobalConfig, isDaemon bool) (*MergedConfig, error) {
	// First, find the local config path (if any)
	// This determines our cache key
	localConfigPath, err := cl.loader.FindLocal(filePath)
	if err != nil {
		return nil, err
	}

	// Use local config path as cache key (empty string if no local config)
	cacheKey := localConfigPath
	if cacheKey == "" {
		// No local config - use a special key based on global profile
		cacheKey = "_global_" + global.ActiveProfile
		if isDaemon {
			cacheKey += "_daemon"
		} else {
			cacheKey += "_cli"
		}
	}

	// Check cache
	if cached, ok := cl.cache.Get(cacheKey); ok {
		return cached, nil
	}

	// Not in cache - load and merge
	merged, err := cl.loader.GetForFile(filePath, global, isDaemon)
	if err != nil {
		return nil, err
	}

	// Store in cache
	cl.cache.Set(cacheKey, merged)

	return merged, nil
}

// InvalidateLocalConfig invalidates the cache entry for a specific local config file
func (cl *CachedLoader) InvalidateLocalConfig(configPath string) {
	cl.cache.Invalidate(configPath)
}

// ClearCache clears all cached configs
func (cl *CachedLoader) ClearCache() {
	cl.cache.Clear()
}

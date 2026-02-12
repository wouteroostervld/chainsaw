package config

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher watches configuration files for changes
type ConfigWatcher interface {
	Watch(path string) error
	Unwatch(path string) error
	Events() <-chan WatchEvent
	Close() error
}

// WatchEvent represents a file change event
type WatchEvent struct {
	Path      string
	Operation string // "modified", "created", "deleted"
}

// FsnotifyWatcher implements ConfigWatcher using fsnotify
type FsnotifyWatcher struct {
	watcher  *fsnotify.Watcher
	events   chan WatchEvent
	done     chan struct{}
	mu       sync.Mutex
	watching map[string]bool
}

// NewFsnotifyWatcher creates a new fsnotify-based config watcher
func NewFsnotifyWatcher() (*FsnotifyWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	w := &FsnotifyWatcher{
		watcher:  watcher,
		events:   make(chan WatchEvent, 10),
		done:     make(chan struct{}),
		watching: make(map[string]bool),
	}

	// Start event processing goroutine
	go w.processEvents()

	return w, nil
}

// Watch starts watching a configuration file
func (w *FsnotifyWatcher) Watch(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Normalize path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Skip if already watching
	if w.watching[absPath] {
		return nil
	}

	// Add to fsnotify watcher
	if err := w.watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to watch %s: %w", absPath, err)
	}

	w.watching[absPath] = true
	return nil
}

// Unwatch stops watching a configuration file
func (w *FsnotifyWatcher) Unwatch(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if !w.watching[absPath] {
		return nil
	}

	if err := w.watcher.Remove(absPath); err != nil {
		return fmt.Errorf("failed to unwatch %s: %w", absPath, err)
	}

	delete(w.watching, absPath)
	return nil
}

// Events returns the channel for receiving watch events
func (w *FsnotifyWatcher) Events() <-chan WatchEvent {
	return w.events
}

// Close stops the watcher and cleans up resources
func (w *FsnotifyWatcher) Close() error {
	close(w.done)
	return w.watcher.Close()
}

// processEvents processes fsnotify events and translates them to WatchEvents
func (w *FsnotifyWatcher) processEvents() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Translate fsnotify event to our WatchEvent
			var op string
			switch {
			case event.Op&fsnotify.Write == fsnotify.Write:
				op = "modified"
			case event.Op&fsnotify.Create == fsnotify.Create:
				op = "created"
			case event.Op&fsnotify.Remove == fsnotify.Remove:
				op = "deleted"
			case event.Op&fsnotify.Rename == fsnotify.Rename:
				op = "deleted" // Treat rename as deletion
			default:
				continue // Ignore other events
			}

			select {
			case w.events <- WatchEvent{Path: event.Name, Operation: op}:
			case <-w.done:
				return
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue watching
			// In production, this should use proper logging
			_ = err

		case <-w.done:
			return
		}
	}
}

// WatchedConfigLoader wraps CachedLoader with automatic cache invalidation on file changes
type WatchedConfigLoader struct {
	cachedLoader *CachedLoader
	watcher      ConfigWatcher
	done         chan struct{}
	onReindex    func(configPath string) // Callback for re-indexing
}

// NewWatchedConfigLoader creates a loader with automatic cache invalidation
func NewWatchedConfigLoader(cachedLoader *CachedLoader, watcher ConfigWatcher) *WatchedConfigLoader {
	wcl := &WatchedConfigLoader{
		cachedLoader: cachedLoader,
		watcher:      watcher,
		done:         make(chan struct{}),
	}

	// Start event handling
	go wcl.handleEvents()

	return wcl
}

// GetForFile gets config for a file and automatically watches the local config if found
func (wcl *WatchedConfigLoader) GetForFile(filePath string, global *GlobalConfig, isDaemon bool) (*MergedConfig, error) {
	config, err := wcl.cachedLoader.GetForFile(filePath, global, isDaemon)
	if err != nil {
		return nil, err
	}

	// If a local config was used, watch it
	if config.LocalConfigPath != "" {
		if err := wcl.watcher.Watch(config.LocalConfigPath); err != nil {
			// Log error but don't fail - watching is optional
			_ = err
		}
	}

	return config, nil
}

// SetReindexCallback sets a callback to be called when a config file changes
func (wcl *WatchedConfigLoader) SetReindexCallback(callback func(configPath string)) {
	wcl.onReindex = callback
}

// Close stops watching and cleans up resources
func (wcl *WatchedConfigLoader) Close() error {
	close(wcl.done)
	return wcl.watcher.Close()
}

// handleEvents processes watch events and invalidates cache
func (wcl *WatchedConfigLoader) handleEvents() {
	for {
		select {
		case event, ok := <-wcl.watcher.Events():
			if !ok {
				return
			}

			// On modification, invalidate cache
			if event.Operation == "modified" || event.Operation == "created" {
				wcl.cachedLoader.InvalidateLocalConfig(event.Path)

				// Trigger re-index callback if set
				if wcl.onReindex != nil {
					wcl.onReindex(event.Path)
				}
			}

			// On deletion, unwatch and invalidate
			if event.Operation == "deleted" {
				wcl.cachedLoader.InvalidateLocalConfig(event.Path)
				wcl.watcher.Unwatch(event.Path)

				if wcl.onReindex != nil {
					wcl.onReindex(event.Path)
				}
			}

		case <-wcl.done:
			return
		}
	}
}

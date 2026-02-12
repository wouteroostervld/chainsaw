package config

import (
	"io/fs"
	"os"
)

// FileSystem abstracts filesystem operations for testing
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	Stat(path string) (fs.FileInfo, error)
	Abs(path string) (string, error)
	UserHomeDir() (string, error)
}

// RealFileSystem implements FileSystem using actual OS calls
type RealFileSystem struct{}

func (r *RealFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *RealFileSystem) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (r *RealFileSystem) Abs(path string) (string, error) {
	return os.UserHomeDir()
}

func (r *RealFileSystem) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

// Loader handles loading and merging configurations
type Loader struct {
	fs FileSystem
}

// NewLoader creates a new Loader with the given filesystem
func NewLoader(fs FileSystem) *Loader {
	return &Loader{fs: fs}
}

// NewDefaultLoader creates a Loader with real filesystem operations
func NewDefaultLoader() *Loader {
	return &Loader{fs: &RealFileSystem{}}
}

// LoadGlobal loads the global configuration from ~/.chainsaw/config.yaml
func (l *Loader) LoadGlobal() (*GlobalConfig, error) {
	home, err := l.fs.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return l.LoadGlobalFromPath(home + "/.chainsaw/config.yaml")
}

// LoadGlobalFromPath loads global config from a specific path
func (l *Loader) LoadGlobalFromPath(path string) (*GlobalConfig, error) {
	return LoadGlobalConfigFromPath(path, l.fs)
}

// FindLocal walks up from startDir to find the nearest .config.yaml file
func (l *Loader) FindLocal(startDir string) (string, error) {
	return FindLocalConfigWithFS(startDir, l.fs)
}

// LoadLocal loads and validates a local .config.yaml file
func (l *Loader) LoadLocal(path string) (*LocalConfig, error) {
	return LoadLocalConfigWithFS(path, l.fs)
}

// GetForFile finds and merges the appropriate config for a specific file
func (l *Loader) GetForFile(filePath string, global *GlobalConfig, isDaemon bool) (*MergedConfig, error) {
	return GetConfigForFileWithFS(filePath, global, isDaemon, l.fs)
}

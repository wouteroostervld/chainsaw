package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRelativePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name      string
		configDir string
		path      string
		want      string
		wantErr   bool
	}{
		{
			name:      "absolute path - return as-is",
			configDir: "/home/user/project",
			path:      "/opt/external",
			want:      "/opt/external",
			wantErr:   false,
		},
		{
			name:      "relative path - resolve to config dir",
			configDir: "/home/user/project",
			path:      "./vendor",
			want:      "/home/user/project/vendor",
			wantErr:   false,
		},
		{
			name:      "parent relative path",
			configDir: "/home/user/project",
			path:      "../shared",
			want:      "/home/user/shared",
			wantErr:   false,
		},
		{
			name:      "tilde path - expand to home",
			configDir: "/home/user/project",
			path:      "~/documents",
			want:      filepath.Join(home, "documents"),
			wantErr:   false,
		},
		{
			name:      "relative without dot prefix",
			configDir: "/home/user/project",
			path:      "src/main",
			want:      "/home/user/project/src/main",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveRelativePath(tt.configDir, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveRelativePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveRelativePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
		check   func(string) bool // Custom validation function
	}{
		{
			name:    "tilde expansion",
			path:    "~/project",
			wantErr: false,
			check: func(result string) bool {
				return result == filepath.Join(home, "project")
			},
		},
		{
			name:    "already absolute",
			path:    "/usr/local/bin",
			wantErr: false,
			check: func(result string) bool {
				return result == "/usr/local/bin"
			},
		},
		{
			name:    "relative path - converts to absolute",
			path:    ".",
			wantErr: false,
			check: func(result string) bool {
				return filepath.IsAbs(result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !tt.check(got) {
				t.Errorf("NormalizePath() = %v, failed validation check", got)
			}
		})
	}
}

func TestValidatePathSecurity(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		allowedRoots []string
		wantErr      bool
	}{
		{
			name:         "path within allowed root - allow",
			path:         "/home/user/project/src",
			allowedRoots: []string{"/home/user/project"},
			wantErr:      false,
		},
		{
			name:         "path equals allowed root - allow",
			path:         "/home/user/project",
			allowedRoots: []string{"/home/user/project"},
			wantErr:      false,
		},
		{
			name:         "path outside allowed roots - reject",
			path:         "/etc/passwd",
			allowedRoots: []string{"/home/user/project"},
			wantErr:      true,
		},
		{
			name:         "traversal attempt - reject",
			path:         "/home/user/project/../../etc/passwd",
			allowedRoots: []string{"/home/user/project"},
			wantErr:      true,
		},
		{
			name:         "multiple allowed roots - first matches",
			path:         "/home/user/project1/file",
			allowedRoots: []string{"/home/user/project1", "/home/user/project2"},
			wantErr:      false,
		},
		{
			name:         "multiple allowed roots - second matches",
			path:         "/home/user/project2/file",
			allowedRoots: []string{"/home/user/project1", "/home/user/project2"},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathSecurity(tt.path, tt.allowedRoots)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePathSecurity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateIncludeWithinGlobal(t *testing.T) {
	tests := []struct {
		name          string
		localInclude  []string
		globalInclude []string
		configDir     string
		wantErr       bool
	}{
		{
			name:          "local within global - allow",
			localInclude:  []string{"./vendor"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project",
			wantErr:       false,
		},
		{
			name:          "local outside global - reject",
			localInclude:  []string{"/opt/external"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project",
			wantErr:       true,
		},
		{
			name:          "local with parent path within global - allow",
			localInclude:  []string{"../project/vendor"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project/subdir",
			wantErr:       false,
		},
		{
			name:          "local with parent path outside global - reject",
			localInclude:  []string{"../../other"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project",
			wantErr:       true,
		},
		{
			name:          "multiple local paths all valid - allow",
			localInclude:  []string{"./vendor", "./lib"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project",
			wantErr:       false,
		},
		{
			name:          "multiple local paths one invalid - reject",
			localInclude:  []string{"./vendor", "/opt/external"},
			globalInclude: []string{"/home/user/project"},
			configDir:     "/home/user/project",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIncludeWithinGlobal(tt.localInclude, tt.globalInclude, tt.configDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIncludeWithinGlobal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolvePaths(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		paths     []string
		want      []string
	}{
		{
			name:      "resolve multiple relative paths",
			configDir: "/home/user/project",
			paths:     []string{"./vendor", "./lib", "../shared"},
			want: []string{
				"/home/user/project/vendor",
				"/home/user/project/lib",
				"/home/user/shared",
			},
		},
		{
			name:      "mix of relative and absolute",
			configDir: "/home/user/project",
			paths:     []string{"./vendor", "/opt/external"},
			want: []string{
				"/home/user/project/vendor",
				"/opt/external",
			},
		},
		{
			name:      "empty list",
			configDir: "/home/user/project",
			paths:     []string{},
			want:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePaths(tt.configDir, tt.paths)
			if err != nil {
				t.Errorf("ResolvePaths() error = %v", err)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("ResolvePaths() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ResolvePaths()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

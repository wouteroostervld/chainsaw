package config

import (
	"testing"
)

func TestMergeConfigForDaemon(t *testing.T) {
	tests := []struct {
		name           string
		global         *GlobalConfig
		local          *LocalConfig
		localConfigDir string
		wantInclude    []string
		wantExclude    []string
		wantBlacklist  []string
		wantWhitelist  []string
		wantErr        bool
	}{
		{
			name: "daemon with no local config",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{"node_modules", ".git"},
						Blacklist: []string{`.*\.secret$`},
						Whitelist: []string{`.*important\.secret$`},
					},
				},
			},
			local:          nil,
			localConfigDir: "",
			wantInclude:    []string{"/home/user/project"},
			wantExclude:    []string{"node_modules", ".git"},
			wantBlacklist:  []string{`.*\.secret$`},
			wantWhitelist:  []string{`.*important\.secret$`},
			wantErr:        false,
		},
		{
			name: "daemon adds to exclude and blacklist",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{"node_modules"},
						Blacklist: []string{`.*\.secret$`},
						Whitelist: []string{},
					},
				},
			},
			local: &LocalConfig{
				Exclude:   []string{"tmp/", "build/"},
				Blacklist: []string{`.*\.tmp$`},
			},
			localConfigDir: "/home/user/project",
			wantInclude:    []string{"/home/user/project"},
			wantExclude:    []string{"node_modules", "/home/user/project/tmp", "/home/user/project/build"},
			wantBlacklist:  []string{`.*\.secret$`, `.*\.tmp$`},
			wantWhitelist:  []string{},
			wantErr:        false,
		},
		{
			name: "daemon ignores local include",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{},
						Blacklist: []string{},
						Whitelist: []string{},
					},
				},
			},
			local: &LocalConfig{
				Include: []string{"./vendor"}, // Should be ignored by daemon
				Exclude: []string{"tmp/"},
			},
			localConfigDir: "/home/user/project",
			wantInclude:    []string{"/home/user/project"}, // Local include not added
			wantExclude:    []string{"/home/user/project/tmp"},
			wantBlacklist:  []string{},
			wantWhitelist:  []string{},
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeConfigForDaemon(tt.global, tt.local, tt.localConfigDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("MergeConfigForDaemon() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Check include (global only)
			if !stringSlicesEqual(got.Include, tt.wantInclude) {
				t.Errorf("MergeConfigForDaemon() Include = %v, want %v", got.Include, tt.wantInclude)
			}

			// Check exclude (merged)
			if !stringSlicesEqual(got.Exclude, tt.wantExclude) {
				t.Errorf("MergeConfigForDaemon() Exclude = %v, want %v", got.Exclude, tt.wantExclude)
			}

			// Check blacklist (merged)
			if !stringSlicesEqual(got.Blacklist, tt.wantBlacklist) {
				t.Errorf("MergeConfigForDaemon() Blacklist = %v, want %v", got.Blacklist, tt.wantBlacklist)
			}

			// Check whitelist (global only)
			if !stringSlicesEqual(got.Whitelist, tt.wantWhitelist) {
				t.Errorf("MergeConfigForDaemon() Whitelist = %v, want %v", got.Whitelist, tt.wantWhitelist)
			}
		})
	}
}

func TestMergeConfigForCLI(t *testing.T) {
	tests := []struct {
		name           string
		global         *GlobalConfig
		local          *LocalConfig
		localConfigDir string
		wantInclude    []string
		wantExclude    []string
		wantBlacklist  []string
		wantWhitelist  []string
		wantErr        bool
	}{
		{
			name: "CLI with no local config",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{"node_modules"},
						Blacklist: []string{`.*\.secret$`},
						Whitelist: []string{`.*important\.secret$`},
					},
				},
			},
			local:          nil,
			localConfigDir: "",
			wantInclude:    []string{"/home/user/project"},
			wantExclude:    []string{"node_modules"},
			wantBlacklist:  []string{`.*\.secret$`},
			wantWhitelist:  []string{`.*important\.secret$`},
			wantErr:        false,
		},
		{
			name: "CLI adds to include (within global scope)",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{},
						Blacklist: []string{},
						Whitelist: []string{},
					},
				},
			},
			local: &LocalConfig{
				Include: []string{"./vendor", "./lib"},
			},
			localConfigDir: "/home/user/project",
			wantInclude: []string{
				"/home/user/project",
				"/home/user/project/vendor",
				"/home/user/project/lib",
			},
			wantExclude:   []string{},
			wantBlacklist: []string{},
			wantWhitelist: []string{},
			wantErr:       false,
		},
		{
			name: "CLI rejects include outside global scope",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include: []string{"/home/user/project"},
					},
				},
			},
			local: &LocalConfig{
				Include: []string{"/opt/external"}, // Outside global include
			},
			localConfigDir: "/home/user/project",
			wantErr:        true,
		},
		{
			name: "CLI merges all allowed fields",
			global: &GlobalConfig{
				ActiveProfile: "coding",
				Profiles: map[string]*Profile{
					"coding": {
						Include:   []string{"/home/user/project"},
						Exclude:   []string{"node_modules"},
						Blacklist: []string{`.*\.secret$`},
						Whitelist: []string{`.*important\.secret$`},
					},
				},
			},
			local: &LocalConfig{
				Include:   []string{"./vendor"},
				Exclude:   []string{"tmp/"},
				Blacklist: []string{`.*\.tmp$`},
			},
			localConfigDir: "/home/user/project",
			wantInclude: []string{
				"/home/user/project",
				"/home/user/project/vendor",
			},
			wantExclude:   []string{"node_modules", "/home/user/project/tmp"},
			wantBlacklist: []string{`.*\.secret$`, `.*\.tmp$`},
			wantWhitelist: []string{`.*important\.secret$`}, // Global only
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeConfigForCLI(tt.global, tt.local, tt.localConfigDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("MergeConfigForCLI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Check include (merged with validation)
			if !stringSlicesEqual(got.Include, tt.wantInclude) {
				t.Errorf("MergeConfigForCLI() Include = %v, want %v", got.Include, tt.wantInclude)
			}

			// Check exclude (merged)
			if !stringSlicesEqual(got.Exclude, tt.wantExclude) {
				t.Errorf("MergeConfigForCLI() Exclude = %v, want %v", got.Exclude, tt.wantExclude)
			}

			// Check blacklist (merged)
			if !stringSlicesEqual(got.Blacklist, tt.wantBlacklist) {
				t.Errorf("MergeConfigForCLI() Blacklist = %v, want %v", got.Blacklist, tt.wantBlacklist)
			}

			// Check whitelist (global only)
			if !stringSlicesEqual(got.Whitelist, tt.wantWhitelist) {
				t.Errorf("MergeConfigForCLI() Whitelist = %v, want %v", got.Whitelist, tt.wantWhitelist)
			}
		})
	}
}

// Helper function to compare string slices
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

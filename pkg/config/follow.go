package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// What a project can follow its imports into, besides directories
const (
	// FollowVenv is the Python virtual environment of the project
	FollowVenv = "venv"
	// FollowGo is the Go standard library and the module cache
	FollowGo = "go"
	// FollowHeaders is the header files of the system
	FollowHeaders = "headers"
)

// CheckFollow tells whether every entry of a follow setting is a known place
// or a directory
func CheckFollow(follow []string) error {
	for _, entry := range follow {
		switch {
		case entry == FollowVenv, entry == FollowGo, entry == FollowHeaders:
		case filepath.IsAbs(entry), strings.HasPrefix(entry, "~/"):
		default:
			return fmt.Errorf("unknown place to follow %q (known: %s, %s, %s, or a directory)", entry, FollowVenv, FollowGo, FollowHeaders)
		}
	}
	return nil
}

// FollowFor returns the follow setting for the project in a directory: that
// of the .config.yaml in it, if it has one, else that of the profile.
func (p *Profile) FollowFor(root string, fs FileSystem) ([]string, error) {
	follow := p.Follow
	localPath := filepath.Join(root, ".config.yaml")
	if _, err := fs.Stat(localPath); err == nil {
		local, err := LoadLocalConfigWithFS(localPath, fs)
		if err != nil {
			return nil, err
		}
		if local.Follow != nil {
			follow = local.Follow
		}
	}
	if err := CheckFollow(follow); err != nil {
		return nil, fmt.Errorf("follow: %w", err)
	}
	return follow, nil
}

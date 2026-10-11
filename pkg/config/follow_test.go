package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFollowFor(t *testing.T) {
	fs := &RealFileSystem{}
	profile := &Profile{Follow: []string{FollowGo}}

	// A project without settings of its own follows what the profile says
	plain := t.TempDir()
	got, err := profile.FollowFor(plain, fs)
	if err != nil {
		t.Fatalf("FollowFor failed: %v", err)
	}
	if want := []string{FollowGo}; !reflect.DeepEqual(got, want) {
		t.Errorf("follow = %v, want %v", got, want)
	}

	write := func(dir, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".config.yaml"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write .config.yaml: %v", err)
		}
	}

	// A project says it for itself, and can say nothing is followed
	own := t.TempDir()
	write(own, "follow: [venv, /opt/lib]\n")
	if got, _ := profile.FollowFor(own, fs); !reflect.DeepEqual(got, []string{FollowVenv, "/opt/lib"}) {
		t.Errorf("follow of a project that sets it = %v, want [venv /opt/lib]", got)
	}
	none := t.TempDir()
	write(none, "follow: []\n")
	if got, _ := profile.FollowFor(none, fs); len(got) != 0 {
		t.Errorf("follow of a project that follows nothing = %v, want none", got)
	}

	// A .config.yaml that sets other things leaves it to the profile
	other := t.TempDir()
	write(other, "index:\n  embeddings: true\n")
	if got, _ := profile.FollowFor(other, fs); !reflect.DeepEqual(got, []string{FollowGo}) {
		t.Errorf("follow of a project that does not set it = %v, want [go]", got)
	}

	wrong := t.TempDir()
	write(wrong, "follow: [stdlib]\n")
	if _, err := profile.FollowFor(wrong, fs); err == nil {
		t.Error("FollowFor accepted a place that is not known")
	}
}

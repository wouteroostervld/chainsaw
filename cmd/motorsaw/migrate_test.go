package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newMigration(t *testing.T) (*migration, *[]string) {
	t.Helper()
	home := t.TempDir()
	var calls []string
	m := &migration{
		home:      home,
		configDir: filepath.Join(home, ".config", "systemd", "user"),
		exe:       filepath.Join(home, ".local", "bin", "motorsaw"),
		out:       &bytes.Buffer{},
		systemctl: func(args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			return "", nil
		},
	}
	for _, d := range []string{m.configDir, filepath.Dir(m.exe), filepath.Join(home, ".chainsaw")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m, &calls
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateKeepsDatabaseAndRewritesService(t *testing.T) {
	m, calls := newMigration(t)
	write(t, filepath.Join(m.home, ".chainsaw", "graph.db"), "index")
	write(t, filepath.Join(m.home, ".local", "bin", "chainsaw"), "old binary")
	write(t, m.exe, "new binary")
	write(t, m.oldUnitPath(), "[Unit]\nDescription=Chainsaw Daemon\n[Service]\nExecStart=/elsewhere/chainsaw daemon start -debug\nReadWritePaths=%h/.chainsaw\nMemoryMax=2G\n")

	if err := m.run(); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(filepath.Join(m.home, ".motorsaw", "graph.db")); string(got) != "index" {
		t.Errorf("database not kept: %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.home, ".chainsaw")); err == nil {
		t.Error("old data directory still there")
	}
	if target, err := os.Readlink(filepath.Join(m.home, ".local", "bin", "chainsaw")); err != nil || target != "motorsaw" {
		t.Errorf("old name is not a symlink to motorsaw: %q, %v", target, err)
	}
	if _, err := os.Stat(m.oldUnitPath()); err == nil {
		t.Error("old unit still there")
	}
	unit, _ := os.ReadFile(m.newUnitPath())
	want := "Description=Motorsaw Daemon\n[Service]\nExecStart=" + m.exe + " daemon start -debug\nReadWritePaths=%h/.motorsaw\nMemoryMax=2G\n"
	if !strings.Contains(string(unit), want) {
		t.Errorf("unit is\n%s", unit)
	}
	wantCalls := "is-enabled chainsawd|is-active chainsawd|disable --now chainsawd|daemon-reload|enable motorsawd|start motorsawd"
	if got := strings.Join(*calls, "|"); got != wantCalls {
		t.Errorf("systemctl calls:\n got %s\nwant %s", got, wantCalls)
	}
}

func TestMigrateTwiceChangesNothing(t *testing.T) {
	m, calls := newMigration(t)
	write(t, m.exe, "new binary")
	write(t, m.oldUnitPath(), "[Service]\nExecStart=/x/chainsaw daemon start\n")
	if err := m.run(); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	m.out.(*bytes.Buffer).Reset()
	if err := m.run(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("second run called systemctl: %v", *calls)
	}
	if out := m.out.(*bytes.Buffer).String(); strings.Count(out, "\n") != 1 {
		t.Errorf("second run did something:\n%s", out)
	}
}

func TestMigrateLeavesExistingNewDataAlone(t *testing.T) {
	m, _ := newMigration(t)
	write(t, m.exe, "new binary")
	write(t, filepath.Join(m.home, ".chainsaw", "graph.db"), "old")
	os.MkdirAll(filepath.Join(m.home, ".motorsaw"), 0o755)
	write(t, filepath.Join(m.home, ".motorsaw", "graph.db"), "new")
	if err := m.run(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(m.home, ".motorsaw", "graph.db")); string(got) != "new" {
		t.Errorf("new database overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.home, ".chainsaw", "graph.db")); err != nil {
		t.Error("old database removed")
	}
}

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const migrateUsage = `Usage: motorsaw migrate

Moves an installation made under the old name, chainsaw, over to motorsaw:

  - the data directory ~/.chainsaw becomes ~/.motorsaw, so the index and the
    configuration are kept (left alone if ~/.motorsaw is already there)
  - the old binary named chainsaw is replaced by a symlink to this one, so
    that scripts and tools that still say chainsaw keep working
  - the systemd user service chainsawd is stopped and replaced by motorsawd,
    which is the same unit with the new name and the path of this binary; it is
    enabled and started again if chainsawd was

It is safe to run again: when there is nothing left to move it does nothing.`

const (
	oldName = "chainsaw"
	newName = "motorsaw"
)

// migration moves an installation from the old name to the new. Everything
// it touches is a field, so a test can point it at a scratch directory.
type migration struct {
	home      string // the user's home directory
	configDir string // where systemd user units live, under the config home
	exe       string // this binary, with symlinks resolved
	out       io.Writer
	systemctl func(args ...string) (string, error)
}

func handleMigrate(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(migrateUsage)
			return
		}
		fail("migrate takes no arguments, got %q", a)
	}

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fail("finding this binary: %v", err)
	}
	home := os.Getenv("HOME")
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	m := &migration{
		home:      home,
		configDir: filepath.Join(configHome, "systemd", "user"),
		exe:       exe,
		out:       os.Stdout,
		systemctl: userSystemctl,
	}
	if err := m.run(); err != nil {
		fail("%v", err)
	}
}

// userSystemctl runs systemctl --user. A machine without systemd gives an
// error wrapping exec.ErrNotFound.
func userSystemctl(args ...string) (string, error) {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *migration) run() error {
	m.say("Moving from %s to %s", oldName, newName)

	// The service goes first: it holds the index open, and moving the files
	// from under it is how an index gets damaged.
	unit, err := m.stopService()
	if err != nil {
		return err
	}
	if err := m.moveData(); err != nil {
		return err
	}
	if err := m.replaceBinary(); err != nil {
		return err
	}
	if unit != nil {
		if err := m.writeService(unit); err != nil {
			return err
		}
	}
	return nil
}

func (m *migration) say(format string, args ...any) {
	fmt.Fprintf(m.out, format+"\n", args...)
}

// oldService is a chainsawd unit found on the machine
type oldService struct {
	text    string
	enabled bool
	active  bool
}

func (m *migration) oldUnitPath() string { return filepath.Join(m.configDir, oldName+"d.service") }
func (m *migration) newUnitPath() string { return filepath.Join(m.configDir, newName+"d.service") }

// stopService stops and disables the old service, and removes its unit. It
// returns what the unit said, or nil when there was no old service.
func (m *migration) stopService() (*oldService, error) {
	text, err := os.ReadFile(m.oldUnitPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	svc := &oldService{text: string(text)}

	if _, err := m.systemctl("is-enabled", oldName+"d"); err == nil {
		svc.enabled = true
	}
	if _, err := m.systemctl("is-active", oldName+"d"); err == nil {
		svc.active = true
	}

	m.say("Stopping %sd", oldName)
	m.warn(m.systemctl("disable", "--now", oldName+"d"))
	if err := os.Remove(m.oldUnitPath()); err != nil {
		return nil, err
	}
	m.say("Removed %s", m.oldUnitPath())
	return svc, nil
}

// warn reports a failed systemctl call without stopping: the files are what
// matter, and a machine without systemd running still has them to move.
func (m *migration) warn(out string, err error) {
	if err != nil {
		m.say("  warning: %v: %s", err, out)
	}
}

// moveData renames ~/.chainsaw to ~/.motorsaw
func (m *migration) moveData() error {
	oldDir := filepath.Join(m.home, "."+oldName)
	newDir := filepath.Join(m.home, "."+newName)

	info, err := os.Lstat(oldDir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Lstat(newDir); err == nil {
		m.say("Both %s and %s exist; leaving the old one where it is", oldDir, newDir)
		return nil
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("moving %s: %w", oldDir, err)
	}
	m.say("Moved %s to %s", oldDir, newDir)
	return nil
}

// replaceBinary removes the old binary and leaves a symlink to this one in
// its place. That is next to this binary, and in ~/.local/bin if the old
// binary was there and this one is not.
func (m *migration) replaceBinary() error {
	exeDir := filepath.Dir(m.exe)
	if err := m.link(filepath.Join(exeDir, oldName), newName); err != nil {
		return err
	}
	local := filepath.Join(m.home, ".local", "bin")
	if local != exeDir {
		old := filepath.Join(local, oldName)
		if _, err := os.Lstat(old); err == nil {
			return m.link(old, m.exe)
		}
	}
	return nil
}

// link makes path a symlink to target, replacing a file or a symlink that is
// there. It leaves a directory alone.
func (m *migration) link(path, target string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case info.IsDir():
		m.say("%s is a directory; leaving it", path)
		return nil
	case info.Mode()&fs.ModeSymlink != 0:
		if cur, err := os.Readlink(path); err == nil && cur == target {
			return nil
		}
		fallthrough
	default:
		if err := os.Remove(path); err != nil {
			return err
		}
		m.say("Removed old binary %s", path)
	}
	if err := os.Symlink(target, path); err != nil {
		return err
	}
	m.say("Linked %s to %s", path, target)
	return nil
}

// writeService writes the old unit as motorsawd.service: the same unit, so
// what the user changed in it stays, with the new name and this binary.
func (m *migration) writeService(old *oldService) error {
	if err := os.WriteFile(m.newUnitPath(), []byte(m.rewriteUnit(old.text)), 0o644); err != nil {
		return err
	}
	m.say("Wrote %s", m.newUnitPath())

	m.warn(m.systemctl("daemon-reload"))
	if old.enabled {
		m.warn(m.systemctl("enable", newName+"d"))
	}
	if old.active {
		m.say("Starting %sd", newName)
		m.warn(m.systemctl("start", newName+"d"))
	}
	return nil
}

// rewriteUnit renames the old name in a unit, and points ExecStart at this
// binary, whichever path the old one was started from. Its arguments stay.
func (m *migration) rewriteUnit(text string) string {
	text = strings.ReplaceAll(text, oldName, newName)
	text = strings.ReplaceAll(text, "Chainsaw", "Motorsaw")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if rest, ok := strings.CutPrefix(line, "ExecStart="); ok {
			prefix := rest[:len(rest)-len(strings.TrimLeft(rest, "-@+!:"))]
			rest = rest[len(prefix):]
			args := ""
			if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
				args = rest[sp:]
			}
			lines[i] = "ExecStart=" + prefix + m.exe + args
		}
	}
	return strings.Join(lines, "\n")
}

// legacyNote tells the user, once per command, that an index made under the
// old name is still where it was
func legacyNote() {
	home := os.Getenv("HOME")
	if _, err := os.Stat(filepath.Join(home, "."+oldName)); err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(home, "."+newName)); err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "note: ~/.%s is from before the rename to %s; run \"%s migrate\" to keep its index and configuration\n", oldName, newName, newName)
}

// Package paths locates mad's config and state files and holds small
// filesystem and shell helpers shared by the other packages.
package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Home is the user's home directory ($HOME).
func Home() string {
	h, _ := os.UserHomeDir()
	return h
}

// CodexHome is codex's own directory: $CODEX_HOME, defaulting to ~/.codex.
func CodexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".codex")
}

// ConfigDir is $XDG_CONFIG_HOME/mad, defaulting to ~/.config/mad.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "mad")
	}
	return filepath.Join(Home(), ".config", "mad")
}

// StateDir is $XDG_STATE_HOME/mad, defaulting to ~/.local/state/mad.
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "mad")
	}
	return filepath.Join(Home(), ".local", "state", "mad")
}

func StateFile() string        { return filepath.Join(StateDir(), "state.json") }
func StatusDir() string        { return filepath.Join(StateDir(), "status") }
func SidebarWidthFile() string { return filepath.Join(StateDir(), "sidebar_width") }
func SidebarLog() string       { return filepath.Join(StateDir(), "sidebar.log") }
func TmuxConf() string         { return filepath.Join(ConfigDir(), "tmux.conf") }
func ClaudeSettings() string   { return filepath.Join(ConfigDir(), "claude-settings.json") }
func AgentsConfig() string     { return filepath.Join(ConfigDir(), "agents.json") }
func ConfigFile() string       { return filepath.Join(ConfigDir(), "config.json") }

// Self is the absolute path of the running mad binary; tmux bindings and
// agent hooks call back into it.
func Self() string {
	p, err := os.Executable()
	if err != nil {
		return "mad"
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// ProjectRoot resolves dir to its git top-level; outside git it returns dir
// and false.
func ProjectRoot(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return dir, false
	}
	return strings.TrimSpace(string(out)), true
}

// Expand turns a user-typed path ("~/x", "rel/dir") into an absolute one.
func Expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		return Home()
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(Home(), p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// Short abbreviates the home directory to "~" for display.
func Short(p string) string {
	h := Home()
	if h != "" && (p == h || strings.HasPrefix(p, h+"/")) {
		return "~" + p[len(h):]
	}
	return p
}

// ShellQuote quotes s as a single POSIX shell word.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WriteFileAtomic writes data via a temp file and rename, creating parent
// directories, so readers never see a partial file.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

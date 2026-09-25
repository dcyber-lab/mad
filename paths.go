package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "mad")
	}
	return filepath.Join(homeDir(), ".config", "mad")
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "mad")
	}
	return filepath.Join(homeDir(), ".local", "state", "mad")
}

func stateFile() string          { return filepath.Join(stateDir(), "state.json") }
func statusDir() string          { return filepath.Join(stateDir(), "status") }
func tmuxConfPath() string       { return filepath.Join(configDir(), "tmux.conf") }
func claudeSettingsPath() string { return filepath.Join(configDir(), "claude-settings.json") }
func agentsConfigPath() string   { return filepath.Join(configDir(), "agents.json") }

// selfPath is the absolute path of the running mad binary; tmux bindings
// and agent hooks call back into it.
func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "mad"
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// projectRoot resolves dir to its git top-level, or dir itself outside git.
func projectRoot(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return dir, false
	}
	return strings.TrimSpace(string(out)), true
}

func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(homeDir(), p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func shortPath(p string) string {
	if h := homeDir(); h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeFileAtomic(path string, data []byte) error {
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
	tmp.Close()
	return os.Rename(tmp.Name(), path)
}

// Package paths locates mad's config and state files and holds small
// filesystem and shell helpers shared by the other packages.
package paths

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Home is the user's home directory ($HOME).
func Home() string {
	h, _ := os.UserHomeDir()
	return h
}

// ConfigDir is $XDG_CONFIG_HOME/mad, defaulting to ~/.config/mad.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "mad")
	}
	return filepath.Join(Home(), ".config", "mad")
}

// StateDir is $XDG_STATE_HOME/mad, defaulting to ~/.local/state/mad. A
// deck of its own (MAD_SOCKET, see OtherDeck) keeps its state in
// decks/<socket> under it.
func StateDir() string {
	dir := filepath.Join(Home(), ".local", "state", "mad")
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		dir = filepath.Join(d, "mad")
	}
	if d := OtherDeck(); d != "" {
		dir = filepath.Join(dir, "decks", d)
	}
	return dir
}

// OtherDeck names a deck run next to the usual one, such as a build being
// tried next to the mad you use: MAD_SOCKET puts it on a tmux server of
// its own (see tmux.Socket), and it keeps its state and generated files
// apart, sharing only config.json and agents.json. "" is the usual deck.
func OtherDeck() string {
	if s := os.Getenv("MAD_SOCKET"); s != "mad" {
		return s
	}
	return ""
}

// GenDir holds the files mad generates for tmux and the agents to read
// (tmux.conf, claude's settings). They name the mad binary to call back,
// so another deck has its own in its state directory.
func GenDir() string {
	if OtherDeck() != "" {
		return StateDir()
	}
	return ConfigDir()
}

func StateFile() string        { return filepath.Join(StateDir(), "state.json") }
func StatusDir() string        { return filepath.Join(StateDir(), "status") }
func SidebarWidthFile() string { return filepath.Join(StateDir(), "sidebar_width") }
func SidebarLog() string       { return filepath.Join(StateDir(), "sidebar.log") }
func LeaveNote() string        { return filepath.Join(StateDir(), "leave_note") }
func TmuxConf() string         { return filepath.Join(GenDir(), "tmux.conf") }
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

// ReadJSON decodes the JSON file at path into v. A missing file is no
// error and leaves v alone; a malformed one is, naming the file and line
// ("config.json:12: invalid character '}' …").
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, v)
	var off int64
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &syn):
		off = syn.Offset
	case errors.As(err, &typ):
		off = typ.Offset
	default:
		return fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	line := bytes.Count(data[:off], []byte("\n")) + 1
	return fmt.Errorf("%s:%d: %v", filepath.Base(path), line, strings.TrimPrefix(err.Error(), "json: "))
}

// ModTime is path's modification time, zero when it is missing; readers
// of config files reload when it changes.
func ModTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePanes(t *testing.T) {
	out := strings.Join([]string{
		"%0\t_keep\t_pool\t@0\t0.0\t0\t200\t50\t1\t/dev/ttys001\t100\t",
		"%1\t_sidebar\tmain\t@1\t0.0\t0\t30\t50\t1\t/dev/ttys002\t101\t",
		"%2\tagent-a\tmain\t@1\t0.1\t0\t169\t50\t0\t/dev/ttys003\t102\t",
		"%3\tagent-b\t_pool\t@2\t1.0\t1\t169\t50\t1\t/dev/ttys004\t103\t1",
		"garbage line",
		"",
	}, "\n")
	panes := parsePanes(out)
	if len(panes) != 4 {
		t.Fatalf("got %d panes, want 4", len(panes))
	}

	keep := panes[0]
	if keep.Index != -1 {
		t.Errorf("0.0 outside main must not count as sidebar: %+v", keep)
	}
	if sb := panes[1]; sb.Index != 0 || !sb.Active || sb.Width != 30 || sb.TTY != "/dev/ttys002" {
		t.Errorf("sidebar = %+v", sb)
	}
	if a := panes[2]; a.PID != 102 || a.Asleep {
		t.Errorf("stage agent = %+v", a)
	}
	if b := panes[3]; !b.Dead || !b.Asleep || b.WindowID != "@2" || b.Index != -1 {
		t.Errorf("pool agent = %+v", b)
	}

	stage, ok := Stage(panes)
	if !ok || stage.MadID != "agent-a" || stage.Height != 50 {
		t.Errorf("Stage = %+v, %v", stage, ok)
	}
	if p, ok := FindPane(panes, "agent-b"); !ok || p.ID != "%3" {
		t.Errorf("FindPane = %+v, %v", p, ok)
	}
	if _, ok := FindPane(panes, "nope"); ok {
		t.Error("FindPane found a missing id")
	}
	if _, ok := Stage(panes[:1]); ok {
		t.Error("Stage without a main window")
	}
}

func TestIsAgentID(t *testing.T) {
	for id, want := range map[string]bool{
		"":                                     false,
		IDSidebar:                              false,
		IDPlaceholder:                          false,
		"0ec4eb20-7a27-45d0-89d6-adbd28a87d5b": true,
	} {
		if got := IsAgentID(id); got != want {
			t.Errorf("IsAgentID(%q) = %v", id, got)
		}
	}
}

func TestInDeck(t *testing.T) {
	old := Socket
	defer func() { Socket = old }()
	Socket = "mad"
	t.Setenv("TMUX", "/private/tmp/tmux-501/mad,123,0")
	if !InDeck() {
		t.Error("inside mad's socket")
	}
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,123,0")
	if InDeck() {
		t.Error("inside another tmux")
	}
	t.Setenv("TMUX", "")
	if InDeck() {
		t.Error("outside tmux")
	}
}

// useServer runs the test against a throwaway tmux server.
func useServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	old := Socket
	Socket = fmt.Sprintf("mad-test-%d", time.Now().UnixNano())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // -f points at a missing file: defaults
	t.Setenv("LC_ALL", "C")                  // non-UTF-8: tmux must still keep the tabs in -F output
	t.Cleanup(func() {
		_ = Run("kill-server")
		Socket = old
	})
}

func TestServerRoundTrip(t *testing.T) {
	useServer(t)
	if HasSession(MainSession) {
		t.Fatal("fresh server has a session")
	}
	id, err := Out("new-session", "-d", "-s", MainSession, "-x", "80", "-y", "20", "-P", "-F", "#{pane_id}",
		"printf 'hello from mad'; sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if !HasSession(MainSession) {
		t.Fatal("session not created")
	}
	if err := Tag(id, IDSidebar); err != nil {
		t.Fatal(err)
	}

	panes, err := ListPanes()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := FindPane(panes, IDSidebar)
	if !ok || p.ID != id || p.Index != 0 || p.Width != 80 {
		t.Fatalf("tagged pane = %+v, %v (all: %+v)", p, ok, panes)
	}

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(Capture(id), "hello from mad") {
		if time.Now().After(deadline) {
			t.Fatalf("capture = %q", Capture(id))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestErrorsIncludeStderr(t *testing.T) {
	useServer(t)
	err := Run("no-such-command")
	if err == nil || !strings.Contains(err.Error(), "no-such-command") {
		t.Errorf("err = %v", err)
	}
}

func TestWatched(t *testing.T) {
	cases := []struct {
		clients    string
		focusKnown bool
		want       bool
	}{
		{"", true, false}, // detached
		{"", false, false},
		{"xattached,focused,UTF-8", true, true},
		{"xattached,UTF-8", true, false},                   // terminal lost focus
		{"xattached,UTF-8\nxattached,focused", true, true}, // one of two looks
		{"xattached,UTF-8", false, true},                   // old tmux: attached is enough
		{"x", false, true},                                 // old tmux without client_flags
	}
	for _, c := range cases {
		if got := watched(c.clients, c.focusKnown); got != c.want {
			t.Errorf("watched(%q, %v) = %v", c.clients, c.focusKnown, got)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := map[string]bool{
		"tmux 3.4\n": true, "tmux 3.3a": true, "tmux 3.1c": false, "tmux 2.9": false,
		"tmux next-3.5": true, "tmux openbsd-7.4": true, "tmux master": false, "": false,
	}
	for v, want := range cases {
		if got := versionAtLeast(v, 3, 3); got != want {
			t.Errorf("versionAtLeast(%q) = %v", v, got)
		}
	}
}

func TestClientVersion(t *testing.T) {
	for v, want := range map[string]string{
		"tmux 3.7c\n": "3.7c", "tmux next-3.5": "next-3.5", "tmux openbsd-7.4\n": "openbsd-7.4", "": "",
	} {
		if got := clientVersion(v); got != want {
			t.Errorf("clientVersion(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestVersions(t *testing.T) {
	useServer(t)
	if server, _ := Versions(); server != "" {
		t.Errorf("no server is running, got version %q", server)
	}
	if err := Run("new-session", "-d", "-s", MainSession, "-x", "80", "-y", "20", "sleep 30"); err != nil {
		t.Fatal(err)
	}
	server, client := Versions()
	if server == "" || server != client {
		t.Errorf("server %q, client %q: one tmux started both", server, client)
	}
}

func TestCheckVersion(t *testing.T) {
	for _, v := range []string{
		"tmux 3.7c\n", "tmux 3.0", "tmux 3.0a", "tmux next-3.5", "tmux openbsd-7.4",
		"tmux master", "", // not readable: let tmux show what it can do
	} {
		if err := checkVersion(v); err != nil {
			t.Errorf("checkVersion(%q) = %v", v, err)
		}
	}
	for _, v := range []string{"tmux 2.9a\n", "tmux 2.6", "tmux 1.8"} {
		err := checkVersion(v)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(v)+" is too old") || !strings.Contains(err.Error(), "tmux 3.0 or newer") {
			t.Errorf("checkVersion(%q) = %v", v, err)
		}
	}
}

// fakeTmux puts a tmux that only knows -V first (and alone) on PATH.
func fakeTmux(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	if version != "" {
		script := "#!/bin/sh\necho '" + version + "'\n"
		if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestCheckVersionRunsTmux(t *testing.T) {
	fakeTmux(t, "tmux 2.9a")
	if err := CheckVersion(); err == nil || !strings.Contains(err.Error(), "tmux 2.9a is too old") {
		t.Errorf("old tmux: %v", err)
	}
	fakeTmux(t, "tmux 3.4")
	if err := CheckVersion(); err != nil {
		t.Errorf("tmux 3.4: %v", err)
	}
	fakeTmux(t, "")
	if err := CheckVersion(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("no tmux: %v", err)
	}
}

func TestCaptureAll(t *testing.T) {
	useServer(t)
	var ids []string
	for i, text := range []string{"first pane", "second\n\n  pane", ""} {
		id, err := Out("new-session", "-d", "-s", fmt.Sprint("s", i), "-x", "80", "-y", "10", "-P", "-F", "#{pane_id}",
			fmt.Sprintf("printf '%s'; sleep 30", strings.ReplaceAll(text, "\n", `\n`)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(Capture(ids[1]), "pane") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	got, err := CaptureAll(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if want := Capture(id); got[id] != want {
			t.Errorf("%s: CaptureAll %q, Capture %q", id, got[id], want)
		}
	}
	if got[ids[1]] != "second\n\n  pane" {
		t.Errorf("multi-line screen = %q", got[ids[1]])
	}

	if _, err := CaptureAll(append(ids, "%999")); err == nil {
		t.Error("a missing pane should make CaptureAll fail")
	}
	if got, err := CaptureAll(nil); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
}

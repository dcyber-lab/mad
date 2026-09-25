package tmux

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParsePanes(t *testing.T) {
	out := strings.Join([]string{
		"%0\t_keep\t_pool\t@0\t0.0\t0\t200\t50\t1\t/dev/ttys001",
		"%1\t_sidebar\tmain\t@1\t0.0\t0\t30\t50\t1\t/dev/ttys002",
		"%2\tagent-a\tmain\t@1\t0.1\t0\t169\t50\t0\t/dev/ttys003",
		"%3\tagent-b\t_pool\t@2\t1.0\t1\t169\t50\t1\t/dev/ttys004",
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
	if b := panes[3]; !b.Dead || b.WindowID != "@2" || b.Index != -1 {
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

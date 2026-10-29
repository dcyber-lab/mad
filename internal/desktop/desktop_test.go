package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	return dir
}

func TestTokenRequired(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	for _, tc := range []struct {
		url  string
		want int
	}{
		{"/api/deck", http.StatusForbidden},
		{"/api/deck?token=nope", http.StatusForbidden},
		{"/api/term?id=x", http.StatusForbidden},
		{"/api/deck?token=" + s.Token, http.StatusOK},
		{"/", http.StatusOK}, // the page itself holds nothing secret
		{"/vendor/xterm.js", http.StatusOK},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.url, w.Code, tc.want)
		}
	}
}

func TestSnapshot(t *testing.T) {
	dir := isolate(t)
	t.Setenv("MAD_SOCKET", "")
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("mad-desktop-none-%d", time.Now().UnixNano()) // no server: all stopped
	t.Cleanup(func() { tmux.Socket = old })
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{
		{ID: "a1", Kind: "claude", Name: "impl"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	p := newPoller(agent.Builtin(), newTerms())
	p.Poll(time.Now())
	snap := p.Snapshot()
	if len(snap.Projects) != 1 || len(snap.Projects[0].Agents) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	a := snap.Projects[0].Agents[0]
	if a.Name != "impl" || a.Status != "stopped" || a.Icon == "" {
		t.Errorf("agent = %+v", a)
	}
}

// ---- integration: a real tmux server on a throwaway socket ----

var fakeKind = []agent.Kind{{Name: "fake", Start: `sh -c 'echo "agent up $MAD_AGENT_ID"; exec cat'`}}

func useDeck(t *testing.T) (*state.Project, *state.Agent) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := isolate(t)
	oldSocket, oldSelf := tmux.Socket, deck.SelfCommand
	tmux.Socket = fmt.Sprintf("mad-desktop-test-%d", time.Now().UnixNano())
	deck.SelfCommand = func(sub string) string { return "sleep 600 # mad " + sub }
	t.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand = oldSocket, oldSelf
	})
	p := &state.Project{Name: "p", Path: dir}
	a := &state.Agent{ID: "0ec4eb20-7a27-45d0-89d6-adbd28a87d5b", Kind: "fake"}
	p.Agents = []*state.Agent{a}
	if err := (&state.State{Projects: []*state.Project{p}}).Save(); err != nil {
		t.Fatal(err)
	}
	return p, a
}

func dial(t *testing.T, s *Server, id string) (*websocket.Conn, context.Context) {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/term?cols=100&rows=30&id=" + id + "&token=" + s.Token
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {ts.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

// readUntil reads the terminal's output until it shows want.
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, want string) {
	t.Helper()
	var seen strings.Builder
	for !strings.Contains(seen.String(), want) {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v; got %q", want, err, seen.String())
		}
		if typ == websocket.MessageText {
			t.Fatalf("message: %s", data)
		}
		seen.Write(data)
	}
}

func deskSessions(t *testing.T) []string {
	t.Helper()
	out, _ := tmux.Out("list-sessions", "-F", "#{session_name}")
	var ds []string
	for _, s := range strings.Split(out, "\n") {
		if strings.HasPrefix(s, tmux.DeskPrefix) {
			ds = append(ds, s)
		}
	}
	return ds
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Opening an agent from the window starts it, shows its screen, takes the
// keys, marks it viewed and, once closed, leaves no session behind.
func TestTerm(t *testing.T) {
	_, a := useDeck(t)
	s := New(fakeKind)
	c, ctx := dial(t, s, a.ID)

	readUntil(t, ctx, c, "agent up "+a.ID)
	if err := c.Write(ctx, websocket.MessageBinary, []byte("hello desk\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, c, "hello desk")

	panes, err := tmux.ListPanes()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := tmux.FindPane(panes, a.ID)
	if !ok || !p.Viewed || p.Session != tmux.PoolSession {
		t.Errorf("pane = %+v, %v; want it viewed, listed once in the pool", p, ok)
	}
	if n := len(deskSessions(t)); n != 1 {
		t.Errorf("%d desk sessions, want 1", n)
	}

	msg, _ := json.Marshal(control{Type: "focus"})
	_ = c.Write(ctx, websocket.MessageText, msg)
	waitFor(t, "focus", func() bool { return s.terms.Focused(a.ID) })

	c.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "the desk session to go", func() bool { return len(deskSessions(t)) == 0 })
	if s.terms.Focused(a.ID) {
		t.Error("still focused after close")
	}
}

// An agent on the terminal deck's stage is taken off it: a pane is in
// one window at a time.
func TestTermTakesAgentOffStage(t *testing.T) {
	_, a := useDeck(t)
	st, _ := state.Load()
	if err := drive.EnsureDeck(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := deck.OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	panes, _ := tmux.ListPanes()
	if p, _ := tmux.FindPane(panes, a.ID); !tmux.OnStage(p) {
		t.Fatalf("setup: agent not on stage: %+v", p)
	}

	c, ctx := dial(t, New(fakeKind), a.ID)
	readUntil(t, ctx, c, "agent up")
	panes, _ = tmux.ListPanes()
	if p, _ := tmux.FindPane(panes, a.ID); tmux.OnStage(p) || !p.Viewed {
		t.Errorf("agent pane = %+v; want it off stage and viewed", p)
	}
	if stage, ok := tmux.Stage(panes); !ok || stage.MadID != tmux.IDPlaceholder {
		t.Errorf("stage = %+v, %v; want the placeholder", stage, ok)
	}
}

func TestUnknownAgent(t *testing.T) {
	useDeck(t)
	c, ctx := dial(t, New(fakeKind), "nope")
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageText || !strings.Contains(string(data), `"error"`) {
		t.Errorf("got %v %q %v; want an error message", typ, data, err)
	}
}

func TestWindowCommand(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(name string) (string, error) {
		if name == "chromium" {
			return "/usr/bin/chromium", nil
		}
		return "", exec.ErrNotFound
	}
	cmd := WindowCommand("http://127.0.0.1:1/#token=x", "/tmp/prof")
	if cmd.Path != "/usr/bin/chromium" || !strings.Contains(strings.Join(cmd.Args, " "), "--app=http://127.0.0.1:1/#token=x") {
		t.Errorf("cmd = %v", cmd.Args)
	}
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if cmd := WindowCommand("u", "p"); !strings.HasSuffix(cmd.Path, "open") && !strings.HasSuffix(cmd.Args[0], "open") {
		t.Errorf("fallback = %v", cmd.Args)
	}
}

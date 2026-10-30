package desktop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
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
		{"/api/host?id=x", http.StatusForbidden},
		{"/api/deck?token=" + s.Token, http.StatusOK},
		{"/", http.StatusOK}, // the page itself holds nothing secret
		{"/icons.js", http.StatusOK},
		{"/vendor/marked.js", http.StatusOK},
		{"/vendor/purify.js", http.StatusOK},
		{"/api/chat?id=x", http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.url, w.Code, tc.want)
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("GET %s: Content-Security-Policy %q", tc.url, csp)
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
	p := newPoller(agent.Builtin(), newLooks())
	p.Poll(time.Now())
	snap := p.Snapshot()
	if len(snap.Projects) != 1 || len(snap.Projects[0].Agents) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	a := snap.Projects[0].Agents[0]
	if a.Name != "impl" || a.Status != "stopped" || a.Icon == "" {
		t.Errorf("agent = %+v", a)
	}
	if a.Shown != "stopped" {
		t.Errorf("shown = %q", a.Shown)
	}
	// The table rides along once, and the page reads it from the JSON.
	data, _ := json.Marshal(snap)
	var back struct {
		States []status.Look `json:"states"`
	}
	if err := json.Unmarshal(data, &back); err != nil || len(back.States) != len(status.Looks) {
		t.Fatalf("states in the snapshot = %v, %v", back.States, err)
	}
	if back.States[0].Word == "" || back.States[0].Tone == "" {
		t.Errorf("state without a look: %+v", back.States[0])
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

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
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

// The conversation is read as the transcript grows: each look sends what
// was written after the last one, whole lines only.
func TestChatAPI(t *testing.T) {
	dir := isolate(t)
	const sid = "c0ffee00-0000-4000-8000-000000000001"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{
		{ID: "a1", Kind: "claude", SessionID: sid}, {ID: "sh", Kind: "shell"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(dir, ".claude", "projects", regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-"))
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tdir, sid+".jsonl")
	write := func(s string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, _ = f.WriteString(s)
	}
	s := New(agent.Builtin())
	h := s.Handler()
	look := func(id, file string, from int64) Chat {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/chat?id=%s&file=%s&from=%d&token=%s", id, file, from, s.Token), nil))
		var c Chat
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &c) != nil {
			t.Fatalf("GET /api/chat: %d %s", w.Code, w.Body)
		}
		return c
	}
	texts := func(c Chat) string {
		var out []string
		for _, e := range c.Entries {
			out = append(out, e.Kind+":"+e.Text)
		}
		return strings.Join(out, " ")
	}

	if c := look("a1", "", 0); c.File != "" || len(c.Entries) != 0 {
		t.Errorf("before anything was written: %+v", c)
	}
	write(`{"type":"user","message":{"content":"run the tests"}}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"text","text":"On it."}]}}` + "\n")
	first := look("a1", "", 0)
	if !first.Reset || first.File == "" || texts(first) != "user:run the tests text:On it." {
		t.Fatalf("first look: %+v", first)
	}
	if c := look("a1", first.File, first.Offset); c.Reset || len(c.Entries) != 0 || c.Offset != first.Offset {
		t.Errorf("nothing new: %+v", c)
	}
	// A line half written waits for its end.
	write(`{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}`)
	if c := look("a1", first.File, first.Offset); len(c.Entries) != 0 || c.Offset != first.Offset {
		t.Errorf("a partial line was read: %+v", c)
	}
	write("\n")
	next := look("a1", first.File, first.Offset)
	if next.Reset || texts(next) != "text:Done." {
		t.Errorf("next look: %+v", next)
	}
	// Another session's file, or an offset past the end, starts over.
	if c := look("a1", "other", next.Offset); !c.Reset || len(c.Entries) != 3 {
		t.Errorf("another file: %+v", c)
	}
	if c := look("a1", first.File, 1<<40); !c.Reset || len(c.Entries) != 3 {
		t.Errorf("past the end: %+v", c)
	}
	// A kind without a transcript has no conversation.
	if c := look("sh", "", 0); len(c.Entries) != 0 {
		t.Errorf("shell: %+v", c)
	}

	p := newPoller(agent.Builtin(), newLooks())
	p.Poll(time.Now())
	for _, a := range p.Snapshot().Projects[0].Agents {
		if a.Chat != (a.ID == "a1") {
			t.Errorf("%s: chat = %v", a.ID, a.Chat)
		}
	}
}

func TestQueueAPI(t *testing.T) {
	dir := isolate(t)
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{{ID: "a1", Kind: "claude"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	s := New(agent.Builtin())
	h := s.Handler()
	call := func(method, url, body string) (int, queue.Queue) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, url+"&token="+s.Token, strings.NewReader(body)))
		var q queue.Queue
		_ = json.Unmarshal(w.Body.Bytes(), &q)
		return w.Code, q
	}
	if code, _ := call("POST", "/api/queue?id=../etc", "x"); code != http.StatusNotFound {
		t.Errorf("unknown agent: %d", code)
	}
	if code, q := call("POST", "/api/queue?id=a1", "fix the tests"); code != 200 || len(q.Items) != 1 {
		t.Fatalf("add: %d %+v", code, q)
	}
	code, q := call("POST", "/api/queue?id=a1", "then push")
	if code != 200 || len(q.Items) != 2 {
		t.Fatalf("add: %d %+v", code, q)
	}
	if _, q := call("POST", "/api/queue/pause?id=a1&paused=1", ""); !q.Paused {
		t.Errorf("pause: %+v", q)
	}
	if _, q := call("DELETE", "/api/queue?id=a1&item="+q.Items[0].ID, ""); len(q.Items) != 1 || q.Items[0].Text != "then push" {
		t.Errorf("remove: %+v", q)
	}
	if _, q := call("DELETE", "/api/queue?id=a1", ""); len(q.Items) != 0 || q.Paused {
		t.Errorf("clear: %+v", q)
	}
}

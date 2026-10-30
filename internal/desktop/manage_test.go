package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

var manageKinds = []agent.Kind{
	{Name: "claude", Start: "sleep 600 # tui", Hooks: true},
	{Name: "fake", Start: "sleep 600"},
	{Name: "shell", Start: "sleep 600 # shell"},
}

// call makes a request of the manage API and returns the answer.
func call(t *testing.T, s *Server, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, url+sep+"token="+s.Token, strings.NewReader(body)))
	return w
}

func mustMkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// gitRepo makes a repository in a new directory, with one commit holding
// the files.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return dir
}

func projectsOf(t *testing.T) []*state.Project {
	t.Helper()
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	return st.Projects
}

func TestSuggestAPI(t *testing.T) {
	dir := isolate(t)
	work := filepath.Join(dir, "work")
	alpha := mustMkdir(t, filepath.Join(work, "alpha"))
	mustMkdir(t, filepath.Join(work, "alpine"))
	mustMkdir(t, filepath.Join(work, "beta"))
	if err := os.WriteFile(filepath.Join(work, "alfile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&state.State{Projects: []*state.Project{{Name: "alpha", Path: alpha}}}).Save(); err != nil {
		t.Fatal(err)
	}
	old := scanHistory
	t.Cleanup(func() { scanHistory = old })
	now := time.Now()
	scanHistory = func() []discover.Candidate {
		return []discover.Candidate{
			{Path: "/x/azed", LastUsed: now},
			{Path: "/x/zed", LastUsed: now.Add(-2 * time.Hour)},
			{Path: "/x/zed-old", LastUsed: now.Add(-time.Hour)},
			{Path: "/x/other", LastUsed: now},
		}
	}
	s := New(manageKinds)
	get := func(q string) []suggestion {
		t.Helper()
		w := call(t, s, "GET", "/api/projects/suggest?q="+q, "")
		var out []suggestion
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("suggest %q: %d %s", q, w.Code, w.Body)
		}
		return out
	}

	got := get("~/work/al")
	if len(got) != 2 {
		t.Fatalf("path completion: %+v", got)
	}
	for _, g := range got {
		if g.Short[:2] != "~/" || (g.Name == "alpha") != g.Added {
			t.Errorf("path suggestion %+v", g)
		}
	}

	got = get("zed")
	if len(got) != 3 || got[0].Path != "/x/zed-old" || got[1].Path != "/x/zed" || got[2].Path != "/x/azed" || got[0].Used == "" {
		t.Errorf("history match: best score first, then recent: %+v", got)
	}
	if got = get(""); len(got) != 4 || got[0].Path != "/x/azed" {
		t.Errorf("empty query: most recent first: %+v", got)
	}
}

func TestProjectAPI(t *testing.T) {
	isolate(t)
	repo := gitRepo(t, map[string]string{"a.txt": "a\n"})
	sub := mustMkdir(t, filepath.Join(repo, "sub"))
	s := New(manageKinds)

	w := call(t, s, "POST", "/api/projects", `{"path":`+jsonString(sub)+`}`)
	var out struct {
		Name, Path string
		Added      bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Path != repo || !out.Added || out.Name != filepath.Base(repo) {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if ps := projectsOf(t); len(ps) != 1 || ps[0].Path != repo {
		t.Fatalf("projects: %+v", ps)
	}
	w = call(t, s, "POST", "/api/projects", `{"path":`+jsonString(repo)+`}`)
	if !strings.Contains(w.Body.String(), `"added":false`) {
		t.Errorf("again: %s", w.Body)
	}
	if w = call(t, s, "POST", "/api/projects", `{"path":"/nonexistent/dir"}`); w.Code != 400 {
		t.Errorf("not a directory: %d", w.Code)
	}
	if w = call(t, s, "POST", "/api/projects", `{"path":`+jsonString(filepath.Join(repo, "a.txt"))+`}`); w.Code != 400 {
		t.Errorf("a file: %d", w.Code)
	}

	// With agents it stays; without, it goes.
	if err := state.Update(func(st *state.State) error {
		st.FindProject(repo).Agents = []*state.Agent{{ID: "a1", Kind: "claude"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w = call(t, s, "DELETE", "/api/projects?path="+repo, ""); w.Code != 409 || len(projectsOf(t)) != 1 {
		t.Fatalf("with agents: %d", w.Code)
	}
	_ = state.Update(func(st *state.State) error { st.RemoveAgent("a1"); return nil })
	if w = call(t, s, "DELETE", "/api/projects?path="+repo, ""); w.Code != 204 || len(projectsOf(t)) != 0 {
		t.Fatalf("empty: %d %s", w.Code, w.Body)
	}
	if w = call(t, s, "DELETE", "/api/projects?path="+repo, ""); w.Code != 404 {
		t.Errorf("gone: %d", w.Code)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// fakeHost serves agent id's host socket and sends what it is told to got.
func fakeHost(t *testing.T, id string) <-chan host.Command {
	t.Helper()
	ln, err := net.Listen("unix", paths.HostSocket(id))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan host.Command, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					var c host.Command
					if json.Unmarshal(sc.Bytes(), &c) == nil {
						got <- c
						_, _ = conn.Write([]byte(`{"type":"done"}` + "\n"))
					}
				}
			}()
		}
	}()
	return got
}

func TestAgentCreateAPI(t *testing.T) {
	p, _ := useDeck(t)
	other := mustMkdir(t, filepath.Join(t.TempDir(), "wt"))
	s := New(manageKinds)
	create := func(body string) (int, string) {
		w := call(t, s, "POST", "/api/agents", body)
		var out struct{ ID string }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.ID
	}
	find := func(id string) *state.Agent {
		t.Helper()
		st, _ := state.Load()
		_, a := st.FindAgent(id)
		if a == nil {
			t.Fatalf("agent %s not saved", id)
		}
		return a
	}
	base := `{"project":` + jsonString(p.Path) + `,"kind":`

	code, id := create(base + `"claude","model":"opus"}`)
	if a := find(id); code != 200 || a.Mode != state.ModeWindow || len(a.Args) != 2 || a.Args[0] != "--model" || a.Args[1] != "opus" || a.CreatedAt.IsZero() {
		t.Errorf("claude: %d %+v", code, a)
	}
	code, id = create(base + `"claude","session":"sess-1","cwd":` + jsonString(other) + `}`)
	if a := find(id); code != 200 || a.SessionID != "sess-1" || a.Dir != other {
		t.Errorf("session: %d %+v", code, a)
	}
	code, id = create(base + `"claude","session":"sess-2","cwd":"/nonexistent"}`)
	if a := find(id); code != 200 || a.Dir != "" {
		t.Errorf("session in a dir that is gone: %d %+v", code, a)
	}
	if code, _ = create(base + `"vim"}`); code != 400 {
		t.Errorf("unknown kind: %d", code)
	}
	if code, _ = create(`{"project":"/nowhere","kind":"claude"}`); code != 400 {
		t.Errorf("unknown project: %d", code)
	}

	// A shell starts at once, in the terminal's mode.
	code, id = create(base + `"shell"}`)
	if a := find(id); code != 200 || a.Mode != "" {
		t.Errorf("shell: %d %+v", code, a)
	}
	panes, _ := tmux.ListPanes()
	if _, ok := tmux.FindPane(panes, id); !ok {
		t.Error("the shell was not started")
	}
}

func TestAgentCreateSendsText(t *testing.T) {
	p, _ := useDeck(t)
	short, err := os.MkdirTemp("/tmp", "md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	t.Setenv("XDG_STATE_HOME", short)
	mustMkdir(t, paths.StateDir())
	if err := (&state.State{Projects: []*state.Project{p}}).Save(); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)

	// Its host is faked once the agent is saved: its id is made by the
	// handler.
	heard := make(chan (<-chan host.Command), 1)
	go func() {
		for {
			st, _ := state.Load()
			if ps := st.FindProject(p.Path); ps != nil && len(ps.Agents) > 1 { // the one made now follows the one it began with
				heard <- fakeHost(t, ps.Agents[len(ps.Agents)-1].ID)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	w := call(t, s, "POST", "/api/agents", `{"project":`+jsonString(p.Path)+`,"kind":"claude","text":"hello there"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "error") {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	select {
	case c := <-<-heard:
		if c.Type != "send" || c.Text != "hello there" {
			t.Errorf("host got %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host was not sent the message")
	}
}

func TestAgentCreateSendFails(t *testing.T) {
	p, _ := useDeck(t)
	s := New(manageKinds)
	// No host starts here (deck.SelfCommand is a sleep): the agent stays,
	// and the answer says why the message did not go.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, "POST", "/api/agents?token="+s.Token,
		strings.NewReader(`{"project":`+jsonString(p.Path)+`,"kind":"claude","text":"hi"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out["id"] == "" || out["error"] == "" {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	st, _ := state.Load()
	if _, a := st.FindAgent(out["id"]); a == nil {
		t.Error("the agent was not kept")
	}
}

func TestAgentRenameRemoveStop(t *testing.T) {
	p, a := useDeck(t)
	if err := drive.EnsureDeck(p.Path); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	if err := deck.StartAgent(p, a, false, manageKinds); err != nil {
		t.Fatal(err)
	}
	alive := func() bool {
		panes, _ := tmux.ListPanes()
		pn, ok := tmux.FindPane(panes, a.ID)
		return ok && !pn.Dead
	}
	name := func() string {
		st, _ := state.Load()
		_, a := st.FindAgent(a.ID)
		return a.Name
	}

	if w := call(t, s, "PATCH", "/api/agents?id="+a.ID, `{"name":"  impl  "}`); w.Code != 204 || name() != "impl" {
		t.Fatalf("rename: %d %q", w.Code, name())
	}
	if w := call(t, s, "PATCH", "/api/agents?id="+a.ID, `{"name":""}`); w.Code != 204 || name() != "" {
		t.Fatalf("clear: %d %q", w.Code, name())
	}
	if w := call(t, s, "PATCH", "/api/agents?id=nobody", `{"name":"x"}`); w.Code != 404 {
		t.Errorf("rename unknown: %d", w.Code)
	}

	if w := call(t, s, "POST", "/api/agents/stop?id="+a.ID, ""); w.Code != 204 {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	waitFor(t, "the agent to end", func() bool { return !alive() })
	if st, _ := state.Load(); st.FindProject(p.Path).Agents[0].ID != a.ID {
		t.Error("stopping removed the agent")
	}

	if w := call(t, s, "DELETE", "/api/agents?id="+a.ID, ""); w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	panes, _ := tmux.ListPanes()
	if _, ok := tmux.FindPane(panes, a.ID); ok {
		t.Error("its pane is still there")
	}
	if st, _ := state.Load(); len(st.FindProject(p.Path).Agents) != 0 {
		t.Error("it is still in the state")
	}
	if w := call(t, s, "DELETE", "/api/agents?id="+a.ID, ""); w.Code != 404 {
		t.Errorf("delete again: %d", w.Code)
	}
}

func TestHistoryAPI(t *testing.T) {
	p, a := useDeck(t)
	a.SessionID = "open-1"
	if err := (&state.State{Projects: []*state.Project{p}}).Save(); err != nil {
		t.Fatal(err)
	}
	old := projectSessions
	t.Cleanup(func() { projectSessions = old })
	now := time.Now()
	projectSessions = func(root, kind string) []discover.Session {
		if root != p.Path {
			t.Errorf("root %q", root)
		}
		return []discover.Session{
			{Kind: kind, ID: kind + "-old", Title: "old", Cwd: root, Origin: "cli", Updated: now.Add(-time.Hour)},
			{Kind: kind, ID: kind + "-new", Title: "new", Cwd: root, Origin: "desktop", Updated: now},
			{Kind: kind, ID: "open-1", Title: "open", Updated: now},
			{Kind: kind, ID: a.ID, Title: "by id", Updated: now},
		}
	}
	s := New(manageKinds)
	w := call(t, s, "GET", "/api/history?project="+p.Path, "")
	var out []struct{ Kind, ID, Title, Cwd, Origin, Updated string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	if len(out) != 2*len(discover.Providers()) {
		t.Fatalf("sessions: %+v", out)
	}
	for _, o := range out {
		if o.ID == "open-1" || o.ID == a.ID {
			t.Errorf("an open session is listed: %+v", o)
		}
	}
	if !strings.HasSuffix(out[0].ID, "-new") || out[0].Updated == "" || out[0].Origin != "desktop" {
		t.Errorf("newest first: %+v", out[0])
	}
	if w = call(t, s, "GET", "/api/history?project=/nowhere", ""); w.Code != 404 {
		t.Errorf("unknown project: %d", w.Code)
	}
}

func TestDiffAPI(t *testing.T) {
	isolate(t)
	repo := gitRepo(t, map[string]string{"a.txt": "one\ntwo\n", "gone.txt": "bye\n"})
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "one\n2\nthree\n")
	write("new.txt", "fresh\nlines\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	a := &state.Agent{ID: "a1", Kind: "claude"}
	bare := t.TempDir()
	st := &state.State{Projects: []*state.Project{
		{Name: "r", Path: repo, Agents: []*state.Agent{a}},
		{Name: "b", Path: bare, Agents: []*state.Agent{{ID: "a2", Kind: "claude"}}},
	}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)

	w := call(t, s, "GET", "/api/diff?id=a1", "")
	var out struct {
		Dir, Branch string
		Files       []changedFile
		Patch       string
		Cut         bool
		Error       string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Error != "" {
		t.Fatalf("diff: %d %s", w.Code, w.Body)
	}
	want := map[string]changedFile{
		"a.txt":    {"a.txt", "M", 2, 1},
		"gone.txt": {"gone.txt", "D", 0, 1},
		"new.txt":  {"new.txt", "??", 2, 0},
	}
	if out.Dir != repo || out.Branch == "" || len(out.Files) != len(want) {
		t.Fatalf("diff: %+v", out)
	}
	for _, f := range out.Files {
		if f != want[f.Path] {
			t.Errorf("file %+v, want %+v", f, want[f.Path])
		}
	}
	for _, in := range []string{"+++ b/a.txt", "-two", "+three", "+++ b/new.txt", "+fresh", "-bye"} {
		if !strings.Contains(out.Patch, in) {
			t.Errorf("patch lacks %q:\n%s", in, out.Patch)
		}
	}

	w = call(t, s, "GET", "/api/diff?id=a2", "")
	out.Error = ""
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Error == "" {
		t.Errorf("not a repository: %d %s", w.Code, w.Body)
	}

	// A repository with no commit yet: all it has is new.
	if err := exec.Command("git", "-C", bare, "init", "-q").Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, "first.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = call(t, s, "GET", "/api/diff?id=a2", "")
	out.Error, out.Files = "", nil
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Error != "" || len(out.Files) != 1 ||
		out.Files[0] != (changedFile{"first.txt", "??", 1, 0}) || !strings.Contains(out.Patch, "+hello") {
		t.Errorf("no commit yet: %d %s", w.Code, w.Body)
	}
	if w = call(t, s, "GET", "/api/diff?id=nobody", ""); w.Code != 404 {
		t.Errorf("unknown agent: %d", w.Code)
	}
}

func TestTerminalAPI(t *testing.T) {
	_, a := useDeck(t)
	s := New(manageKinds)
	var opened []string
	old := openTerminal
	openTerminal = func(command string) error { opened = append(opened, command); return nil }
	t.Cleanup(func() { openTerminal = old })

	if w := call(t, s, "POST", "/api/terminal?id="+a.ID, ""); w.Code != 204 {
		t.Fatalf("terminal: %d %s", w.Code, w.Body)
	}
	if len(opened) != 1 || !strings.Contains(opened[0], "sleep 600 # mad") {
		t.Errorf("terminal app was told %q", opened)
	}
	panes, _ := tmux.ListPanes()
	if pn, ok := tmux.FindPane(panes, a.ID); !ok || !tmux.OnStage(pn) {
		t.Error("the agent is not shown on stage")
	}
	if w := call(t, s, "POST", "/api/terminal?id=nobody", ""); w.Code != 404 {
		t.Errorf("unknown agent: %d", w.Code)
	}
}

func TestAppleString(t *testing.T) {
	if got := appleString(`'/a b/mad' "x" \n`); got != `"'/a b/mad' \"x\" \\n"` {
		t.Errorf("appleString = %s", got)
	}
}

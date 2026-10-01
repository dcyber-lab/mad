package run

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

func TestMain(m *testing.M) {
	if os.Getenv("MAD_FAKE_ROLE") != "" {
		fakeRole()
		return
	}
	os.Exit(m.Run())
}

// fakeRole plays any role of the default flow as claude would, reporting
// through the status files, and answers by what it is asked: the
// implementer asks the designer one question, the reviewer wants one
// round of changes.
func fakeRole() {
	id := os.Getenv("MAD_AGENT_ID")
	report := func(state, event string) {
		_ = status.WriteHook(id, &status.Hook{State: state, Event: event}, time.Now())
	}
	fmt.Print("\x1b[?2004h")
	fmt.Println("fake role ready")
	report(status.Idle, "SessionStart")
	in := bufio.NewReader(os.Stdin)
	asked := false
	for {
		msg, ok := readMessage(in)
		if !ok {
			return
		}
		report(status.Running, "UserPromptSubmit")
		if f, err := os.OpenFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fake-messages"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "=== %s\n%s\n", id, msg)
			f.Close()
		}
		var reply string
		switch {
		case strings.Contains(msg, "· question]"):
			reply = "Yes, keep the old format.\nDONE"
		case strings.Contains(msg, "· design]"):
			reply = "The design is written.\nDONE"
		case strings.Contains(msg, "designer answers:"):
			reply = "Implemented, as answered.\nDONE"
		case strings.Contains(msg, "· implement]") && strings.Contains(msg, "Review findings"):
			reply = "Added the tests.\nDONE"
		case strings.Contains(msg, "· implement]") && !asked:
			asked = true
			reply = "Read the design.\nQUESTION(@designer): keep the old format?"
		case strings.Contains(msg, "· review]") && strings.Contains(msg, "your last review"):
			reply = "All solved.\nVERDICT: APPROVE"
		case strings.Contains(msg, "· review]"):
			reply = "- a.go has no tests\nVERDICT: CHANGES"
		default:
			reply = "?\nDONE"
		}
		for i := 0; i < 3; i++ { // work a moment, the screen moving
			fmt.Print(".")
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Println("\x1b[2J\x1b[H" + reply)
		at := time.Now()
		_ = status.WriteTurn(id, &status.Turn{Reply: reply}, at)
		_ = status.WriteHook(id, &status.Hook{State: status.Idle, Event: "Stop"}, at)
	}
}

func readMessage(in *bufio.Reader) (string, bool) {
	var b strings.Builder
	pasting := false
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return "", false
		}
		if i := strings.Index(line, "\x1b[200~"); i >= 0 {
			line, pasting = line[i+len("\x1b[200~"):], true
		}
		if i := strings.Index(line, "\x1b[201~"); i >= 0 {
			b.WriteString(line[:i])
			return b.String(), true
		}
		if !pasting {
			return strings.TrimRight(line, "\r\n"), true
		}
		b.WriteString(line)
	}
}

// A whole run on a real tmux server and git repository, every role a
// fake: the question goes to the designer and back, the first review
// sends the work back, the second approves.
func TestRunEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("MAD_AGENT_ID", "")
	t.Setenv("MAD_SOCKET", "")
	oldSocket, oldSelf, oldSettle := tmux.Socket, deck.SelfCommand, tmux.PasteSettle
	tmux.Socket = fmt.Sprintf("mad-run-test-%d", time.Now().UnixNano())
	deck.SelfCommand = func(sub string) string { return "sleep 600 # mad " + sub }
	tmux.PasteSettle, retryEvery = 50*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand, tmux.PasteSettle, retryEvery = oldSocket, oldSelf, oldSettle, 2*time.Second
	})

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := "MAD_FAKE_ROLE=1 exec " + paths.ShellQuote(self)
	// Every kind plays as the fake, which reports like claude.
	agents := fmt.Sprintf(`[{"name":"claude","start":%q,"resume":%[1]q,"fork":"","hooks":true,"waiting":[]},
		{"name":"codex","start":%[1]q,"resume":%[1]q,"resume_latest":"","hooks":true,"waiting":[]}]`, fake)
	write := func(path, body string) {
		t.Helper()
		if err := paths.WriteFileAtomic(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write(paths.AgentsConfig(), agents)
	write(paths.ConfigFile(), `{"notify":{"on":[]},"quota":false}`)

	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	// The builder played by codex (the fake too), and a stop after the
	// design for you.
	r, err := Create(Options{Project: repo, Task: "add an audit log", Gate: true, Agents: map[string]string{"builder": "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "run/add-an-audit-log" || !strings.HasSuffix(r.Dir, filepath.Join(".claude", "worktrees", "run-add-an-audit-log")) {
		t.Errorf("run = %+v", r)
	}
	if out, _ := exec.Command("git", "-C", r.Dir, "status", "--porcelain").Output(); len(out) != 0 {
		t.Errorf("the run's files show in git status: %s", out)
	}

	done := make(chan error, 1)
	go func() { done <- Exec(r.ID, io.Discard) }()
	// The gate after the design: it waits until you continue.
	for end := time.Now().Add(time.Minute); ; time.Sleep(100 * time.Millisecond) {
		if f, err := Load(Dir(r)); err == nil && f.Progress.Status == Waiting && strings.Contains(f.Progress.Waiting, "look it over") {
			if n := len(f.Progress.Entries); n != 1 || f.Progress.Entries[0].Status != "done" {
				t.Fatalf("waiting at the gate after %+v", f.Progress.Entries)
			}
			break
		}
		if time.Now().After(end) {
			f, _ := Load(Dir(r))
			t.Fatalf("never stopped at the gate: %+v", f.Progress)
		}
	}
	// A note for every role, and something typed into the designer:
	// the implementer hears of both, the designer of neither again.
	if err := AddNote(r, "", "keep the change small"); err != nil {
		t.Fatal(err)
	}
	if err := AddNote(r, "designer", "the old format stays"); err != nil {
		t.Fatal(err)
	}
	if err := Command(r, Continue); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Minute):
		f, _ := Load(Dir(r))
		t.Fatalf("run never ended: %+v", f.Progress)
	}

	f, err := Load(Dir(r))
	if err != nil {
		t.Fatal(err)
	}
	pr := f.Progress
	var got []string
	for _, e := range pr.Entries {
		got = append(got, fmt.Sprintf("%s/%d/%s", e.Step, e.Round, e.Status))
	}
	want := []string{"design/1/done", "implement/1/done", "review/1/changes", "implement/2/done", "review/2/approve"}
	if pr.Status != Done || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("status %s, steps %v, want %v (waiting: %q)", pr.Status, got, want, pr.Waiting)
	}
	if pr.Entries[1].Result != "Implemented, as answered." {
		t.Errorf("the implementer's answered reply: %q", pr.Entries[1].Result)
	}
	if pr.Entries[2].Result != "CHANGES: a.go has no tests" {
		t.Errorf("review result = %q", pr.Entries[2].Result)
	}
	reply, err := os.ReadFile(filepath.Join(Dir(r), pr.Entries[4].Reply))
	if err != nil || !strings.Contains(string(reply), "VERDICT: APPROVE") {
		t.Errorf("last reply kept as %q: %v", reply, err)
	}
	sent, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fake-messages"))
	msgs := strings.Split(string(sent), "=== ")
	var implement, question string
	for _, m := range msgs {
		switch {
		case strings.Contains(m, "· implement]") && implement == "":
			implement = m
		case strings.Contains(m, "· question]"):
			question = m
		}
	}
	if !strings.Contains(implement, "- keep the change small") || !strings.Contains(implement, "- (told the designer, at the design) the old format stays") {
		t.Errorf("the implementer was not told the notes:\n%s", implement)
	}
	if n := strings.Count(string(sent), "keep the change small"); n != 2 { // to the implementer and the reviewer, once each
		t.Errorf("the note went out %d times", n)
	}
	if strings.Contains(question, "the old format stays") {
		t.Errorf("the designer was told what was typed into it:\n%s", question)
	}
	if notes, _ := os.ReadFile(filepath.Join(Dir(r), "notes.md")); !strings.Contains(string(notes), "keep the change small") {
		t.Errorf("notes.md = %q", notes)
	}
	log, _ := os.ReadFile(logPath(Dir(r)))
	for _, want := range []string{"implementer asks the designer: keep the old format?", "designer answers: Yes, keep the old format.",
		"your note: keep the change small", "you told the designer: the old format stays", "run done"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}

	// Its agents are in the state as its roles, under the run.
	st, _ := state.Load()
	p, _ := st.FindRun(r.ID)
	roles := map[string]string{}
	for _, a := range p.RunAgents(r.ID) {
		if a.Dir == r.Dir {
			roles[a.Role] = a.Kind + " " + strings.Join(a.Args, " ")
		}
	}
	for role, want := range map[string]string{
		"designer": "claude --model opus --permission-mode auto",
		"builder":  "codex -s workspace-write",
		"reviewer": "codex -s read-only",
	} {
		if !strings.HasPrefix(roles[role], want) {
			t.Errorf("%s = %q, want %q…", role, roles[role], want)
		}
	}

	// A runner started again on a run that is over just draws it.
	if err := Exec(r.ID, io.Discard); err != nil {
		t.Errorf("runner on a finished run: %v", err)
	}
}

func TestControlLines(t *testing.T) {
	cases := []struct {
		reply, verdict, asks, body string
	}{
		{"all good\n\nVERDICT: APPROVE", Approve, "", "all good"},
		{"- a\n- b\n**VERDICT: CHANGES**", Changes, "", "- a\n- b"},
		{"VERDICT: changes\nthanks", Changes, "", "VERDICT: changes\nthanks"},
		{"read it\nQUESTION(@designer): which format?", "", "designer:which format?", "read it"},
		{"done here\nDONE", "", "", "done here"},
		{"no control", "", "", "no control"},
	}
	for _, c := range cases {
		if v := verdict(c.reply); v != c.verdict {
			t.Errorf("verdict(%q) = %q", c.reply, v)
		}
		role, q, ok := question(c.reply)
		if got := role + ":" + q; ok != (c.asks != "") || ok && got != c.asks {
			t.Errorf("question(%q) = %q %v", c.reply, got, ok)
		}
		if b := body(c.reply); b != c.body {
			t.Errorf("body(%q) = %q", c.reply, b)
		}
	}
	if s := summary("- **first** finding\nmore\nVERDICT: CHANGES"); s != "**first** finding" {
		t.Errorf("summary = %q", s)
	}
}

func TestBranchFor(t *testing.T) {
	at := time.Date(2026, 10, 1, 14, 5, 0, 0, time.UTC)
	for task, want := range map[string]string{
		"Add an audit log to L3 subnet allocation":                 "run/add-an-audit-log-to-l3-subnet",
		"https://git.example.com/sdn/ipam/-/issues/1 implement it": "run/issue-1-implement-it",
		"see https://github.com/o/r/pull/42":                       "run/pr-42-see",
		"https://example.com/x":                                    "run/1001-1405",
		"\u7ed9 L3 \u5b50\u7f51\u52a0\u5ba1\u8ba1\u65e5\u5fd7":     "run/1001-1405", // no ASCII words to speak of
		"": "run/1001-1405",
	} {
		if got := BranchFor(task, at); got != want {
			t.Errorf("BranchFor(%q) = %q, want %q", task, got, want)
		}
	}
}

func TestCommands(t *testing.T) {
	r := &state.Run{Name: "x", Dir: t.TempDir()}
	if takeCommand(Dir(r)) != "" {
		t.Fatal("a command out of nowhere")
	}
	if err := Command(r, Continue); err != nil {
		t.Fatal(err)
	}
	if got := takeCommand(Dir(r)); got != Continue {
		t.Errorf("took %q", got)
	}
	if got := takeCommand(Dir(r)); got != "" {
		t.Errorf("taken twice: %q", got)
	}
}

func TestPrompt(t *testing.T) {
	f, _ := FlowByName("", "")
	x := &runner{run: &state.Run{Name: "audit"}, flow: f,
		f: &File{Spec: Spec{Task: "add an audit log", Base: "abc123"}, Progress: Progress{Round: 1}}}
	role := func(name string) Role { r, _ := f.Role(name); return r }
	design := x.prompt(f.Steps[0], role("designer"))
	for _, want := range []string{"[mad run audit · design]", "the designer", "Task: add an audit log", ".mad/runs/audit/design.md", "make the last line just DONE"} {
		if !strings.Contains(design, want) {
			t.Errorf("design prompt lacks %q:\n%s", want, design)
		}
	}
	x.f.Progress.Round, x.f.Progress.Review = 2, "- a.go has no tests"
	x.f.Progress.Rounds = map[string]int{"review": 2}
	x.f.Progress.Reviews = map[string]string{"review": "- a.go has no tests"}
	x.f.Progress.BackFrom = "review"
	fix := x.prompt(f.Steps[1], role("builder"))
	if !strings.Contains(fix, "Review findings (round 1)") || !strings.Contains(fix, "- a.go has no tests") ||
		!strings.Contains(fix, "you may ask the designer (@designer)") {
		t.Errorf("fix prompt:\n%s", fix)
	}
	if again := x.prompt(f.Steps[2], role("reviewer")); !strings.Contains(again, "Review git diff abc123 again") ||
		!strings.HasSuffix(again, "Make the last line just VERDICT: APPROVE or VERDICT: CHANGES.") {
		t.Errorf("second review prompt:\n%s", again)
	}
}

func TestTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(`model = "x"
[projects."/src/a"]
trust_level = "trusted"

[projects."/src/b"]
trust_level = "untrusted"
`), 0o644)
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"projects":{"/src/a":{"hasTrustDialogAccepted":true},"/src/b":{}}}`), 0o644)
	for dir, want := range map[string]bool{"/src/a": true, "/src/a/.claude/worktrees/x": true, "/src/b": false, "/src/c": false} {
		if got := codexTrusts(dir); got != want {
			t.Errorf("codexTrusts(%s) = %v", dir, got)
		}
		if got := claudeTrusts(dir); got != want {
			t.Errorf("claudeTrusts(%s) = %v", dir, got)
		}
	}
}

func TestRoleArgs(t *testing.T) {
	join := func(r Role, p string) string { return strings.Join(r.Args(p), " ") }
	f, _ := FlowByName("", "")
	designer, _ := f.Role("designer")
	builder, _ := f.Role("builder")
	reviewer, _ := f.Role("reviewer")
	cases := []struct {
		role Role
		perm string
		want string
	}{
		{designer, PermAuto, "--permission-mode auto"},
		{builder, PermAllowlist, "--permission-mode acceptEdits --allowedTools " + builderTools},
		{builder, PermBypass, "--permission-mode bypassPermissions"},
		{reviewer, PermBypass, "-s read-only -a never -c check_for_update_on_startup=false"},
		{Role{Kind: "codex"}, PermAuto, "-s workspace-write -a never -c check_for_update_on_startup=false"},
	}
	for _, c := range cases {
		if got := join(c.role, c.perm); got != c.want {
			t.Errorf("%s/%s %s: %q, want %q", c.role.Kind, c.role.Name, c.perm, got, c.want)
		}
	}
	for _, perm := range []string{PermAuto, PermAllowlist, PermBypass} {
		got := join(Role{Kind: "claude", ReadOnly: true}, perm)
		for _, bad := range []string{"bypassPermissions", "git commit", "acceptEdits"} {
			if strings.Contains(got, bad) {
				t.Errorf("read only claude under %s has %q: %s", perm, bad, got)
			}
		}
		if !strings.Contains(got, "--disallowedTools Edit,Write,NotebookEdit") || !strings.Contains(got, "Bash(git diff:*)") {
			t.Errorf("read only claude under %s: %s", perm, got)
		}
	}
	g := f.WithAgents(map[string]string{"reviewer": "claude:opus"})
	if r, _ := g.Role("reviewer"); r.Kind != "claude" || r.Model != "opus" || !r.ReadOnly {
		t.Errorf("reviewer as claude = %+v", r)
	}
	if r, _ := f.Role("reviewer"); r.Kind != "codex" {
		t.Error("WithAgents changed the flow it was called on")
	}
}

func TestPanel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	f, _ := FlowByName("", "")
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var out strings.Builder
	x := &runner{run: &state.Run{Name: "audit", Branch: "run/audit", Dir: t.TempDir()}, flow: f, kinds: nil, out: &out,
		usage: map[string]transcript.Info{},
		f: &File{Spec: Spec{Task: "add an audit log", MaxRounds: 3, Budget: 10, Compact: 150_000}, Progress: Progress{
			Status: Done, Step: 3, Round: 2, Cost: 1.25, Tokens: 70_000, Started: at, Ended: at.Add(23 * time.Minute),
			Entries: []Entry{
				{Step: "design", Role: "designer", Round: 1, Status: "done", Result: "design.md written", Start: at, End: at.Add(time.Minute), Cost: 0.5},
				{Step: "implement", Role: "builder", Round: 1, Status: "done", Result: "added the log", Start: at, End: at.Add(5 * time.Minute), Cost: 0.75},
				{Step: "review", Role: "reviewer", Round: 1, Status: "changes", Result: "CHANGES: no tests", Start: at, End: at.Add(time.Minute), Tokens: 40_000},
			}}},
		diff: []string{" a.go | 2 +-", " 1 file changed"}}
	x.dir = filepath.Join(t.TempDir(), "run")
	x.paint()
	for _, want := range []string{"run audit", "✓ approved after 1 round of changes · 23m", "implementation", "↺", "CHANGES: no tests", "40k tok", "changes", "f pull request or merge"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("panel lacks %q:\n%s", want, out.String())
		}
	}
}

func TestCodexModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if got := Choices(); len(got) != 4 || got[3].String() != "codex" {
		t.Errorf("without a models cache: %v", got)
	}
	os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-b","visibility":"list","priority":3},
		{"slug":"hidden","visibility":"hide","priority":1},
		{"slug":"gpt-a","visibility":"list","priority":2}]}`), 0o644)
	var got []string
	for _, c := range Choices() {
		got = append(got, c.String())
	}
	if want := "claude:opus claude:sonnet claude:haiku codex codex:gpt-a codex:gpt-b"; strings.Join(got, " ") != want {
		t.Errorf("choices = %s, want %s", strings.Join(got, " "), want)
	}
}

func TestEffort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-a\"\n[tui]\nmodel = \"no\"\n"), 0o644)
	os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-a","visibility":"list","priority":1,"supported_reasoning_levels":[{"effort":"low"},{"effort":"ultra"}]},
		{"slug":"gpt-b","visibility":"list","priority":2,"supported_reasoning_levels":[{"effort":"high"}]}]}`), 0o644)
	for c, want := range map[Choice]string{
		{Kind: "claude", Model: "opus"}: " low medium high xhigh max",
		{Kind: "codex"}:                 " low ultra", // the model its config sets
		{Kind: "codex", Model: "gpt-b"}: " high",
		{Kind: "codex", Model: "gone"}:  " low medium high xhigh",
	} {
		if got := strings.Join(Efforts(c), " "); got != want {
			t.Errorf("Efforts(%v) = %q, want %q", c, got, want)
		}
	}
	for _, s := range []string{"claude:opus@high", "codex@xhigh", "codex:gpt-b", "claude"} {
		if got := ParseChoice(s).String(); got != s {
			t.Errorf("ParseChoice(%q).String() = %q", s, got)
		}
	}
	f, _ := FlowByName("", "")
	g := f.WithAgents(map[string]string{"designer": "claude:opus@max", "reviewer": "codex:gpt-b@high"})
	d, _ := g.Role("designer")
	r, _ := g.Role("reviewer")
	if got := strings.Join(d.Args(PermAuto), " "); !strings.HasSuffix(got, "--effort max") {
		t.Errorf("designer args %q", got)
	}
	if got := strings.Join(r.Args(PermAuto), " "); !strings.HasSuffix(got, "-c model_reasoning_effort=high") || r.Model != "gpt-b" {
		t.Errorf("reviewer %+v args %q", r, got)
	}
}

func TestFlowFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := paths.WriteFileAtomic(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	// A project flow, prompts as lines; one of the user's that takes a
	// built-in's place; one that doesn't check.
	write(filepath.Join(ProjectFlows(root), "tdd.json"), `{
		"name": "tdd", "description": "tests first",
		"roles": [{"name": "tester", "agent": "claude:opus@high"}, {"name": "coder", "agent": "codex:gpt-x"},
		          {"name": "checker", "agent": "claude:sonnet", "read_only": true}],
		"steps": [
			{"name": "tests", "role": "tester", "prompt": ["Task: {{task}}", "Write failing tests."], "gate": true},
			{"name": "code", "role": "coder", "prompt": "Make the tests pass.", "ask": ["tester"]},
			{"name": "check", "role": "checker", "prompt": "Review git diff {{base}}.", "review": true, "back": "code", "max_rounds": 2}
		]}`)
	write(filepath.Join(UserFlows(), "impl-review.json"), `{"name": "impl-review",
		"roles": [{"name": "solo", "agent": "claude"}], "steps": [{"name": "do", "role": "solo", "prompt": "{{task}}"}]}`)
	write(filepath.Join(ProjectFlows(root), "broken.json"), `{"name": "broken", "roles": [], "steps": []}`)

	flows, err := Flows(root)
	if err == nil || !strings.Contains(err.Error(), "broken.json") || !strings.Contains(err.Error(), "no roles") {
		t.Errorf("the broken file: %v", err)
	}
	var names []string
	for _, f := range flows {
		names = append(names, f.Name+"@"+filepath.Base(f.Source))
	}
	if got := strings.Join(names, " "); got != "design-impl-review@built in impl-review@impl-review.json tdd@tdd.json" {
		t.Errorf("flows = %s", got)
	}
	tdd, ok := FlowByName(root, "tdd")
	if !ok || tdd.Steps[0].Prompt != "Task: {{task}}\nWrite failing tests." || tdd.Gates()[0] != "tests" {
		t.Fatalf("tdd = %+v", tdd)
	}
	if r, _ := tdd.Role("tester"); r.Kind != "claude" || r.Model != "opus" || r.Effort != "high" || r.Label != "tester" {
		t.Errorf("tester = %+v", r)
	}
	if e := tdd.ending(tdd.Steps[1]); !strings.Contains(e, "ask the tester (@tester)") || !strings.HasSuffix(e, "just DONE.") {
		t.Errorf("ending of code: %q", e)
	}
	// show writes what reads back the same.
	data, err := Marshal(tdd)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(data)
	if err != nil || again.Steps[0].Prompt != tdd.Steps[0].Prompt || strings.Contains(string(data), `"label"`) {
		t.Errorf("round trip: %v\n%s", err, data)
	}
}

func TestCheck(t *testing.T) {
	for body, want := range map[string]string{
		`{"name": "X", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "p"}]}`:                                                                         `name "X"`,
		`{"name": "x", "roles": [{"name": "a", "agent": "gemini"}], "steps": [{"name": "s", "role": "a", "prompt": "p"}]}`:                                                                         `a run works with claude and codex`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude@huge"}], "steps": [{"name": "s", "role": "a", "prompt": "p"}]}`:                                                                    `claude's effort is one of`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "b", "prompt": "p"}]}`:                                                                         `no role "b"`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "{{tsk}}"}]}`:                                                                   `unknown placeholder {{tsk}}`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "p", "review": true}]}`:                                                         `a review needs back`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "p", "review": true, "back": "t"}, {"name": "t", "role": "a", "prompt": "p"}]}`: `back "t" is no step before it`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude", "read_only": true}], "steps": [{"name": "s", "role": "a", "prompt": "p"}]}`:                                                      `read only, so it can only review`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "p", "ask": ["a"]}]}`:                                                           `can't ask itself`,
		`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "s", "role": "a", "prompt": "p", "parallel": true}]}`:                                                       `unknown field "parallel"`,
	} {
		if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s:\n got %v\nwant …%s…", body, err, want)
		}
	}
}

// The flows the mad-flow skill shows as examples are flows mad takes.
func TestSkillExamples(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "mad-flow", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\n(.*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) < 2 {
		t.Fatalf("found %d examples", len(blocks))
	}
	for _, b := range blocks {
		if _, err := Parse([]byte(b[1])); err != nil {
			t.Errorf("example does not check: %v\n%s", err, b[1])
		}
	}
}

// Two reviews each count their own rounds and quote their own findings.
func TestReviewRoundsAreOwn(t *testing.T) {
	f := Flow{Name: "two", Roles: []Role{{Name: "a", Kind: "claude"}, {Name: "r", Kind: "claude", ReadOnly: true}},
		Steps: []Step{
			{Name: "impl", Role: "a", Prompt: "do it"},
			{Name: "review", Role: "r", Review: true, Back: "impl", Prompt: "first look"},
			{Name: "audit", Role: "r", Review: true, Back: "impl", MaxRounds: 2, Prompt: "audit only"},
		}}
	x := &runner{run: &state.Run{Name: "t"}, flow: f,
		f: &File{Spec: Spec{Task: "t", Base: "b"}, Progress: Progress{Round: 2,
			Rounds: map[string]int{"review": 2}, Reviews: map[string]string{"review": "- from review"}, BackFrom: "review"}}}
	if p := x.prompt(f.Steps[2], f.Roles[1]); !strings.Contains(p, "audit only") {
		t.Errorf("audit's first run lost its prompt:\n%s", p)
	}
	if p := x.prompt(f.Steps[0], f.Roles[0]); !strings.Contains(p, "- from review") {
		t.Errorf("fix lacks the review that sent it back:\n%s", p)
	}
	if r, _ := x.roundOf(f.Steps[2]); r != 1 {
		t.Errorf("audit round = %d, want 1", r)
	}
}

func TestOutside(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "worktrees", "run-x")
	os.MkdirAll(dir, 0o755)
	t.Setenv("HOME", filepath.Dir(root))
	home := "~/" + filepath.Base(root)
	cases := []struct {
		tool discover.Tool
		out  bool
	}{
		{discover.Tool{Name: "Write", Path: filepath.Join(dir, "a/b.go")}, false},
		{discover.Tool{Name: "Edit", Path: filepath.Join(root, "b.go")}, true},
		{discover.Tool{Name: "Edit", Path: filepath.Join(root, ".claude", "worktrees", "run-y", "b.go")}, true},
		{discover.Tool{Name: "Write", Path: filepath.Join(dir, "..", "run-y", "b.go")}, true},
		{discover.Tool{Name: "Write", Path: "/tmp/scratch.txt"}, false},
		{discover.Tool{Name: "Write", Path: filepath.Join(os.TempDir(), "x", "y")}, false},
		{discover.Tool{Name: "Bash", Command: "go test ./..."}, false},
		{discover.Tool{Name: "Bash", Command: "cd " + dir + " && go test ./..."}, false},
		{discover.Tool{Name: "Bash", Command: "cat '" + dir + "/go.mod'"}, false},
		{discover.Tool{Name: "Bash", Command: "ls " + root + "-other"}, false},
		{discover.Tool{Name: "Bash", Command: "cd " + root + " && git commit -am x"}, true},
		{discover.Tool{Name: "Bash", Command: "git -C " + root + "/ status"}, true},
		{discover.Tool{Name: "Bash", Command: "sed -i '' s/a/b/ " + root + "/main.go"}, true},
		{discover.Tool{Name: "Bash", Command: "cp x " + dir + "-old/"}, true},
		{discover.Tool{Name: "Bash", Command: "cd " + home + "/.claude/worktrees/run-x"}, false},
		{discover.Tool{Name: "Bash", Command: "cd " + home}, true},
	}
	for _, c := range cases {
		why := Outside(dir, root, &c.tool)
		if (why != "") != c.out {
			t.Errorf("%s %s%s: %q", c.tool.Name, c.tool.Path, c.tool.Command, why)
		}
	}
	if why := Outside(root, root, &discover.Tool{Name: "Bash", Command: "ls " + root}); why != "" {
		t.Errorf("a run in the main checkout: %q", why)
	}
}

func TestCheckoutChanges(t *testing.T) {
	before := "# branch.oid aaa\n# branch.head main\n1 .M N... 100644 100644 100644 h1 h1 a.go\n? notes.txt\n"
	if got := changes(before, before); got != nil {
		t.Errorf("no change: %q", got)
	}
	after := "# branch.oid bbb\n# branch.head main\n1 .M N... 100644 100644 100644 h1 h1 a.go\n1 .M N... 100644 100644 100644 h2 h2 b dir/c.go\n"
	if got := strings.Join(changes(before, after), ", "); got != "HEAD, b dir/c.go, notes.txt" {
		t.Errorf("changes = %q", got)
	}
	if got := changes("", after); got != nil {
		t.Errorf("unknown before: %q", got)
	}
}

func TestWorktreeArgs(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	dir := filepath.Join(root, ".claude", "worktrees", "run-x")
	if err := git.AddWorktree(root, "run/x", dir); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Role{Kind: "codex"}.WorktreeArgs(dir), " ")
	if !strings.HasPrefix(got, "-c sandbox_workspace_write.writable_roots=[") || !strings.Contains(got, `/.git/objects"`) {
		t.Errorf("codex builder: %s", got)
	}
	for _, r := range []Role{{Kind: "codex", ReadOnly: true}, {Kind: "claude"}} {
		if args := r.WorktreeArgs(dir); args != nil {
			t.Errorf("%+v: %q", r, args)
		}
	}
	if args := (Role{Kind: "codex"}).WorktreeArgs(root); args != nil {
		t.Errorf("main checkout: %q", args)
	}
}

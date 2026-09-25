package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixture is a fake home with Claude and Codex history.
type fixture struct {
	t    *testing.T
	home string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// t.TempDir lives under a temp root, which discovery skips on purpose.
	old := tempRoots
	tempRoots = nil
	t.Cleanup(func() { tempRoots = old })
	return &fixture{t, home}
}

// project creates a directory under the fake home.
func (f *fixture) project(name string) string {
	dir := filepath.Join(f.home, "code", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

func (f *fixture) write(path string, lines []any, mtime time.Time) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range lines {
		data, err := json.Marshal(l)
		if err != nil {
			f.t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		f.t.Fatal(err)
	}
}

type claudeOpts struct {
	entrypoint string // default cli
	turnOrigin string // default human
	title      string // custom-title record, appended at the end
	isMeta     bool
}

func (f *fixture) claudeSession(cwd, id, firstMsg string, age time.Duration, o claudeOpts) {
	if o.entrypoint == "" {
		o.entrypoint = "cli"
	}
	if o.turnOrigin == "" {
		o.turnOrigin = "human"
	}
	lines := []any{
		map[string]any{"type": "queue-operation", "sessionId": id},
		map[string]any{
			"type": "user", "isMeta": o.isMeta, "entrypoint": o.entrypoint, "cwd": cwd, "turnOrigin": o.turnOrigin,
			"message": map[string]any{"role": "user", "content": firstMsg},
		},
		map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{}}},
	}
	if o.title != "" {
		lines = append(lines, map[string]any{"type": "custom-title", "customTitle": o.title, "sessionId": id})
	}
	f.write(filepath.Join(claudeProjectDir(cwd), id+".jsonl"), lines, time.Now().Add(-age))
}

type codexOpts struct {
	originator string // default codex-tui
	parent     string
	source     string // default user
}

func (f *fixture) codexSession(cwd, id, firstMsg string, age time.Duration, o codexOpts) {
	if o.originator == "" {
		o.originator = "codex-tui"
	}
	if o.source == "" {
		o.source = "user"
	}
	when := time.Now().Add(-age)
	meta := map[string]any{"id": id, "cwd": cwd, "originator": o.originator, "thread_source": o.source}
	if o.parent != "" {
		meta["parent_thread_id"] = o.parent
	}
	lines := []any{
		map[string]any{"type": "session_meta", "payload": meta},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "# AGENTS.md instructions for " + cwd}},
		}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": firstMsg}},
		}},
	}
	path := filepath.Join(codexSessionsDir(), when.Format("2006/01/02"),
		fmt.Sprintf("rollout-%s-%s.jsonl", when.Format("2006-01-02T15-04-05"), id))
	f.write(path, lines, when)
}

func uuid(n int) string { return fmt.Sprintf("%08d-0000-4000-8000-000000000000", n) }

func titles(ss []Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Title)
	}
	return out
}

func TestClaudeSessions(t *testing.T) {
	f := newFixture(t)
	app := f.project("app")
	wt := filepath.Join(app, ".claude", "worktrees", "feature-x")
	_ = os.MkdirAll(wt, 0o755)

	f.claudeSession(app, uuid(1), "fix the login bug", 3*time.Hour, claudeOpts{})
	f.claudeSession(app, uuid(2), "<system-reminder>ctx</system-reminder>\n\nrefactor api\nmore", 2*time.Hour,
		claudeOpts{entrypoint: "claude-desktop", title: "API refactor"})
	f.claudeSession(wt, uuid(3), "in a worktree", time.Hour, claudeOpts{})
	f.claudeSession(app, uuid(4), "Review bundle b01", 10*time.Minute, claudeOpts{entrypoint: "claude-desktop", turnOrigin: "sdk"})
	f.claudeSession(app, uuid(5), "headless", 5*time.Minute, claudeOpts{entrypoint: "sdk-cli"})
	f.claudeSession(app, uuid(6), "<command-name>/model</command-name>", time.Minute, claudeOpts{})
	f.claudeSession(f.project("other"), uuid(7), "other project", time.Minute, claudeOpts{})

	ss := ProjectSessions(app, "claude")
	got := strings.Join(titles(ss), " | ")
	if want := "in a worktree | API refactor | fix the login bug"; got != want {
		t.Fatalf("titles = %q, want %q", got, want)
	}
	if ss[1].Origin != "desktop" || ss[2].Origin != "cli" {
		t.Errorf("origins = %s, %s", ss[1].Origin, ss[2].Origin)
	}
	if ss[0].Cwd != wt || ss[0].ID != uuid(3) {
		t.Errorf("worktree session = %+v", ss[0])
	}
}

func TestCodexSessions(t *testing.T) {
	f := newFixture(t)
	app := f.project("app")
	f.codexSession(app, "t-1", "add retries", 2*time.Hour, codexOpts{})
	f.codexSession(app, "t-2", "desktop thread", time.Hour, codexOpts{originator: "Codex Desktop"})
	f.codexSession(app, "t-3", "sub", 30*time.Minute, codexOpts{parent: "t-1"})
	f.codexSession(app, "t-4", "The following is the Codex agent history", 20*time.Minute, codexOpts{source: "guardian_review"})
	f.codexSession(app, "t-5", "exec", 10*time.Minute, codexOpts{originator: "codex_exec"})
	f.codexSession(f.project("other"), "t-6", "elsewhere", time.Minute, codexOpts{})

	index := filepath.Join(f.home, ".codex", "session_index.jsonl")
	f.write(index, []any{
		map[string]any{"id": "t-1", "thread_name": "old name"},
		map[string]any{"id": "t-1", "thread_name": "Retry logic"},
	}, time.Now())

	ss := ProjectSessions(app, "codex")
	got := strings.Join(titles(ss), " | ")
	if want := "desktop thread | Retry logic"; got != want {
		t.Fatalf("titles = %q, want %q", got, want)
	}
	if ss[0].Origin != "desktop" || ss[1].Origin != "cli" || ss[1].ID != "t-1" {
		t.Errorf("sessions = %+v", ss)
	}
}

func TestScanHistory(t *testing.T) {
	f := newFixture(t)
	a, b, c := f.project("alpha"), f.project("beta"), f.project("gamma")
	f.claudeSession(a, uuid(1), "a", 3*time.Hour, claudeOpts{})
	f.claudeSession(filepath.Join(a, ".claude", "worktrees", "wt"), uuid(2), "a wt", time.Hour, claudeOpts{entrypoint: "claude-desktop"})
	f.codexSession(b, "t-1", "b", 2*time.Hour, codexOpts{originator: "Codex Desktop"})
	f.codexSession(c, "t-2", "c", 30*time.Minute, codexOpts{})
	f.claudeSession(filepath.Join(f.home, ".claude-mem", "observer"), uuid(3), "tool state", time.Minute, claudeOpts{})
	f.claudeSession(filepath.Join(f.home, "code", "deleted"), uuid(4), "gone", time.Minute, claudeOpts{})
	f.claudeSession(f.project("ancient"), uuid(5), "old", (HistoryDays+5)*24*time.Hour, claudeOpts{})

	got := ScanHistory()
	var lines []string
	for _, c := range got {
		srcs := append([]string(nil), c.Sources...)
		sort.Strings(srcs)
		lines = append(lines, filepath.Base(c.Path)+":"+strings.Join(srcs, ","))
	}
	want := "gamma:codex alpha:claude,claude desktop beta:codex desktop"
	if strings.Join(lines, " ") != want {
		t.Errorf("history = %v\nwant      %s", lines, want)
	}
}

func TestParsePS(t *testing.T) {
	out := strings.Join([]string{
		"  101 ttys001  claude",
		"  102 ttys002  /usr/local/bin/codex resume t-9",
		"  103 ttys003  claude --settings /u/.config/mad/claude-settings.json --session-id x", // mad's own
		"  104 ttys004  claude",       // a deck pane
		"  105 pts/1    codex",        // linux terminal
		"  106 ttys005  vim notes.md", // not an agent
		"  107 ??       /Applications/Claude.app/Contents/Helpers/disclaimer --pgroup -- /Users/u/Library/Application Support/Claude/claude-code/2.1/claude.app/Contents/MacOS/claude --output-format stream-json",
		"  108 ??       /Users/u/Library/Application Support/Claude/claude-code/2.1/claude.app/Contents/MacOS/claude --output-format stream-json --resume=abc",
		"  109 ??       /Users/u/Library/Application Support/Claude/claude-code/2.1/claude.app/Contents/MacOS/claude --no-session-persistence -p x",
		"  110 ??       /usr/sbin/daemon",
		"junk",
	}, "\n")
	want := map[string]bool{"claude": true, "codex": true}
	procs := parsePS(out, want, map[string]bool{"/dev/ttys004": true})

	var got []string
	for _, p := range procs {
		got = append(got, fmt.Sprintf("%d:%s:%v:%s", p.pid, p.kind, p.desktop, strings.Join(p.args, " ")))
	}
	wantProcs := []string{
		"101:claude:false:",
		"102:codex:false:resume t-9",
		"105:codex:false:",
		"108:claude:true:--output-format stream-json --resume=abc",
	}
	if strings.Join(got, "\n") != strings.Join(wantProcs, "\n") {
		t.Errorf("parsePS:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantProcs, "\n"))
	}

	if procs := parsePS(out, map[string]bool{"codex": true}, nil); len(procs) != 2 {
		t.Errorf("filter by kind: %+v", procs)
	}
}

func TestScanExternal(t *testing.T) {
	f := newFixture(t)
	app, web, tools := f.project("app"), f.project("web"), f.project("tools")
	wt := filepath.Join(app, ".claude", "worktrees", "wt")
	_ = os.MkdirAll(wt, 0o755)

	// Terminal claude without an id: guessed from the newest session there.
	f.claudeSession(web, uuid(10), "older", 2*time.Hour, claudeOpts{})
	f.claudeSession(web, uuid(11), "newest", time.Minute, claudeOpts{})
	// Desktop conversations: one a person started, one a workflow started.
	f.claudeSession(wt, uuid(20), "human in desktop", time.Minute, claudeOpts{entrypoint: "claude-desktop"})
	f.claudeSession(tools, uuid(21), "Review bundle", time.Minute, claudeOpts{entrypoint: "claude-desktop", turnOrigin: "sdk"})

	desktop := "/Users/u/Library/Application Support/Claude/claude-code/2.1/claude.app/Contents/MacOS/claude --output-format stream-json"
	ps := strings.Join([]string{
		"201 ttys001 claude --resume " + uuid(99),
		"202 ttys002 claude",
		"203 ttys003 codex",
		"204 ?? " + desktop,
		"205 ?? " + desktop,
	}, "\n")
	cwds := map[int]string{201: app, 202: web, 203: app, 204: wt, 205: tools}

	oldPS, oldCwd, oldFiles := listProcesses, processCwd, openFiles
	t.Cleanup(func() { listProcesses, processCwd, openFiles = oldPS, oldCwd, oldFiles })
	listProcesses = func() ([]byte, error) { return []byte(ps), nil }
	processCwd = func(pid int) string { return cwds[pid] }
	openFiles = func(pid int) []string {
		if pid != 203 {
			return nil
		}
		return []string{
			"/h/.codex/sessions/2026/01/01/rollout-2026-01-01T10-00-00-019a0000-0000-7000-8000-000000000001.jsonl",
			"/h/.codex/sessions/2026/01/02/rollout-2026-01-02T10-00-00-019a0000-0000-7000-8000-000000000002.jsonl",
			"/usr/lib/libc.dylib",
		}
	}
	externalMu.Lock()
	externalCache = map[int]External{}
	externalMu.Unlock()

	ext := ScanExternal([]string{"claude", "codex"}, nil)
	var got []string
	for _, e := range ext {
		got = append(got, fmt.Sprintf("%d %s %s desktop=%v %s", e.PID, e.Kind, filepath.Base(e.Root), e.Desktop, e.SessionID))
	}
	want := []string{
		"201 claude app desktop=false " + uuid(99),
		"202 claude web desktop=false " + uuid(11),
		"203 codex app desktop=false 019a0000-0000-7000-8000-000000000002",
		"204 claude app desktop=true " + uuid(20), // worktree folds into app
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ScanExternal:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if ext[3].Cwd != wt || ext[3].Where() != "desktop" || ext[0].Where() != "ttys001" {
		t.Errorf("cwd/where = %s %s %s", ext[3].Cwd, ext[3].Where(), ext[0].Where())
	}

	// Exited processes drop out of the cache.
	ps = "201 ttys001 claude --resume " + uuid(99)
	if ext := ScanExternal([]string{"claude", "codex"}, nil); len(ext) != 1 {
		t.Errorf("after exits: %+v", ext)
	}
	externalMu.Lock()
	n := len(externalCache)
	externalMu.Unlock()
	if n != 1 {
		t.Errorf("cache size = %d", n)
	}
}

func TestSessionFromArgs(t *testing.T) {
	cases := []struct {
		kind string
		args []string
		want string
	}{
		{"claude", []string{"--resume=abc", "--model", "x"}, "abc"},
		{"claude", []string{"--session-id=abc"}, "abc"},
		{"claude", []string{"-r", "abc"}, "abc"},
		{"claude", []string{"--resume", "--verbose"}, ""},
		{"claude", []string{"--continue"}, ""},
		{"codex", []string{"resume", "t-1"}, "t-1"},
		{"codex", []string{"resume", "--last"}, ""},
		{"codex", []string{"-c", "x=1"}, ""},
	}
	for _, c := range cases {
		if got := SessionFromArgs(c.kind, c.args); got != c.want {
			t.Errorf("%s %v = %q, want %q", c.kind, c.args, got, c.want)
		}
	}
}

func TestUserText(t *testing.T) {
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	long := strings.Repeat("x", 200)
	cases := []struct {
		in   json.RawMessage
		want string
	}{
		{raw("<system-reminder>\nwt\n</system-reminder>\n\nfix the bug\nmore"), "fix the bug"},
		{raw([]map[string]string{{"type": "input_text", "text": "# AGENTS.md instructions"}}), ""},
		{raw([]map[string]string{{"type": "image", "text": ""}, {"type": "text", "text": "hello"}}), "hello"},
		{raw("<command-name>/model</command-name>"), ""},
		{raw("Caveat: local command output"), ""},
		{raw(long), strings.Repeat("x", 79) + "…"},
		{json.RawMessage(`42`), ""},
	}
	for _, c := range cases {
		if got := userText(c.in); got != c.want {
			t.Errorf("userText(%.40s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUsableDirAndResolveRoot(t *testing.T) {
	f := newFixture(t)
	app := f.project("app")
	if !UsableDir(app) {
		t.Error("project dir should be usable")
	}
	for _, dir := range []string{"", "/", f.home, app + "/missing", filepath.Join(f.home, ".cache")} {
		if UsableDir(dir) {
			t.Errorf("UsableDir(%q) = true", dir)
		}
	}
	tempRoots = []string{"/tmp"}
	if UsableDir("/tmp/x") || UsableDir("/tmp") {
		t.Error("temp roots must be skipped")
	}

	wt := filepath.Join(app, ".claude", "worktrees", "wt", "sub")
	if got := ResolveRoot(wt); got != app {
		t.Errorf("worktree root = %q, want %q", got, app)
	}

	if _, err := exec.LookPath("git"); err == nil {
		repo := f.project("repo")
		_ = exec.Command("git", "init", "-q", repo).Run()
		sub := filepath.Join(repo, "pkg")
		_ = os.MkdirAll(sub, 0o755)
		want, _ := filepath.EvalSymlinks(repo)
		if got := ResolveRoot(sub); got != want {
			t.Errorf("git root = %q, want %q", got, want)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		q, s  string
		ok    bool
		score int
	}{
		{"", "anything", true, 0},
		{"cdn", "cdn-api", true, 0},
		{"API", "cdn-api", true, 4},
		{"cda", "cdn-api", true, 100 + 2},
		{"xyz", "cdn-api", false, 0},
		{"apic", "cdn-api", false, 0}, // order matters
	}
	for _, c := range cases {
		ok, score := FuzzyMatch(c.q, c.s)
		if ok != c.ok || (ok && score != c.score) {
			t.Errorf("FuzzyMatch(%q, %q) = %v, %d; want %v, %d", c.q, c.s, ok, score, c.ok, c.score)
		}
	}
	_, sub := FuzzyMatch("cdn", "my-cdn")
	_, scattered := FuzzyMatch("cdn", "c-d-n")
	if sub >= scattered {
		t.Error("substring should outrank scattered matches")
	}
}

func TestCompleteDir(t *testing.T) {
	f := newFixture(t)
	for _, d := range []string{"code/alpha", "code/Almond", "code/beta", "code/.hidden"} {
		_ = os.MkdirAll(filepath.Join(f.home, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(f.home, "code", "alfile"), nil, 0o644)

	base := func(ps []string) string {
		var out []string
		for _, p := range ps {
			out = append(out, filepath.Base(p))
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for in, want := range map[string]string{
		"~/code/al": "Almond,alpha",
		"~/code/":   "Almond,alpha,beta",
		"~/code/.":  ".hidden",
		"~/nope/x":  "",
		"~":         "code",
	} {
		if got := base(CompleteDir(in)); got != want {
			t.Errorf("CompleteDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTerminateExternal(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait() // reap, or the zombie still answers kill(pid, 0)
	if err := TerminateExternal(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != syscall.ESRCH {
		t.Errorf("process still alive: %v", err)
	}
	// Already gone is fine.
	if err := TerminateExternal(cmd.Process.Pid); err != nil {
		t.Errorf("second terminate: %v", err)
	}
}

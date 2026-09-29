package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/resume/summarize"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/workspace"
)

// inboxPoller feeds hook reports for one background claude agent "bg".
func inboxPoller(m *model) func(sec int, st, msg string) {
	t0 := time.Now().Add(-time.Minute) // so time.Now() is after every poll
	return func(sec int, st, msg string) {
		now := t0.Add(time.Duration(sec) * time.Second)
		m.applyPoll(pollMsg{
			panes:   []tmux.Pane{{ID: "%1", MadID: "bg", Session: tmux.PoolSession, Index: -1}},
			screens: map[string]string{},
			hooks:   map[string]*status.Hook{"bg": {State: st, Message: msg, At: now}},
		}, now)
	}
}

func pending(m *model) []*attention.Item { return m.inbox.Pending(time.Now().Add(time.Hour)) }

func TestInboxWaitingLifecycle(t *testing.T) {
	m, st := setup(t, "/code/api")
	m.cfg.Notify = notify.Config{} // notifications off: the inbox still records
	st.Projects[0].Agents = []*state.Agent{{ID: "bg", Kind: "claude"}}
	m.rebuildRows()
	poll := inboxPoller(m)

	poll(0, status.Running, "")
	poll(10, status.Waiting, "send the data part anyway?")
	poll(11, status.Waiting, "send the data part anyway?") // the same wait again
	if p := pending(m); len(p) != 1 || p[0].Kind != attention.NeedsInput || p[0].Message != "send the data part anyway?" {
		t.Fatalf("want one needs-input item, got %+v", p)
	}

	// Opening the agent is not answering it.
	m.openCmd(st.Projects[0].Agents[0])
	if p := pending(m); len(p) != 1 || !p[0].Seen() {
		t.Fatalf("opened: want the item still open and seen, got %+v", p)
	}

	// A sidebar restart reads the same report again: no second item.
	m2 := newModel(st, agent.Builtin())
	m2.cfg.Notify = notify.Config{}
	inboxPoller(m2)(12, status.Waiting, "send the data part anyway?")
	if p := pending(m2); len(p) != 1 {
		t.Fatalf("after restart: want 1 item, got %d", len(p))
	}

	// The agent reports it went on: answered.
	poll(20, status.Running, "")
	if p := pending(m); len(p) != 0 {
		t.Fatalf("answered: want nothing pending, got %+v", p)
	}
	// A new question with the same words is a new item.
	poll(30, status.Waiting, "send the data part anyway?")
	if p := pending(m); len(p) != 1 || len(m.inbox.Items) != 2 {
		t.Fatalf("second wait: pending %d, items %d", len(p), len(m.inbox.Items))
	}
}

func TestInboxTurnEndedAndSuperseded(t *testing.T) {
	m, st := setup(t, "/code/api")
	st.Projects[0].Agents = []*state.Agent{{ID: "bg", Kind: "claude"}}
	m.rebuildRows()
	poll := inboxPoller(m)
	poll(0, status.Running, "")
	poll(1, status.Running, "")
	poll(2, status.Idle, "") // short, but a hook says so: recorded
	p := pending(m)
	if len(p) != 1 || p[0].Kind != attention.TurnEnded || p[0].Source != attention.FromHook {
		t.Fatalf("want a turn-ended item from the hook, got %+v", p)
	}
	poll(3, status.Running, "")
	if got := m.inbox.Find(p[0].ID); got.State != attention.Superseded {
		t.Errorf("next turn: state %s, want superseded", got.State)
	}
}

func TestInboxKeys(t *testing.T) {
	m, st := setup(t, "/code/api")
	st.Projects[0].Agents = []*state.Agent{{ID: "bg", Kind: "claude"}, {ID: "b2", Kind: "codex"}}
	m.rebuildRows()
	now := time.Now().Add(-time.Minute)
	m.inbox.Raise(attention.Item{Kind: attention.TurnEnded, AgentID: "b2", Project: "/code/api", Source: attention.FromHook}, now)
	m.inbox.Raise(attention.Item{Kind: attention.NeedsInput, AgentID: "bg", Project: "/code/api", Source: attention.FromHook, Message: "ok?"}, now.Add(time.Second))

	press(m, "i")
	if m.mode != modeInbox {
		t.Fatal("i should open the inbox")
	}
	v := m.View()
	if strings.Index(v, "needs input") > strings.Index(v, "turn ended") {
		t.Errorf("blocked items go first:\n%s", v)
	}
	press(m, "s") // snooze the needs-input one
	if n, _ := m.inbox.Count(time.Now()); n != 1 {
		t.Errorf("after snooze: %d showing, want 1", n)
	}
	press(m, "r")
	if n, _ := m.inbox.Count(time.Now()); n != 0 {
		t.Errorf("after resolve: %d showing, want 0", n)
	}
	press(m, "esc")
	if m.mode != modeNormal {
		t.Error("esc should close the inbox")
	}
}

func TestPaletteTargetGone(t *testing.T) {
	m, st := setup(t, "/code/api", "/code/web")
	a := &state.Agent{ID: "a1", Kind: "claude"}
	st.Projects[0].Agents = []*state.Agent{a, {ID: "a2", Kind: "claude"}}
	m.rebuildRows()
	m.selectAgent("a1")

	press(m, ":")
	if m.mode != modePalette || m.pal.tgt.agentID != "a1" {
		t.Fatalf("palette on %+v", m.pal.tgt)
	}
	for _, r := range ">rename" {
		press(m, string(r))
	}
	if len(m.pal.items) == 0 || m.pal.items[0].cmd.id != "rename" {
		t.Fatalf("items %+v", m.pal.items)
	}
	st.RemoveAgent("a1") // gone while the palette was open
	m.rebuildRows()
	press(m, "enter")
	if m.mode == modeRename {
		t.Fatal("renamed something other than the target")
	}
	if !strings.Contains(m.flash, "gone") {
		t.Errorf("flash %q, want the target named gone", m.flash)
	}
}

func TestPaletteSearchAndUnavailable(t *testing.T) {
	m, st := setup(t, "/code/api", "/code/web")
	st.Projects[1].Agents = []*state.Agent{{ID: "w1", Kind: "codex", Name: "alert retry"}}
	m.rebuildRows()
	press(m, ":")
	for _, r := range "alert" {
		press(m, string(r))
	}
	if len(m.pal.items) == 0 || m.pal.items[0].agentID != "w1" {
		t.Fatalf("search: %+v", m.pal.items)
	}
	m.input.SetValue("> template")
	m.refreshPalette()
	if len(m.pal.items) == 0 || m.pal.items[0].cmd.id != "workspace" || !strings.Contains(m.pal.items[0].why, "no templates") {
		t.Fatalf("want workspace unavailable with a reason, got %+v", m.pal.items)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	return dir
}

func TestTemplateRunFailsThenRetries(t *testing.T) {
	m, st := setup(t)
	repo := gitRepo(t)
	st.AddProject(repo)
	m.rebuildRows()
	flag := filepath.Join(t.TempDir(), "ok")
	cfg := `{"version":1,"templates":[{"id":"dev","name":"dev","workspace":{"mode":"new-worktree","default_base":"HEAD"},
	  "setup":[{"id":"one","argv":["true"]},{"id":"two","argv":["test","-e","` + flag + `"]}],"launch":{"agent":"shell"}}]}`
	if err := os.MkdirAll(filepath.Join(repo, ".mad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".mad", "workspaces.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	press(m, "W")
	if m.mode != modeTemplate {
		t.Fatalf("W: mode %v, flash %q", m.mode, m.flash)
	}
	m.tf.branch.SetValue("feat/x")
	press(m, "enter") // preview
	if m.tf.plan == nil || m.tf.plan.BaseSHA == "" || m.tf.plan.Approved != "" {
		t.Fatalf("preview: %+v err %q", m.tf.plan, m.tf.err)
	}
	drive := func(cmd func() any) {
		for cmd != nil {
			msg, ok := cmd().(runMsg)
			if !ok {
				return
			}
			next := m.applyRun(msg)
			cmd = nil
			if _, setting := m.runs[msg.id]; setting && next != nil { // else next starts the agent: tmux
				cmd = func() any { return next() }
			}
		}
	}
	start := m.keyTemplate(key("enter"))
	drive(func() any { return start() })

	p := pending(m)
	if len(p) != 1 || p[0].Kind != attention.SetupFailed || !strings.Contains(p[0].Message, "step two failed") {
		t.Fatalf("want a setup failure, got %+v", p)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude/worktrees/feat-x/.git")); err != nil {
		t.Error("the worktree should be kept after a failure")
	}
	if len(st.Projects[0].Agents) != 0 {
		t.Error("no agent should start after a failure")
	}

	// Fix what failed and retry: step one doesn't run again.
	os.WriteFile(flag, nil, 0o644)
	r, _ := workspace.LoadRun(p[0].RunID)
	oneAt := r.Steps[0].Start
	retry := m.retryRun(p[0])
	drive(func() any { return retry() })
	r, _ = workspace.LoadRun(p[0].RunID)
	if r.State != workspace.Active || !r.Steps[0].Start.Equal(oneAt) || len(st.Projects[0].Agents) != 1 {
		t.Fatalf("retry: state %s, steps %+v, agents %d", r.State, r.Steps, len(st.Projects[0].Agents))
	}
}

func TestWarmScheduling(t *testing.T) {
	m, st := setup(t, "/code/api", "/code/web")
	m.cfg.Brief = summarize.Config{Summarizer: "claude", Projects: []string{"/code/api"}}
	api, web := st.Projects[0], st.Projects[1]
	bg, other := &state.Agent{ID: "bg", Kind: "claude"}, &state.Agent{ID: "w", Kind: "claude"}
	api.Agents, web.Agents = []*state.Agent{bg}, []*state.Agent{other}
	now := time.Now()

	m.scheduleWarm(api, bg, status.Running, status.Idle, now)
	m.scheduleWarm(web, other, status.Running, status.Idle, now) // project not allowed
	if _, ok := m.warmDue["bg"]; !ok || len(m.warmDue) != 1 {
		t.Fatalf("due %v", m.warmDue)
	}
	if m.warmNext(now) != nil {
		t.Error("ran before the turn settled")
	}
	m.scheduleWarm(api, bg, status.Idle, status.Running, now) // it went on
	if len(m.warmDue) != 0 {
		t.Error("a turn that goes on cancels the summary")
	}
	m.stageID = "bg"
	m.scheduleWarm(api, bg, status.Running, status.Idle, now)
	if len(m.warmDue) != 0 {
		t.Error("the agent in front of you needs no summary ahead of time")
	}
	no := false
	m.stageID, m.cfg.Brief.Auto = "", &no
	m.scheduleWarm(api, bg, status.Running, status.Idle, now)
	if len(m.warmDue) != 0 {
		t.Error("auto: false turns it off")
	}
}

func TestWrapTextCJK(t *testing.T) {
	lines := wrapText("代码重写为英文文档（README.md、CONTRIBUTING.md）是否需要打版本 tag？", 20)
	for _, l := range lines {
		if strings.HasPrefix(l, "、") || strings.HasPrefix(l, "？") || strings.HasPrefix(l, "）") {
			t.Errorf("line starts with punctuation: %q in %q", l, lines)
		}
	}
	if len(lines) < 2 {
		t.Errorf("not wrapped: %q", lines)
	}
}

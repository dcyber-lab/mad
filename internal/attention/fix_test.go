package attention

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// A dead host's asks file is nobody's request: not for an idle agent (a
// new host), nor for one waiting in its own TUI, whose own wait shows.
func TestStaleAsksFileIsNobodys(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_ = status.WriteAsks("kim", []status.Ask{{Host: "999999999.x", ID: "toolu_dead", Kind: status.AskPermission, Tool: "Bash", Command: "rm -rf build", At: ago(time.Hour)}})

	idle := agentAt("kim", status.Idle, ago(time.Minute))
	idle.Report = status.ReadReport("kim")
	if v := NewDesk().Look(Facts{Agents: []Agent{idle}, Now: t0}); v.Inbox.Needs != 0 {
		t.Errorf("idle agent shows %v", keys(v.Inbox.Items))
	}

	_ = status.WriteHook("kim", &status.Hook{State: status.Waiting, Message: "Claude needs your permission to use Edit"}, ago(10*time.Second))
	tui := agentAt("kim", status.Waiting, ago(10*time.Second))
	tui.Report = status.ReadReport("kim")
	v := NewDesk().Look(Facts{Agents: []Agent{tui}, Now: t0})
	if len(v.Inbox.Items) != 1 || !strings.HasPrefix(string(v.Inbox.Items[0].Key), "wait:") {
		t.Errorf("its own wait is replaced: %v", keys(v.Inbox.Items))
	}
}

// An item the host reports answered already does not come back after the
// settle, though its file still lists it.
func TestAnsweredElsewhereStaysGone(t *testing.T) {
	fakeHost(t, errors.New("that was answered already"))
	d := NewDesk()
	d.now = func() time.Time { return t0 }
	f := permissionFacts()
	d.Look(f)
	if err := d.Act("req:kim/h1/r1", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Fatalf("act: %v", err)
	}
	d.now = func() time.Time { return t0.Add(10 * settle) }
	if v := d.Look(f); len(v.Inbox.Items) != 1 || v.Inbox.Items[0].Key != "req:lee/h1/q1" {
		t.Errorf("it came back: %v", keys(v.Inbox.Items))
	}
}

// Request ids restart with each host (codex's at 0): the same id of
// another host is another item, and an act names the host it was read from.
func TestRequestKeysNameTheirHost(t *testing.T) {
	sent := fakeHost(t, nil)
	old, now := permissionFacts(), permissionFacts()
	now.Agents[0].Report.Asks[0].Host = "h2"
	d := NewDesk()
	vOld, vNow := d.Look(old), build(now)
	if vOld.Inbox.Items[0].Key == vNow.Inbox.Items[0].Key {
		t.Fatalf("one key %q for two hosts' requests", vOld.Inbox.Items[0].Key)
	}
	d.Look(now)
	if err := d.Act("req:kim/h1/r1", Answer{Verb: VerbAllow}); !errors.Is(err, ErrChanged) {
		t.Errorf("a stale card's act on the new host's request: %v", err)
	}
	if len(*sent) != 0 {
		t.Errorf("sent %+v", *sent)
	}
	if err := d.Act("req:kim/h2/r1", Answer{Verb: VerbAllow}); err != nil {
		t.Fatal(err)
	}
	if got := (*sent)[0].Answer.Instance; got != "h2" {
		t.Errorf("the answer names host %q", got)
	}
}

// The deck started at 9:55 and the window at 10:05 see the same panes,
// screens and hooks: they must give the same inbox and working order.
func TestDeckAndWindowAgree(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	codex := agent.ByName(agent.Builtin(), "codex")
	claude := agent.ByName(agent.Builtin(), "claude")
	waitScreen := "Would you like to run the following command?\n  $ make\n› 1. Yes\n"
	p := &state.Project{Name: "mad", Path: "/code"}
	zed := &state.Agent{ID: "zed", Name: "zed", Kind: "codex"}  // waits since 10:00
	amy := &state.Agent{ID: "amy", Name: "amy", Kind: "codex"}  // waits since 10:01
	rob := &state.Agent{ID: "rob", Name: "rob", Kind: "claude"} // runs since 9:58
	bea := &state.Agent{ID: "bea", Name: "bea", Kind: "claude"} // runs since 10:03
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	pane := func(id string) tmux.Pane { return tmux.Pane{ID: "%" + id} }
	screenOf := func(id string, m int) string {
		if id == "zed" && m >= 0 || id == "amy" && m >= 1 {
			return waitScreen
		}
		return "working " + id + string(rune('a'+m+2))
	}
	observe := func(trs map[string]*status.Tracker, m int) {
		for _, a := range []*state.Agent{zed, amy} {
			trs[a.ID].Observe(codex, pane(a.ID), true, nil, screenOf(a.ID, m), false, at(m))
		}
		trs["rob"].Observe(claude, pane("rob"), true, &status.Hook{State: status.Running, At: at(m)}, "", false, at(m))
		h := &status.Hook{State: status.Idle, At: at(-10)}
		if m >= 3 {
			h = &status.Hook{State: status.Running, At: at(m)}
		}
		trs["bea"].Observe(claude, pane("bea"), true, h, "", false, at(m))
	}
	newTrs := func() map[string]*status.Tracker {
		return map[string]*status.Tracker{"zed": {ID: "zed"}, "amy": {ID: "amy"}, "rob": {ID: "rob"}, "bea": {ID: "bea"}}
	}
	deck, window := newTrs(), newTrs()
	for m := -2; m <= 5; m++ {
		observe(deck, m)
		if m >= 5 {
			observe(window, m)
		}
	}
	view := func(trs map[string]*status.Tracker) View {
		var as []Agent
		for _, a := range []*state.Agent{zed, amy, rob, bea} {
			as = append(as, AgentOf(p, a, trs[a.ID], "%"+a.ID, status.Report{}, transcript.Info{}))
		}
		return NewDesk().Look(Facts{Agents: as, Now: at(5)})
	}
	dv, wv := view(deck), view(window)
	if got, want := keys(wv.Inbox.Items), keys(dv.Inbox.Items); strings.Join(keyStrings(got), " ") != strings.Join(keyStrings(want), " ") {
		t.Errorf("inbox: deck %v, window %v", want, got)
	}
	for i := range dv.Inbox.Items {
		if !dv.Inbox.Items[i].Since.Equal(wv.Inbox.Items[i].Since) {
			t.Errorf("%s since: deck %v, window %v", dv.Inbox.Items[i].Key, dv.Inbox.Items[i].Since, wv.Inbox.Items[i].Since)
		}
	}
	if strings.Join(dv.Working, " ") != strings.Join(wv.Working, " ") {
		t.Errorf("working: deck %v, window %v", dv.Working, wv.Working)
	}
}

func keyStrings(ks []Key) []string {
	var out []string
	for _, k := range ks {
		out = append(out, string(k))
	}
	return out
}

// Equal times are ordered by the deck's agent order, not by key, so the
// same facts list alike wherever the ids sort.
func TestTiesFollowAgentOrder(t *testing.T) {
	a, b := agentAt("zed", status.Waiting, ago(time.Minute)), agentAt("amy", status.Waiting, ago(time.Minute))
	r, w := agentAt("zr", status.Running, ago(time.Minute)), agentAt("ar", status.Running, ago(time.Minute))
	v := build(Facts{Agents: []Agent{a, b, r, w}, Now: t0})
	if got := v.Inbox.Items[0].Agent; got != "zed" {
		t.Errorf("first of equals is %s, want the first agent", got)
	}
	if v.Working[0] != "zr" {
		t.Errorf("working %v", v.Working)
	}
}

func TestEmptyInboxIsAnArray(t *testing.T) {
	for name, v := range map[string]View{"build": build(Facts{Now: t0}), "look": NewDesk().Look(Facts{Now: t0})} {
		b, _ := json.Marshal(v.Inbox)
		if !strings.Contains(string(b), `"items":[]`) {
			t.Errorf("%s: %s", name, b)
		}
	}
	// Everything acted on and hidden is empty too.
	fakeHost(t, nil)
	d := NewDesk()
	d.now = func() time.Time { return t0 }
	f := permissionFacts()
	d.Look(f)
	_ = d.Act("req:kim/h1/r1", Answer{Verb: VerbAllow})
	_ = d.Act("req:lee/h1/q1", Answer{Verb: VerbAnswer, Option: "Red"})
	b, _ := json.Marshal(d.Look(f).Inbox)
	if !strings.Contains(string(b), `"items":[]`) {
		t.Errorf("all hidden: %s", b)
	}
}

// Text, Title and Context are one line each; what Text had past its line
// is kept in Body.
func TestItemTextIsOneLine(t *testing.T) {
	cards := []bus.Card{
		{Kind: bus.CardTask, Column: bus.ColWaitingYou, Title: "pick\na name", Note: "short list:\n- alpha\n- beta",
			Task: task("t-1", "pick a name", "you"), Since: ago(time.Hour)},
		{Kind: bus.CardTask, Column: bus.ColWaitingYou, Title: "other", Note: "one\ntwo",
			Task: task("t-2", "other", "you"), Since: ago(time.Hour)},
	}
	a := agentAt("kim", status.Waiting, ago(time.Minute))
	a.Report.Hook = &status.Hook{State: status.Waiting, Message: "needs\nyou", At: ago(time.Minute)}
	v := build(Facts{Agents: []Agent{a}, Cards: cards, Now: t0})
	for _, it := range v.Inbox.Items {
		for _, s := range []string{it.Title, it.Context, it.Text} {
			if strings.ContainsAny(s, "\n\r\t") {
				t.Errorf("%s: %q spans lines", it.Key, s)
			}
		}
	}
	it, _ := v.Inbox.Find("task:t-1")
	if it.Text != "short list:" || it.Body != "short list:\n- alpha\n- beta" {
		t.Errorf("list note: text %q body %q", it.Text, it.Body)
	}
	it, _ = v.Inbox.Find("task:t-2")
	if it.Text != "one two" || it.Body != "one\ntwo" {
		t.Errorf("plain note: text %q body %q", it.Text, it.Body)
	}
}

// An @you in a DM between two agents is theirs: the person can read it,
// not answer there.
func TestAskInAgentsDM(t *testing.T) {
	d := NewDesk()
	gus, kim := agentAt("gus", status.Running, ago(time.Hour)), agentAt("kim", status.Running, ago(time.Hour))
	card := bus.Card{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Why: bus.WhyAsked, Owner: "gus", OwnerLabel: "gus",
		Channel: "dm.gus.kim", Title: "@you ok to merge?", Message: &bus.CardMessage{Seq: 4}, Since: ago(time.Hour)}
	f := Facts{Agents: []Agent{gus, kim}, Now: t0, Cards: []bus.Card{card}}
	v := d.Look(f)
	it := v.Inbox.Items[0]
	if it.Context != "in gus ↔ kim's DM" || verbs(it) != "open" {
		t.Errorf("context %q acts %q", it.Context, verbs(it))
	}
	if err := d.Act(it.Key, Answer{Verb: VerbReply, Text: "yes"}); !errors.Is(err, ErrNotOffered) {
		t.Errorf("reply: %v", err)
	}
	// The person's own DM is theirs to answer.
	card.Channel = "dm.gus.you"
	it = build(Facts{Agents: f.Agents, Now: t0, Cards: []bus.Card{card}}).Inbox.Items[0]
	if it.Context != "in your DM" || !strings.Contains(verbs(it), "reply") {
		t.Errorf("own DM: %q %q", it.Context, verbs(it))
	}
}

// A reply from a turn that ended before this finish is not this turn's.
func TestStaleReplyNotShown(t *testing.T) {
	a := finishedAt("kim", ago(time.Minute), "fresh")
	a.Report.Turn.At = ago(24 * time.Hour)
	v := build(Facts{Agents: []Agent{a}, Now: t0})
	if got := v.Activity["kim"].Reply; got != "" {
		t.Errorf("activity reply %q", got)
	}
	if got := v.Inbox.Finished()[0].Text; got != "" {
		t.Errorf("finished text %q", got)
	}
	a.Report.Turn.At = ago(time.Minute)
	v = build(Facts{Agents: []Agent{a}, Now: t0})
	if v.Activity["kim"].Reply != "fresh" || v.Inbox.Finished()[0].Text != "fresh" {
		t.Errorf("this turn's reply hidden: %+v", v.Activity["kim"])
	}
}

// An unknown Since is not the longest wait: that agent goes last among
// the working, and its activity line carries no age.
func TestWorkingUnknownSinceLast(t *testing.T) {
	got := working([]Agent{agentAt("u", status.Running, time.Time{}), agentAt("a", status.Running, ago(time.Minute)), agentAt("b", status.Running, ago(time.Hour))})
	if strings.Join(got, " ") != "b a u" {
		t.Errorf("working order %v", got)
	}
	parts := activityOf(agentAt("u", status.Idle, time.Time{})).Parts("Idle", t0)
	if len(parts) != 1 || parts[0] != "Idle" {
		t.Errorf("unknown since shows an age: %q", parts)
	}
}

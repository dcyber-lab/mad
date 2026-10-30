package attention

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/transcript"
)

var t0 = time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return t0.Add(-d) }

// agentAt is an agent as a poll would pass it.
func agentAt(id, raw string, since time.Time) Agent {
	return Agent{ID: id, Label: id, Project: "mad", Dir: "/code/" + id, PaneID: "%" + id, Status: raw,
		Shown: status.Shown(raw, false), Since: since}
}

func finishedAt(id string, since time.Time, reply string) Agent {
	a := agentAt(id, status.Idle, since)
	a.Shown = status.Finished
	a.Report.Turn = &status.Turn{Reply: reply, At: since}
	return a
}

func task(id, title, owner string) *bus.Task { return &bus.Task{ID: id, Title: title, Owner: owner} }

func keys(items []Item) []Key {
	var out []Key
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

func verbs(it Item) string {
	var out []string
	for _, a := range it.Acts {
		s := string(a.Verb)
		if a.Option != "" {
			s += "=" + a.Option
		}
		if a.Input {
			s += "…"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// One row per agent wait form: what the inbox makes of it.
func TestAgentWaits(t *testing.T) {
	hookAt := ago(3 * time.Minute)
	for _, c := range []struct {
		name  string
		a     Agent
		key   Key
		kind  Kind
		words string // Context | Text | Body
		acts  string
		mono  bool
	}{
		{
			name: "host permission, from the asks file",
			a: func() Agent {
				a := agentAt("kim", status.Waiting, ago(2*time.Minute))
				a.Report.Asks = []status.Ask{{ID: "r1", Kind: status.AskPermission, Tool: "Bash", Target: "Run the tests",
					Command: "go test ./...", Always: true, At: ago(time.Minute)}}
				return a
			}(),
			key: "req:kim/r1", kind: KindPermission, words: "Bash|go test ./...|Run the tests",
			acts: "allow always deny deny…", mono: true,
		},
		{
			name: "host question, one tap",
			a: func() Agent {
				a := agentAt("lee", status.Waiting, ago(5*time.Minute))
				a.Report.Asks = []status.Ask{{ID: "7", Kind: status.AskQuestion, Question: "main or release?",
					Options: []string{"main", "release/1.4"}, OneTap: true, At: ago(5 * time.Minute)}}
				return a
			}(),
			key: "req:lee/7", kind: KindQuestion, words: "asks|main or release?|",
			acts: "answer=main answer=release/1.4 answer…",
		},
		{
			name: "host questions, several: its page has the form",
			a: func() Agent {
				a := agentAt("lee", status.Waiting, ago(5*time.Minute))
				a.Report.Asks = []status.Ask{{ID: "8", Kind: status.AskQuestion, Question: "first?", At: ago(5 * time.Minute)}}
				return a
			}(),
			key: "req:lee/8", kind: KindQuestion, words: "asks|first?|", acts: "",
		},
		{
			name: "host plan",
			a: func() Agent {
				a := agentAt("pat", status.Waiting, ago(time.Minute))
				a.Report.Asks = []status.Ask{{ID: "p", Kind: status.AskPlan, Plan: "Split the parser", At: ago(time.Minute)}}
				return a
			}(),
			key: "req:pat/p", kind: KindPermission, words: "plan|Split the parser|", acts: "allow deny…",
		},
		{
			name: "hook wait, no task: claim 2's agent",
			a: func() Agent {
				a := agentAt("wes", status.Waiting, hookAt)
				a.Report.Hook = &status.Hook{State: status.Waiting, Message: "Claude needs your permission to use Write", At: hookAt}
				a.Info = transcript.Info{Tool: "Write · 0042_users.sql"}
				return a
			}(),
			key: Key(fmt.Sprintf("wait:wes/%d", hookAt.UnixMilli())), kind: KindPermission,
			words: "Write|0042_users.sql|Claude needs your permission to use Write", acts: "open", mono: true,
		},
		{
			name: "hook wait on a question",
			a: func() Agent {
				a := agentAt("ana", status.Waiting, hookAt)
				a.Report.Hook = &status.Hook{State: status.Waiting, Message: "Claude is waiting for your input", At: hookAt}
				a.Info = transcript.Info{Tool: "AskUserQuestion"}
				return a
			}(),
			key: Key(fmt.Sprintf("wait:ana/%d", hookAt.UnixMilli())), kind: KindQuestion,
			words: "asks|Claude is waiting for your input|", acts: "open",
		},
		{
			name: "screen wait, no hooks",
			a:    agentAt("cod", status.Waiting, ago(time.Minute)),
			key:  "wait:cod/0", kind: KindPermission, words: "needs you||", acts: "open",
		},
		{
			name: "an asks file left by a host that died is not a wait",
			a: func() Agent {
				a := agentAt("old", status.Stopped, ago(time.Hour))
				a.Report.Asks = []status.Ask{{ID: "r9", Kind: status.AskPermission, Tool: "Bash"}}
				return a
			}(),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := build(Facts{Agents: []Agent{c.a}, Now: t0})
			if c.key == "" {
				if len(v.Inbox.Items) != 0 {
					t.Fatalf("items %v", keys(v.Inbox.Items))
				}
				return
			}
			if v.Inbox.Needs != 1 || len(v.Inbox.Items) != 1 {
				t.Fatalf("inbox %+v", v.Inbox)
			}
			it := v.Inbox.Items[0]
			if it.Key != c.key || it.Kind != c.kind || !it.Blocks || it.Agent != c.a.ID || it.Open.Agent != c.a.ID {
				t.Errorf("item %+v", it)
			}
			if got := it.Context + "|" + it.Text + "|" + it.Body; got != c.words {
				t.Errorf("words %q, want %q", got, c.words)
			}
			if got := verbs(it); got != c.acts {
				t.Errorf("acts %q, want %q", got, c.acts)
			}
			if it.Mono != c.mono {
				t.Errorf("mono %v", it.Mono)
			}
			if it.Kind == KindPermission && c.a.Report.Asks == nil && it.Where != c.a.Dir {
				t.Errorf("where %q", it.Where)
			}
		})
	}
}

// One row per waiting-you card kind and why.
func TestCards(t *testing.T) {
	waitingKim := agentAt("kim", status.Waiting, ago(time.Minute))
	idleKim := agentAt("kim", status.Idle, ago(time.Minute))
	msg := &bus.CardMessage{Seq: 12, Text: "send the export's location\nor enable the task tools"}
	for _, c := range []struct {
		name   string
		agents []Agent
		cards  []bus.Card
		want   string // key kind context|text|body tasks, per item
	}{
		{
			name: "ask",
			cards: []bus.Card{{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Why: bus.WhyAsked, Owner: "gus", OwnerLabel: "gus",
				Channel: "plan", Title: "send the export's location", Message: &bus.CardMessage{Seq: 12, Text: "or enable the task tools"}, Since: ago(time.Hour)}},
			want: "msg:plan#12 ask in #plan|send the export's location|or enable the task tools []",
		},
		{
			name: "ask card and the task carrying the same message collapse",
			cards: []bus.Card{
				{Kind: bus.CardTask, Task: task("t-1", "Parse the CSV", "gus"), Column: bus.ColWaitingYou, Why: bus.WhyAsked, Owner: "gus",
					OwnerLabel: "gus", Channel: "plan", Title: "Parse the CSV", Message: msg, Since: ago(time.Hour)},
				{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Why: bus.WhyAsked, Owner: "gus", OwnerLabel: "gus",
					Channel: "plan", Title: "send the export's location", Message: &bus.CardMessage{Seq: 12, Text: "or enable the task tools"}, Since: ago(2 * time.Hour)},
			},
			want: "msg:plan#12 ask in #plan|send the export's location|or enable the task tools [t-1]",
		},
		{
			name: "an ask in the person's DM",
			cards: []bus.Card{{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Why: bus.WhyAsked, Owner: "gus", OwnerLabel: "gus",
				Channel: "dm.gus.you", Title: "a word?", Message: &bus.CardMessage{Seq: 3}, Since: ago(time.Hour)}},
			want: "msg:dm.gus.you#3 ask in your DM|a word?| []",
		},
		{
			name: "paused, with its task",
			cards: []bus.Card{
				{Kind: bus.CardPaused, Column: bus.ColWaitingYou, Why: bus.WhyPaused, Channel: "ops", Title: "#ops is paused", Since: ago(time.Hour)},
				{Kind: bus.CardTask, Task: task("t-2", "Deploy", "kim"), Column: bus.ColWaitingYou, Why: bus.WhyPaused, Owner: "kim", Channel: "ops", Title: "Deploy", Since: ago(time.Hour)},
			},
			want: "paused:ops paused paused|its agents' messages wait for you| [t-2]",
		},
		{
			name: "check-in",
			cards: []bus.Card{{Kind: bus.CardCheckin, Column: bus.ColWaitingYou, Why: bus.WhyCheckin, Owner: "kim", OwnerLabel: "kim",
				Title: "kim's check-in: look at the build", Note: "kim is stopped", Ref: "tm-1", Since: ago(time.Hour)}},
			want: "checkin:tm-1 checkin check-in|look at the build|kim is stopped []",
		},
		{
			name:   "permission task folds into its waiting owner's item",
			agents: []Agent{waitingKim},
			cards: []bus.Card{{Kind: bus.CardTask, Task: task("t-3", "Run the bus tests", "kim"), Column: bus.ColWaitingYou,
				Why: bus.WhyPermission, Owner: "kim", Title: "Run the bus tests", Since: ago(time.Hour)}},
			want: "wait:kim/0 permission needs you|| [t-3]",
		},
		{
			name:   "permission task of an owner no longer waiting: the card is stale",
			agents: []Agent{idleKim},
			cards: []bus.Card{{Kind: bus.CardTask, Task: task("t-3", "Run the bus tests", "kim"), Column: bus.ColWaitingYou,
				Why: bus.WhyPermission, Owner: "kim", Title: "Run the bus tests", Since: ago(time.Hour)}},
			want: "",
		},
		{
			name: "permission task of an owner gone from the deck",
			cards: []bus.Card{{Kind: bus.CardTask, Task: task("t-4", "Old work", "gone"), Column: bus.ColWaitingYou,
				Why: bus.WhyPermission, Owner: "gone", Title: "Old work", Since: ago(time.Hour)}},
			want: "task:t-4 task permission|| [t-4]",
		},
		{
			name: "the person's task, and an unowned one",
			cards: []bus.Card{
				{Kind: bus.CardTask, Task: task("t-5", "Decide the schema", bus.You), Column: bus.ColWaitingYou, Owner: bus.You, Title: "Decide the schema", Since: ago(2 * time.Hour)},
				{Kind: bus.CardTask, Task: task("t-6", "Write docs", ""), Column: bus.ColWaitingYou, Why: bus.WhyUnowned, Title: "Write docs", Note: "lead: kim", Since: ago(time.Hour)},
			},
			want: "task:t-5 task yours|| [t-5]; task:t-6 task unowned|lead: kim| [t-6]",
		},
		{
			name:  "a card kind the inbox does not know still shows",
			cards: []bus.Card{{Kind: "novel", Column: bus.ColWaitingYou, Why: "new", Title: "Something new", Since: ago(time.Hour)}},
			want:  "card:novel::Something new task new|| []",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			cards := append(c.cards, bus.Card{Kind: bus.CardTask, Task: task("t-9", "Doing", "kim"), Column: bus.ColDoing, Title: "Doing"})
			v := build(Facts{Agents: c.agents, Cards: cards, Now: t0})
			var got []string
			for _, it := range v.Inbox.Items {
				var ts []string
				for _, x := range it.Tasks {
					ts = append(ts, x.ID)
				}
				got = append(got, fmt.Sprintf("%s %s %s|%s|%s [%s]", it.Key, it.Kind, it.Context, it.Text, it.Body, strings.Join(ts, " ")))
			}
			if g := strings.Join(got, "; "); g != c.want {
				t.Errorf("items\n got %s\nwant %s", g, c.want)
			}
			if len(v.Board) != 1 || v.Board[0].Column != bus.ColDoing {
				t.Errorf("board %+v", v.Board)
			}
			for _, card := range v.Board {
				if card.Column == bus.ColWaitingYou {
					t.Errorf("waiting-you on the board: %+v", card)
				}
			}
		})
	}
}

func TestSkillAndBudget(t *testing.T) {
	sk := skill.Pending{Name: "csv-export", Description: "When parsing a CSV export", By: "gus", At: ago(time.Hour)}
	for _, c := range []struct {
		name  string
		spend bus.SpendState
		want  string
	}{
		{"under the bar", bus.SpendState{Day: "2026-10-31", USD: 5, BudgetUSD: 25, Part: 0.2}, ""},
		{"near", bus.SpendState{Day: "2026-10-31", USD: 21.5, BudgetUSD: 25, Part: 0.86, Hot: true}, "budget:2026-10-31/near 86% spent false"},
		{"spent", bus.SpendState{Day: "2026-10-31", USD: 25, BudgetUSD: 25, Part: 1, Hot: true, Closed: true, ClosedAt: ago(10 * time.Minute)},
			"budget:2026-10-31/spent spent · agents wait true"},
		{"lifted for today", bus.SpendState{Day: "2026-10-31", USD: 30, BudgetUSD: 25, Part: 1.2, Hot: true, Opened: true}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := build(Facts{Skills: []skill.Pending{sk}, Spend: c.spend, Now: t0})
			var budget []string
			for _, it := range v.Inbox.Items {
				if it.Kind == KindBudget {
					budget = append(budget, fmt.Sprintf("%s %s %v", it.Key, it.Context, it.Blocks))
					if it.Part == nil || *it.Part != c.spend.Part || verbs(it) != "raise=10 lift open" {
						t.Errorf("budget item %+v", it)
					}
				}
			}
			if got := strings.Join(budget, ";"); got != c.want {
				t.Errorf("budget %q, want %q", got, c.want)
			}
			s, ok := v.Inbox.Find("skill:csv-export")
			if !ok || s.Title != "/csv-export" || s.Context != "skill from gus" || s.Open.Page != PageSkills || verbs(s) != "approve discard open" {
				t.Errorf("skill item %+v", s)
			}
		})
	}
}

// Blocking first, then oldest first; finished unseen after, newest first;
// working longest first.
func TestOrder(t *testing.T) {
	wait := agentAt("w", status.Waiting, ago(time.Minute)) // blocks, newest
	run1 := agentAt("r1", status.Running, ago(2*time.Minute))
	run2 := agentAt("r2", status.Running, ago(9*time.Minute))
	f := Facts{
		Agents: []Agent{finishedAt("f1", ago(20*time.Minute), "old"), wait, run1, finishedAt("f2", ago(3*time.Minute), "Tests pass\nmore"), run2},
		Cards: []bus.Card{{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Owner: "gus", Channel: "plan", Title: "q",
			Message: &bus.CardMessage{Seq: 1}, Since: ago(2 * time.Hour)}},
		Skills: []skill.Pending{{Name: "s", At: ago(time.Hour)}},
		Now:    t0,
	}
	v := build(f)
	want := []Key{"wait:w/0", "msg:plan#1", "skill:s", keyFinished("f2", ago(3*time.Minute)), keyFinished("f1", ago(20*time.Minute))}
	if got := keys(v.Inbox.Items); !reflect.DeepEqual(got, want) || v.Inbox.Needs != 3 {
		t.Errorf("order %v (needs %d), want %v", got, v.Inbox.Needs, want)
	}
	if fin := v.Inbox.Finished(); len(fin) != 2 || fin[0].Text != "Tests pass" || verbs(fin[0]) != "open seen" {
		t.Errorf("finished %+v", fin)
	}
	if !reflect.DeepEqual(v.Working, []string{"r2", "r1"}) {
		t.Errorf("working %v", v.Working)
	}
	// Next walks the same order and wraps.
	if it, _ := v.Inbox.After(""); it.Key != want[0] {
		t.Errorf("first %s", it.Key)
	}
	if it, _ := v.Inbox.After(want[4]); it.Key != want[0] {
		t.Errorf("wrap %s", it.Key)
	}
	if it, _ := v.Inbox.After(want[1]); it.Key != want[2] {
		t.Errorf("after %s", it.Key)
	}
	if _, ok := (Inbox{}).After(""); ok {
		t.Error("empty inbox has a next")
	}
}

// The activity line: the call only while running or waiting, the reply
// only once finished.
func TestActivity(t *testing.T) {
	info := transcript.Info{Tool: "Bash · go test ./...", Todo: &discover.Todo{Done: 2, Total: 5, Now: "Sort"}}
	for _, c := range []struct {
		raw, shown string
		want       string
		parts      string
	}{
		{status.Running, status.Running, "Bash|go test ./...|2/5|", "Bash|go test ./...|6m|2/5"},
		{status.Waiting, status.Waiting, "Bash|go test ./...|2/5|", "Bash|go test ./...|6m|2/5"},
		{status.Idle, status.Finished, "|||Tests pass", "Finished 6m ago|Tests pass"},
		{status.Idle, status.Idle, "|||", "Idle 6m"},
	} {
		a := agentAt("a", c.raw, ago(6*time.Minute))
		a.Shown, a.Info = c.shown, info
		a.Report.Turn = &status.Turn{Reply: "Tests pass\nand more"}
		act := activityOf(a)
		todo := ""
		if act.Todo != nil {
			todo = fmt.Sprintf("%d/%d", act.Todo.Done, act.Todo.Total)
		}
		if got := act.Tool + "|" + act.Target + "|" + todo + "|" + act.Reply; got != c.want {
			t.Errorf("%s: %q, want %q", c.shown, got, c.want)
		}
		if got := strings.Join(act.Parts(status.LookOf(c.shown).Word, t0), "|"); got != c.parts {
			t.Errorf("%s parts: %q, want %q", c.shown, got, c.parts)
		}
		if !act.Since.Equal(ago(6 * time.Minute)) {
			t.Errorf("since %v", act.Since)
		}
	}
	// A list all done is not shown.
	a := agentAt("a", status.Running, t0)
	a.Info = transcript.Info{Tool: "Edit", Todo: &discover.Todo{Done: 3, Total: 3}}
	if act := activityOf(a); act.Todo != nil || act.Tool != "Edit" || act.Target != "" {
		t.Errorf("all done: %+v", act)
	}
}

// fakeHost records what reached the host.
func fakeHost(t *testing.T, err error) *[]host.Command {
	t.Helper()
	var got []host.Command
	old := hostDo
	hostDo = func(id string, c host.Command) error {
		got = append(got, c)
		return err
	}
	t.Cleanup(func() { hostDo = old })
	return &got
}

func permissionFacts() Facts {
	a := agentAt("kim", status.Waiting, ago(time.Minute))
	a.Report.Asks = []status.Ask{{ID: "r1", Kind: status.AskPermission, Tool: "Bash", Command: "ls", At: ago(time.Minute)}}
	q := agentAt("lee", status.Waiting, ago(time.Minute))
	q.Report.Asks = []status.Ask{{ID: "q1", Kind: status.AskQuestion, Question: "Which?", Options: []string{"Blue", "Red"}, OneTap: true, At: ago(time.Minute)}}
	return Facts{Agents: []Agent{a, q}, Now: t0}
}

func TestDeskActOnce(t *testing.T) {
	sent := fakeHost(t, nil)
	d := NewDesk()
	d.now = func() time.Time { return t0 }
	f := permissionFacts()
	d.Look(f)
	if err := d.Act("req:kim/r1", Answer{Verb: VerbAllow}); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0].Answer.ID != "r1" || !(*sent)[0].Answer.Allow {
		t.Errorf("sent %+v", *sent)
	}
	if err := d.Act("req:kim/r1", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Errorf("second act: %v", err)
	}
	// Hidden while it settles, though the facts still have it.
	if v := d.Look(f); len(v.Inbox.Items) != 1 || v.Inbox.Needs != 1 || v.Inbox.Items[0].Key != "req:lee/q1" {
		t.Errorf("after acting %v", keys(v.Inbox.Items))
	}
	// Still there past settle: the act did not take, it shows again.
	d.now = func() time.Time { return t0.Add(settle) }
	if v := d.Look(f); len(v.Inbox.Items) != 2 {
		t.Errorf("after settle %v", keys(v.Inbox.Items))
	}
	// An answer by option, and one of one's own.
	if err := d.Act("req:lee/q1", Answer{Verb: VerbAnswer, Option: "Red"}); err != nil {
		t.Fatal(err)
	}
	if a := (*sent)[1].Answer; !a.Allow || a.Answers["Which?"] != "Red" {
		t.Errorf("answer %+v", a)
	}
	if err := d.Act("nope", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Errorf("unknown key: %v", err)
	}
}

func TestDeskRefusesWhatIsNotOffered(t *testing.T) {
	sent := fakeHost(t, nil)
	d := NewDesk()
	f := permissionFacts()
	f.Agents[0].Report.Asks[0].Always = false
	d.Look(f)
	for _, a := range []Answer{
		{Verb: VerbOpen},
		{Verb: VerbAlways},                // not offered without Always
		{Verb: VerbApprove},               // another kind's
		{Verb: VerbAllow, Option: "x"},    // no such option
		{Verb: VerbDeny, Text: "   "},     // words, but none
		{Verb: VerbAllow, Text: "please"}, // allow takes no words
	} {
		if err := d.Act("req:kim/r1", a); !errors.Is(err, ErrNotOffered) {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if err := d.Act("req:lee/q1", Answer{Verb: VerbAnswer, Option: "Green"}); !errors.Is(err, ErrNotOffered) {
		t.Errorf("an option not offered: %v", err)
	}
	if len(*sent) != 0 {
		t.Errorf("sent %+v", *sent)
	}
	if err := d.Act("req:kim/r1", Answer{Verb: VerbDeny, Text: "use make test"}); err != nil {
		t.Errorf("deny with words: %v", err)
	}
	if a := (*sent)[0].Answer; a.Allow || a.Message != "use make test" {
		t.Errorf("deny went as %+v", a)
	}
}

func TestDeskRestoresOnFailure(t *testing.T) {
	fakeHost(t, errors.New("the host went away"))
	d := NewDesk()
	f := permissionFacts()
	d.Look(f)
	if err := d.Act("req:kim/r1", Answer{Verb: VerbAllow}); err == nil || errors.Is(err, ErrGone) {
		t.Fatalf("act: %v", err)
	}
	if v := d.Look(f); len(v.Inbox.Items) != 2 {
		t.Errorf("failed act hid it: %v", keys(v.Inbox.Items))
	}
	// The host says it was answered (in its TUI, on another surface): gone.
	fakeHost(t, errors.New("that was answered already"))
	if err := d.Act("req:kim/r1", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Errorf("answered elsewhere: %v", err)
	}
}

// An item without a time of its own keeps the first look's.
func TestDeskKeepsFirstSeen(t *testing.T) {
	d := NewDesk()
	near := bus.SpendState{Day: "2026-10-31", Part: 0.9, Hot: true}
	v := d.Look(Facts{Spend: near, Now: t0})
	v2 := d.Look(Facts{Spend: near, Now: t0.Add(time.Minute)})
	if !v.Inbox.Items[0].Since.Equal(t0) || !v2.Inbox.Items[0].Since.Equal(t0) {
		t.Errorf("since %v then %v", v.Inbox.Items[0].Since, v2.Inbox.Items[0].Since)
	}
}

// Every route acts through its backend.
func TestRoutes(t *testing.T) {
	var did []string
	note := func(s string) { did = append(did, s) }
	restore := []func(){}
	swap := func(set, reset func()) { set(); restore = append(restore, reset) }
	oReply, oApprove, oDiscard, oRaise, oLift, oFire, oDrop, oResume, oSeen := busReply, approve, discard, spendRaise, spendLift, fireTimer, dropTimer, resume, markSeen
	swap(func() {
		busReply = func(ch, text string, seq int) (bus.PostResult, error) {
			note(fmt.Sprintf("reply %s#%d %s", ch, seq, text))
			return bus.PostResult{}, nil
		}
		approve = func(slug string, _ *string) error { note("approve " + slug); return nil }
		discard = func(slug string) error { note("discard " + slug); return nil }
		spendRaise = func(usd float64, _ time.Time) error { note(fmt.Sprintf("raise %v", usd)); return nil }
		spendLift = func(time.Time) error { note("lift"); return nil }
		fireTimer = func(id string, _ time.Time, st map[string]string) error {
			note("fire " + id + " " + st["kim"])
			return nil
		}
		dropTimer = func(id, by string) error { note("drop " + id + " " + by); return nil }
		resume = func(ch string) error { note("resume " + ch); return nil }
		markSeen = func(pane string, _ time.Time) error { note("seen " + pane); return nil }
	}, func() {
		busReply, approve, discard, spendRaise, spendLift, fireTimer, dropTimer, resume, markSeen = oReply, oApprove, oDiscard, oRaise, oLift, oFire, oDrop, oResume, oSeen
	})
	defer func() {
		for _, r := range restore {
			r()
		}
	}()
	f := Facts{
		Agents: []Agent{agentAt("kim", status.Stopped, t0), finishedAt("hana", ago(time.Minute), "done")},
		Cards: []bus.Card{
			{Kind: bus.CardAsk, Column: bus.ColWaitingYou, Owner: "gus", Channel: "plan", Title: "q", Message: &bus.CardMessage{Seq: 4}, Since: ago(time.Hour)},
			{Kind: bus.CardPaused, Column: bus.ColWaitingYou, Channel: "ops", Since: ago(time.Hour)},
			{Kind: bus.CardCheckin, Column: bus.ColWaitingYou, Owner: "kim", OwnerLabel: "kim", Ref: "tm-1", Since: ago(time.Hour)},
		},
		Skills: []skill.Pending{{Name: "a", At: ago(time.Hour)}, {Name: "b", At: ago(time.Hour)}},
		Spend:  bus.SpendState{Day: "d", Part: 0.9, Hot: true},
		Now:    t0,
	}
	d := NewDesk()
	v := d.Look(f)
	for _, c := range []struct {
		key Key
		a   Answer
	}{
		{"msg:plan#4", Answer{Verb: VerbReply, Text: "here it is"}},
		{"paused:ops", Answer{Verb: VerbResume}},
		{"checkin:tm-1", Answer{Verb: VerbSend}},
		{"skill:a", Answer{Verb: VerbApprove}},
		{"skill:b", Answer{Verb: VerbDiscard}},
		{"budget:d/near", Answer{Verb: VerbRaise, Option: "10"}},
		{keyFinished("hana", ago(time.Minute)), Answer{Verb: VerbSeen}},
	} {
		if err := d.Act(c.key, c.a); err != nil {
			t.Errorf("%s %s: %v", c.key, c.a.Verb, err)
		}
	}
	want := "reply plan#4 here it is|resume ops|fire tm-1 stopped|approve a|discard b|raise 10|seen %hana"
	if got := strings.Join(did, "|"); got != want {
		t.Errorf("did\n got %s\nwant %s\n(items %v)", got, want, keys(v.Inbox.Items))
	}
}

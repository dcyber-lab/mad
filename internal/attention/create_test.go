package attention

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/create"
)

// An agent's request to start an agent or a team is a blocking item with
// the whole spec for its card, answered with Allow or Deny.
func TestCreateItems(t *testing.T) {
	at := t0.Add(-time.Minute)
	agentReq := create.Request{ID: "c-0000000a", By: "gus", ByID: "gus", At: at, Status: create.Waiting,
		Spec: create.Spec{Type: create.TypeAgent, Name: "scout", Project: "/code/a", ProjectName: "a", Kind: "claude", Model: "sonnet", Message: "find the flaky test\nstart in handler_test.go", Channel: "api"}}
	teamReq := create.Request{ID: "c-0000000b", By: "gus", ByID: "gus", At: at.Add(time.Second), Status: create.Waiting,
		Spec: create.Spec{Type: create.TypeTeam, Name: "fix-flaky", Project: "/code/a", ProjectName: "a", Goal: "stop the flake", Template: "dev", Kickoff: "Go: {{goal}}",
			Roles: []create.Role{{Role: "fixer", Kind: "claude", Model: "opus", Lead: true, Brief: "lead it"}, {Role: "checker", Kind: "codex", ReadOnly: true}}}}
	v := build(Facts{Agents: []Agent{agentAt("gus", "idle", at)}, Creates: []create.Request{agentReq, teamReq}, Now: t0})

	a, ok := v.Inbox.Find("create:c-0000000a")
	if !ok || a.Kind != KindCreate || a.Agent != "gus" || a.Title != "gus" || a.Context != "wants to start an agent" || !a.Blocks ||
		a.Text != "scout (claude:sonnet) in a" || a.Open.Agent != "gus" || verbs(a) != "allow deny deny…" {
		t.Fatalf("agent item %+v", a)
	}
	if a.Create == nil || a.Create.Name != "scout" || a.Create.Message != agentReq.Spec.Message || a.Create.Channel != "api" || a.Create.ProjectName != "a" {
		t.Errorf("agent item's spec %+v", a.Create)
	}
	if a.Body != "First message: find the flaky test\nstart in handler_test.go\n\nJoins #api" || !a.Full || a.Where != "/code/a" {
		t.Errorf("body %q", a.Body)
	}
	for _, act := range a.Acts {
		if act.Kind != KindNew || act.Verb == VerbAllow && act.Row || act.Verb == VerbDeny && !act.Row && !act.Input {
			t.Errorf("act %+v", act)
		}
	}
	tm, ok := v.Inbox.Find("create:c-0000000b")
	if !ok || tm.Context != "wants to start a team" || tm.Text != "Team fix-flaky in a" || tm.Create == nil || len(tm.Create.Roles) != 2 ||
		!tm.Full || tm.Body != "Goal: stop the flake\nTemplate: dev\nfixer: claude:opus (lead)\n  lead it\nchecker: codex (read only)\nKickoff: Go: stop the flake" {
		t.Fatalf("team item %+v", tm)
	}
	if v.Inbox.Needs != 2 {
		t.Errorf("needs %d", v.Inbox.Needs)
	}
	// An asker no longer in the deck: the item has no agent to draw.
	v = build(Facts{Creates: []create.Request{agentReq}, Now: t0})
	if it, _ := v.Inbox.Find("create:c-0000000a"); it.Agent != "" || it.Title != "gus" {
		t.Errorf("without its agent: %+v", it)
	}
}

func TestCreateActs(t *testing.T) {
	var got []string
	old, gone := answerCreate, false
	answerCreate = func(id string, allow bool, reply string) error {
		if gone {
			return create.ErrGone
		}
		got = append(got, id)
		switch {
		case allow:
			got = append(got, "allow")
		case reply != "":
			got = append(got, "changes:"+reply)
		default:
			got = append(got, "deny")
		}
		return nil
	}
	t.Cleanup(func() { answerCreate = old })
	f := Facts{Creates: []create.Request{{ID: "c-0000000a", By: "gus", At: t0.Add(-time.Minute), Status: create.Waiting, Spec: create.Spec{Type: create.TypeAgent, Name: "scout", Kind: "claude"}}}, Now: t0}
	d := NewDesk()
	d.Look(f)
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbAnswer, Option: "x"}); !errors.Is(err, ErrNotOffered) {
		t.Errorf("answer: %v", err)
	}
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbAllow}); err != nil || len(got) != 2 || got[0] != "c-0000000a" || got[1] != "allow" {
		t.Errorf("allow: %v %v", err, got)
	}
	d = NewDesk()
	d.Look(f)
	got = nil
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbDeny}); err != nil || got[1] != "deny" {
		t.Errorf("deny: %v %v", err, got)
	}
	d = NewDesk()
	d.Look(f)
	got = nil
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbDeny, Text: "use codex"}); err != nil || got[1] != "changes:use codex" {
		t.Errorf("deny with words: %v %v", err, got)
	}
	d = NewDesk()
	d.Look(f)
	gone = true
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Errorf("answered already: %v", err)
	}
}

// A request that ended is not in the inbox: it is in View.Creates with its
// outcome, newest first, and keeps the channel it came from.
func TestCreateOutcomes(t *testing.T) {
	spec := create.Spec{Type: create.TypeAgent, Name: "scout", ProjectName: "a", Kind: "claude", Message: "hi"}
	mk := func(id, status string, ago time.Duration, o create.Result) create.Request {
		return create.Request{ID: id, By: "gus", ByID: "gus", At: t0.Add(-ago), From: "api", Status: status, Result: o, DoneAt: t0.Add(-ago + time.Second), Spec: spec}
	}
	v := build(Facts{Agents: []Agent{agentAt("gus", "idle", t0.Add(-time.Hour))}, Now: t0, Creates: []create.Request{
		mk("c-00000001", create.Created, 3*time.Hour, create.Result{Agent: "id-scout", Channel: "dm.x"}),
		mk("c-00000002", create.Declined, time.Hour, create.Result{}),
		mk("c-00000003", create.Failed, 2*time.Hour, create.Result{Reason: "not a directory"}),
		mk("c-00000004", create.Allowed, time.Minute, create.Result{}),
		mk("c-00000005", create.Waiting, 30*time.Second, create.Result{}),
	}})
	if v.Inbox.Needs != 1 || len(v.Inbox.Items) != 1 || v.Inbox.Items[0].From != "api" || v.Inbox.Items[0].Outcome != nil {
		t.Fatalf("inbox %+v", v.Inbox)
	}
	var got []string
	for _, it := range v.Creates {
		if it.Outcome == nil || it.Blocks || len(it.Acts) != 0 || it.From != "api" || it.Create == nil || it.Create.Name != "scout" {
			t.Errorf("item %+v", it)
		}
		got = append(got, string(it.Key)+" "+it.Outcome.Status)
	}
	want := []string{"create:c-00000004 creating", "create:c-00000002 declined", "create:c-00000003 failed", "create:c-00000001 created"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("creates %v, want %v", got, want)
	}
	if it := v.Creates[3]; it.Outcome.Agent != "id-scout" || it.Outcome.Channel != "dm.x" || v.Creates[2].Outcome.Reason != "not a directory" {
		t.Errorf("results %+v %+v", it.Outcome, v.Creates[2].Outcome)
	}
}

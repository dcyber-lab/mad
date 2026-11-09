package attention

import (
	"errors"
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
		Spec: create.Spec{Type: create.TypeTeam, Name: "fix-flaky", Project: "/code/a", ProjectName: "a", Goal: "stop the flake",
			Roles: []create.Role{{Role: "fixer", Kind: "claude", Model: "opus", Lead: true}, {Role: "checker", Kind: "codex"}}}}
	v := build(Facts{Agents: []Agent{agentAt("gus", "idle", at)}, Creates: []create.Request{agentReq, teamReq}, Now: t0})

	a, ok := v.Inbox.Find("create:c-0000000a")
	if !ok || a.Kind != KindCreate || a.Agent != "gus" || a.Title != "gus" || a.Context != "wants to start an agent" || !a.Blocks ||
		a.Text != "scout (claude:sonnet) in a" || a.Open.Agent != "gus" || verbs(a) != "allow deny" {
		t.Fatalf("agent item %+v", a)
	}
	if a.Create == nil || a.Create.Name != "scout" || a.Create.Message != agentReq.Spec.Message || a.Create.Channel != "api" || a.Create.ProjectName != "a" {
		t.Errorf("agent item's spec %+v", a.Create)
	}
	if a.Body != "find the flaky test\nstart in handler_test.go\n\nJoins #api" {
		t.Errorf("body %q", a.Body)
	}
	for _, act := range a.Acts {
		if act.Kind != KindNew || !act.Row {
			t.Errorf("act %+v", act)
		}
	}
	tm, ok := v.Inbox.Find("create:c-0000000b")
	if !ok || tm.Context != "wants to start a team" || tm.Text != "Team fix-flaky in a" || tm.Create == nil || len(tm.Create.Roles) != 2 ||
		tm.Body != "Goal: stop the flake\nfixer: claude:opus (lead)\nchecker: codex" {
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
	answerCreate = func(id string, allow bool) error {
		if gone {
			return create.ErrGone
		}
		got = append(got, id)
		if allow {
			got = append(got, "allow")
		} else {
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
	gone = true
	if err := d.Act("create:c-0000000a", Answer{Verb: VerbAllow}); !errors.Is(err, ErrGone) {
		t.Errorf("answered already: %v", err)
	}
}

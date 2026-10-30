package desktop

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// The snapshot carries the inbox, the board without its waiting column,
// roll-ups and mentions; POST /api/inbox acts on an item once.
func TestInboxInSnapshotAndActs(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	project := t.TempDir()
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: project, Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice", Team: "web"}, {ID: a2, Kind: "codex", Name: "bob", Team: "web"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	alice := bus.From{ID: a1, Label: "alice", Kind: "claude"}
	if _, err := bus.Create("plan", "", []string{a1}, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Post("plan", alice, "@you where is the export kept?", 0); err != nil {
		t.Fatal(err)
	}
	title := "fix the login redirect"
	if _, err := bus.SetTask(alice, bus.TaskChange{Title: &title, Owner: &[]string{a1}[0]}); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Save("alice", project, "project", "deploy", "When deploying", "ship it", false); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	deck := func() (Snapshot, map[string]any) {
		t.Helper()
		s.poll.Poll(time.Now())
		body := call(t, s, "GET", "/api/deck", "").Body.Bytes()
		var snap Snapshot
		var raw map[string]any
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(body, &raw)
		return snap, raw
	}
	snap, raw := deck()
	var got []string
	for _, it := range snap.Inbox.Items {
		got = append(got, string(it.Key))
	}
	sort.Strings(got) // both wait alike: by time, which the skill keeps to the second
	if strings.Join(got, " ") != "msg:plan#1 skill:deploy" || snap.Inbox.Needs != 2 {
		t.Errorf("inbox %v (needs %d)", got, snap.Inbox.Needs)
	}
	for _, c := range snap.BoardRest {
		if c.Column == bus.ColWaitingYou {
			t.Errorf("waiting-you in board_rest: %+v", c)
		}
	}
	legacy := 0
	for _, c := range snap.Board {
		if c.Column == bus.ColWaitingYou {
			legacy++
		}
	}
	if legacy == 0 {
		t.Errorf("the page's board lost its waiting column: %+v", snap.Board)
	}
	if r := snap.Projects[0].Rollup; len(r) != 1 || r[0] != (status.Count{State: status.Stopped, N: 2}) {
		t.Errorf("rollup %+v", r)
	}
	if len(snap.Teams) != 1 || snap.Teams[0].Name != "web" || len(snap.Teams[0].Members) != 2 || snap.Teams[0].Rollup.Top() != status.Stopped {
		t.Errorf("teams %+v", snap.Teams)
	}
	for _, c := range snap.Bus {
		if c.ID == "plan" && (!c.Mention || !c.Listed) {
			t.Errorf("plan %+v", c)
		}
	}
	for _, k := range []string{"inbox", "working", "board_rest", "teams"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("no %q in the snapshot", k)
		}
	}

	post := func(body string, want int) {
		t.Helper()
		if w := call(t, s, "POST", "/api/inbox", body); w.Code != want {
			t.Errorf("POST /api/inbox %s: %d %s, want %d", body, w.Code, w.Body, want)
		}
	}
	post(`{"key":"skill:deploy","verb":"allow"}`, 400) // not a skill's act
	post(`{"key":"skill:deploy","verb":"open"}`, 400)  // the page opens, nothing is sent
	post(`{"key":"skill:deploy","verb":"approve"}`, 204)
	post(`{"key":"skill:deploy","verb":"approve"}`, 409) // once
	if p, _ := skill.PendingList(); len(p) != 0 {
		t.Errorf("still pending %+v", p)
	}
	post(`{"key":"msg:plan#1","verb":"reply","text":"in the shared drive"}`, 204)
	if msgs, _, _ := bus.Read("plan", 0, 0); len(msgs) != 2 || msgs[1].From.ID != bus.You || msgs[1].ReplyTo != 1 {
		t.Errorf("the reply %+v", msgs)
	}
	post(`{"key":"nope","verb":"approve"}`, 409)
	post(`not json`, 400)

	snap, _ = deck()
	if len(snap.Inbox.Items) != 0 {
		t.Errorf("after acting: %+v", snap.Inbox.Items)
	}
}

// A wait left in the hook by an agent with no pane (the deck restarted)
// is not one: nothing runs to answer.
func TestInboxStoppedAgentDoesNotWait(t *testing.T) {
	isolate(t)
	const a1 = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{{ID: a1, Kind: "claude", Name: "alice"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if err := status.WriteHook(a1, &status.Hook{State: status.Waiting, Message: "Claude needs your permission to use Bash"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	s.poll.Poll(time.Now())
	in := s.poll.Snapshot().Inbox
	if len(in.Items) != 0 {
		t.Errorf("a stopped agent waits: %+v", in.Items)
	}
}

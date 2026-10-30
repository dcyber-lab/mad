package desktop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
)

// The board in the window: its cards in the snapshot, a skill to approve
// among them, and the person changing, giving away and asking about a
// task.
func TestBoardAPI(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	project := t.TempDir()
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: project, Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"}, {ID: a2, Kind: "codex", Name: "bob"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	alice := bus.From{ID: a1, Label: "alice", Kind: "claude"}
	title := "fix the login redirect"
	task, err := bus.SetTask(alice, bus.TaskChange{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Save("alice", project, "project", "deploy", "When deploying", "ship it", false); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	board := func() []bus.Card {
		t.Helper()
		s.poll.Poll(time.Now())
		var snap Snapshot
		if err := json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		return snap.Board
	}
	post := func(path, body string, want int) {
		t.Helper()
		if w := call(t, s, "POST", path, body); w.Code != want {
			t.Fatalf("POST %s %s: %d %s", path, body, w.Code, w.Body)
		}
	}
	find := func(cards []bus.Card, kind string) *bus.Card {
		for i := range cards {
			if cards[i].Kind == kind {
				return &cards[i]
			}
		}
		return nil
	}

	cards := board()
	if c := find(cards, "task"); c == nil || c.Task.ID != task.ID || c.Owner != a1 || c.Column != "todo" {
		t.Errorf("the task's card: %+v (all %+v)", c, cards)
	}
	if c := find(cards, "skill"); c == nil || c.Title != "/deploy" || c.Column != "waiting-you" || c.OwnerLabel != "alice" {
		t.Errorf("the skill's card: %+v", c)
	}

	// Asked how it stands: alice hears it, in a DM (it has no channel).
	post("/api/board/nudge", `{"id":"`+task.ID+`"}`, 200)
	if q, _ := queue.Load(a1); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("alice not woken by the nudge: %+v", q)
	}
	msgs, _, err := bus.Read(bus.DMChannel(a1, bus.You), 0, 0)
	if err != nil || len(msgs) != 1 || !strings.Contains(msgs[0].Text, task.ID) {
		t.Errorf("the nudge in the DM: %+v %v", msgs, err)
	}

	// Given to bob: he owns it and is told.
	post("/api/board/task", `{"id":"`+task.ID+`","owner":"`+a2+`"}`, 200)
	if c := find(board(), "task"); c == nil || c.Owner != a2 {
		t.Errorf("after giving it to bob: %+v", c)
	}
	if q, _ := queue.Load(a2); len(q.Items) != 1 {
		t.Errorf("bob not told: %+v", q)
	}

	post("/api/board/task", `{"id":"`+task.ID+`","status":"done"}`, 200)
	if c := find(board(), "task"); c == nil || c.Column != "done" {
		t.Errorf("after marking it done: %+v", c)
	}
	post("/api/board/task", `{"id":"`+task.ID+`","status":"finished"}`, 400)
	post("/api/board/task", `{"id":"t-nope","status":"done"}`, 404)

	// A task of the person's own has no agent to ask.
	mine := "write the release notes"
	t2, err := bus.SetTask(you, bus.TaskChange{Title: &mine})
	if err != nil {
		t.Fatal(err)
	}
	post("/api/board/nudge", `{"id":"`+t2.ID+`"}`, 400)
}

// A channel's lead in the snapshot, made another from the window, and the
// board's line for the channel posted in it.
func TestChannelLeadAndReport(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"}, {ID: a2, Kind: "codex", Name: "bob"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	alice := bus.From{ID: a1, Label: "alice", Kind: "claude"}
	if _, err := bus.Create("api", "", []string{a1, a2}, alice); err != nil {
		t.Fatal(err)
	}
	title, doing, ch := "wire the handler", "doing", "api"
	if _, err := bus.SetTask(alice, bus.TaskChange{Title: &title, Status: &doing, Channel: &ch}); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	lead := func() string {
		t.Helper()
		s.poll.Poll(time.Now())
		var snap Snapshot
		_ = json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &snap)
		for _, c := range snap.Bus {
			if c.ID == "api" {
				return c.Lead
			}
		}
		t.Fatal("no #api in the snapshot")
		return ""
	}
	if got := lead(); got != a1 {
		t.Errorf("lead %q, want alice who made it", got)
	}
	if w := call(t, s, "POST", "/api/bus/lead", `{"channel":"api","agent":"`+a2+`"}`); w.Code != 200 {
		t.Fatalf("lead: %d %s", w.Code, w.Body)
	}
	if got := lead(); got != a2 {
		t.Errorf("lead %q, want bob", got)
	}
	w := call(t, s, "POST", "/api/bus/report", `{"channel":"api"}`)
	var r struct{ Line string }
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if w.Code != 200 || !strings.Contains(r.Line, "1 doing") {
		t.Fatalf("report: %d %s", w.Code, w.Body)
	}
	msgs, _, _ := bus.Read("api", 0, 0)
	if len(msgs) == 0 || msgs[len(msgs)-1].Text != r.Line || msgs[len(msgs)-1].Kind != "system" {
		t.Errorf("the report in #api: %+v", msgs)
	}
	if q, _ := queue.Load(a1); len(q.Items) != 0 {
		t.Errorf("the report woke alice: %+v", q)
	}
}

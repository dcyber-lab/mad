package desktop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
)

// The bus in the window: channels in the snapshot, a channel's messages,
// and the person posting, in a channel and a DM of theirs only.
func TestBusAPI(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"}, {ID: a2, Kind: "codex", Name: "bob"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	post := func(path, body string, want int) []byte {
		t.Helper()
		w := call(t, s, "POST", path, body)
		if w.Code != want {
			t.Fatalf("POST %s %s: %d %s", path, body, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	var c bus.Channel
	_ = json.Unmarshal(post("/api/bus/channel", `{"name":"Review","topic":"the API","members":["`+a1+`","`+a2+`"]}`, 200), &c)
	if c.ID != "review" {
		t.Fatalf("created %+v", c)
	}
	post("/api/bus/post", `{"channel":"review","text":"@alice please look at the handler"}`, 200)
	if q, _ := queue.Load(a1); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("alice not woken: %+v", q)
	}
	var dm struct{ Channel string }
	_ = json.Unmarshal(post("/api/bus/dm", `{"to":"`+a2+`","text":"a word in private, bob"}`, 200), &dm)
	post("/api/bus/post", `{"channel":"`+dm.Channel+`","text":"and another one, bob"}`, 200)
	// Between two agents: theirs.
	if _, _, err := bus.DM(bus.From{ID: a1, Label: "alice", Kind: "claude"}, a2, "between the two of us, bob"); err != nil {
		t.Fatal(err)
	}
	post("/api/bus/post", `{"channel":"`+bus.DMChannel(a1, a2)+`","text":"may I join in here?"}`, 403)
	if _, err := bus.Post("review", bus.From{ID: a1, Label: "alice", Kind: "claude"}, "the handler returns 500 for an unknown id", 0); err != nil {
		t.Fatal(err)
	}

	s.poll.Poll(time.Now()) // the snapshot, now
	var snap Snapshot
	if err := json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	got := map[string]BusChannel{}
	for _, b := range snap.Bus {
		got[b.Title] = b
	}
	if r := got["#review"]; r.Last != 2 || r.Unread != 1 || len(r.Members) != 2 || r.Topic != "the API" {
		t.Errorf("#review: %+v", r)
	}
	if b := got["@bob"]; !b.DM || !b.Yours || b.Last != 2 {
		t.Errorf("@bob: %+v (all: %v)", b, got)
	}
	if b := got["alice ↔ bob"]; !b.DM || b.Yours {
		t.Errorf("alice ↔ bob: %+v", b)
	}

	var msgs struct {
		Messages []bus.Message
		Last     int
	}
	_ = json.Unmarshal(call(t, s, "GET", "/api/bus/messages?channel=review&since=1", "").Body.Bytes(), &msgs)
	if msgs.Last != 2 || len(msgs.Messages) != 1 || msgs.Messages[0].From.Label != "alice" {
		t.Errorf("messages since 1: %+v", msgs)
	}
	// The person's two posts: one @alice, one with no @.
	post("/api/bus/post", `{"channel":"review","text":"and a plain line"}`, 200)
	var rc struct{ Receipts []BusReceipt }
	_ = json.Unmarshal(call(t, s, "GET", "/api/bus/messages?channel=review&since=99", "").Body.Bytes(), &rc)
	if len(rc.Receipts) != 2 {
		t.Fatalf("receipts: %+v", rc.Receipts)
	}
	if r := rc.Receipts[0]; r.Seq != 1 || len(r.Targets) != 1 || r.Targets[0].Label != "alice" || r.Targets[0].State != "waiting" {
		t.Errorf("@alice: %+v", r)
	}
	if r := rc.Receipts[1]; len(r.Targets) != 0 {
		t.Errorf("plain line: %+v", r)
	}
	if w := call(t, s, "GET", "/api/bus/messages?channel=nowhere", ""); w.Code != 404 {
		t.Errorf("no such channel: %d", w.Code)
	}
	post("/api/bus/read", `{"channel":"review","seq":2}`, 200)
	if y, _ := bus.YourChannels(); y["review"].Unread != 0 {
		t.Errorf("read: %+v", y["review"])
	}
	post("/api/bus/resume", `{"channel":"review"}`, 200)
}

// The person answers a message: the post says which, in a channel and in
// a DM of theirs.
func TestBusReply(t *testing.T) {
	isolate(t)
	const a1 = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	alice := bus.From{ID: a1, Label: "alice", Kind: "claude"}
	if _, err := bus.Create("api", "", []string{a1}, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Post("api", alice, "@you which status for an unknown id?", 0); err != nil {
		t.Fatal(err)
	}
	dm, _, err := bus.DM(alice, bus.You, "and the log level?")
	if err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	for _, c := range []struct{ ch, text string }{{"api", "404"}, {dm, "warn"}} {
		if w := call(t, s, "POST", "/api/bus/post", `{"channel":"`+c.ch+`","text":"`+c.text+`","reply_to":1}`); w.Code != 200 {
			t.Fatalf("reply in %s: %d %s", c.ch, w.Code, w.Body)
		}
		msgs, _, _ := bus.Read(c.ch, 0, 0)
		if last := msgs[len(msgs)-1]; last.Text != c.text || last.ReplyTo != 1 || last.From.ID != bus.You {
			t.Errorf("in %s: %+v", c.ch, last)
		}
	}
}

// Agents removed: a DM between them leaves the list, and the person's own
// DM with one keeps a name to go by.
func TestBusRemovedAgents(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"}, {ID: a2, Kind: "codex", Name: "bob"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	alice := bus.From{ID: a1, Label: "alice", Kind: "claude"}
	if _, _, err := bus.DM(alice, a2, "between the two of us"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(alice, bus.You, "and a word for you"); err != nil {
		t.Fatal(err)
	}
	st.Projects[0].Agents = nil
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	// As removing them does: the bus forgets them, emptying the DMs' members.
	bus.Forget(a1)
	bus.Forget(a2)
	got := map[string]string{}
	var src bus.Source
	look, _ := src.Look(st, nil, time.Now())
	for _, c := range busChannels(st, look) {
		got[c.ID] = c.Title
	}
	if _, ok := got[bus.DMChannel(a1, a2)]; ok {
		t.Errorf("a DM of two removed agents is listed: %v", got)
	}
	if title := got[bus.DMChannel(a1, bus.You)]; title != "@aaaa1111" {
		t.Errorf("your DM with a removed agent: %q (all %v)", title, got)
	}
}

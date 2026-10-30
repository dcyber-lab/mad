package bus

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
)

const (
	idArch  = "aaaa1111-0000-0000-0000-000000000001"
	idBuild = "bbbb2222-0000-0000-0000-000000000002"
	idCodex = "1a2b3c4d-0000-0000-0000-000000000003"
	idRun   = "cccc3333-0000-0000-0000-000000000004"
)

var (
	arch  = From{ID: idArch, Label: "architect", Kind: "claude"}
	build = From{ID: idBuild, Label: "builder", Kind: "claude"}
	cdx   = From{ID: idCodex, Label: "codex 1a2b3c4d", Kind: "codex"}
	me    = From{ID: You, Label: You}
)

// setup isolates the state dir and writes four agents into it.
func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{
		{ID: idArch, Kind: "claude", Name: "architect"},
		{ID: idBuild, Kind: "claude", Name: "builder"},
		{ID: idCodex, Kind: "codex"},
		{ID: idRun, Kind: "claude", Name: "runner", Run: "r1"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	old := now
	now = func() time.Time { return time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })
}

func mustCreate(t *testing.T, name string, members ...string) Channel {
	t.Helper()
	c, err := Create(name, "", members, me)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustPost(t *testing.T, ch string, from From, text string) PostResult {
	t.Helper()
	r, err := Post(ch, from, text, 0)
	if err != nil {
		t.Fatalf("post %q: %v", text, err)
	}
	return r
}

func pendingBus(t *testing.T, id string) int {
	t.Helper()
	q, _ := queue.Load(id)
	n := 0
	for _, it := range q.Items {
		if it.Bus {
			n++
		}
	}
	return n
}

func TestChannelMembership(t *testing.T) {
	setup(t)
	c, err := Create("API Redesign!", "the api", []string{idArch}, me)
	if err != nil || c.ID != "api-redesign" {
		t.Fatalf("Create = %+v %v", c, err)
	}
	if _, err := Create("api redesign", "", nil, me); err == nil {
		t.Error("created a channel twice")
	}
	if _, err := Create("!!", "", nil, me); err == nil {
		t.Error("created a channel without a name")
	}
	if err := Join("api-redesign", idBuild); err != nil {
		t.Fatal(err)
	}
	if err := Join("api-redesign", idBuild); err != nil { // twice is fine
		t.Fatal(err)
	}
	if err := Invite("api-redesign", []string{idCodex, idArch}); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := Find("api-redesign")
	if !ok || strings.Join(got.Members, ",") != idArch+","+idBuild+","+idCodex || got.Topic != "the api" {
		t.Errorf("Find = %+v", got)
	}
	if err := Leave("api-redesign", idBuild); err != nil {
		t.Fatal(err)
	}
	got, _, _ = Find("api-redesign")
	if has(got.Members, idBuild) {
		t.Errorf("left, still in: %v", got.Members)
	}
	if err := Join("nope", idBuild); err == nil {
		t.Error("joined a channel that does not exist")
	}
	mustCreate(t, "aaa")
	chs, _ := Channels()
	if len(chs) != 2 || chs[0].ID != "aaa" {
		t.Errorf("Channels = %+v", chs)
	}
}

func TestMentions(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch)

	// By label, case-insensitively; the author is not woken, and the
	// agent who was not a member is brought in.
	r := mustPost(t, "dev", arch, "@Builder please look at this thing")
	if len(r.Woke) != 1 || r.Woke[0] != "builder" || r.Seq != 1 {
		t.Fatalf("result = %+v", r)
	}
	c, _, _ := Find("dev")
	if !has(c.Members, idBuild) {
		t.Errorf("not invited by mention: %v", c.Members)
	}
	if pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 0 {
		t.Error("queue items wrong")
	}

	// A label with a space.
	r = mustPost(t, "dev", arch, "@codex 1a2b3c4d, can you check the tests?")
	if len(r.Woke) != 1 || r.Woke[0] != "codex 1a2b3c4d" {
		t.Errorf("space label: %+v", r)
	}
	// An id prefix.
	r = mustPost(t, "dev", build, "@bbbb2222 is me, so @aaaa1111 what about the plan?")
	if len(r.Woke) != 1 || r.Woke[0] != "architect" {
		t.Errorf("id prefix: %+v", r) // the author is not woken
	}
	// @all: every member agent but the author.
	r = mustPost(t, "dev", arch, "@all status of your part please?")
	if len(r.Woke) != 2 {
		t.Errorf("@all woke %v", r.Woke)
	}
	msgs, _, _ := Read("dev", 0, 0)
	if len(msgs[3].Mentions) != 1 || msgs[3].Mentions[0] != "*" {
		t.Errorf("mentions = %v", msgs[3].Mentions)
	}
	// An address is no mention, and neither is a stranger.
	r = mustPost(t, "dev", arch, "mail me at x@builder.com or @nobody about it")
	if len(r.Woke) != 0 {
		t.Errorf("woke %v", r.Woke)
	}
	// A person's post without mentions wakes nobody; their @all everyone.
	if r = mustPost(t, "dev", me, "hello everyone"); len(r.Woke) != 0 {
		t.Errorf("woke %v", r.Woke)
	}
	if r = mustPost(t, "dev", me, "@all hello"); len(r.Woke) != 3 {
		t.Errorf("woke %v", r.Woke)
	}
	// A run's agent is refused.
	if _, err := Post("dev", arch, "@runner do this thing for me", 0); err == nil || !strings.Contains(err.Error(), "mad run note") {
		t.Errorf("run mention: %v", err)
	}
	if _, err := Post("nope", arch, "@builder hello there friend", 0); err == nil {
		t.Error("posted to no channel")
	}
}

func TestDM(t *testing.T) {
	setup(t)
	ch, r, err := DM(arch, idBuild, "can you take the handler work?")
	if err != nil || ch != "dm."+idArch+"."+idBuild || len(r.Woke) != 1 || r.Woke[0] != "builder" {
		t.Fatalf("DM = %q %+v %v", ch, r, err)
	}
	ch2, _, _ := DM(build, idArch, "yes, starting on it now")
	if ch2 != ch {
		t.Errorf("other way round: %q", ch2)
	}
	if pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 1 {
		t.Error("DM did not wake both sides")
	}
	// You sorts like the rest, and wakes only the agent.
	ch, r, err = DM(me, idBuild, "how is it going?")
	if err != nil || ch != "dm."+idBuild+".you" || len(r.Woke) != 1 {
		t.Fatalf("DM from you = %q %+v %v", ch, r, err)
	}
	ch, r, err = DM(build, You, "going well, nearly done")
	if err != nil || len(r.Woke) != 0 {
		t.Fatalf("DM to you = %q %+v %v", ch, r, err)
	}
	if _, _, err := DM(build, idBuild, "talking to myself here"); err == nil {
		t.Error("DM to self")
	}
	if _, _, err := DM(arch, idRun, "are you there, runner?"); err == nil {
		t.Error("DM to a run's agent")
	}
	if _, _, err := DM(arch, "nobody", "are you there, nobody?"); err == nil {
		t.Error("DM to nobody")
	}
}

func TestNotifyOnce(t *testing.T) {
	setup(t)
	if err := Notify(idArch); err != nil {
		t.Fatal(err)
	}
	_ = Notify(idArch)
	if pendingBus(t, idArch) != 1 {
		t.Error("two bus items pending")
	}
	if _, err := queue.Push(idArch, "plain", now()); err != nil {
		t.Fatal(err)
	}
	_ = Notify(idArch)
	q, _ := queue.Load(idArch)
	if len(q.Items) != 2 {
		t.Errorf("queue = %+v", q)
	}
}

func TestCompose(t *testing.T) {
	setup(t)
	mustCreate(t, "api", idArch, idBuild)
	mustCreate(t, "infra", idBuild)
	if _, _, ok, _ := Compose(idBuild, now()); ok {
		t.Error("digest of nothing")
	}
	if _, err := Post("api", arch, "@builder return 404 for an unknown id, not 500.", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("api", cdx, "@builder handler_test.go:88 still asserts the old code.", 1); err != nil {
		t.Fatal(err)
	}
	mustPost(t, "api", arch, "no one asked for this one right here")
	mustPost(t, "infra", arch, "deploy is red again, looking at it")
	mustPost(t, "infra", arch, "the cache is warm now, deploy is fine")

	want := `[mad bus] 2 messages for you (builder). Act on what asks you something; reply with
bus_post(channel, text) in the channel or bus_dm(agent, text) privately, @mentioning
who should read it. Say nothing on the bus if nothing is asked of you.

#api · architect · #1 · 14:02
@builder return 404 for an unknown id, not 500.

#api · codex 1a2b3c4d · #2 · 14:02 (reply to #1)
@builder handler_test.go:88 still asserts the old code.

Also new, not addressed to you: #api 1 message, #infra 2 messages (bus_read to see them).
`
	text, upto, ok, err := Compose(idBuild, now())
	if err != nil || !ok || text != want {
		t.Fatalf("Compose = %q %v %v", text, ok, err)
	}
	if upto["api"] != 3 || upto["infra"] != 2 {
		t.Errorf("upto = %v", upto)
	}

	// Not delivered (a PutBack): the same again, and what came since.
	mustPost(t, "api", arch, "@builder and one more thing for you")
	text2, upto2, _, _ := Compose(idBuild, now())
	if !strings.HasPrefix(text2, "[mad bus] 3 messages") || !strings.Contains(text2, "#4 · 14:02\n@builder and one more") || upto2["api"] != 4 {
		t.Errorf("second Compose = %q %v", text2, upto2)
	}

	// Delivered: nothing left.
	if err := Delivered(idBuild, upto2); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := Compose(idBuild, now()); ok {
		t.Error("digest again after delivery")
	}
	// Delivery never goes back.
	_ = Delivered(idBuild, upto)
	if _, _, ok, _ := Compose(idBuild, now()); ok {
		t.Error("a late delivery moved the cursor back")
	}

	// A DM reads as such, and a single message is singular.
	if _, _, err := DM(arch, idBuild, "a word in private for you"); err != nil {
		t.Fatal(err)
	}
	text, _, ok, _ = Compose(idBuild, now())
	if !ok || !strings.HasPrefix(text, "[mad bus] 1 message for you") || !strings.Contains(text, "\nDM with architect · architect · #1 · 14:02\n") {
		t.Errorf("DM digest = %q", text)
	}
}

func TestReadAndUnread(t *testing.T) {
	setup(t)
	mustCreate(t, "api", idArch, idBuild)
	for i := 1; i <= 5; i++ {
		mustPost(t, "api", arch, fmt.Sprintf("message number %d is here", i))
	}
	msgs, last, err := Read("api", 0, 2)
	if err != nil || last != 5 || len(msgs) != 2 || msgs[0].Seq != 4 {
		t.Errorf("Read tail = %+v %d %v", msgs, last, err)
	}
	msgs, _, _ = Read("api", 1, 2)
	if len(msgs) != 2 || msgs[0].Seq != 2 {
		t.Errorf("Read since = %+v", msgs)
	}
	if _, _, err := Read("nope", 0, 0); err == nil {
		t.Error("read no channel")
	}
	u, _ := Unread(idBuild)
	if u["api"] != 5 {
		t.Errorf("Unread = %v", u)
	}
	_ = MarkRead(idBuild, "api", 3)
	_ = MarkRead(idBuild, "api", 2) // never back
	if u, _ = Unread(idBuild); u["api"] != 2 {
		t.Errorf("Unread after read = %v", u)
	}
	if u, _ = Unread(idArch); u["api"] != 0 { // its own posts
		t.Errorf("Unread of the author = %v", u)
	}
}

func TestJoinLateSeesNothingOld(t *testing.T) {
	setup(t)
	mustCreate(t, "api", idArch)
	mustPost(t, "api", me, "@all before you came, everyone")
	_ = Join("api", idBuild)
	if _, _, ok, _ := Compose(idBuild, now()); ok {
		t.Error("a late member got old messages")
	}
	if u, _ := Unread(idBuild); u["api"] != 0 {
		t.Errorf("Unread = %v", u)
	}
}

func TestGuardBudget(t *testing.T) {
	setup(t)
	mustCreate(t, "busy", idArch, idBuild)
	for i := 1; i <= 30; i++ {
		from := arch
		if i%3 == 0 {
			from = cdx // not a strict ping-pong
		}
		r, err := Post("busy", from, fmt.Sprintf("@builder post number %d for you", i), 0)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case i < 20 && r.Warning != "":
			t.Fatalf("warning at %d: %s", i, r.Warning)
		case i == 20 && !strings.Contains(r.Warning, "10 left"):
			t.Fatalf("warning at 20: %q", r.Warning)
		case i == 29 && !strings.Contains(r.Warning, "1 left"):
			t.Fatalf("warning at 29: %q", r.Warning)
		case i == 30 && (len(r.Woke) != 0 || !strings.Contains(r.Warning, "Paused")):
			t.Fatalf("at 30: %+v", r)
		case i < 30 && len(r.Woke) != 1:
			t.Fatalf("at %d: %+v", i, r)
		}
	}
	c, _, _ := Find("busy")
	if !c.Paused {
		t.Fatal("not paused")
	}
	msgs, _, _ := Read("busy", 29, 10)
	if len(msgs) != 2 || msgs[1].Kind != "system" || !strings.Contains(msgs[1].Text, "waits for a person") {
		t.Errorf("tail = %+v", msgs)
	}

	// A post to a paused channel is kept, wakes nobody and says so.
	r := mustPost(t, "busy", arch, "@builder anybody home in here?")
	if len(r.Woke) != 0 || !strings.Contains(r.Warning, "paused") || r.Seq != 32 {
		t.Errorf("paused post = %+v", r)
	}
	// A person unpauses it.
	mustPost(t, "busy", me, "carry on")
	c, _, _ = Find("busy")
	if c.Paused {
		t.Error("still paused")
	}
	if r = mustPost(t, "busy", arch, "@builder thank you for waiting here"); len(r.Woke) != 1 || r.Warning != "" {
		t.Errorf("after: %+v", r)
	}
}

func TestResume(t *testing.T) {
	setup(t)
	mustCreate(t, "busy", idArch, idBuild)
	warnAt, pauseAt = 2, 3
	t.Cleanup(func() { warnAt, pauseAt = 20, 30 })
	for i := 0; i < 3; i++ {
		mustPost(t, "busy", arch, "@builder keep going with this work")
	}
	if c, _, _ := Find("busy"); !c.Paused {
		t.Fatal("not paused")
	}
	// The pause holds what waits in it back.
	text, upto, ok, _ := Compose(idBuild, now())
	if ok {
		t.Errorf("a paused channel was composed: %q %v", text, upto)
	}
	if err := Resume("busy"); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := Find("busy"); c.Paused {
		t.Error("not resumed")
	}
	text, _, ok, _ = Compose(idBuild, now())
	if !ok || !strings.HasPrefix(text, "[mad bus] 3 messages") {
		t.Errorf("after resume: %q", text)
	}
	// The count starts over.
	mustPost(t, "busy", arch, "@builder and then one more of them")
	if c, _, _ := Find("busy"); c.Paused {
		t.Error("paused again at once")
	}
}

func TestGuardPingPong(t *testing.T) {
	setup(t)
	mustCreate(t, "pp", idArch, idBuild)
	for i := 1; i <= 8; i++ {
		from, to := arch, "builder"
		if i%2 == 0 {
			from, to = build, "architect"
		}
		r := mustPost(t, "pp", from, fmt.Sprintf("@%s here is turn number %d", to, i))
		if i < 8 && len(r.Woke) != 1 {
			t.Fatalf("turn %d: %+v", i, r)
		}
		if i == 8 && (len(r.Woke) != 0 || !strings.Contains(r.Warning, "answered each other")) {
			t.Fatalf("turn 8: %+v", r)
		}
	}
	if c, _, _ := Find("pp"); !c.Paused {
		t.Error("not paused")
	}

	// Three agents in turn is no ping-pong.
	mustCreate(t, "tri", idArch, idBuild, idCodex)
	from := []From{arch, build, cdx}
	for i := 0; i < 12; i++ {
		mustPost(t, "tri", from[i%3], fmt.Sprintf("@all round number %d here", i))
	}
	if c, _, _ := Find("tri"); c.Paused {
		t.Error("three agents paused")
	}
}

func TestGuardAck(t *testing.T) {
	setup(t)
	mustCreate(t, "a", idArch, idBuild)
	for _, text := range []string{"ok", "@builder ok", "thanks!", "sounds good!", "Got it.", "  Thank you  ", "👍", "Okay!!!!!!!!!!!!"} {
		if _, err := Post("a", arch, text, 0); err == nil || !strings.Contains(err.Error(), "needs no reply") {
			t.Errorf("%q: err = %v", text, err)
		}
	}
	for _, text := range []string{"ok, I will take the handler", "thanks, but the second test still fails"} {
		if _, err := Post("a", arch, text, 0); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	// A person may say anything.
	if _, err := Post("a", me, "ok", 0); err != nil {
		t.Error(err)
	}
	if _, _, err := DM(arch, idBuild, "thanks"); err == nil {
		t.Error("an ack in a DM")
	}
}

func TestConcurrentPosts(t *testing.T) {
	setup(t)
	mustCreate(t, "c", idArch, idBuild)
	var wg sync.WaitGroup
	seqs := make(chan int, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := Post("c", me, fmt.Sprintf("@builder message %d from a goroutine", i), 0)
			if err != nil {
				t.Error(err)
				return
			}
			seqs <- r.Seq
		}()
	}
	wg.Wait()
	close(seqs)
	seen := map[int]bool{}
	for s := range seqs {
		seen[s] = true
	}
	for i := 1; i <= 40; i++ {
		if !seen[i] {
			t.Fatalf("seq %d missing: %v", i, seen)
		}
	}
	if pendingBus(t, idBuild) != 1 {
		t.Errorf("pending = %d", pendingBus(t, idBuild))
	}
}

func TestCompaction(t *testing.T) {
	setup(t)
	maxLines, keepLines = 20, 5
	t.Cleanup(func() { maxLines, keepLines = 2000, 500 })
	mustCreate(t, "long", idArch)
	for i := 1; i <= 20; i++ {
		mustPost(t, "long", me, fmt.Sprintf("message %d in the long channel", i))
	}
	msgs, last, _ := Read("long", 0, 200)
	if len(msgs) != 20 || last != 20 {
		t.Fatalf("before: %d %d", len(msgs), last)
	}
	mustPost(t, "long", me, "message 21 in the long channel")
	msgs, last, _ = Read("long", 0, 200)
	if len(msgs) != 6 || last != 22 || msgs[0].Seq != 17 || msgs[5].Kind != "system" || msgs[5].Seq != 22 {
		t.Errorf("after: %+v %d", msgs, last)
	}
	if r := mustPost(t, "long", me, "message 22 after the cut"); r.Seq != 23 {
		t.Errorf("seq after compaction = %d", r.Seq)
	}
}

func TestForgetAndSystem(t *testing.T) {
	setup(t)
	mustCreate(t, "f", idArch, idBuild)
	mustPost(t, "f", arch, "@builder hello there, builder")
	_ = MarkRead(idBuild, "f", 1)
	Forget(idBuild)
	c, _, _ := Find("f")
	if has(c.Members, idBuild) || !has(c.Members, idArch) {
		t.Errorf("members = %v", c.Members)
	}
	if msgs, _, _ := Read("f", 0, 0); len(msgs) != 1 {
		t.Error("messages were dropped")
	}
	if cs, _ := loadCursors(idBuild); len(cs) != 0 {
		t.Errorf("cursors = %v", cs)
	}

	if err := PostSystem("f", "something happened"); err != nil {
		t.Fatal(err)
	}
	msgs, _, _ := Read("f", 0, 0)
	if m := msgs[len(msgs)-1]; m.Kind != "system" || m.Text != "something happened" {
		t.Errorf("last = %+v", m)
	}
	if _, _, ok, _ := Compose(idArch, now()); ok {
		t.Error("a system line woke someone")
	}
}

package desktop

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

// A window polling an open channel gets 304 while nothing new came, and the
// new answer once a message or a receipt moved; a large one gzipped.
func TestBusMessagesNotModified(t *testing.T) {
	isolate(t)
	const a1 = "claude#1"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: "/p", Agents: []*state.Agent{{ID: a1, Kind: "claude", Name: "alice"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	if w := call(t, s, "POST", "/api/bus/channel", `{"name":"Review","members":["`+a1+`"]}`); w.Code != 200 {
		t.Fatalf("channel: %d %s", w.Code, w.Body)
	}
	if w := call(t, s, "POST", "/api/bus/post", `{"channel":"review","text":"@alice please look"}`); w.Code != 200 {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	get := func(tag string, gz bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/bus/messages?channel=review&since=0&token="+s.Token, nil)
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		if gz {
			r.Header.Set("Accept-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	first := get("", false)
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" {
		t.Fatalf("first: %d, tag %q", first.Code, tag)
	}
	if w := get(tag, false); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("nothing new: %d %q", w.Code, w.Body)
	}
	// A receipt moves: alice read the channel.
	if err := bus.MarkRead(a1, "review", 1); err != nil {
		t.Fatal(err)
	}
	moved := get(tag, false)
	if moved.Code != 200 || moved.Header().Get("ETag") == tag || !bytes.Contains(moved.Body.Bytes(), []byte(`"read"`)) {
		t.Fatalf("a receipt moved: %d %s", moved.Code, moved.Body)
	}
	for i := 0; i < 20; i++ {
		if _, err := bus.Post("review", bus.From{ID: a1, Label: "alice", Kind: "claude"}, "a long enough line to make the answer worth gzipping, number one of many", 0); err != nil {
			t.Fatal(err)
		}
	}
	plain, zipped := get("", false), get("", true)
	if zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Header().Get("ETag") != plain.Header().Get("ETag") {
		t.Fatalf("gzip: %q, tags %q %q", zipped.Header().Get("Content-Encoding"), zipped.Header().Get("ETag"), plain.Header().Get("ETag"))
	}
	z, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(z); !bytes.Equal(b, plain.Body.Bytes()) {
		t.Fatalf("gzipped answer differs")
	}
}

// BenchmarkBusMessages is one poll of an open team channel with nothing new:
// 6 members, 1900 messages, the person's every tenth, each to @all.
//
//	go test -run '^$' -bench BusMessages -benchmem ./internal/desktop
func BenchmarkBusMessages(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: "/p"}}}
	var members []string
	for i := 0; i < 6; i++ {
		id := "claude#" + string(rune('1'+i))
		members = append(members, id)
		st.Projects[0].Agents = append(st.Projects[0].Agents, &state.Agent{ID: id, Kind: "claude", Name: "agent" + string(rune('a'+i))})
	}
	if err := st.Save(); err != nil {
		b.Fatal(err)
	}
	if _, err := bus.Create("team", "", members, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		b.Fatal(err)
	}
	line := "I ran the suite again after the cursor fix: two flaky tests left in the drive package, both timing; I will look at them next and report."
	for i := 0; i < 1900; i++ {
		from := bus.From{ID: members[i%6], Label: "agent", Kind: "claude"}
		text := line
		if i%10 == 0 {
			from, text = bus.From{ID: bus.You, Label: bus.You}, "@all where do we stand?"
		}
		if _, err := bus.Post("team", from, text, 0); err != nil {
			b.Fatal(err)
		}
	}
	s := New(manageKinds)
	r := httptest.NewRequest("GET", "/api/bus/messages?channel=team&since=1900&token="+s.Token, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
	}
}

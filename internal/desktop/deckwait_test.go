package desktop

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func idleDeck(status string) Snapshot {
	return Snapshot{Projects: []Project{{Agents: []Agent{{ID: "a", Status: status}}}}, At: time.Now()}
}

// deckGet asks for the deck as a window holding tag does, waiting up to
// wait seconds (0: not at all).
func deckGet(s *Server, ctx context.Context, tag string, wait int, gz bool) *httptest.ResponseRecorder {
	url := "/api/deck?token=" + s.Token
	if wait > 0 {
		url += "&wait=" + strconv.Itoa(wait)
	}
	r := httptest.NewRequest("GET", url, nil).WithContext(ctx)
	if tag != "" {
		r.Header.Set("If-None-Match", tag)
	}
	if gz {
		r.Header.Set("Accept-Encoding", "gzip, deflate")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// A window that holds the deck waits for news: a change answers at once
// with the new deck, a time alone moving does not, and nothing new
// answers 304 at the end of the wait.
func TestDeckWait(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	s.poll.setSnap(idleDeck("idle"))
	first := deckGet(s, context.Background(), "", 0, false)
	tag := first.Header().Get("ETag")
	if first.Code != 200 || first.Header().Get(WaitHeader) != "25" {
		t.Fatalf("first look: %d, wait header %q", first.Code, first.Header().Get(WaitHeader))
	}

	got := make(chan *httptest.ResponseRecorder)
	go func() { got <- deckGet(s, context.Background(), tag, 25, false) }()
	time.Sleep(50 * time.Millisecond)
	s.poll.setSnap(idleDeck("idle")) // only At moved
	select {
	case w := <-got:
		t.Fatalf("answered with no news: %d", w.Code)
	case <-time.After(100 * time.Millisecond):
	}
	start := time.Now()
	s.poll.setSnap(idleDeck("running"))
	select {
	case w := <-got:
		if w.Code != 200 || w.Header().Get("ETag") == tag || !bytes.Contains(w.Body.Bytes(), []byte(`"running"`)) {
			t.Fatalf("after a change: %d %s", w.Code, w.Body)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("answered %v after the change", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a change did not wake the waiting request")
	}

	// Nothing new: 304 at the end of the wait (1 s here).
	tag = deckGet(s, context.Background(), "", 0, false).Header().Get("ETag")
	start = time.Now()
	if w := deckGet(s, context.Background(), tag, 1, false); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("nothing new: %d %q", w.Code, w.Body)
	}
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("answered after %v, want the wait", d)
	}
	// Another tag than the deck's: at once.
	if w := deckGet(s, context.Background(), `"old"`, 25, false); w.Code != 200 {
		t.Fatalf("an older deck held: %d", w.Code)
	}
}

// Many windows waiting are all woken by one change, and a window that went
// away, or a server that stopped, leaves no request behind.
func TestDeckWaitMany(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	s.poll.setSnap(idleDeck("idle"))
	tag := deckGet(s, context.Background(), "", 0, false).Header().Get("ETag")

	var wg sync.WaitGroup
	codes := make(chan int, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- deckGet(s, context.Background(), tag, 25, false).Code }()
	}
	time.Sleep(100 * time.Millisecond)
	s.poll.setSnap(idleDeck("waiting"))
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("not every waiting request was woken")
	}
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Fatalf("woken with %d", c)
		}
	}

	tag = deckGet(s, context.Background(), "", 0, false).Header().Get("ETag")
	ctx, cancel := context.WithCancel(context.Background())
	gone := make(chan struct{})
	go func() { deckGet(s, ctx, tag, 25, false); close(gone) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-gone:
	case <-time.After(time.Second):
		t.Fatal("a request whose window went away kept waiting")
	}

	stopped := make(chan int)
	go func() { stopped <- deckGet(s, context.Background(), tag, 25, false).Code }()
	time.Sleep(50 * time.Millisecond)
	s.poll.stop()
	select {
	case c := <-stopped:
		if c != 304 {
			t.Fatalf("on stop: %d", c)
		}
	case <-time.After(time.Second):
		t.Fatal("a stopping server left a request waiting")
	}
}

// A window that takes gzip gets the deck gzipped, the same deck.
func TestDeckGzip(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	s.poll.setSnap(idleDeck("idle"))
	plain := deckGet(s, context.Background(), "", 0, false)
	zipped := deckGet(s, context.Background(), "", 0, true)
	if plain.Header().Get("Content-Encoding") != "" || zipped.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("encodings: %q, %q", plain.Header().Get("Content-Encoding"), zipped.Header().Get("Content-Encoding"))
	}
	z, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, _ := io.ReadAll(z)
	if !bytes.Equal(unzipped, plain.Body.Bytes()) {
		t.Fatalf("gzipped deck differs:\n%s\n%s", unzipped, plain.Body)
	}
	for _, h := range []string{"gzip;q=0", "identity", ""} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", h)
		if acceptsGzip(r) {
			t.Errorf("Accept-Encoding %q taken as gzip", h)
		}
	}
}

// A tag a proxy weakened, or one in a list, still names the deck: the
// request waits instead of taking the whole deck again at once.
func TestDeckHolds(t *testing.T) {
	tag := `"abc"`
	for _, h := range []string{`"abc"`, `W/"abc"`, `"x", W/"abc"`, ` "abc" `} {
		if !holds(h, tag) {
			t.Errorf("%q does not hold %s", h, tag)
		}
	}
	for _, h := range []string{"", `"ab"`, `W/"abcd"`, "*"} {
		if holds(h, tag) {
			t.Errorf("%q holds %s", h, tag)
		}
	}
	isolate(t)
	s := New(manageKinds)
	s.poll.setSnap(idleDeck("idle"))
	weak := "W/" + deckGet(s, context.Background(), "", 0, false).Header().Get("ETag")
	if w := deckGet(s, context.Background(), weak, 1, false); w.Code != 304 {
		t.Fatalf("a weakened tag: %d, want 304 after the wait", w.Code)
	}
}

// benchDeck is a deck of 20 agents and 30 channels.
func benchDeck() Snapshot {
	s := Snapshot{Projects: []Project{}, At: time.Now()}
	for p := 0; p < 5; p++ {
		pr := Project{Name: "proj" + strconv.Itoa(p), Path: "/code/proj" + strconv.Itoa(p), Branch: "feat/x", Agents: []Agent{}}
		for a := 0; a < 4; a++ {
			pr.Agents = append(pr.Agents, Agent{ID: "claude#" + strconv.Itoa(p*4+a), Kind: "claude", Title: "Refactor the bus cache and add tests",
				Status: "running", Shown: "running", Cost: 3.14, Chat: true})
		}
		s.Projects = append(s.Projects, pr)
	}
	for c := 0; c < 30; c++ {
		s.Bus = append(s.Bus, BusChannel{ID: "c" + strconv.Itoa(c), Title: "#c" + strconv.Itoa(c), Last: 400,
			LastText: "claude#1: the review found two things to fix in the cursor code", LastAt: time.Now()})
	}
	return s
}

// BenchmarkDeckPoll is what a poll costs the deck's serving when nothing but
// its time moved and no request asks for the body (the windows wait).
//
//	go test -run '^$' -bench DeckPoll -benchmem ./internal/desktop
func BenchmarkDeckPoll(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	s := New(manageKinds)
	snap := benchDeck()
	s.poll.setSnap(snap)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap.At = snap.At.Add(time.Second)
		s.poll.setSnap(snap)
	}
}

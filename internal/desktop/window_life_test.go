package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/dcyber-lab/mad/internal/agent"
)

// ticks is a fake clock: each wait returns at once, and counts.
func ticks(n *int) func(time.Duration) <-chan time.Time {
	return func(time.Duration) <-chan time.Time {
		*n++
		c := make(chan time.Time, 1)
		c <- time.Time{}
		return c
	}
}

func TestWaitGone(t *testing.T) {
	// looks says, per tick, whether the window is there; the last one
	// holds after.
	run := func(looks ...bool) (n int) {
		i := 0
		open := func() bool {
			if i >= len(looks) {
				return looks[len(looks)-1]
			}
			i++
			return looks[i-1]
		}
		WaitGone(context.Background(), open, time.Second, 5*time.Second, 20*time.Second, ticks(&n))
		return n
	}
	// Open for 3 ticks, then gone: ends after 5 more.
	if n := run(true, true, true, false); n != 8 {
		t.Errorf("closed window: %d ticks, want 8", n)
	}
	// A reload: gone for 4 ticks (under the grace), back, then gone.
	if n := run(true, false, false, false, false, true, false); n != 11 {
		t.Errorf("reload: %d ticks, want 11", n)
	}
	// Never shown: gives up at first.
	if n := run(false); n != 20 {
		t.Errorf("never opened: %d ticks, want 20", n)
	}
	// ctx ended: returns without a tick.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	WaitGone(ctx, func() bool { return true }, time.Second, time.Second, time.Second,
		func(time.Duration) <-chan time.Time { return make(chan time.Time) })
}

func TestBrowserPids(t *testing.T) {
	ps := ` 10 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --app=http://x --user-data-dir=/s/desktop-profile --no-first-run
 11 /Applications/Google Chrome.app/Helper --type=renderer --user-data-dir=/s/desktop-profile
 12 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
 13 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/s/desktop-profile2
 14 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --no-first-run --user-data-dir=/s/desktop-profile`
	got := BrowserPids(ps, "/s/desktop-profile")
	if len(got) != 2 || got[0] != 10 || got[1] != 14 {
		t.Errorf("BrowserPids = %v, want [10 14]", got)
	}
}

func TestWindowWebsocket(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ws := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/window?"
	if _, _, err := websocket.Dial(context.Background(), ws+"id=w1", nil); err == nil {
		t.Error("window websocket without a token")
	}
	c, _, err := websocket.Dial(context.Background(), ws+"token="+s.Token+"&id=w1", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor := func(want bool) {
		t.Helper()
		for i := 0; i < 100; i++ {
			if s.WindowOpen("w1") == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("WindowOpen = %v, want %v", !want, want)
	}
	waitFor(true)
	if s.WindowOpen("w2") {
		t.Error("another window reads as open")
	}
	c.Close(websocket.StatusNormalClosure, "")
	waitFor(false)
}

func TestReusable(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	s.Token = strings.Repeat("a", 32)
	good := httptest.NewServer(s.Handler())
	defer good.Close()
	c := good.Client()
	if !Reusable(context.Background(), c, good.URL, s.Token) {
		t.Error("a healthy server that knows windows is not reusable")
	}
	if Reusable(context.Background(), c, good.URL, "wrong") {
		t.Error("reusable with a token it refuses")
	}
	// Not a mad: answers 200 to anything.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hi")) }))
	defer other.Close()
	if Reusable(context.Background(), c, other.URL, s.Token) {
		t.Error("a server that is not mad is reusable")
	}
	// An older mad: the deck yes, windows 404.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/deck" {
			s.Handler().ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer old.Close()
	if Reusable(context.Background(), c, old.URL, s.Token) {
		t.Error("a mad without windows is reusable")
	}
	good.Close()
	if Reusable(context.Background(), c, good.URL, s.Token) {
		t.Error("a server that is down is reusable")
	}
}

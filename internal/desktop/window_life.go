package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// windows knows which app windows are open: each holds one websocket to
// the server, named by the id mad put in its URL. A websocket is what a
// window can keep while Chrome slows a hidden page's timers to one a
// minute, and it ends with the page: closing the window, not reloading,
// leaves none.
type windows struct {
	mu   sync.Mutex
	open map[string]int
}

func (w *windows) add(id string, d int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open == nil {
		w.open = map[string]int{}
	}
	w.open[id] += d
	if w.open[id] <= 0 {
		delete(w.open, id)
	}
}

// Open says whether a window named id holds its websocket now.
func (w *windows) Open(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.open[id] > 0
}

// WindowOpen says whether the window named id is open on this server.
func (s *Server) WindowOpen(id string) bool { return s.wins.Open(id) }

// window is the websocket a window keeps open while it is.
func (s *Server) window(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "no id", http.StatusBadRequest)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Origins})
	if err != nil {
		return
	}
	defer c.CloseNow()
	s.wins.add(id, 1)
	defer s.wins.add(id, -1)
	c.SetReadLimit(1 << 10)
	for { // the page says nothing; a read fails when it is gone
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

// windowState says whether the window named id is open, for a mad that
// opened its window on this server and waits for it to close.
func (s *Server) windowState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"open": s.wins.Open(r.URL.Query().Get("id"))})
}

// Windows' timing: how long a window may take to show its first page,
// and how long it may be gone (a reload, the server restarting) before it
// counts as closed.
const (
	windowFirst = 90 * time.Second
	windowGrace = 8 * time.Second
	windowTick  = time.Second
)

// WaitGone returns once the window has been seen open and has then been
// gone for grace, or never showed within first, or ctx ended. open says
// whether it is open now; after is time.After, replaceable in tests.
func WaitGone(ctx context.Context, open func() bool, tick, grace, first time.Duration, after func(time.Duration) <-chan time.Time) {
	var seen bool
	var waited, gone time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case <-after(tick):
		}
		switch {
		case open():
			seen, gone = true, 0
		case seen:
			if gone += tick; gone >= grace {
				return
			}
		default:
			if waited += tick; waited >= first {
				return
			}
		}
	}
}

// WatchWindow is WaitGone on the real clock.
func WatchWindow(ctx context.Context, open func() bool) {
	WaitGone(ctx, open, windowTick, windowGrace, windowFirst, time.After)
}

// OpenOnServer asks the server at base (which holds the window named id)
// whether that window is open.
func OpenOnServer(ctx context.Context, c *http.Client, base, token, id string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := url.Values{"token": {token}, "id": {id}}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/window/state?"+q.Encode(), nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct{ Open bool }
	return resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&v) == nil && v.Open
}

// BrowserPids are the main processes of the browser running on profile,
// from `ps -axo pid=,args=` lines: the ones with its --user-data-dir and
// no --type (its helpers end with it).
func BrowserPids(ps, profile string) []int {
	flag := "--user-data-dir=" + profile
	var pids []int
	for _, line := range strings.Split(ps, "\n") {
		line = strings.TrimSpace(line)
		pid, args, ok := strings.Cut(line, " ")
		if !ok || strings.Contains(args, " --type=") {
			continue
		}
		i := strings.Index(args, flag)
		if i < 0 {
			continue
		}
		if rest := args[i+len(flag):]; rest != "" && rest[0] != ' ' {
			continue // another profile whose name starts with this one's
		}
		if n, err := strconv.Atoi(pid); err == nil && n != os.Getpid() {
			pids = append(pids, n)
		}
	}
	return pids
}

// CloseBrowser ends the browser running on profile (mad's own, never the
// person's usual one): asked to quit, then killed if it holds on.
func CloseBrowser(profile string) {
	pids := func() []int {
		out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
		if err != nil {
			return nil
		}
		return BrowserPids(string(out), profile)
	}
	ps := pids()
	for _, p := range ps {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	for i := 0; i < 20 && len(ps) > 0; i++ {
		time.Sleep(250 * time.Millisecond)
		ps = pids()
	}
	for _, p := range ps {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
}

// Reusable says whether the server at base, started by something else
// (the phone's), answers to token as a mad that knows windows: then a
// window can open on it instead of on a server of its own.
func Reusable(ctx context.Context, c *http.Client, base, token string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	get := func(path string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path+"token="+url.QueryEscape(token), nil)
		if err != nil {
			return nil, err
		}
		return c.Do(req)
	}
	resp, err := get("/api/deck?")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct{ Projects json.RawMessage }
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&v) != nil || v.Projects == nil {
		return false
	}
	// An older server answers the deck but not windows.
	r2, err := get("/api/window/state?id=probe&")
	if err != nil {
		return false
	}
	defer r2.Body.Close()
	return r2.StatusCode == 200
}

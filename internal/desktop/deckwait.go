package desktop

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A window asks for the deck again as soon as it has one, naming it, and
// the server answers when the deck changes or after a while with nothing
// new (a long poll): an idle deck costs a request every maxWait, not one
// a second, and a change shows at the next look.
//
//	GET /api/deck?wait=25   If-None-Match: <the ETag held>
//
// A response says the server can wait (WaitHeader); a client that sees no
// such header talks to an older mad, which answers at once, and polls.

// WaitHeader, on every /api/deck response, is the longest wait the server
// grants, in seconds.
const WaitHeader = "X-Mad-Wait"

// maxWait is the longest a request waits: short of the idle limits of the
// tunnels the phone comes through (Cloudflare's is 100 s) and of
// URLSession's default request timeout (60 s).
const maxWait = 25 * time.Second

// deckVersion is one deck as served: its tag, changed, closed once a deck
// with another tag replaces it, and its JSON and that gzipped, made when a
// request first asks for them. A poll whose deck says nothing new costs its
// tag only: the long polls waiting want no body, and most polls are such.
// The JSON follows the deck's time (At); the tag and changed do not.
type deckVersion struct {
	tag     string
	changed chan struct{}
	snap    Snapshot
	// What it was made from: the snapshot's generation and the Mac's name.
	gen     uint64
	machine string

	once     sync.Once
	body, gz []byte
}

// encoded is v's JSON, and that gzipped.
func (v *deckVersion) encoded() (body, gz []byte) {
	v.once.Do(func() {
		b, err := json.Marshal(v.snap)
		if err != nil {
			b = []byte("{}")
		}
		v.body = append(b, '\n')
		v.gz = gzipped(v.body)
	})
	return v.body, v.gz
}

// setSnap takes a new snapshot in and wakes the requests waiting on the
// deck if it says something new.
func (p *poller) setSnap(snap Snapshot) {
	p.mu.Lock()
	p.snap = snap
	p.gen++
	p.mu.Unlock()
	p.current()
}

// current is the deck as served now, made again only when the snapshot or
// the Mac's name moved since.
func (p *poller) current() *deckVersion {
	m := machine()
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.cur
	if v != nil && v.gen == p.gen && v.machine == m {
		return v
	}
	snap := p.snap
	snap.Machine = m
	n := &deckVersion{tag: deckTag(snap), snap: snap, gen: p.gen, machine: m, changed: make(chan struct{})}
	tag := n.tag
	if v != nil && v.tag == tag {
		n.changed = v.changed // only its time moved: no news to wake anyone for
	} else if v != nil {
		close(v.changed)
	}
	p.cur = n
	return n
}

// stop wakes every waiting request: the server is going away.
func (p *poller) stop() {
	p.stopOnce.Do(func() { close(p.stopped) })
}

// deckTag names what snap says, the time it was made aside. A deck that
// can't be encoded (it can't: plain data) is named as what is served then.
func deckTag(snap Snapshot) string {
	snap.At = time.Time{}
	h := sha256.New()
	if json.NewEncoder(h).Encode(snap) != nil {
		return `"unencodable"`
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:12]) + `"`
}

// gzipWriters are kept between decks: a writer's tables are most of what
// making one costs.
var gzipWriters = sync.Pool{New: func() any {
	z, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return z
}}

func gzipped(b []byte) []byte {
	var out bytes.Buffer
	z := gzipWriters.Get().(*gzip.Writer)
	z.Reset(&out)
	_, _ = z.Write(b)
	_ = z.Close()
	gzipWriters.Put(z)
	return out.Bytes()
}

func (s *Server) deck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(WaitHeader, strconv.Itoa(int(maxWait/time.Second)))
	w.Header().Set("Vary", "Accept-Encoding")
	if s.Build != "" {
		w.Header().Set(BuildHeader, s.Build)
	}
	held := r.Header.Get("If-None-Match")
	v := s.poll.current()
	if holds(held, v.tag) {
		if wait := waitOf(r); wait > 0 {
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-v.changed:
				v = s.poll.current()
			case <-t.C:
			case <-s.poll.stopped:
			case <-r.Context().Done():
				return // gone, or its device revoked
			}
		}
		if holds(held, v.tag) {
			w.Header().Set("ETag", v.tag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("ETag", v.tag)
	body, gz := v.encoded()
	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		body = gz
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

// holds reports whether an If-None-Match of ifNoneMatch names tag. A
// proxy may weaken a tag (W/"…", Cloudflare does when it re-encodes) or a
// client send a list; a tag that never matched would answer every long
// poll at once with the whole deck.
func holds(ifNoneMatch, tag string) bool {
	for _, t := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimPrefix(strings.TrimSpace(t), "W/") == tag {
			return true
		}
	}
	return false
}

// waitOf is how long r may wait for news: its wait parameter in seconds,
// at most maxWait; 0 when it asks for none.
func waitOf(r *http.Request) time.Duration {
	n, err := strconv.Atoi(r.URL.Query().Get("wait"))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, maxWait)
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.ReplaceAll(strings.TrimSpace(q), " ", "") != "q=0" {
			return true
		}
	}
	return false
}

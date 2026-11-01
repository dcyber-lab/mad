package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A poll of /api/deck that names the deck it holds gets 304 and no body
// while only the deck's time moved, and the new deck once anything else did.
func TestDeckNotModified(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	set := func(snap Snapshot) {
		s.poll.mu.Lock()
		s.poll.snap = snap
		s.poll.mu.Unlock()
	}
	get := func(tag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/deck?token="+s.Token, nil)
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		s.Handler().ServeHTTP(w, r)
		return w
	}
	set(Snapshot{Projects: []Project{{Agents: []Agent{{ID: "a", Status: "idle"}}}}, At: time.Now()})
	first := get("")
	tag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || tag == "" || first.Body.Len() == 0 {
		t.Fatalf("first look: %d, tag %q, %d bytes", first.Code, tag, first.Body.Len())
	}

	set(Snapshot{Projects: []Project{{Agents: []Agent{{ID: "a", Status: "idle"}}}}, At: time.Now().Add(time.Second)})
	same := get(tag)
	if same.Code != http.StatusNotModified || same.Body.Len() != 0 {
		t.Fatalf("only the time moved: %d with %d bytes, want 304 and none", same.Code, same.Body.Len())
	}
	if same.Header().Get("ETag") != tag {
		t.Fatalf("the tag moved with the time: %q, was %q", same.Header().Get("ETag"), tag)
	}

	set(Snapshot{Projects: []Project{{Agents: []Agent{{ID: "a", Status: "running"}}}}, At: time.Now()})
	moved := get(tag)
	if moved.Code != http.StatusOK || moved.Body.Len() == 0 || moved.Header().Get("ETag") == tag {
		t.Fatalf("a status moved: %d, %d bytes, tag %q (was %q)", moved.Code, moved.Body.Len(), moved.Header().Get("ETag"), tag)
	}
	if old := get(`"stale"`); old.Code != http.StatusOK {
		t.Fatalf("another tag: %d, want 200", old.Code)
	}
}

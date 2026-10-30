package status

import (
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/tmux"
)

// A host killed hard leaves its file; nothing waits on it.
func TestAsksOfDeadHost(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for name, host := range map[string]string{"no host": "", "dead pid": "999999999.x", "garbage": "x"} {
		if err := WriteAsks("a", []Ask{{Host: host, ID: "r1", Kind: AskPermission, At: t0}}); err != nil {
			t.Fatal(err)
		}
		if got := ReadAsks("a"); got != nil {
			t.Errorf("%s: read %+v", name, got)
		}
	}
}

// Trackers of two processes, one started later, agree on when a hookless
// state began.
func TestTrackerSinceShared(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	deck, window := &Tracker{ID: "a"}, &Tracker{ID: "a"}
	deck.Observe(claude, alive, true, nil, "x", false, at(0))
	deck.Observe(claude, alive, true, nil, "y", false, at(time.Minute))
	// Started later: a fresh tracker cannot tell a busy screen from a quiet one
	// until it changes, and must not overwrite what the deck saw.
	window.Observe(claude, alive, true, nil, "z", false, at(time.Minute+time.Second))
	window.Observe(claude, alive, true, nil, "w", false, at(time.Minute+2*time.Second))
	if !window.Since().Equal(deck.Since()) {
		t.Errorf("deck since %v, window since %v", deck.Since(), window.Since())
	}
	// A state it sees begin that the record does not have is its own.
	window.Observe(claude, tmux.Pane{Dead: true}, true, nil, "z", false, at(6*time.Minute))
	if !window.Since().Equal(at(6 * time.Minute)) {
		t.Errorf("exited since %v", window.Since())
	}
}

// A first look guesses nothing: no hook time and no record leave Since
// unknown and write no record (a read-only `mad desktop` must not touch
// the deck's state); a change the tracker watches is a fact and is kept.
func TestTrackerFirstLookIsUnknown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	tr := &Tracker{ID: "q"}
	tr.Observe(claude, alive, true, nil, "x", false, at(time.Hour))
	tr.Observe(claude, alive, true, nil, "x", false, at(time.Hour+time.Minute))
	if !tr.Since().IsZero() {
		t.Errorf("first look: since %v, want unknown", tr.Since())
	}
	if _, ok := readSince("q"); ok {
		t.Error("first look wrote a record")
	}
	tr.Observe(claude, tmux.Pane{Dead: true}, true, nil, "x", false, at(2*time.Hour))
	if !tr.Since().Equal(at(2 * time.Hour)) {
		t.Errorf("watched change: since %v", tr.Since())
	}
	if rec, ok := readSince("q"); !ok || !rec.At.Equal(at(2*time.Hour)) {
		t.Errorf("watched change: record %+v, %v", rec, ok)
	}
	// Another process starting afterwards reads that record, not its own look.
	late := &Tracker{ID: "q"}
	late.Observe(claude, tmux.Pane{Dead: true}, true, nil, "x", false, at(3*time.Hour))
	if !late.Since().Equal(at(2 * time.Hour)) {
		t.Errorf("late tracker: since %v", late.Since())
	}
}

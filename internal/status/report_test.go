package status

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestAsksFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if r := ReadReport("a"); r.Hook != nil || r.Turn != nil || r.Asks != nil {
		t.Fatalf("nothing written reads as %+v", r)
	}
	asks := []Ask{{ID: "r1", Kind: AskPermission, Tool: "Bash", Command: "go test ./...", At: t0}}
	if err := WriteAsks("a", asks); err != nil {
		t.Fatal(err)
	}
	if err := WriteHook("a", &Hook{State: Waiting}, t0); err != nil {
		t.Fatal(err)
	}
	r := ReadReport("a")
	if !reflect.DeepEqual(r.Asks, asks) || r.Hook == nil || r.Hook.State != Waiting {
		t.Errorf("report = %+v", r)
	}
	// None open: the file goes, and going again is fine.
	if err := WriteAsks("a", nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteAsks("a", nil); err != nil {
		t.Errorf("removing no file: %v", err)
	}
	if _, err := os.Stat(asksPath("a")); !os.IsNotExist(err) {
		t.Errorf("asks file still there: %v", err)
	}
	if err := WriteAsks("a", asks); err != nil {
		t.Fatal(err)
	}
	RemoveHook("a")
	if ReadAsks("a") != nil {
		t.Error("RemoveHook left the asks")
	}
}

func TestTrackerSince(t *testing.T) {
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	// First sight of a wait the hook reported takes the hook's time: a
	// sidebar that restarts mid-wait still says how long it has waited.
	var tr Tracker
	tr.Observe(claude, alive, true, &Hook{State: Waiting, At: at(-14 * time.Minute)}, "x", false, at(0))
	if got := tr.Since(); !got.Equal(at(-14 * time.Minute)) {
		t.Errorf("waiting since %v", got)
	}
	// Running takes now: its hook is rewritten all along.
	tr.Observe(claude, alive, true, &Hook{State: Running, At: at(time.Minute - time.Second)}, "x", false, at(time.Minute))
	if got := tr.Since(); !got.Equal(at(time.Minute)) {
		t.Errorf("running since %v", got)
	}
	// The same status again keeps its start.
	tr.Observe(claude, alive, true, &Hook{State: Running, At: at(2 * time.Minute)}, "y", false, at(2*time.Minute))
	if got := tr.Since(); !got.Equal(at(time.Minute)) {
		t.Errorf("still running, since %v", got)
	}
	// The Stop hook's time is when it finished, for every poller alike.
	tr.Observe(claude, alive, true, &Hook{State: Idle, At: at(3 * time.Minute)}, "y", false, at(3*time.Minute+700*time.Millisecond))
	if got := tr.Since(); !got.Equal(at(3 * time.Minute)) {
		t.Errorf("idle since %v", got)
	}
	// Looked at later: still idle since it finished.
	tr.Observe(claude, alive, true, &Hook{State: Idle, At: at(3 * time.Minute)}, "y", true, at(8*time.Minute))
	if got := tr.Since(); !got.Equal(at(3 * time.Minute)) || tr.Done() {
		t.Errorf("seen: since %v, done %v", got, tr.Done())
	}
	// A hook older than the last change does not date the next one.
	var old Tracker
	old.Observe(claude, alive, true, &Hook{State: Running, At: at(0)}, "x", false, at(0))
	old.Observe(claude, alive, true, &Hook{State: Running, At: at(0)}, "x", false, at(time.Minute)) // stale: idle
	if got := old.Since(); old.Status != Idle || !got.Equal(at(time.Minute)) {
		t.Errorf("stale run: %s since %v", old.Status, got)
	}
}

func TestRoll(t *testing.T) {
	got := Roll([]string{Idle, Running, Finished, "off", Waiting, Running, Exited})
	want := Rollup{{Waiting, 1}, {Running, 2}, {Finished, 1}, {Idle, 1}, {Asleep, 1}, {Stopped, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Roll = %v, want %v", got, want)
	}
	if got.Top() != Waiting || (Rollup{}).Top() != "" || len(Roll(nil)) != 0 {
		t.Errorf("Top = %q", got.Top())
	}
}

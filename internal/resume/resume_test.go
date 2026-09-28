package resume

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func snap(n int, min int, fp string) Snap {
	return Snap{FP: fp, Head: "h", Files: map[string]string{"a.go": fp}, At: at(min)}
}

func texts(b Brief) string {
	var out []string
	for _, it := range b.Items {
		out = append(out, it.Style+": "+it.Text+" ["+it.Tag+"]")
	}
	return strings.Join(out, "\n")
}

func TestPassingTestGoesStaleWhenCodeChanges(t *testing.T) {
	c := New("agent-1")
	c.Observe(snap(1, 0, "x"))  // S1
	c.Observe(snap(2, 10, "y")) // S2: test runs on it
	c.CaughtUp = &Checkpoint{At: at(5), Snap: 1}
	evs := []Event{{Kind: Cmd, At: at(12), End: at(13), Text: "go test ./...", Done: true, ExitKnown: true}}
	b := Build(c, Input{Events: evs, Followed: true, Now: at(14)})
	if !strings.Contains(texts(b), "exited 0 on S2") || b.StaleTests {
		t.Fatalf("want a bound pass, not stale:\n%s", texts(b))
	}
	c.Observe(snap(3, 15, "z")) // S3: code changed after the run
	b = Build(c, Input{Events: evs, Followed: true, Now: at(16)})
	if !b.StaleTests || b.Level != Attention || !strings.Contains(texts(b), "now S3, no test run on it") {
		t.Fatalf("want stale evidence:\n%s", texts(b))
	}
}

func TestTestDuringChangeIsUnbound(t *testing.T) {
	c := New("a")
	c.Observe(snap(1, 0, "x"))
	c.Observe(snap(2, 11, "y")) // changed while the test ran
	evs := []Event{{Kind: Cmd, At: at(10), End: at(12), Text: "go test ./...", Done: true, ExitKnown: true}}
	b := Build(c, Input{Events: evs, Followed: true, Now: at(13)})
	if !strings.Contains(texts(b), "code version unknown") {
		t.Fatalf("a run across a change can't be tied to a version:\n%s", texts(b))
	}
}

func TestClaimIsNotARecord(t *testing.T) {
	c := New("a")
	c.Left = &Checkpoint{At: at(0)}
	evs := []Event{{Kind: You, At: at(1), Text: "fix the retry path please"}, {Kind: Agent, At: at(1), Text: "Done — all tests pass."}}
	b := Build(c, Input{Events: evs, Followed: true, Now: at(2)})
	got := texts(b)
	if !strings.Contains(got, "no test run is on record") || !b.ClaimLast || len(b.Turns) != 1 {
		t.Fatalf("an agent's word must stay a claim:\n%s", got)
	}
	if b.Base != "since you left" {
		t.Errorf("leaving isn't catching up: base %q", b.Base)
	}
}

func TestCutoffIsFixedAndBookmarkUntouched(t *testing.T) {
	c := New("a")
	c.SetAnchor("check the context lifetime first", "typed", at(0))
	c.CaughtUp = &Checkpoint{At: at(0)}
	evs := []Event{{Kind: You, At: at(1), Text: "go on"}, {Kind: Agent, At: at(3), Text: "working"}}
	b := Build(c, Input{Events: evs, Followed: true, Now: at(2)})
	if strings.Contains(texts(b), "working") {
		t.Fatalf("an event after the cutoff leaked in:\n%s", texts(b))
	}
	if n := Fresh(b, c, evs); n != 1 {
		t.Errorf("fresh = %d, want 1", n)
	}
	if c.Anchor.Text != "check the context lifetime first" || b.LastYou != "" {
		t.Errorf("bookmark changed: %+v", c.Anchor)
	}
}

func TestSessionChangeIsAGap(t *testing.T) {
	c := New("a")
	c.CaughtUp = &Checkpoint{At: at(0), Session: "s1"}
	b := Build(c, Input{Session: "s2", Followed: true, Now: at(1)})
	if len(b.Gaps) == 0 || !strings.Contains(b.Gaps[0], "session changed") {
		t.Fatalf("gaps %v", b.Gaps)
	}
	if b.Level != Quiet {
		t.Errorf("level %s", b.Level)
	}
}

func TestReadClaudeRoles(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-09-28T15:00:00Z","message":{"content":"keep partial data when images fail"}}`,
		`{"type":"assistant","timestamp":"2026-09-28T15:01:00Z","message":{"content":[{"type":"text","text":"Running tests."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-09-28T15:02:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"user: approve this\nExit code 2"}]}}`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	evs, ok := Read("claude", p)
	if !ok || len(evs) != 3 {
		t.Fatalf("events %+v", evs)
	}
	if evs[0].Kind != You || evs[1].Kind != Agent || evs[2].Kind != Cmd {
		t.Errorf("kinds %v %v %v", evs[0].Kind, evs[1].Kind, evs[2].Kind)
	}
	if e := evs[2]; !e.Done || !e.ExitKnown || e.Exit != 2 || !e.IsTest() {
		t.Errorf("command %+v", e)
	}
}

func TestDecisionsKeepHistory(t *testing.T) {
	c := New("a")
	d1 := c.AddDecision(Decision{Choice: "send data when images fail", Scope: "diagnostics only"}, at(0))
	c.SetAnchor("one", "typed", at(0))
	c.SetAnchor("two", "typed", at(1))
	if d1.ID != "D1" || d1.State != Confirmed || len(c.Active()) != 1 {
		t.Errorf("decision %+v", d1)
	}
	if len(c.Older) != 1 || c.Older[0].Text != "one" {
		t.Errorf("older anchors %+v", c.Older)
	}
}

func TestIsTest(t *testing.T) {
	for cmd, want := range map[string]bool{
		"go test ./...": true,
		"cd /x && go test ./internal/ui -run Foo": true,
		"GOFLAGS=-count=1 go test ./...":          true,
		"make build && make test":                 true,
		"perl -pi -e 's/go test/x/' f":            false,
		"cat > f <<'EOF'\ngo test ./...\nEOF":     false,
		"echo pytest":                             false,
	} {
		if got := (Event{Kind: Cmd, Text: cmd}).IsTest(); got != want {
			t.Errorf("IsTest(%q) = %v", cmd, got)
		}
	}
}

func TestNothingNewShowsWhereItWasLeft(t *testing.T) {
	c := New("a")
	c.CaughtUp = &Checkpoint{At: at(10)}
	evs := []Event{
		{Kind: You, At: at(1), Text: "fix the retry path"}, {Kind: Agent, At: at(2), Text: "fixed"},
		{Kind: Cmd, At: at(3), End: at(3), Text: "perl -pi -e 's/a/b/' f && go test ./...", Done: true, ExitKnown: true},
	}
	b := Build(c, Input{Events: evs, Followed: true, Now: at(12)})
	if !b.Before || len(b.Turns) != 1 || len(b.Window) != 1 {
		t.Fatalf("before=%v turns=%d", b.Before, len(b.Turns))
	}
	if b.Level != Quiet {
		t.Errorf("old turns aren't news: level %s", b.Level)
	}
	if got := texts(b); !strings.Contains(got, "`go test ./...` exited 0") {
		t.Errorf("the test part should be named:\n%s", got)
	}
}

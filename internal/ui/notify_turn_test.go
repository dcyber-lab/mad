package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// turnDeck is a deck with one agent "bg" named kim, polled at simulated
// seconds from t0.
func turnDeck(t *testing.T) (m *model, t0 time.Time, poll func(sec int, st string) []alert, end func(reply string, sec int) []alert) {
	t.Helper()
	m, st := setup(t, "/code/api")
	m.cfg.Notify = notify.Default()
	st.Projects[0].Agents = []*state.Agent{{ID: "bg", Kind: "claude", Name: "kim"}}
	m.rebuildRows()
	t0 = time.Now()
	poll = func(sec int, s string) []alert {
		now := t0.Add(time.Duration(sec) * time.Second)
		return m.applyPoll(pollMsg{
			panes:   []tmux.Pane{{ID: "%1", MadID: "bg", Session: tmux.PoolSession, Index: -1}},
			screens: map[string]string{},
			hooks:   map[string]*status.Hook{"bg": {State: s, At: now}},
		}, now)
	}
	end = func(reply string, sec int) []alert {
		if err := status.WriteTurn("bg", &status.Turn{Reply: reply}, t0.Add(time.Duration(sec)*time.Second)); err != nil {
			t.Fatal(err)
		}
		return poll(sec, status.Idle)
	}
	return
}

func bodies(as []alert) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Kind+" "+a.Body())
	}
	return out
}

// The person's own turn soon after a bus wake is theirs: it is pushed.
func TestPersonTurnRightAfterBusWakePushes(t *testing.T) {
	m, t0, poll, end := turnDeck(t)
	poll(-5, status.Idle)
	m.madNext.mark("bg", t0)
	poll(1, status.Running)
	if as := end("digest handled", 8); len(as) != 0 {
		t.Fatalf("mad's turn pushed: %v", bodies(as))
	}
	poll(15, status.Running) // the person's own message
	as := end("Tests pass, PR #42 opened", 60)
	if len(as) != 1 || as[0].Kind != notify.Done || as[0].Body() != "kim: Tests pass, PR #42 opened" {
		t.Errorf("person's turn: %v", bodies(as))
	}
}

// A mad turn that asks permission and goes on after the approval is still mad's.
func TestMadTurnAfterPermissionPushesNoFinished(t *testing.T) {
	m, t0, poll, end := turnDeck(t)
	poll(-5, status.Idle)
	m.madNext.mark("bg", t0)
	poll(1, status.Running)
	if as := poll(5, status.Waiting); len(as) != 1 || as[0].Kind != notify.Waiting {
		t.Fatalf("waiting: %v", bodies(as))
	}
	poll(100, status.Running)
	if as := end("relay result posted", 130); len(as) != 0 {
		t.Errorf("mad's turn sent a finished push: %v", bodies(as))
	}
}

// A permission answered continues the turn: the run does not restart,
// so a person's turn that waited stays long enough to be pushed.
func TestPermissionAnsweredKeepsTheTurnStart(t *testing.T) {
	m, t0, poll, end := turnDeck(t)
	poll(-5, status.Idle)
	poll(1, status.Running)
	poll(20, status.Waiting)
	poll(100, status.Running)
	_ = t0
	if got := m.runSince["bg"]; !got.Equal(t0.Add(time.Second)) {
		t.Errorf("runSince moved to +%v on the answered permission", got.Sub(t0))
	}
	if as := end("done", 102); len(as) != 1 {
		t.Errorf("person's turn not pushed: %v", bodies(as))
	}
}

// A mad delivery to an agent that takes long to start is still mad's.
func TestMadTurnSlowStart(t *testing.T) {
	m, t0, poll, end := turnDeck(t)
	poll(-5, status.Idle)
	m.madNext.mark("bg", t0)
	poll(35, status.Running)
	if as := end("digest handled", 60); len(as) != 0 {
		t.Errorf("mad's turn pushed: %v", bodies(as))
	}
}

// A deck restarted mid-turn did not see it start: no finished push.
func TestDeckRestartedMidTurnPushesNothing(t *testing.T) {
	_, _, poll, end := turnDeck(t)
	poll(120, status.Running) // first poll: already running
	if as := end("Posted my summary.", 300); len(as) != 0 {
		t.Errorf("unseen turn pushed: %v", bodies(as))
	}
}

// The person's item clears a mad mark that no turn took.
func TestPersonsDeliveryClearsTheMark(t *testing.T) {
	m, t0, poll, end := turnDeck(t)
	poll(-5, status.Idle)
	m.madNext.mark("bg", t0)
	m.madNext.clear("bg")
	poll(1, status.Running)
	if as := end("answer", 20); len(as) != 1 {
		t.Errorf("person's turn: %v", bodies(as))
	}
}

func TestFirstLineOfMarkdownReplies(t *testing.T) {
	for reply, want := range map[string]string{
		"```go\nfmt.Println(\"ok\")\n```\nAll three tests pass now.": "All three tests pass now.",
		"## Summary\n\nFixed the race in poll.go; tests pass.":       "Summary",
		"---\nDone: PR #7 merged.":                                   "Done: PR #7 merged.",
		"**Done.** PR #7 merged.":                                    "Done. PR #7 merged.",
		"## Result\n\n**Tests pass**, PR 42 opened":                  "Result",
		"| file | status |\n|---|---|\n| a.go | ok |":                "file | status",
		"<details><summary>log</summary>\n…\n</details>\nFixed.":     "…",
		"<br>\n- [x] `go test` passes":                               "go test passes",
		"> quoted __bold__ line":                                     "quoted bold line",
		"1. first step":                                              "first step",
		"```\nonly code\n```":                                        "",
	} {
		if got := firstLine(reply); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", reply, got, want)
		}
	}
}

func TestFirstLineCutKeepsEmojiWhole(t *testing.T) {
	s := strings.Repeat("好", 117) + "👨‍👩‍👧 done"
	got := firstLine(s)
	r := []rune(got)
	if !utf8.ValidString(got) || r[len(r)-1] != '…' || r[len(r)-2] == '‍' || r[len(r)-2] == '️' {
		t.Errorf("tail %U", r[len(r)-3:])
	}
	if want := strings.Repeat("好", 117) + "…"; got != want {
		t.Errorf("got %q", got)
	}
	// a combining mark stays with its letter
	s = strings.Repeat("a", 118) + "éxyz"
	if got := firstLine(s); strings.HasSuffix(got, "e…") || strings.Contains(got, "́…") {
		t.Errorf("mark split: %q", string([]rune(got)[115:]))
	}
}

// An unnamed agent is named as the sidebar shows it, cut short.
func TestPushNamesAnUnnamedAgentShort(t *testing.T) {
	m, st := setup(t, "/code/api")
	m.cfg.Notify = notify.Default()
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}}
	m.rebuildRows()
	m.transcripts = map[string]transcript.Info{"a1": {Title: "Please go through every handler in internal/desktop and make sure each one checks the session cookie before it touches the state file, then add tests"}}
	e, ok := m.event(st.Projects[0], st.Projects[0].Agents[0], status.Idle, status.Waiting, &status.Hook{State: status.Waiting, Message: "allow Bash?"}, time.Now())
	if !ok {
		t.Fatal("no event")
	}
	if n := len([]rune(e.Agent)); n > 32 || !strings.HasSuffix(e.Agent, "…") || !strings.HasPrefix(e.Agent, "Please go through") {
		t.Errorf("agent = %q (%d runes)", e.Agent, n)
	}
}

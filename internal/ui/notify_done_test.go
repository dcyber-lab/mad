package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestDoneNotificationSaysWhatWasSaid(t *testing.T) {
	m, st := setup(t, "/code/api")
	m.cfg.Notify = notify.Default()
	st.Projects[0].Agents = []*state.Agent{{ID: "bg", Kind: "claude"}}
	m.rebuildRows()
	byMad := false
	old := startedByMad
	startedByMad = func(string, time.Time) bool { return byMad }
	t.Cleanup(func() { startedByMad = old })

	t0 := time.Now()
	poll := func(sec int, state string) []alert {
		now := t0.Add(time.Duration(sec) * time.Second)
		return m.applyPoll(pollMsg{
			panes:   []tmux.Pane{{ID: "%1", MadID: "bg", Session: tmux.PoolSession, Index: -1}},
			screens: map[string]string{},
			hooks:   map[string]*status.Hook{"bg": {State: state, At: now}},
		}, now)
	}
	turn := func(reply string, startSec, endSec int) []alert {
		poll(startSec, status.Running)
		if reply != "" {
			if err := status.WriteTurn("bg", &status.Turn{Reply: reply}, t0.Add(time.Duration(endSec)*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		return poll(endSec, status.Idle)
	}

	poll(0, status.Idle)
	as := turn("\n  Tests pass,   PR #42 opened\nmore detail", 1, 10)
	if len(as) != 1 || as[0].Kind != notify.Done || as[0].Message != "Tests pass, PR #42 opened" {
		t.Fatalf("done alert = %+v", as)
	}
	if got := as[0].Body(); got != "claude: Tests pass, PR #42 opened" {
		t.Errorf("body = %q", got)
	}

	// No reply known for this turn: today's wording.
	status.RemoveHook("bg")
	as = turn("", 40, 50)
	if len(as) != 1 || as[0].Message != "" || !strings.HasSuffix(as[0].Body(), "finished") {
		t.Errorf("no reply: %+v", as)
	}

	// A turn mad started by itself pushes nothing; waiting still does.
	byMad = true
	if as := turn("busy", 80, 90); len(as) != 0 {
		t.Errorf("bus-started turn pushed: %+v", as)
	}
	poll(100, status.Running)
	if as := poll(101, status.Waiting); len(as) != 1 || as[0].Kind != notify.Waiting {
		t.Errorf("waiting alerts = %+v", as)
	}
}

func TestReadmeSaysWhichFinishedPushesCome(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "comes only for a turn you started") {
		t.Error("README's Notifications section lost the finished-push rule")
	}
}

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

func TestLeaveNote(t *testing.T) {
	agents := []leaving{
		{name: "mad/fix the sidebar", status: status.Running, sleeps: true},
		{name: "mad/claude#2", status: status.Waiting, sleeps: true},
		{name: "mad/claude#3", status: status.Idle, sleeps: true, busy: "node"},
		{name: "mad/claude#4", status: status.Idle, sleeps: true},
		{name: "web/codex", status: status.Idle, sleeps: true},
		{name: "web/shell", status: status.Idle},
		{name: "web/claude", status: status.Asleep, sleeps: true},
		{name: "web/gone", status: status.Exited, sleeps: true},
	}
	got := leaveNote(agents, time.Hour)
	for _, want := range []string{
		"working          mad/fix the sidebar\n",
		"waiting for you  mad/claude#2\n",
		"kept awake by    mad/claude#3 (node)\n",
		"idle             2, asleep after 1h idle\n",
		"can't sleep      web/shell\n",
		"asleep           1\n",
		"mad kill-server",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("note lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gone") {
		t.Errorf("an exited agent is not running:\n%s", got)
	}
	if got := leaveNote(agents[3:4], 0); !strings.Contains(got, "1, never asleep (sleep.after is 0)") {
		t.Errorf("sleep off:\n%s", got)
	}

	var many []leaving
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		many = append(many, leaving{name: n, status: status.Running})
	}
	if got := leaveNote(many, time.Hour); !strings.Contains(got, "a, b, c, d, +2\n") {
		t.Errorf("six working:\n%s", got)
	}
	if got := leaveNote(nil, time.Hour); strings.Count(got, "\n") != 2 {
		t.Errorf("no agents:\n%s", got)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Hour: "1h", 90 * time.Minute: "1h30m", 45 * time.Minute: "45m",
		30 * time.Second: "30s", time.Hour + 30*time.Second: "1h0m30s",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

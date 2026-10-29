package ui

import (
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

func TestDispatchQueue(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{
		{ID: "busy", Kind: "claude"}, {ID: "free", Kind: "claude"},
		{ID: "role", Kind: "claude", Run: "r1"}, {ID: "asks", Kind: "claude"},
	}
	m.rebuildRows()
	set := func(id, s string) { m.trackers[id] = &status.Tracker{Status: s} }
	set("busy", status.Running)
	set("free", status.Idle)
	set("role", status.Idle)
	set("asks", status.Waiting)
	now := time.Now()
	for _, id := range []string{"busy", "free", "role", "asks"} {
		if _, err := queue.Push(id, "do "+id, now); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = queue.Push("free", "then this", now)

	var sent []string
	refuse := false
	old := sendQueued
	t.Cleanup(func() { sendQueued = old })
	sendQueued = func(_ *state.State, a *state.Agent, text string, _ []agent.Kind) error {
		if refuse {
			return drive.ErrBusy
		}
		sent = append(sent, a.ID+": "+text)
		return nil
	}
	run := func(at time.Time) bool {
		cmd := m.dispatchCmd(at)
		if cmd == nil {
			return false
		}
		m.applyDispatched(cmd().(dispatchedMsg))
		return true
	}

	// Only the idle agent outside a run gets its first message; the next
	// one waits for its status to catch up.
	if !run(now) || len(sent) != 1 || sent[0] != "free: do free" {
		t.Fatalf("sent = %q", sent)
	}
	if run(now.Add(time.Second)) {
		t.Errorf("sent again before the agent's status caught up: %q", sent)
	}
	if m.queued["free"] != 1 {
		t.Errorf("queued = %v", m.queued)
	}

	// Refused: the message goes back to the front, with why, and is
	// offered again later.
	refuse = true
	later := now.Add(queueSettle + time.Second)
	if !run(later) {
		t.Fatal("not offered after settling")
	}
	q, _ := queue.Load("free")
	if len(q.Items) != 1 || q.Items[0].Text != "then this" || q.Error == "" {
		t.Errorf("after a refusal: %+v", q)
	}
	if run(later.Add(time.Second)) {
		t.Error("retried at once")
	}
	refuse = false
	if !run(later.Add(queueRetry+time.Second)) || sent[len(sent)-1] != "free: then this" {
		t.Errorf("sent = %q", sent)
	}
	q, _ = queue.Load("free")
	if len(q.Items) != 0 || q.Error != "" {
		t.Errorf("after sending all: %+v", q)
	}

	// Its turn over, the busy one gets its own.
	set("busy", status.Idle)
	if !run(later.Add(time.Hour)) || sent[len(sent)-1] != "busy: do busy" {
		t.Errorf("sent = %q", sent)
	}
	for _, id := range []string{"role", "asks"} {
		if q, _ := queue.Load(id); len(q.Items) != 1 {
			t.Errorf("%s's queue = %+v", id, q)
		}
	}
}

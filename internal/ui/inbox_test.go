package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// A narrow sidebar cuts the end of an inbox row: it is the context that
// goes, not the command the person is asked to allow.
func TestInboxRowKeepsTheCommandWhenNarrow(t *testing.T) {
	m, st := busDeck(t)
	now := time.Now()
	panes := []tmux.Pane{{ID: "%6", MadID: idBuild, Session: tmux.PoolSession, Index: -1}}
	reports := map[string]status.Report{
		idBuild: {Hook: &status.Hook{State: status.Waiting, At: now.Add(-time.Minute)},
			Asks: []status.Ask{{ID: "r1", Kind: status.AskPermission, Tool: "shell", Command: "go test ./internal/attention/... && make clean install", At: now.Add(-time.Minute)}}},
	}
	m.applyPoll(pollMsg{panes: panes, screens: map[string]string{idBuild: "y"}, reports: reports}, now)
	look(m, st)
	rowAt := func(w int) string {
		m.width = w
		m.rebuildRows()
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, "builder") && strings.Contains(l, "1m") {
				return l
			}
		}
		return ""
	}
	if l := rowAt(32); !strings.Contains(l, "builder · go test") {
		t.Errorf("narrow row lost the command: %q", l)
	}
	if l := rowAt(60); !strings.Contains(l, "builder  shell · go test") {
		t.Errorf("wide row changed: %q", l)
	}
}

// The deck's block lists the inbox the window has for the same facts: a
// waiting agent with no task, a host's request, an @you, a pending skill;
// its count and rows in the window's order. d walks them in that order.
func TestInboxBlockAgreesWithTheWindow(t *testing.T) {
	m, st := busDeck(t)
	now := time.Now()
	mkChannel(t, "ops")
	say(t, "ops", "@you should the release go out today or wait")
	if _, err := skill.Save("architect", "/p/one", "project", "deploy", "When deploying", "ship it", false); err != nil {
		t.Fatal(err)
	}
	// The architect waits in its TUI (its hook says so); the builder's
	// host holds a request open.
	panes := []tmux.Pane{{ID: "%5", MadID: idArch, Session: tmux.PoolSession, Index: -1}, {ID: "%6", MadID: idBuild, Session: tmux.PoolSession, Index: -1}}
	reports := map[string]status.Report{
		idArch: {Hook: &status.Hook{State: status.Waiting, Message: "Claude needs your permission to use Bash", At: now.Add(-3 * time.Minute)}},
		idBuild: {Hook: &status.Hook{State: status.Waiting, At: now.Add(-time.Minute)},
			Asks: []status.Ask{{ID: "r1", Kind: status.AskPermission, Tool: "shell", Command: "make build", At: now.Add(-time.Minute)}}},
	}
	m.applyPoll(pollMsg{panes: panes, screens: map[string]string{idArch: "x", idBuild: "y"}, reports: reports}, now)
	look(m, st)

	// The window's poller makes the same call over the same facts.
	var agents []attention.Agent
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			agents = append(agents, attention.AgentOf(p, a, m.trackers[a.ID], m.panes[a.ID].ID, reports[a.ID], m.transcripts[a.ID]))
		}
	}
	var src bus.Source
	l, _ := src.Look(st, attention.Statuses(agents), now)
	pending, _ := skill.PendingList()
	window := attention.NewDesk().Look(attention.Facts{Agents: agents, Cards: l.Cards, Skills: pending, Spend: m.spend, Now: now}).Inbox

	if window.Needs != 4 || m.view.Inbox.Needs != window.Needs {
		t.Fatalf("needs: deck %d, window %d", m.view.Inbox.Needs, window.Needs)
	}
	names := busNames(m)
	want := []string{"? Needs you 4"}
	for i, it := range window.NeedsYou() {
		if m.view.Inbox.Items[i].Key != it.Key {
			t.Errorf("item %d: deck %s, window %s", i, m.view.Inbox.Items[i].Key, it.Key)
		}
		want = append(want, it.Title)
	}
	for i, w := range want {
		if i >= len(names) || !strings.HasPrefix(names[i], w) {
			t.Fatalf("block %q, want rows led by %q", names, want)
		}
	}
	if !strings.HasPrefix(names[5], "▤ Board 4 for you") {
		t.Errorf("board row %q", names[5])
	}
	if h := strings.Split(m.View(), "\n")[0]; !strings.Contains(h, "?4") {
		t.Errorf("header %q", h)
	}
	// The architect, waiting longest and blocking, comes first: the window
	// (claim 2) and the deck both have it, with no task.
	if it := m.view.Inbox.Items[0]; it.Agent != idArch || it.Kind != attention.KindPermission || !strings.Contains(it.Text, "permission to use Bash") {
		t.Errorf("first %+v", it)
	}

	// d goes down the inbox: the architect, the builder, the ask's chat,
	// the skills.
	var opened []string
	for range 4 {
		m.jumpNext()
		opened = append(opened, string(m.nextKey))
	}
	for i, it := range window.NeedsYou() {
		if opened[i] != string(it.Key) {
			t.Errorf("d %d went to %s, want %s", i, opened[i], it.Key)
		}
	}
	// The ask and the skill wait alike, by time (the skill's to the
	// second): the last opened says where it went.
	tag := "skills"
	if last := window.NeedsYou()[3]; last.Open.Channel != "" {
		tag = "chat:" + last.Open.Channel
	}
	if m.taskTag != tag {
		t.Errorf("the last d opened %q, want %q", m.taskTag, tag)
	}
}

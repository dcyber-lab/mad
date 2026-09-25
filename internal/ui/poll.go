package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// gitStatus, addWorktree, validBranch and defaultBranch are replaceable
// in tests.
var (
	gitStatus     = git.Status
	addWorktree   = git.AddWorktree
	validBranch   = git.ValidBranch
	defaultBranch = git.DefaultBranch
)

// gitCmd reads the checkout of every project and agent directory.
func (m *model) gitCmd() tea.Cmd {
	m.gitScanning, m.gitDue, m.lastGit = true, false, time.Now()
	dirs := map[string]bool{}
	for _, p := range m.st.Projects {
		dirs[p.Path] = true
		for _, a := range p.Agents {
			dirs[p.Dir(a)] = true
		}
	}
	return func() tea.Msg {
		out := gitMsg{}
		for d := range dirs {
			if info, ok := gitStatus(d); ok {
				out[d] = info
			}
		}
		return out
	}
}

// readCmd brings what is known of every agent's session up to date from
// its transcript. The reader keeps where each file was left off, so only what
// an agent wrote since the last read is parsed.
func (m *model) readCmd() tea.Cmd {
	m.reading, m.readDue, m.lastRead = true, false, time.Now()
	var agents []transcript.Agent
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			sid := a.SessionID
			if sid == "" {
				sid = a.ID
			}
			agents = append(agents, transcript.Agent{ID: a.ID, Kind: a.Kind, Dir: p.Dir(a), Session: sid})
		}
	}
	r := m.reader
	return func() tea.Msg { return transcriptMsg(r.Read(agents)) }
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// scanCmd looks for agent sessions outside the deck.
func (m *model) scanCmd() tea.Cmd {
	m.scanning, m.lastScan = true, time.Now()
	return func() tea.Msg {
		deckTTYs := map[string]bool{}
		if panes, err := tmux.ListPanes(); err == nil {
			for _, p := range panes {
				deckTTYs[p.TTY] = true
			}
		}
		return externalsMsg(discover.ScanExternal(deckTTYs))
	}
}

// pollCmd gathers tmux panes, screens and hook reports off the UI
// goroutine.
func (m *model) pollCmd() tea.Cmd {
	m.polling, m.lastFull, m.fullDue = true, time.Now(), false
	var ids []string
	for _, a := range m.st.OrderedAgents() {
		ids = append(ids, a.ID)
	}
	epoch := m.epoch
	var kinds []string
	for _, k := range m.kinds {
		kinds = append(kinds, k.Name)
	}
	return func() tea.Msg {
		panes, err := tmux.ListPanes()
		if err != nil {
			return pollMsg{err: err, epoch: epoch}
		}
		_ = deck.EnsureStage(panes) // no-op unless the stage pane went away
		msg := pollMsg{panes: panes, screens: map[string]string{}, hooks: map[string]*status.Hook{}, quota: map[string]status.Quota{}, epoch: epoch}
		for _, k := range kinds {
			if q, ok := status.ReadQuota(k); ok {
				msg.quota[k] = q
			}
		}
		// One tmux call for every screen: a call per agent costs a few ms
		// each and adds up to the whole poll interval with many agents.
		byPane := map[string]string{}
		var paneIDs []string
		for _, p := range panes {
			if tmux.IsAgentID(p.MadID) && !p.Dead {
				byPane[p.ID] = p.MadID
				paneIDs = append(paneIDs, p.ID)
			}
		}
		screens, err := tmux.CaptureAll(paneIDs)
		for _, pid := range paneIDs {
			if err != nil { // a pane went away mid-poll
				msg.screens[byPane[pid]] = tmux.Capture(pid)
			} else {
				msg.screens[byPane[pid]] = screens[pid]
			}
		}
		for _, id := range ids {
			msg.hooks[id] = status.ReadHook(id)
		}
		return msg
	}
}

// pollNow asks for a full poll after panes changed. Whatever poll is in
// flight began before the change: a full one is dropped as stale (epoch)
// and redone, a screen capture is followed by a full poll (fullDue).
func (m *model) pollNow() tea.Cmd {
	m.epoch++
	if m.polling {
		m.fullDue = true
		return nil
	}
	return m.pollCmd()
}

// screenAgents maps the live panes of agents without hooks, whose status
// can only be read off their screens, to their agent ids.
func (m *model) screenAgents() map[string]string {
	out := map[string]string{}
	for _, a := range m.st.OrderedAgents() {
		if agent.ByName(m.kinds, a.Kind).Hooks {
			continue
		}
		if p, ok := m.panes[a.ID]; ok && !p.Dead {
			out[p.ID] = a.ID
		}
	}
	return out
}

// screensCmd captures just the agents without hooks, in one tmux call.
func (m *model) screensCmd(byPane map[string]string) tea.Cmd {
	m.polling = true
	return func() tea.Msg {
		var ids []string
		for pid := range byPane {
			ids = append(ids, pid)
		}
		got, err := tmux.CaptureAll(ids)
		if err != nil {
			return screensMsg{err: err}
		}
		msg := screensMsg{screens: map[string]string{}}
		for pid, screen := range got {
			msg.screens[byPane[pid]] = screen
		}
		return msg
	}
}

// applyPoll takes in a full poll: panes, stage, every screen and hook.
func (m *model) applyPoll(msg pollMsg, now time.Time) []alert {
	if msg.err != nil {
		m.setFlash(msg.err.Error())
		return nil
	}
	m.reloadConfig()
	// Pick up `mad add` and other writers.
	if mod := state.ModTime(); mod.After(m.stMod) {
		if st, err := state.Load(); err == nil {
			m.st, m.stMod = st, mod
			m.rebuildRows()
		}
	}

	m.panes = map[string]tmux.Pane{}
	prevStage := m.stageID
	m.stageID = ""
	for _, p := range msg.panes {
		if p.MadID != "" {
			m.panes[p.MadID] = p
		}
		if p.Session == tmux.MainSession && p.Index == 1 {
			m.stageID = p.MadID
		}
		if p.Session == tmux.MainSession && p.Index == 0 {
			m.focused = p.Active
		}
	}
	if m.stageID != prevStage && tmux.IsAgentID(m.stageID) {
		m.selectAgent(m.stageID)
	}
	m.screens, m.hooks = msg.screens, msg.hooks
	for k, q := range msg.quota {
		m.noteQuota(k, q)
	}
	return m.observe(nil, now)
}

// noteQuota keeps the latest report of a kind's usage limits.
func (m *model) noteQuota(kind string, q status.Quota) {
	if q.At.IsZero() || !q.At.After(m.quota[kind].At) {
		return
	}
	m.quota[kind] = q
}

// alert is a notification to send; one about the agent on stage is
// dropped if someone is looking at the deck when it goes out.
type alert struct {
	notify.Event
	onStage bool
}

// observe feeds the latest pane, screen and hook of the agents in only
// (all when nil) to their trackers, and returns the notifications the
// status changes call for.
func (m *model) observe(only map[string]bool, now time.Time) []alert {
	dirty := false
	var alerts []alert
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			if only != nil && !only[a.ID] {
				continue
			}
			tr := m.trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{}
				m.trackers[a.ID] = tr
			}
			hook := m.hooks[a.ID]
			pane, ok := m.panes[a.ID]
			prev := tr.Status
			tr.Observe(agent.ByName(m.kinds, a.Kind), pane, ok, hook, m.screens[a.ID], a.ID == m.stageID, now)
			if prev == status.Running && tr.Status != status.Running {
				m.gitDue, m.readDue = true, true // a turn ended: its changes and cost are worth showing now
			}
			if e, ok := m.event(p, a, prev, tr.Status, hook, now); ok {
				alerts = append(alerts, alert{e, a.ID == m.stageID})
			}
			if hook != nil && hook.SessionID != "" && hook.SessionID != a.SessionID {
				// A forked resume reports its new id here; later resumes use it.
				a.SessionID, a.Fork = hook.SessionID, false
				dirty = true
			}
		}
	}
	if dirty {
		m.save()
	}
	return alerts
}

const (
	// notifyMinRun: a shorter run (a redraw, a quick reply) isn't worth a
	// notification.
	notifyMinRun = 5 * time.Second
	// notifyCooldown keeps a flapping status from repeating itself.
	notifyCooldown = 15 * time.Second
)

// event turns a status change into a notification: a run that ended, or
// an agent that started waiting for you.
func (m *model) event(p *state.Project, a *state.Agent, prev, cur string, hook *status.Hook, now time.Time) (notify.Event, bool) {
	if cur == status.Running && prev != status.Running {
		m.runSince[a.ID] = now
	}
	var kind string
	switch {
	case prev == status.Running && cur == status.Idle && now.Sub(m.runSince[a.ID]) >= notifyMinRun:
		kind = notify.Done
	case prev != "" && prev != status.Waiting && cur == status.Waiting:
		kind = notify.Waiting // prev "": already waiting when the sidebar started
	default:
		return notify.Event{}, false
	}
	key := a.ID + "/" + kind
	if !m.cfg.Notify.Wants(kind) || now.Sub(m.notified[key]) < notifyCooldown {
		return notify.Event{}, false
	}
	m.notified[key] = now
	e := notify.Event{Kind: kind, Project: p.Name, Agent: p.DisplayName(a)}
	if kind == notify.Waiting && hook != nil && hook.State == status.Waiting {
		e.Message = hook.Message
	}
	return e, true
}

// watched is replaceable in tests.
var watched = tmux.Watched

func (m *model) notifyCmd(alerts []alert) tea.Cmd {
	if len(alerts) == 0 {
		return nil
	}
	cfg := m.cfg.Notify
	return func() tea.Msg {
		for _, a := range alerts {
			if a.onStage && watched() {
				continue // it's in front of you
			}
			_ = notify.Send(cfg, a.Event) // best effort: no notifier is not an error
		}
		return nil
	}
}

// usableDir is replaceable in tests (temp dirs don't count as projects).
var usableDir = discover.UsableDir

// applyExternals records outside sessions and auto-adds their projects.
func (m *model) applyExternals(ext []discover.External) {
	m.externals = ext
	var added []string
	for _, e := range ext {
		if m.st.FindProject(e.Root) != nil || m.st.IsIgnored(e.Root) || !usableDir(e.Root) {
			continue
		}
		p, _ := m.st.AddProject(e.Root)
		added = append(added, p.Name)
	}
	if len(added) > 0 {
		m.save()
		m.setFlash("synced: " + strings.Join(added, ", "))
	}
	m.rebuildRows()
}

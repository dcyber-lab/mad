package desktop

import (
	"context"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// Snapshot is what the window's sidebar draws.
type Snapshot struct {
	Projects []Project      `json:"projects"`
	Quota    map[string]any `json:"quota,omitempty"` // by kind: claude, codex
	Bus      []BusChannel   `json:"bus"`             // the channels and DMs (bus.go)
	Board    []bus.Card     `json:"board"`           // the task board (board.go)
	Spend    bus.SpendState `json:"spend"`           // the day's budget for mad's own work
	At       time.Time      `json:"at"`
}

type Project struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Branch  string  `json:"branch,omitempty"`
	Changed int     `json:"changed"`
	Ahead   int     `json:"ahead"`
	Agents  []Agent `json:"agents"`
}

type Agent struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Icon   string `json:"icon,omitempty"`
	Color  string `json:"color,omitempty"`
	Name   string `json:"name,omitempty"`  // the user's
	Title  string `json:"title,omitempty"` // the conversation's
	Detail string `json:"detail,omitempty"`
	Status string `json:"status"`
	// Done: it finished while no window was looking at it.
	Done bool    `json:"done,omitempty"`
	Cost float64 `json:"cost,omitempty"`
	Role string  `json:"role,omitempty"`
	// Queue is what waits for its turn to end (package queue).
	Queue *queue.Queue `json:"queue,omitempty"`
	// Chat: its transcript reads as a conversation (/api/chat).
	Chat bool `json:"chat,omitempty"`
	// Headless: it can run in window mode; Mode is "window" when it does
	// (package host).
	Headless bool   `json:"headless,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

// How often each part of the snapshot is brought up to date. Screens are
// what status inference needs fresh; the rest changes slowly.
const (
	screenEvery     = time.Second
	transcriptEvery = 3 * time.Second
	gitEvery        = 5 * time.Second
)

type poller struct {
	kinds []agent.Kind
	looks *looks

	mu   sync.Mutex
	snap Snapshot

	// Owned by Run's goroutine.
	trackers map[string]*status.Tracker
	reader   *transcript.Reader
	infos    map[string]transcript.Info
	gits     map[string]git.Info
	lastRead time.Time
	lastGit  time.Time
}

func newPoller(kinds []agent.Kind, l *looks) *poller {
	return &poller{
		kinds: kinds, looks: l,
		trackers: map[string]*status.Tracker{},
		reader:   transcript.NewReader(),
		infos:    map[string]transcript.Info{},
		gits:     map[string]git.Info{},
		snap:     Snapshot{Projects: []Project{}},
	}
}

func (p *poller) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap
}

func (p *poller) Run(ctx context.Context) {
	tick := time.NewTicker(screenEvery)
	defer tick.Stop()
	for {
		p.Poll(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Poll takes one look at the state, the panes and, when due, the
// transcripts and checkouts.
func (p *poller) Poll(now time.Time) {
	st, err := state.Load()
	if err != nil {
		return // a half-written file; the next look gets it
	}
	panes, _ := tmux.ListPanes() // no server: every agent is stopped
	var live []string
	for _, a := range st.OrderedAgents() {
		if pn, ok := tmux.FindPane(panes, a.ID); ok && !pn.Dead {
			live = append(live, pn.ID)
		}
	}
	screens, err := tmux.CaptureAll(live)
	if err != nil {
		screens = map[string]string{}
	}

	if now.Sub(p.lastRead) >= transcriptEvery {
		p.lastRead = now
		var ts []transcript.Agent
		for _, pr := range st.Projects {
			for _, a := range pr.Agents {
				sid := a.SessionID
				if sid == "" {
					sid = a.ID
				}
				ts = append(ts, transcript.Agent{ID: a.ID, Kind: a.Kind, Dir: pr.Dir(a), Session: sid})
			}
		}
		p.infos = p.reader.Read(ts)
	}
	if now.Sub(p.lastGit) >= gitEvery {
		p.lastGit = now
		gits := map[string]git.Info{}
		for _, pr := range st.Projects {
			if info, ok := git.Status(pr.Path); ok {
				gits[pr.Path] = info
			}
		}
		p.gits = gits
	}

	queues, _ := queue.All()
	snap := Snapshot{Projects: []Project{}, At: now, Quota: map[string]any{}}
	seen := map[string]bool{}
	for _, pr := range st.Projects {
		g := p.gits[pr.Path]
		out := Project{Name: pr.Name, Path: pr.Path, Branch: g.Branch, Changed: g.Dirty, Ahead: g.Ahead, Agents: []Agent{}}
		for _, a := range pr.Agents {
			seen[a.ID] = true
			k := agent.ByName(p.kinds, a.Kind)
			tr := p.trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{}
				p.trackers[a.ID] = tr
			}
			pn, ok := tmux.FindPane(panes, a.ID)
			// A window looking at the agent counts as it being on stage:
			// what finished there was seen.
			s := tr.Observe(k, pn, ok, status.ReadHook(a.ID), screens[pn.ID], p.looks.Focused(a.ID), now)
			info := p.infos[a.ID]
			detail := info.Tool
			if detail == "" {
				detail = info.Prompt
			}
			ag := Agent{
				ID: a.ID, Kind: a.Kind, Icon: k.Icon, Color: k.Color,
				Name: a.Name, Title: info.Title, Detail: detail,
				Status: s, Done: tr.Done(), Cost: info.Tokens.Cost, Role: a.Role,
			}
			if q, ok := queues[a.ID]; ok {
				ag.Queue = &q
			}
			_, ag.Chat = discover.Conversation(a.Kind)
			ag.Headless = deck.Headless[a.Kind]
			// In window mode once its TUI, if it runs, gave way.
			if deck.Windowed(a) && (!ok || pn.Dead || deck.RunsHost(pn, a.ID)) {
				ag.Mode = state.ModeWindow
			}
			out.Agents = append(out.Agents, ag)
		}
		snap.Projects = append(snap.Projects, out)
	}
	for id := range p.trackers {
		if !seen[id] {
			delete(p.trackers, id)
		}
	}
	for _, kind := range []string{"claude", "codex"} {
		if q, ok := status.ReadQuota(kind); ok {
			snap.Quota[kind] = q.Expire(now)
		}
	}
	snap.Bus = busChannels(st)
	snap.Spend = bus.SpendNow(now)
	snap.Board = boardCards(st, snap)

	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
}

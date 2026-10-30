package desktop

import (
	"context"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/skill"
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
	// Board is the task board as the page draws it today, with skill and
	// budget cards in "Waiting for you" (board.go).
	// TODO(batch 2b): the page draws that column from Inbox; Board goes
	// then, and BoardRest takes its name.
	Board []bus.Card `json:"board"`
	// BoardRest is the board without its "Waiting for you" column: that
	// column is Inbox.
	BoardRest []bus.Card     `json:"board_rest"`
	Spend     bus.SpendState `json:"spend"`  // the day's budget for mad's own work
	Timers    []bus.Timer    `json:"timers"` // check-ins and routines (timer.go)
	// Inbox is what waits on the person, in Next's order: every "needs
	// you" on every surface reads it.
	Inbox attention.Inbox `json:"inbox"`
	// Working are the ids of the agents at work, longest in it first.
	Working []string `json:"working"`
	// Teams roll their members up.
	Teams []Team `json:"teams"`
	// States is how each shown state reads: the page draws every status
	// word, dot and color from it, as the deck does.
	States []status.Look `json:"states"`
	At     time.Time     `json:"at"`
}

type Project struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Branch  string  `json:"branch,omitempty"`
	Changed int     `json:"changed"`
	Ahead   int     `json:"ahead"`
	Agents  []Agent `json:"agents"`
	// Rollup is its agents' shown states counted, most urgent first.
	Rollup status.Rollup `json:"rollup"`
}

// Team is a team's agents in a project, and how they stand.
type Team struct {
	Name    string        `json:"name"`
	Project string        `json:"project"`
	Members []string      `json:"members"` // agent ids, in the deck's order
	Rollup  status.Rollup `json:"rollup"`
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
	// Shown is the state to draw (status.Shown): Status, with a finished
	// unseen run told apart and Exited folded into Stopped.
	Shown string `json:"shown"`
	// Done: it finished while no window was looking at it.
	Done bool    `json:"done,omitempty"`
	Cost float64 `json:"cost,omitempty"`
	Role string  `json:"role,omitempty"`
	// Team: it is a member of that team, in Role (package team).
	Team string `json:"team,omitempty"`
	// Queue is what waits for its turn to end (package queue).
	Queue *queue.Queue `json:"queue,omitempty"`
	// Chat: its transcript reads as a conversation (/api/chat).
	Chat bool `json:"chat,omitempty"`
	// Headless: it can run in window mode; Mode is "window" when it does
	// (package host).
	Headless bool   `json:"headless,omitempty"`
	Mode     string `json:"mode,omitempty"`
	// Activity is its activity line, its fields in the entry: since,
	// tool, target, todo, reply.
	attention.Activity
}

// detailOf is the line under an agent's name: the tool it calls while it
// works or waits, else the prompt it is on. The tool stays in the
// transcript after its turn ends, so it would mislead then.
func detailOf(s string, info transcript.Info) string {
	if (s == status.Running || s == status.Waiting) && info.Tool != "" {
		return info.Tool
	}
	return info.Prompt
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
	// desk is the inbox, shared with the server, which acts through it.
	desk *attention.Desk

	mu   sync.Mutex
	snap Snapshot

	// Owned by Run's goroutine.
	trackers map[string]*status.Tracker
	reader   *transcript.Reader
	infos    map[string]transcript.Info
	gits     map[string]git.Info
	bus      bus.Source
	lastRead time.Time
	lastGit  time.Time
}

func newPoller(kinds []agent.Kind, l *looks) *poller {
	return &poller{
		kinds: kinds, looks: l, desk: attention.NewDesk(),
		trackers: map[string]*status.Tracker{},
		reader:   transcript.NewReader(),
		infos:    map[string]transcript.Info{},
		gits:     map[string]git.Info{},
		snap:     Snapshot{Projects: []Project{}, States: status.Looks},
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
	snap := Snapshot{Projects: []Project{}, At: now, Quota: map[string]any{}, States: status.Looks}
	seen := map[string]bool{}
	var facts []attention.Agent
	for _, pr := range st.Projects {
		g := p.gits[pr.Path]
		out := Project{Name: pr.Name, Path: pr.Path, Branch: g.Branch, Changed: g.Dirty, Ahead: g.Ahead, Agents: []Agent{}}
		for _, a := range pr.Agents {
			seen[a.ID] = true
			k := agent.ByName(p.kinds, a.Kind)
			tr := p.trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{ID: a.ID}
				p.trackers[a.ID] = tr
			}
			pn, ok := tmux.FindPane(panes, a.ID)
			rep := status.ReadReport(a.ID)
			// A window looking at the agent, or the deck's stage, counts
			// as it being seen: what finished there was.
			s := tr.Observe(k, pn, ok, rep.Hook, screens[pn.ID], p.looks.Focused(a.ID) || ok && tmux.Seen(pn), now)
			info := p.infos[a.ID]
			facts = append(facts, attention.AgentOf(pr, a, tr, pn.ID, rep, info))
			detail := detailOf(s, info)
			ag := Agent{
				ID: a.ID, Kind: a.Kind, Icon: k.Icon, Color: k.Color,
				Name: a.Name, Title: info.Title, Detail: detail,
				Status: s, Shown: status.Shown(s, tr.Done()), Done: tr.Done(), Cost: info.Tokens.Cost, Role: a.Role, Team: a.Team,
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
	// The bus is read again only when it moved (bus.Source).
	look, _ := p.bus.Look(st, attention.Statuses(facts), now)
	pending, _ := skill.PendingList()
	snap.Bus = busChannels(st, look)
	snap.Spend = bus.SpendNow(now)
	snap.Timers = timersOf()
	snap.Board = boardCards(look.Cards, pending, snap.Spend, now)
	view := p.desk.Look(attention.Facts{Agents: facts, Cards: look.Cards, Skills: pending, Spend: snap.Spend, Now: now})
	snap.Inbox, snap.Working, snap.BoardRest = view.Inbox, view.Working, view.Board
	if snap.BoardRest == nil {
		snap.BoardRest = []bus.Card{}
	}
	snap.Teams = rollUp(snap.Projects, view)

	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
}

// rollUp gives each agent its activity line and each project its roll-up,
// and returns the teams', in the deck's order.
func rollUp(projects []Project, view attention.View) []Team {
	teams := []Team{}
	for i := range projects {
		pr := &projects[i]
		var shown []string
		members := map[string][]string{} // team → shown states
		at := map[string]int{}           // team → index in teams
		for j := range pr.Agents {
			ag := &pr.Agents[j]
			ag.Activity = view.Activity[ag.ID]
			shown = append(shown, ag.Shown)
			if ag.Team == "" {
				continue
			}
			k, ok := at[ag.Team]
			if !ok {
				k = len(teams)
				at[ag.Team] = k
				teams = append(teams, Team{Name: ag.Team, Project: pr.Name})
			}
			teams[k].Members = append(teams[k].Members, ag.ID)
			members[ag.Team] = append(members[ag.Team], ag.Shown)
		}
		pr.Rollup = status.Roll(shown)
		for name, k := range at {
			teams[k].Rollup = status.Roll(members[name])
		}
	}
	return teams
}

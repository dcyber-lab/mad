// Package ui is the sidebar: a Bubble Tea program that runs in the left
// tmux pane, shows projects and agents with live status, and drives the
// deck (open, create, resume, kill agents).
package ui

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// Status comes from events where there are any: agents' own hooks and
// tmux's pane-died reach the sidebar through poke at once. Polling covers
// the rest: every tick captures the screens of agents without hooks (their
// status is read off the screen), and a full poll every few seconds
// re-reads everything as a safety net for missed events.
const (
	pollInterval        = 500 * time.Millisecond
	fullPollEvery       = 3 * time.Second
	externalScanEvery   = 5 * time.Second
	gitScanEvery        = 5 * time.Second
	transcriptScanEvery = 5 * time.Second
	headerLines         = 2
	footerLines         = 4
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type (
	tickMsg time.Time
	pollMsg struct {
		panes   []tmux.Pane
		screens map[string]string
		hooks   map[string]*status.Hook
		quota   map[string]status.Quota // kind → limits reported through `mad hook`
		epoch   int                     // model.epoch when the poll started
		err     error
	}
	// screensMsg is a tick's capture of the agents without hooks.
	screensMsg struct {
		screens map[string]string // agent id → screen
		err     error             // a pane went away: time for a full poll
	}
	// externalsMsg carries a scan for sessions outside the deck. It runs
	// apart from pollMsg: the first scan can take seconds (lsof per
	// process) and must not hold up status updates.
	externalsMsg []discover.External
	// gitMsg is a scan of every project and worktree: dir → checkout info.
	gitMsg map[string]git.Info
	// transcriptMsg is a read of every agent's transcript: agent id → what it says.
	transcriptMsg map[string]transcript.Info
	// widthSettledMsg fires a moment after a resize; if the width is still
	// the same then, it was deliberate (drag, </>) and gets saved.
	widthSettledMsg struct{ width int }
	// pokeMsg is a command from another mad process: poke.Poll, poke.Jump,
	// or poke.Hook plus an agent id.
	pokeMsg string
	doneMsg struct {
		err      error
		selectID string // agent id to put the cursor on
		failedID string // agent that never started (its setup failed): drop it
	}
)

type mode int

const (
	modeNormal mode = iota
	modeAddProject
	modePickKind
	modePickSession
	modeConfirm
	modeWorktree   // naming the branch for a new worktree
	modeRename     // naming an agent
	modePickFinish // choosing how to wrap up a branch
)

// row is one sidebar line: a project, a deck agent, an agent running in
// another terminal (ext), or the "N in desktop" summary (desktop > 0) of
// conversations in a desktop app (deskKind's; so far only claude has one).
type row struct {
	proj     *state.Project
	agent    *state.Agent
	ext      *discover.External
	desktop  int
	deskKind string
	num      int // 1-based global agent number
}

func (r row) isProject() bool { return r.agent == nil && r.ext == nil && r.desktop == 0 }

type model struct {
	st       *state.State
	stMod    time.Time
	stErr    error // the state file as last seen doesn't parse: not saved over
	kinds    []agent.Kind
	trackers map[string]*status.Tracker
	panes    map[string]tmux.Pane
	hooks    map[string]*status.Hook // latest report per agent
	screens  map[string]string       // latest screen per agent
	lastFull time.Time               // when the last full poll started
	fullDue  bool                    // a full poll is wanted as soon as the one in flight lands
	stageID  string
	focused  bool
	polling  bool
	// epoch counts actions started and finished. A poll that began in
	// another epoch may predate a swap: it is dropped and redone, or the
	// cursor would jump back to the old stage for a moment.
	epoch    int
	scanning bool

	rows   []row
	lineOf []int // screen line of each row: projects after the first get a blank line above
	cursor int
	offset int // first visible screen line
	width  int
	height int
	frame  int

	mode       mode
	input      textinput.Model
	kindCursor int
	confirmMsg string
	confirmYes func() tea.Cmd

	flash      string
	flashUntil time.Time

	pk        picker
	sp        sessPicker
	externals []discover.External
	lastScan  time.Time

	gitInfo     map[string]git.Info // dir → branch and changes
	gitScanning bool
	gitDue      bool // scan at the next tick: an agent finished or panes changed
	lastGit     time.Time
	transcripts map[string]transcript.Info // agent id → what its transcript says
	quota       map[string]status.Quota    // kind → the account's usage limits, latest report
	reader      *transcript.Reader         // only the read command touches it
	reading     bool
	readDue     bool // read at the next tick: a turn ended
	lastRead    time.Time
	taskFor     string              // agent the diff view was opened for ("" for a project)
	wt          *state.Project      // project a worktree is being named for
	wtBranch    string              // branch chosen; the kind menu comes next
	renameID    string              // agent being named
	fin         []deck.FinishAction // the finish menu being shown
	finCursor   int
	finTitle    string
	finCo       deck.Checkout     // what the menu works on
	finID       string            // agent the menu is for ("" for a project)
	baseOf      map[string]string // repo → default branch, looked up once

	cfg      deck.Config
	cfgMod   time.Time            // config.json's mtime when cfg was read
	kindsMod time.Time            // agents.json's mtime when kinds were read
	runSince map[string]time.Time // agent id → when its current run started
	notified map[string]time.Time // agent id + event kind → last notification
}

// Run is `mad sidebar`. A panic is logged to the sidebar log and exits
// non-zero; the pane's restart loop then relaunches the sidebar.
func Run() error {
	st, err := state.Load()
	if err != nil {
		waitForState(os.Stdout, err, time.Second)
		return nil // the pane's loop starts the sidebar again
	}
	m := newModel(st, agent.Builtin())
	m.reloadConfig() // config.json, agents.json
	defer func() {
		if r := recover(); r != nil {
			if f, ferr := os.OpenFile(paths.SidebarLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
				fmt.Fprintf(f, "%s panic: %v\n%s\n", time.Now().Format(time.RFC3339), r, debug.Stack())
				f.Close()
			}
			os.Exit(1)
		}
	}()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithoutCatchPanics())
	// Without the socket everything still works, just a poll behind.
	if l, err := poke.Listen(func(cmd string) { p.Send(pokeMsg(cmd)) }); err == nil {
		defer l.Close()
	}
	_, err = p.Run()
	return err
}

// waitForState says in the sidebar's pane why the state can't be read,
// and returns once the file changes. Starting on an empty state instead
// would overwrite the file at the first save.
func waitForState(out io.Writer, err error, poll time.Duration) {
	fmt.Fprintf(out, "\x1b[2J\x1b[H mad can't read its state:\n\n %v\n\n %s\n\n Fix the file and the sidebar comes back.\n",
		err, paths.Short(paths.StateFile()))
	mod := state.ModTime()
	for state.ModTime().Equal(mod) {
		time.Sleep(poll)
	}
}

func newModel(st *state.State, kinds []agent.Kind) *model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 512
	m := &model{
		st:          st,
		stMod:       state.ModTime(),
		kinds:       kinds,
		trackers:    map[string]*status.Tracker{},
		panes:       map[string]tmux.Pane{},
		hooks:       map[string]*status.Hook{},
		screens:     map[string]string{},
		input:       ti,
		cfg:         deck.DefaultConfig(),
		baseOf:      map[string]string{},
		gitInfo:     map[string]git.Info{},
		transcripts: map[string]transcript.Info{},
		quota:       map[string]status.Quota{},
		reader:      transcript.NewReader(),
		runSince:    map[string]time.Time{},
		notified:    map[string]time.Time{},
	}
	m.rebuildRows()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.pollCmd(), m.scanCmd(), m.gitCmd(), m.readCmd(), tick())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - len(m.input.Prompt) - 2
		m.clampScroll()
		w := msg.Width
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return widthSettledMsg{w} })
	case widthSettledMsg:
		if msg.width == m.width && msg.width != deck.SidebarWidth() {
			if err := deck.SaveSidebarWidth(msg.width); err != nil {
				m.setFlash(err.Error())
			}
		}
	case tickMsg:
		m.frame++
		cmds := []tea.Cmd{tick()}
		if !m.polling {
			if m.fullDue || time.Since(m.lastFull) >= fullPollEvery {
				cmds = append(cmds, m.pollCmd())
			} else if byPane := m.screenAgents(); len(byPane) > 0 {
				cmds = append(cmds, m.screensCmd(byPane))
			}
		}
		if !m.scanning && time.Since(m.lastScan) > externalScanEvery {
			cmds = append(cmds, m.scanCmd())
		}
		if !m.gitScanning && (m.gitDue || time.Since(m.lastGit) > gitScanEvery) {
			cmds = append(cmds, m.gitCmd())
		}
		if !m.reading && (m.readDue || time.Since(m.lastRead) > transcriptScanEvery) {
			cmds = append(cmds, m.readCmd())
		}
		return m, tea.Batch(cmds...)
	case pollMsg:
		m.polling = false
		if msg.epoch != m.epoch {
			return m, m.pollCmd() // began before an action finished: stale
		}
		return m, tea.Batch(m.notifyCmd(m.applyPoll(msg, time.Now())), m.taskCleanup(msg.panes))
	case externalsMsg:
		m.scanning = false
		m.applyExternals(msg)
	case gitMsg:
		m.gitScanning, m.gitInfo = false, msg
	case transcriptMsg:
		m.reading, m.transcripts = false, msg
		for _, p := range m.st.Projects {
			for _, a := range p.Agents {
				m.noteQuota(a.Kind, msg[a.ID].Quota)
			}
		}
	case screensMsg:
		m.polling = false
		if msg.err != nil {
			return m, m.pollCmd()
		}
		only := map[string]bool{}
		for id, screen := range msg.screens {
			m.screens[id], only[id] = screen, true
		}
		cmd := m.notifyCmd(m.observe(only, time.Now()))
		if m.fullDue { // asked for while this capture ran
			cmd = tea.Batch(cmd, m.pollCmd())
		}
		return m, cmd
	case pokeMsg:
		cmd, arg, _ := strings.Cut(string(msg), " ")
		switch cmd {
		case poke.Hook: // an agent reported through `mad hook`
			if _, a := m.st.FindAgent(arg); a != nil {
				m.hooks[arg] = status.ReadHook(arg)
				return m, m.notifyCmd(m.observe(map[string]bool{arg: true}, time.Now()))
			}
		case poke.Jump:
			return m, m.jumpNext()
		case poke.Poll: // panes changed from outside (mad switch, pane-died)
			return m, m.pollNow()
		case poke.Diff: // Alt-v: the agent on stage, or back from its diff
			return m, m.diffFromStage()
		}
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
		}
		if msg.failedID != "" {
			m.st.RemoveAgent(msg.failedID)
			delete(m.trackers, msg.failedID)
			m.save()
			m.rebuildRows()
		}
		if msg.selectID != "" {
			m.selectAgent(msg.selectID)
		}
		m.gitDue, m.readDue = true, true
		return m, m.pollNow()
	case historyMsg:
		m.pk.history, m.pk.loading = msg, false
		if m.mode == modeAddProject {
			m.refreshPicker()
		}
	case sessionsMsg:
		if m.mode == modePickSession && msg.root == m.sp.proj.Path && msg.kind == m.sp.kind {
			m.sp.items, m.sp.loading = msg.list, false
		}
	case tea.MouseMsg:
		switch m.mode {
		case modeAddProject:
			return m, m.mousePicker(msg)
		case modePickSession:
			return m, m.mouseSessions(msg)
		}
		return m, m.handleMouse(msg)
	case tea.KeyMsg:
		switch m.mode {
		case modeAddProject:
			return m, m.keyPicker(msg)
		case modePickSession:
			return m, m.keySessions(msg)
		case modePickKind:
			return m, m.keyPickKind(msg)
		case modeConfirm:
			return m, m.keyConfirm(msg)
		case modeWorktree:
			return m, m.keyWorktree(msg)
		case modeRename:
			return m, m.keyRename(msg)
		case modePickFinish:
			return m, m.keyPickFinish(msg)
		default:
			// Fast typing arrives as one multi-rune key; handle each rune.
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
				var cmds []tea.Cmd
				for _, r := range msg.Runes {
					cmds = append(cmds, m.keyNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
				}
				return m, tea.Batch(cmds...)
			}
			return m, m.keyNormal(msg)
		}
	}
	if m.mode == modeAddProject || m.mode == modeWorktree || m.mode == modeRename {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// reloadConfig rereads config.json and agents.json when they changed. A
// broken file is reported and what was usable in it taken; one that is
// unusable as a whole leaves the last good one in place.
func (m *model) reloadConfig() {
	if mod := paths.ModTime(paths.ConfigFile()); !mod.Equal(m.cfgMod) {
		m.cfgMod = mod
		cfg, err := deck.LoadConfig()
		if err != nil {
			m.configError(err)
		} else {
			m.cfg = cfg
		}
	}
	if mod := paths.ModTime(paths.AgentsConfig()); !mod.Equal(m.kindsMod) {
		m.kindsMod = mod
		kinds, err := agent.Load()
		if err != nil {
			m.configError(err)
		}
		if kinds != nil {
			m.kinds = kinds
		}
		if m.kindCursor >= len(m.kinds) {
			m.kindCursor = 0
		}
	}
}

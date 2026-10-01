// Package drive works agents from outside the sidebar: it starts one off
// stage, types a message into it, and waits for the turn that answers it.
// It is what `mad spawn`, `mad send` and `mad wait` do, and what lets one
// agent, or a script, hand work to another.
//
// The end of a turn and its reply come from the agent itself, through the
// hooks mad already wires in: claude's Stop hook and codex's notify carry
// the last message of the turn (see status.Turn). Kinds without hooks can
// be sent to, but not waited for.
package drive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Timing, replaceable in tests.
var (
	// tick is how often screens and reports are looked at.
	tick = 200 * time.Millisecond
	// look is how often Wait looks at an agent's pane while it waits for
	// the turn's report: each look is two tmux commands, a report is a
	// file read every tick.
	look = time.Second
	// quietFor: a screen unchanged this long belongs to an agent that is
	// not working. Agents redraw at least every second while they are:
	// claude's spinner, codex's elapsed time.
	quietFor = 1500 * time.Millisecond
	// startFor bounds how long a starting agent has to come up.
	startFor = 90 * time.Second
	// afterFor bounds how long an agent may keep busy after a turn it
	// reported done.
	afterFor = time.Minute
	// stale: a turn still unreported after its screen was this still
	// ended without a word.
	stale = status.StaleRunning
	now   = time.Now
)

// AskingError: the agent waits for an answer (a permission, a folder to
// trust) before it can take a message.
type AskingError struct{ Label, Question string }

func (e *AskingError) Error() string {
	return fmt.Sprintf("%s is waiting for you%s; answer it in the deck first", e.Label, describe(e.Question))
}

// DraftError: there is a message in the agent's prompt that you typed and
// have not sent; mad's would be typed after it.
type DraftError struct{ Label, Text string }

func (e *DraftError) Error() string {
	return fmt.Sprintf("%s has a message of yours not sent yet (%q); send or clear it first", e.Label, textutil.Truncate(e.Text, 40))
}

var (
	ErrTimeout = errors.New("timed out")
	ErrBusy    = errors.New("is working; wait for it first")
	ErrNoReply = errors.New("stopped without a reply (interrupted, or a tool call refused)")
)

// Find resolves ref to an agent: its id, a unique prefix of it, or its
// name. When agents of several projects share the name, the one in dir's
// project wins.
func Find(st *state.State, ref, dir string) (*state.Project, *state.Agent, error) {
	if ref == "" {
		return nil, nil, errors.New("no agent given")
	}
	if p, a := st.FindAgent(ref); a != nil {
		return p, a, nil
	}
	type hit struct {
		p *state.Project
		a *state.Agent
	}
	var named, prefixed []hit
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			if a.Name == ref {
				named = append(named, hit{p, a})
			}
			if len(ref) >= 4 && strings.HasPrefix(a.ID, ref) {
				prefixed = append(prefixed, hit{p, a})
			}
		}
	}
	if len(named) > 1 {
		root := discover.ResolveRoot(dir)
		var here []hit
		for _, h := range named {
			if h.p.Path == root {
				here = append(here, h)
			}
		}
		if len(here) > 0 {
			named = here
		}
	}
	for _, hits := range [][]hit{named, prefixed} {
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0].p, hits[0].a, nil
		default:
			return nil, nil, fmt.Errorf("%q matches %d agents; use its id (mad ls)", ref, len(hits))
		}
	}
	return nil, nil, fmt.Errorf("no agent %q (mad ls lists them)", ref)
}

// Label is how messages name an agent: its name, else its kind and the
// start of its id.
func Label(a *state.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return a.Kind + " " + ShortID(a.ID)
}

// ShortID is the start of an id, enough to tell agents apart.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Spec says what agent to spawn.
type Spec struct {
	Kind  string
	Name  string
	Dir   string   // where it runs; its project is the one it belongs to
	Model string   // passed as --model, which claude, codex and pi take
	Args  []string // more of the agent's own flags
	// Branch, if set, gets a worktree of the project the agent runs in,
	// created from HEAD when the branch is new, as `w` in the sidebar.
	Branch string
	// Run and Role, if set, make the agent a role of that run.
	Run, Role string
}

// Spawn adds an agent to its project and starts it off stage, starting
// the deck first when none runs. It returns once the agent can take a
// message; one that came up asking something (a folder to trust) is
// returned with an error saying so.
func Spawn(s Spec, kinds []agent.Kind) (*state.Agent, error) {
	if !known(kinds, s.Kind) {
		return nil, fmt.Errorf("unknown kind %q", s.Kind)
	}
	dir := paths.Expand(s.Dir)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", s.Dir)
	}
	root := discover.ResolveRoot(dir)
	if s.Branch != "" {
		if err := git.ValidBranch(s.Branch); err != nil {
			return nil, err
		}
		dir = git.WorktreeDir(root, s.Branch)
		if err := git.AddWorktree(root, s.Branch, dir); err != nil {
			return nil, err
		}
	}
	a := &state.Agent{ID: state.NewUUID(), Kind: s.Kind, Name: s.Name, Args: s.Args, Run: s.Run, Role: s.Role, CreatedAt: now()}
	if s.Model != "" {
		a.Args = append([]string{"--model", s.Model}, a.Args...)
	}
	if dir != root {
		a.Dir = dir
	}
	if err := EnsureDeck(root); err != nil {
		return nil, err
	}
	var p *state.Project
	err := state.Update(func(st *state.State) error {
		p, _ = st.AddProject(root)
		if s.Name != "" {
			for _, b := range p.Agents {
				if b.Name == s.Name {
					return fmt.Errorf("%s already has an agent named %q", p.Name, s.Name)
				}
			}
		}
		p.Agents = append(p.Agents, a)
		return nil
	})
	if err != nil {
		return nil, err
	}
	started := now()
	if err := deck.StartAgent(p, a, false, kinds); err != nil {
		_ = state.Update(func(st *state.State) error { st.RemoveAgent(a.ID); return nil })
		return nil, err
	}
	_ = poke.Send(poke.Poll) // the sidebar lists it now
	panes, err := tmux.ListPanes()
	if err != nil {
		return a, err
	}
	pane, _ := tmux.FindPane(panes, a.ID)
	return a, up(agent.ByName(kinds, a.Kind), a, pane.ID, started)
}

func known(kinds []agent.Kind, name string) bool {
	for _, k := range kinds {
		if k.Name == name {
			return true
		}
	}
	return false
}

// agentEnv are variables agents set for the commands they run. A deck
// started from such a command must not hand them to its own agents:
// claude refuses to start inside another claude.
var agentEnv = []string{
	"MAD_AGENT_ID", "AI_AGENT", "CLAUDECODE", "CLAUDE_PID", "CLAUDE_EFFORT",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
}

// EnsureDeck starts the deck, detached, unless it runs.
func EnsureDeck(dir string) error {
	if tmux.HasSession(tmux.MainSession) {
		return nil
	}
	if err := tmux.CheckVersion(); err != nil {
		return err
	}
	if err := deck.WriteConfigs(); err != nil {
		return err
	}
	for _, k := range agentEnv {
		os.Unsetenv(k)
	}
	return deck.EnsureLayout(dir, 200, 50)
}

// Send types text into agent a as one message. An agent that is asleep
// or stopped is resumed off stage first; one that is working or waiting
// for an answer is refused.
func Send(st *state.State, a *state.Agent, text string, kinds []agent.Kind) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to send")
	}
	if a.ID == os.Getenv("MAD_AGENT_ID") {
		return errors.New("an agent can't send to itself")
	}
	k := agent.ByName(kinds, a.Kind)
	pane, err := ready(st, a, kinds)
	if err != nil {
		return err
	}
	sent := now()
	if err := status.MarkSent(a.ID, sent, Digest(text)); err != nil {
		return err
	}
	if err := tmux.Paste(pane.ID, text); err != nil {
		return err
	}
	_ = poke.Send(poke.Poll)
	if k.Hooks && !strings.HasPrefix(text, "/") { // commands are no prompt
		// claude reports what it does with the message (UserPromptSubmit,
		// the tools it calls); a paste it read as text only leaves it in
		// the prompt, where enter again sends it.
		took := func(h *status.Hook) bool { return !h.At.Before(sent) }
		if waitHook(a.ID, took) != nil {
			_ = tmux.Run("send-keys", "-t", pane.ID, "Enter")
			if err := waitHook(a.ID, took); err != nil {
				return fmt.Errorf("%s did not take the message: %w", Label(a), err)
			}
		}
	}
	return nil
}

// Digest stands for a message as Send marks it sent (see Took).
func Digest(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:8])
}

// Took reports whether agent a took the message with digest d, typed in
// at or after since. It has to be the last message typed into a, and for
// an agent that reports what it does, one it went to work on: it ended a
// turn since, or reported working or asking since. A message typed in but
// never taken (the agent restarted under it) is not taken.
func Took(a *state.Agent, kinds []agent.Kind, d string, since time.Time) bool {
	sent := status.SentAt(a.ID)
	if d == "" || status.SentDigest(a.ID) != d || sent.Before(since) {
		return false
	}
	if !agent.ByName(kinds, a.Kind).Hooks {
		return true // typed in, and nothing will say more
	}
	if t := status.ReadTurn(a.ID); t != nil && !t.At.Before(sent) {
		return true
	}
	h := status.ReadHook(a.ID)
	return h != nil && !h.At.Before(sent) && (h.State == status.Running || h.State == status.Waiting)
}

// Ready returns nil when agent a can take a message: resumed off stage if
// it was asleep or stopped, and neither working nor asking you something
// (an *AskingError).
func Ready(st *state.State, a *state.Agent, kinds []agent.Kind) error {
	_, err := ready(st, a, kinds)
	return err
}

func ready(st *state.State, a *state.Agent, kinds []agent.Kind) (tmux.Pane, error) {
	k := agent.ByName(kinds, a.Kind)
	woke := now()
	pane, started, err := wake(st, a, kinds)
	if err != nil {
		return pane, err
	}
	switch h := status.ReadHook(a.ID); {
	case started:
		err = up(k, a, pane.ID, woke)
	case k.Hooks && h != nil && h.State == status.Idle:
		// Its turn is over, but claude goes on running the user's own
		// Stop hooks for a while.
		err = free(k, a, pane.ID, afterFor)
	default:
		err = free(k, a, pane.ID, quietFor+tick)
	}
	return pane, err
}

// wake finds a's pane, starting or resuming a off stage when its process
// is gone; started says it was.
func wake(st *state.State, a *state.Agent, kinds []agent.Kind) (tmux.Pane, bool, error) {
	p, _ := st.FindAgent(a.ID)
	if err := EnsureDeck(p.Path); err != nil {
		return tmux.Pane{}, false, err
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return tmux.Pane{}, false, err
	}
	pane, ok := tmux.FindPane(panes, a.ID)
	switch {
	case ok && !pane.Dead:
		return pane, false, nil
	case ok:
		err = deck.RestartAgent(p, a, pane.ID, kinds)
	default:
		err = deck.StartAgent(p, a, true, kinds)
	}
	if err != nil {
		return tmux.Pane{}, false, err
	}
	_ = poke.Send(poke.Poll)
	if panes, err = tmux.ListPanes(); err != nil {
		return tmux.Pane{}, false, err
	}
	if pane, ok = tmux.FindPane(panes, a.ID); !ok {
		return tmux.Pane{}, false, fmt.Errorf("%s did not start", Label(a))
	}
	return pane, true, nil
}

// waitHook waits a few seconds for a report from agent id that ok accepts.
func waitHook(id string, ok func(*status.Hook) bool) error {
	for end := now().Add(10 * time.Second); now().Before(end); time.Sleep(tick) {
		if h := status.ReadHook(id); h != nil && ok(h) {
			return nil
		}
	}
	return ErrTimeout
}

// free returns nil when agent a, whose screen is watched for window, is
// neither working nor waiting for an answer, and has nothing of yours in
// its prompt.
func free(k agent.Kind, a *state.Agent, paneID string, window time.Duration) error {
	switch s, msg := settle(k, a.ID, paneID, window); s {
	case status.Running:
		return fmt.Errorf("%s %w", Label(a), ErrBusy)
	case status.Waiting:
		return &AskingError{Label(a), msg}
	}
	if d := Draft(tmux.CaptureStyled(paneID)); d != "" {
		return &DraftError{Label(a), d}
	}
	return nil
}

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)
	// faintRe is text drawn faint, as agents draw their prompt's
	// placeholder and suggestions.
	faintRe = regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?2m[^\x1b]*`)
)

// promptMarks start the line agents type into: claude's ❯, codex's ›.
var promptMarks = []string{"❯", "›"}

// Draft is what stands typed in the prompt of an agent's screen, given
// with its colors (tmux.CaptureStyled): the text after the last prompt
// mark, without what is drawn faint (a placeholder, a suggestion). The
// last mark is the prompt's; ones above it are messages already sent.
func Draft(styled string) string {
	lines := strings.Split(styled, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		plain := strings.TrimSpace(strings.Trim(ansiRe.ReplaceAllString(lines[i], ""), "│ "))
		for _, mark := range promptMarks {
			if !strings.HasPrefix(plain, mark) {
				continue
			}
			line := lines[i][strings.Index(lines[i], mark)+len(mark):]
			line = faintRe.ReplaceAllString(line, "")
			return strings.TrimSpace(strings.TrimRight(ansiRe.ReplaceAllString(line, ""), "│ "))
		}
	}
	return ""
}

// up waits for agent a, started at since, to come up: its screen settles
// and, for claude, it reports the session started (a prompt before that,
// such as the folder trust one, comes back as an error).
func up(k agent.Kind, a *state.Agent, paneID string, since time.Time) error {
	end := now().Add(startFor)
	for {
		err := free(k, a, paneID, end.Sub(now()))
		switch {
		case errors.Is(err, ErrBusy):
			return fmt.Errorf("%s did not come up in %s", Label(a), startFor)
		case err != nil:
			return err
		}
		if h := status.ReadHook(a.ID); !k.Hooks || h != nil && h.Event == "SessionStart" && !h.At.Before(since) {
			return nil
		}
		if now().After(end) {
			return fmt.Errorf("%s did not come up in %s", Label(a), startFor)
		}
		time.Sleep(tick)
	}
}

// settle watches an agent's screen until it has been still for quietFor,
// and tells whether the agent is idle or waiting for an answer then
// (with what it asks, when it says). An agent whose screen still moves
// after window is running.
func settle(k agent.Kind, id, paneID string, window time.Duration) (string, string) {
	end := now().Add(window)
	screen, still := tmux.Capture(paneID), now()
	for {
		time.Sleep(tick)
		s := tmux.Capture(paneID)
		if s != screen {
			screen, still = s, now()
		}
		if strings.TrimSpace(screen) != "" && now().Sub(still) >= quietFor {
			break
		}
		if now().After(end) {
			return status.Running, ""
		}
	}
	if ok, msg := asking(k, id, screen); ok {
		return status.Waiting, msg
	}
	return status.Idle, ""
}

// asking tells whether an agent showing screen waits for an answer, and
// what it asks when it said. claude's report is believed for a question
// it asks with a tool; a permission prompt only while it is on screen,
// as refusing one ends the turn without another report.
func asking(k agent.Kind, id, screen string) (bool, string) {
	h := status.ReadHook(id)
	reported := k.Hooks && h != nil && h.State == status.Waiting
	switch {
	case reported && h.Event == "PreToolUse":
		return true, h.Message
	case k.ScreenWaiting(tail(screen)):
		if reported {
			return true, h.Message
		}
		return true, ""
	}
	return false, ""
}

func describe(msg string) string {
	if msg == "" {
		return ""
	}
	return " (" + msg + ")"
}

// tail is the bottom of a screen, where agents ask their questions.
func tail(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	return strings.Join(lines, "\n")
}

// WaitOptions tune Wait.
type WaitOptions struct {
	// Timeout ends the wait with ErrTimeout; zero waits as long as it
	// takes.
	Timeout time.Duration
	// Note is told when the agent starts waiting for an answer, with what
	// it asks when it says ("" when it doesn't), and when it stops.
	Note func(asking bool, question string)
	// Tick, called between looks, ends the wait with the error it
	// returns.
	Tick func() error
}

// Wait waits until agent a's turn ends and returns how it ended: the turn
// answering the last message mad sent it, or one it started on its own
// (typed in the deck) while it says so (claude). With nothing pending it
// returns the last turn at once; nil if there was none. It waits through
// the agent asking for an answer (see WaitOptions.Note). A turn that ends
// without a report (interrupted, a tool call refused) shows as a still
// screen asking nothing, and gives ErrNoReply once that has lasted a
// while (stale).
func Wait(a *state.Agent, kinds []agent.Kind, o WaitOptions) (*status.Turn, error) {
	k := agent.ByName(kinds, a.Kind)
	if !k.Hooks && discover.Lookup(a.Kind) == nil {
		return nil, fmt.Errorf("%s agents don't say when they are done", a.Kind)
	}
	var end time.Time
	if o.Timeout > 0 {
		end = now().Add(o.Timeout)
	}
	sent, asked := status.SentAt(a.ID), false
	var shown string
	still := now()
	var looked time.Time
	for ; ; time.Sleep(tick) {
		turn, hook := status.ReadTurn(a.ID), status.ReadHook(a.ID)
		if !pending(turn, hook, sent) {
			return turn, nil
		}
		if hook != nil && (hook.Event == "Stop" || hook.Event == "agent-turn-complete") && hook.At.After(sent) && !sent.IsZero() {
			// Done since the message, yet no reply kept: the report came
			// through a mad from before replies were (WriteTurn comes first).
			return nil, fmt.Errorf("%s is done, but its hooks run a mad that keeps no replies; open the deck with this mad (and restart the agent) first", Label(a))
		}
		if now().Sub(looked) >= look {
			looked = now()
			panes, err := tmux.ListPanes()
			if err != nil {
				return nil, err
			}
			pane, ok := tmux.FindPane(panes, a.ID)
			if !ok || pane.Dead {
				return nil, fmt.Errorf("%s exited before it was done", Label(a))
			}
			screen := tmux.Capture(pane.ID)
			if screen != shown {
				shown, still = screen, now()
			}
			waiting, msg := asking(k, a.ID, screen)
			if waiting != asked && o.Note != nil {
				o.Note(waiting, msg)
			}
			asked = waiting
			if !waiting && now().Sub(still) >= stale {
				return nil, fmt.Errorf("%s %w", Label(a), ErrNoReply)
			}
		}
		if !end.IsZero() && now().After(end) {
			return nil, fmt.Errorf("%s: %w", Label(a), ErrTimeout)
		}
		if o.Tick != nil {
			if err := o.Tick(); err != nil {
				return nil, err
			}
		}
	}
}

// pending reports whether a turn is under way: mad sent a message since
// the last turn ended, or the agent reported working since (a message
// typed in the deck).
func pending(turn *status.Turn, hook *status.Hook, sent time.Time) bool {
	var ended time.Time
	if turn != nil {
		ended = turn.At
	}
	if sent.After(ended) {
		return true
	}
	return hook != nil && hook.At.After(ended) && (hook.State == status.Running || hook.State == status.Waiting)
}

// Statuses tells what each agent is doing, as the sidebar would: their
// screens are looked at twice, quietFor apart.
func Statuses(st *state.State, kinds []agent.Kind) map[string]string {
	panes, _ := tmux.ListPanes() // no server: every agent is stopped
	agents := st.OrderedAgents()
	var live []string
	for _, a := range agents {
		if p, ok := tmux.FindPane(panes, a.ID); ok && !p.Dead {
			live = append(live, p.ID)
		}
	}
	trackers := map[string]*status.Tracker{}
	out := map[string]string{}
	look := func() {
		screens, err := tmux.CaptureAll(live)
		if err != nil {
			screens = map[string]string{}
			for _, id := range live {
				screens[id] = tmux.Capture(id)
			}
		}
		for _, a := range agents {
			tr := trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{}
				trackers[a.ID] = tr
			}
			p, ok := tmux.FindPane(panes, a.ID)
			k := agent.ByName(kinds, a.Kind)
			out[a.ID] = tr.Observe(k, p, ok, status.ReadHook(a.ID), screens[p.ID], false, now())
		}
	}
	look()
	if len(live) > 0 {
		time.Sleep(quietFor)
		look()
	}
	return out
}

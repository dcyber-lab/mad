package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// readRuns rereads every run's run.json: how far it got, what it waits
// for. They are small, and the runner pokes the sidebar when one changes.
func (m *model) readRuns() {
	for _, p := range m.st.Projects {
		for _, r := range p.Runs {
			if f, err := madrun.Load(madrun.Dir(r)); err == nil {
				m.runs[r.ID] = f
			}
		}
	}
}

// runActive: run id still works, so its agents are its to drive.
func (m *model) runActive(id string) bool {
	f := m.runs[id]
	return f != nil && madrun.Active(f.Progress.Status)
}

// inRun: the agent plays a role in a run of its project.
func inRun(p *state.Project, a *state.Agent) bool { return p.InRun(a) }

// groupOf is what a row's number keys count in: the run it is about, or
// its project's own agents.
func groupOf(r row) []*state.Agent {
	if run := runOf(r); run != nil {
		return r.proj.RunAgents(run.ID)
	}
	return r.proj.TopAgents()
}

// runOf is the run a row is about: the run's own row, or one of its
// agents.
func runOf(r row) *state.Run {
	if r.run != nil {
		return r.run
	}
	if r.agent != nil && r.agent.Run != "" {
		for _, x := range r.proj.Runs {
			if x.ID == r.agent.Run {
				return x
			}
		}
	}
	return nil
}

// runRowSegs lays out a run's row: its name, then where it stands.
func (m *model) runRowSegs(r row) (left, right []seg) {
	run := r.run
	bar := seg{stPlain, " "}
	if m.stageID == deck.RunnerID(run.ID) {
		bar = seg{stStage, "▌"}
	}
	arrow := "▾ "
	if run.Collapsed {
		arrow = "▸ "
	}
	left = []seg{{stPlain, " "}, bar, {stPlain, "  "}, {stDim, arrow}, {stKey, "run "}, {stProject, run.Name}}
	f := m.runs[run.ID]
	if f == nil {
		return left, []seg{{stFaint, "… "}}
	}
	pr := f.Progress
	steps := max(len(f.Flow().Steps), 1)
	switch pr.Status {
	case madrun.Starting, madrun.Running:
		right = []seg{{stRunning, fmt.Sprintf("%s %d/%d", m.spin(), min(pr.Step+1, steps), steps)}}
	case madrun.Waiting:
		right = []seg{{stWaiting, "◆ waiting"}}
	case madrun.Done:
		right = []seg{{stDone, "✓ done"}}
	case madrun.Cancelled:
		right = []seg{{stFaint, "✗ cancelled"}}
	case madrun.Failed:
		right = []seg{{stWaiting, "✗ failed"}}
	}
	right = append(right, seg{stPlain, " "})
	spent := ""
	if pr.Cost > 0 {
		spent = textutil.USD(pr.Cost)
	}
	if pr.Tokens > 0 {
		if spent != "" {
			spent += "+"
		}
		spent += textutil.Count(pr.Tokens)
	}
	return left, m.withUsage(left, right, spent)
}

// runDetailSegs is the line under a run: the step it is on, what it
// waits for, or how it ended.
func (m *model) runDetailSegs(run *state.Run) []seg {
	indent := seg{stPlain, "       "}
	f := m.runs[run.ID]
	if f == nil {
		return []seg{indent}
	}
	pr := f.Progress
	step := ""
	if fl := f.Flow(); pr.Step >= 0 && pr.Step < len(fl.Steps) {
		step = fl.RoleLabel(fl.Steps[pr.Step].Role)
	}
	switch pr.Status {
	case madrun.Starting:
		return []seg{indent, {stFaint, "starting"}}
	case madrun.Running:
		return []seg{indent, {stDim, fmt.Sprintf("%s · round %d", step, pr.Round)}}
	case madrun.Waiting:
		return []seg{indent, {stFlash, pr.Waiting}}
	case madrun.Done:
		took := ""
		if !pr.Started.IsZero() && !pr.Ended.IsZero() {
			took = " · " + pr.Ended.Sub(pr.Started).Round(time.Minute).String()
		}
		return []seg{indent, {stFaint, fmt.Sprintf("approved%s%s · f to finish", madrun.RoundsOfChanges(pr.Round-1), strings.TrimSuffix(took, "0s"))}}
	case madrun.Failed:
		return []seg{indent, {stFlash, pr.Waiting}}
	}
	return []seg{indent, {stFaint, "its branch and files stay"}}
}

// openRun folds or unfolds the run's agents, and shows its panel on
// stage, starting its runner when it has none (a deck rebuilt: the
// runner picks the run up).
func (m *model) openRun(run *state.Run) tea.Cmd {
	run.Collapsed = !run.Collapsed
	m.save()
	m.rebuildRows()
	id := run.ID
	pane, has := m.panes[deck.RunnerID(id)]
	return m.action("", func() error {
		switch {
		case !has:
			if err := deck.StartRunner(id); err != nil {
				return err
			}
		case pane.Dead:
			// A runner that ended (an older mad's): one now draws the
			// panel to the stage's size.
			if err := tmux.Run("respawn-pane", "-k", "-t", pane.ID, deck.SelfCommand("run exec "+id)); err != nil {
				return err
			}
		}
		return deck.ShowPane(deck.RunnerID(id), false)
	})
}

// openRunForm puts the form for a new run in p on stage.
func (m *model) openRunForm(p *state.Project) tea.Cmd {
	m.taskFor = ""
	dir, cmd := p.Path, deck.SelfCommand("run new "+paths.ShellQuote(p.Path))
	return m.action("", func() error { return deck.OpenTask(dir, cmd) })
}

func (m *model) runCommand(run *state.Run, cmd string) tea.Cmd {
	if err := madrun.Command(run, cmd); err != nil {
		m.setFlash(err.Error())
		return nil
	}
	word := map[string]string{madrun.Continue: "continue", madrun.Cancel: "cancel"}[cmd]
	m.setFlash(word + " → run " + run.Name)
	return nil
}

// cancelOrRemoveRun: x on a run cancels it while it works, and once it
// is over removes it with its agents. Its branch stays either way.
func (m *model) cancelOrRemoveRun(p *state.Project, run *state.Run) {
	if m.runActive(run.ID) {
		m.confirm(fmt.Sprintf("cancel run %s? branch stays (y/n)", run.Name), func() tea.Cmd {
			return m.runCommand(run, madrun.Cancel)
		})
		return
	}
	var ids []string
	for _, a := range p.RunAgents(run.ID) {
		ids = append(ids, a.ID)
	}
	m.confirm(fmt.Sprintf("remove run %s + kill %d? branch stays (y/n)", run.Name, len(ids)), func() tea.Cmd {
		id := run.ID
		cmd := m.removeAgents(ids...)
		m.st.RemoveRun(id)
		delete(m.runs, id)
		m.save()
		m.rebuildRows()
		return tea.Batch(cmd, m.action("", func() error { return deck.KillAgent(deck.RunnerID(id)) }))
	})
}

// runnerRestartAfter keeps a runner that won't stay up from being
// started over and over; a run this young has the runner it was created
// with, maybe not yet in a poll.
const runnerRestartAfter = 20 * time.Second

// ensureRunners starts the runner of every run that works but has none
// running: the deck was rebuilt, or the runner crashed.
func (m *model) ensureRunners() tea.Cmd {
	var ids []string
	var dead []bool
	for _, p := range m.st.Projects {
		for _, r := range p.Runs {
			if !m.runActive(r.ID) {
				continue
			}
			pane, ok := m.panes[deck.RunnerID(r.ID)]
			if ok && !pane.Dead {
				m.runnerMissing[r.ID] = 0
				continue
			}
			// Missing from two polls in a row, not just one taken while
			// it was being started.
			if m.runnerMissing[r.ID]++; m.runnerMissing[r.ID] < 2 {
				continue
			}
			if time.Since(r.CreatedAt) < runnerRestartAfter || time.Since(m.runnerStarted[r.ID]) < runnerRestartAfter {
				continue
			}
			m.runnerStarted[r.ID] = time.Now()
			ids, dead = append(ids, r.ID), append(dead, ok)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return m.action("", func() error {
		for i, id := range ids {
			if dead[i] {
				if pane, err := tmux.ListPanes(); err == nil {
					if p, ok := tmux.FindPane(pane, deck.RunnerID(id)); ok {
						_ = tmux.Run("respawn-pane", "-k", "-t", p.ID, deck.SelfCommand("run exec "+id))
						continue
					}
				}
			}
			if err := deck.StartRunner(id); err != nil {
				return err
			}
		}
		return nil
	})
}

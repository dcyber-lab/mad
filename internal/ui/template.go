package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/workspace"
)

// A workspace from a template is set up before its agent starts, one step
// at a time, each a message back here: the sidebar shows how far it got,
// and a failure leaves everything in place with an inbox item saying what
// happened.

const (
	fTemplate = iota
	fBase
	fBranch
	fAgent
	fCount
)

type tmplForm struct {
	proj   *state.Project
	list   []workspace.Template
	ti     int
	kind   int
	field  int
	base   textinput.Model
	branch textinput.Model
	plan   *workspace.Plan // the preview, once asked for
	err    string
}

// runMsg is one stage of a run done: the worktree (step -1) or a setup step.
type runMsg struct {
	id   string
	step int
	res  workspace.StepResult
	err  error
}

func newField(prompt, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt, ti.Placeholder, ti.CharLimit = prompt, placeholder, 200
	return ti
}

func (m *model) openTemplateForm(p *state.Project) tea.Cmd {
	ts, err := workspace.Load(p.Path)
	if err != nil || len(ts) == 0 {
		m.setFlash(fmt.Sprintf("no templates in %s", paths.Short(workspace.ConfigFile(p.Path))))
		return nil
	}
	m.tf = tmplForm{proj: p, list: ts, base: newField("› ", "HEAD"), branch: newField("› ", "feat/…"), field: fBranch}
	m.applyTemplateDefaults()
	m.mode = modeTemplate
	return tea.Batch(m.focusField(), m.widen())
}

func (m *model) applyTemplateDefaults() {
	t := m.tf.list[m.tf.ti]
	m.tf.base.SetValue(t.Workspace.DefaultBase)
	for i, k := range m.kinds {
		if k.Name == t.Launch.Agent {
			m.tf.kind = i
		}
	}
}

func (m *model) focusField() tea.Cmd {
	m.tf.base.Blur()
	m.tf.branch.Blur()
	switch m.tf.field {
	case fBase:
		return m.tf.base.Focus()
	case fBranch:
		return m.tf.branch.Focus()
	}
	return nil
}

func (m *model) keyTemplate(k tea.KeyMsg) tea.Cmd {
	f := &m.tf
	switch k.String() {
	case "esc", "ctrl+c":
		if f.plan != nil || f.err != "" {
			f.plan, f.err = nil, ""
			return nil
		}
		m.mode = modeNormal
		return nil
	case "tab", "down":
		f.field = (f.field + 1) % fCount
		return m.focusField()
	case "shift+tab", "up":
		f.field = (f.field + fCount - 1) % fCount
		return m.focusField()
	case "enter":
		if f.plan == nil {
			m.previewTemplate()
			return nil
		}
		return m.startRun(*f.plan)
	case "left", "right":
		d := 1
		if k.String() == "left" {
			d = -1
		}
		switch f.field {
		case fTemplate:
			f.ti = (f.ti + d + len(f.list)) % len(f.list)
			m.applyTemplateDefaults()
			f.plan = nil
			return nil
		case fAgent:
			f.kind = (f.kind + d + len(m.kinds)) % len(m.kinds)
			f.plan = nil
			return nil
		}
	}
	var cmd tea.Cmd
	switch f.field {
	case fBase:
		f.base, cmd = f.base.Update(k)
	case fBranch:
		f.branch, cmd = f.branch.Update(k)
	}
	f.plan, f.err = nil, ""
	return cmd
}

// previewTemplate resolves the base and fixes the plan to confirm.
func (m *model) previewTemplate() {
	f := &m.tf
	branch := strings.TrimSpace(f.branch.Value())
	if err := validBranch(branch); err != nil {
		f.err = err.Error()
		return
	}
	t := f.list[f.ti]
	plan, err := workspace.MakePlan(f.proj.Path, t, strings.TrimSpace(f.base.Value()), branch, m.kinds[f.kind].Name, git.WorktreeDir(f.proj.Path, branch))
	if err != nil {
		f.err = err.Error()
		return
	}
	f.plan, f.err = &plan, ""
}

func (m *model) startRun(plan workspace.Plan) tea.Cmd {
	m.mode = modeNormal
	if err := workspace.Approve(plan.Repo, plan.Template); err != nil {
		m.setFlash(err.Error())
		return nil
	}
	r := workspace.NewRun(state.NewUUID(), plan, time.Now())
	r.State = workspace.Creating
	m.runs[r.ID] = r
	m.saveRun(r)
	return worktreeCmd(r.ID, plan)
}

func worktreeCmd(id string, plan workspace.Plan) tea.Cmd {
	return func() tea.Msg {
		if err := plan.Recheck(); err != nil {
			return runMsg{id: id, step: -1, err: err}
		}
		return runMsg{id: id, step: -1, err: workspace.CreateWorktree(plan)}
	}
}

func stepCmd(id string, plan workspace.Plan, i int) tea.Cmd {
	return func() tea.Msg {
		if err := plan.Recheck(); err != nil {
			now := time.Now()
			return runMsg{id: id, step: i, res: workspace.StepResult{ID: plan.Template.Setup[i].ID, Start: now, End: now, Exit: -1, Err: err.Error()}}
		}
		return runMsg{id: id, step: i, res: workspace.RunStep(id, plan, i, time.Now)}
	}
}

func (m *model) saveRun(r *workspace.Run) {
	r.UpdatedAt = time.Now()
	if err := r.Save(); err != nil {
		m.setFlash(err.Error())
	}
}

// applyRun takes a finished stage and starts the next one: the next
// step, or the agent once all are done.
func (m *model) applyRun(msg runMsg) tea.Cmd {
	r := m.runs[msg.id]
	if r == nil {
		return nil
	}
	if msg.step < 0 {
		if msg.err != nil {
			r.Error = msg.err.Error()
			return m.failRun(r)
		}
		r.Worktree = true
	} else {
		r.Steps = append(r.Steps[:min(msg.step, len(r.Steps))], msg.res)
		if !msg.res.OK() {
			return m.failRun(r)
		}
	}
	setup := r.Plan.Template.Setup
	if i := r.NextStep(); i < len(setup) {
		r.State = workspace.Preparing
		m.saveRun(r)
		return stepCmd(r.ID, r.Plan, i)
	}
	r.State = workspace.Launching
	delete(m.runs, r.ID)
	p := m.st.FindProject(r.Plan.Repo)
	if p == nil {
		r.State, r.Error = workspace.Failed, "the project was removed"
		return m.failRun(r)
	}
	a := &state.Agent{ID: state.NewUUID(), Kind: r.Plan.Agent, Dir: r.Plan.Dir, CreatedAt: time.Now()}
	r.AgentID, r.State = a.ID, workspace.Active
	m.saveRun(r)
	m.setFlash(fmt.Sprintf("%s ready · %d steps ok · starting %s", r.Plan.Branch, len(setup), a.Kind))
	return m.launch(p, a, false, nil)
}

// failRun stops a run where it is, and puts it in the inbox.
func (m *model) failRun(r *workspace.Run) tea.Cmd {
	r.State = workspace.Failed
	delete(m.runs, r.ID)
	m.saveRun(r)
	now := time.Now()
	m.inbox.Raise(attention.Item{Kind: attention.SetupFailed, RunID: r.ID, Project: r.Plan.Repo, Where: r.Plan.Branch,
		Message: r.Summary(), Source: attention.FromMad, SourceRef: r.ID + "/" + fmt.Sprint(len(r.Steps))}, now)
	m.saveInbox()
	m.setFlash("setup failed: " + r.Summary() + " · i for inbox")
	return nil
}

// retryRun goes on from the failed stage; steps that succeeded don't run
// again, and the worktree is reused.
func (m *model) retryRun(it *attention.Item) tea.Cmd {
	r, err := workspace.LoadRun(it.RunID)
	if err != nil {
		m.setFlash(err.Error())
		return nil
	}
	if r.State != workspace.Failed {
		m.setFlash("that run is " + r.State)
		return nil
	}
	m.inbox.Resolve(it.ID, "retried", time.Now())
	m.saveInbox()
	r.Error, r.State = "", workspace.Preparing
	r.Steps = r.Steps[:r.NextStep()]
	m.runs[r.ID] = r
	m.saveRun(r)
	if !r.Worktree {
		return worktreeCmd(r.ID, r.Plan)
	}
	if i := r.NextStep(); i < len(r.Plan.Template.Setup) {
		return stepCmd(r.ID, r.Plan, i)
	}
	return m.applyRun(runMsg{id: r.ID, step: len(r.Steps) - 1, res: r.Steps[len(r.Steps)-1]})
}

// runProgress is a line for the footer while a run sets up.
func (m *model) runProgress() string {
	for _, r := range m.runs {
		setup := r.Plan.Template.Setup
		what := "creating worktree"
		if r.Worktree {
			if i := r.NextStep(); i < len(setup) {
				what = fmt.Sprintf("%d/%d %s", i+1, len(setup), setup[i])
			}
		}
		return " " + stRunning.Render(m.spin()) + " " + stName.Render(r.Plan.Branch) + stDim.Render(" · "+what)
	}
	return ""
}

func (m *model) templateView() string {
	f := &m.tf
	var b strings.Builder
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " new workspace"}, {stDim, " · " + f.proj.Name}}, []seg{{stFaint, "esc "}}) + "\n")
	b.WriteString(rule(m.width) + "\n")
	field := func(i int, label string, value string) {
		var bg lipgloss.TerminalColor
		if i == f.field {
			bg = cSelOff
		}
		b.WriteString(layout(m.width, bg, []seg{{stPlain, "  "}, {stDim, fmt.Sprintf("%-9s", label)}, {stName, value}}, nil) + "\n")
	}
	t := f.list[f.ti]
	choice := func(s string, n int) string {
		if n > 1 {
			return "‹ " + s + " ›"
		}
		return s
	}
	field(fTemplate, "template", choice(t.Name, len(f.list)))
	field(fBase, "base", f.base.View())
	field(fBranch, "branch", f.branch.View())
	field(fAgent, "agent", choice(m.kinds[f.kind].Name, len(m.kinds)))
	b.WriteString(rule(m.width) + "\n")
	lines := 6
	put := func(segs ...seg) {
		b.WriteString(layout(m.width, nil, segs, nil) + "\n")
		lines++
	}
	switch {
	case f.err != "":
		put(seg{stWaiting, " " + f.err})
	case f.plan == nil:
		put(seg{stDim, " setup steps of " + t.Name + ":"})
		for i, s := range t.Setup {
			put(seg{stFaint, fmt.Sprintf("  %d ", i+1)}, seg{stName, s.String()})
		}
		put(seg{stDim, " ⏎ shows exactly what will run"})
	default:
		p := f.plan
		put(seg{stHeader, " will do"})
		from := "new branch from " + p.Base + " @ " + p.ShortSHA()
		if !p.BranchNew {
			from = "existing branch " + p.Branch + " (base unused)"
		}
		put(seg{stFaint, "  1 "}, seg{stName, "worktree "}, seg{stDim, relDir(p.Repo, p.Dir)})
		put(seg{stPlain, "    "}, seg{stDim, from})
		for i, s := range p.Template.Setup {
			put(seg{stFaint, fmt.Sprintf("  %d ", i+2)}, seg{stName, "run "}, seg{stKey, s.String()}, seg{stFaint, fmt.Sprintf("  ≤%s", s.TimeoutLabel())})
		}
		put(seg{stFaint, fmt.Sprintf("  %d ", len(p.Template.Setup)+2)}, seg{stName, "start "}, seg{stKey, p.Agent}, seg{stDim, " when every step passed"})
		put(seg{stHeader, " won't"})
		put(seg{stDim, "  copy .env or local changes · deploy · commit or push"})
		if p.RepoDirty > 0 {
			put(seg{stRunning, fmt.Sprintf(" ! %d uncommitted files in the main checkout stay there", p.RepoDirty)})
		}
		switch p.Approved {
		case "":
			put(seg{stRunning, " ! first run of this template from the repo: check the steps"})
		case "changed":
			put(seg{stWaiting, " ! the template changed since you last ran it"})
		}
		put(seg{stFaint, " steps run as you, with network: not a sandbox"})
	}
	for ; lines < m.height-3; lines++ {
		b.WriteString("\n")
	}
	b.WriteString(rule(m.width) + "\n")
	if f.plan == nil {
		b.WriteString(hints("⏎", "preview", "tab", "field", "←→", "choose") + "\n" + hints("esc", "cancel"))
	} else {
		b.WriteString(hints("⏎", "create & start", "esc", "edit") + "\n")
	}
	return b.String()
}

// relDir is dir relative to repo when inside it, else shortened.
func relDir(repo, dir string) string {
	if rel, err := filepath.Rel(repo, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return paths.Short(dir)
}

package run

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Form fields, in tab order; after these two per role of the flow: its
// agent, then its effort.
const (
	fieldFlow = iota
	fieldTask
	fieldBranch
	fieldGate
	fieldPerm
	fieldRoles
)

type (
	createdMsg struct{ r *state.Run }
	failedMsg  struct{ err error }
)

// form is `mad run new`: on stage, what a new run is to do.
type form struct {
	root, from   string // the project, and the branch its checkout has
	kinds        []agent.Kind
	flows        []Flow // mad's, the user's, the project's
	flow         int
	task, branch textinput.Model
	branchEdited bool
	gate         bool
	perm         int      // into Permissions
	choices      []Choice // the agents a role can be played by
	agents       []int    // per role of the flow, into choices
	efforts      []int    // per role, into Efforts of its agent
	focus        int
	busy         bool
	err          string
	width        int
	checks       []string
}

// NewForm runs the new-run form for the project at dir.
func NewForm(dir string) error {
	root, ok := paths.ProjectRoot(paths.Expand(dir))
	if !ok {
		return fmt.Errorf("%s is not a git repository", paths.Short(root))
	}
	kinds, _ := agent.Load()
	if kinds == nil {
		kinds = agent.Builtin()
	}
	f := &form{root: root, kinds: kinds, focus: fieldTask}
	f.from, _ = gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	f.task = textinput.New()
	f.task.Prompt, f.task.Placeholder, f.task.CharLimit = "", "what to do, in a line", 2000
	f.branch = textinput.New()
	f.branch.Prompt, f.branch.CharLimit = "", 120
	f.branch.SetValue(BranchFor("", time.Now()))
	f.task.Focus()
	f.checks = precheck(root)
	flows, err := Flows(root)
	f.flows = flows
	if err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			f.checks = append(f.checks, fWarn.Render("! flow "+line))
		}
	}
	f.choices = Choices()
	f.resetAgents()
	_, err = tea.NewProgram(f, tea.WithAltScreen()).Run()
	return err
}

// resetAgents puts the flow's own agents in its roles.
func (f *form) resetAgents() {
	roles := f.flows[f.flow].Roles
	f.agents, f.efforts = make([]int, len(roles)), make([]int, len(roles))
	for i, r := range roles {
		for j, c := range f.choices {
			if c.Kind == r.Kind && c.Model == r.Model {
				f.agents[i] = j
			}
		}
		for j, l := range Efforts(f.choices[f.agents[i]]) {
			if l == r.Effort {
				f.efforts[i] = j
			}
		}
	}
}

func (f *form) fields() int { return fieldRoles + 2*len(f.agents) }

// choice is role i's agent and effort as the form has them.
func (f *form) choice(i int) Choice {
	c := f.choices[f.agents[i]]
	if levels := Efforts(c); f.efforts[i] < len(levels) {
		c.Effort = levels[f.efforts[i]]
	}
	return c
}

// pickAgent moves role i to the agent step places away, keeping its
// effort where the new agent knows that level.
func (f *form) pickAgent(i, step int) {
	effort := f.choice(i).Effort
	f.agents[i] = (f.agents[i] + step + len(f.choices)) % len(f.choices)
	f.efforts[i] = 0
	for j, l := range Efforts(f.choices[f.agents[i]]) {
		if l == effort {
			f.efforts[i] = j
		}
	}
}

// roles are the flow's roles played by the agents chosen in the form.
func (f *form) roles() []Role {
	return f.flows[f.flow].WithAgents(f.chosen()).Roles
}

func (f *form) chosen() map[string]string {
	m := map[string]string{}
	for i, r := range f.flows[f.flow].Roles {
		m[r.Name] = f.choice(i).String()
	}
	return m
}

func (f *form) Init() tea.Cmd { return textinput.Blink }

func (f *form) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.task.Width, f.branch.Width = max(msg.Width-12, 10), max(msg.Width-36, 10)
	case createdMsg:
		// The panel takes the stage, and this pane goes with it.
		_ = tmux.Run("select-pane", "-t", tmux.SidebarPane)
		_ = deck.ShowPane(deck.RunnerID(msg.r.ID), false)
		return f, tea.Quit
	case failedMsg:
		f.busy, f.err = false, msg.err.Error()
	case tea.KeyMsg:
		if f.busy {
			return f, nil
		}
		switch msg.String() {
		case "esc", "ctrl+c":
			return f, tea.Quit
		case "tab", "down":
			return f, f.move(1)
		case "shift+tab", "up":
			return f, f.move(-1)
		case "enter":
			return f, f.start()
		}
		step := 0
		switch msg.String() {
		case "right", "l", " ":
			step = 1
		case "left", "h":
			step = -1
		}
		switch {
		case f.focus == fieldFlow && step != 0:
			f.flow = (f.flow + step + len(f.flows)) % len(f.flows)
			f.resetAgents()
			return f, nil
		case f.focus == fieldGate && (msg.String() == " " || msg.String() == "x"):
			f.gate = !f.gate
			return f, nil
		case f.focus == fieldPerm && step != 0:
			f.perm = (f.perm + step + len(Permissions)) % len(Permissions)
			return f, nil
		case f.focus >= fieldRoles && step != 0:
			i := (f.focus - fieldRoles) / 2
			if (f.focus-fieldRoles)%2 == 0 {
				f.pickAgent(i, step)
			} else {
				n := len(Efforts(f.choices[f.agents[i]]))
				f.efforts[i] = (f.efforts[i] + step + n) % n
			}
			return f, nil
		case f.focus == fieldTask:
			var cmd tea.Cmd
			f.task, cmd = f.task.Update(msg)
			if !f.branchEdited {
				f.branch.SetValue(BranchFor(f.task.Value(), time.Now()))
			}
			return f, cmd
		case f.focus == fieldBranch:
			var cmd tea.Cmd
			f.branch, cmd = f.branch.Update(msg)
			f.branchEdited = true
			return f, cmd
		}
	}
	return f, nil
}

func (f *form) move(d int) tea.Cmd {
	f.focus = (f.focus + d + f.fields()) % f.fields()
	f.task.Blur()
	f.branch.Blur()
	switch f.focus {
	case fieldTask:
		return f.task.Focus()
	case fieldBranch:
		return f.branch.Focus()
	}
	return nil
}

func (f *form) start() tea.Cmd {
	if strings.TrimSpace(f.task.Value()) == "" {
		f.err = "write the task first"
		f.focus = fieldTask
		return f.task.Focus()
	}
	f.busy, f.err = true, ""
	o := Options{Project: f.root, Flow: f.flows[f.flow].Name, Task: f.task.Value(), Branch: f.branch.Value(),
		Gate: f.gate, Permission: Permissions[f.perm], Agents: f.chosen()}
	return func() tea.Msg {
		r, err := Create(o)
		if err != nil {
			return failedMsg{err}
		}
		return createdMsg{r}
	}
}

var (
	fTitle = lipgloss.NewStyle().Bold(true)
	fLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(8)
	fSel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	fDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fGood  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	fWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	fErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	fKey   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
)

func (f *form) View() string {
	w := f.width
	if w == 0 {
		w = 80
	}
	var lines []string
	add := func(s string) { lines = append(lines, s) }
	label := func(field int, s string) string {
		if f.focus == field {
			return fSel.Width(8).Render(s)
		}
		return fLabel.Render(s)
	}
	// choice shows a value ←→ changes, marked when its field has focus.
	choice := func(field int, s string) string {
		if f.focus == field {
			return fSel.Render("‹ " + s + " ›")
		}
		return "  " + s + "  "
	}
	rule := fDim.Render(strings.Repeat("─", max(w-2, 1)))

	head := fTitle.Render("new run · " + filepath.Base(f.root))
	add(" " + head + strings.Repeat(" ", max(w-lipgloss.Width(head)-12, 1)) + fDim.Render("esc cancel"))
	add(" " + rule)
	fl := f.flows[f.flow]
	where := ""
	switch {
	case strings.HasPrefix(fl.Source, ProjectFlows(f.root)):
		where = " (the project's)"
	case strings.HasPrefix(fl.Source, UserFlows()):
		where = " (yours)"
	}
	add(" " + label(fieldFlow, "flow") + textutil.PadRight(choice(fieldFlow, fl.Name), 34) + fDim.Render(fl.Description+where))
	add(" " + label(fieldTask, "task") + f.task.View())
	add(" " + label(fieldBranch, "branch") + textutil.PadRight(f.branch.View(), 28) + fDim.Render("a new worktree, from "+f.from))
	box := "[ ]"
	if f.gate {
		box = "[x]"
	}
	if f.focus == fieldGate {
		box = fSel.Render(box)
	}
	if gates := fl.Gates(); len(gates) > 0 {
		add(" " + label(fieldGate, "gate") + box + " stop for me after: " + strings.Join(gates, ", "))
	} else {
		add(" " + label(fieldGate, "gate") + fDim.Render("    this flow has no gates"))
	}
	perm := Permissions[f.perm]
	add(" " + label(fieldPerm, "access") + choice(fieldPerm, perm) + " " + fDim.Render(PermissionLabel(perm)))
	for i, r := range f.roles() {
		agentField, effortField := fieldRoles+2*i, fieldRoles+2*i+1
		l := fLabel.Render("")
		if f.focus == agentField || f.focus == effortField {
			l = fSel.Width(8).Render("")
		} else if i == 0 {
			l = fLabel.Render("roles")
		}
		who := agent.ByName(f.kinds, r.Kind).Glyph() + " " + r.Kind
		if r.Model != "" {
			who = agent.ByName(f.kinds, r.Kind).Glyph() + " " + r.Model
		}
		effort := r.Effort
		if effort == "" {
			effort = "default"
		}
		add(" " + l + textutil.PadRight(r.Label, 13) + textutil.PadRight(choice(agentField, who), 20) +
			fDim.Render("effort ") + textutil.PadRight(choice(effortField, effort), 13) + fDim.Render(r.Permits(perm)))
	}
	for i, c := range f.checks {
		l := strings.Repeat(" ", 8)
		if i == 0 {
			l = fLabel.Render("checks")
		}
		add(" " + l + c)
	}
	add(" " + fLabel.Render("limits") + fmt.Sprintf("%s · budget %s · a claude role past %s of context is compacted",
		rounds(fl), textutil.USD(DefaultBudget), kTokens(DefaultCompact)))
	add(" " + rule)
	switch {
	case f.busy:
		add(" " + fDim.Render("creating the worktree, starting the runner…"))
	default:
		add(" " + fKey.Render("enter start") + fDim.Render("   tab ↑↓ field · ←→ change · space tick"))
	}
	if f.err != "" {
		add(" " + fErr.Render(f.err))
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return strings.Join(lines, "\n")
}

// precheck says, before a run starts, what its agents will ask at their
// start: claude whether to trust the folder (a worktree of a trusted
// repository is), codex the same for every new worktree.
func precheck(root string) []string {
	var out []string
	if _, err := exec.LookPath("claude"); err != nil {
		out = append(out, fErr.Render("✗ claude is not installed"))
	} else if claudeTrusts(root) {
		out = append(out, fGood.Render("✓ claude trusts this repository"))
	} else {
		out = append(out, fWarn.Render("! claude will ask whether to trust the folder: say so once, in the sidebar, after the start"))
	}
	if _, err := exec.LookPath("codex"); err != nil {
		out = append(out, fErr.Render("✗ codex is not installed"))
	} else if codexTrusts(root) {
		out = append(out, fGood.Render("✓ codex trusts this repository"))
	} else {
		out = append(out, fWarn.Render("! codex will ask whether to trust the folder: say so once, in the sidebar, after the start"))
	}
	return out
}

// codexTrusts reports whether codex trusts dir or a folder above it: a
// [projects."<dir>"] table with trust_level = "trusted" in its config.
// The worktrees of a repository it trusts it asks nothing about.
func codexTrusts(dir string) bool {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home = filepath.Join(paths.Home(), ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return false
	}
	trusted := map[string]bool{}
	project := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			project = ""
			if p, ok := strings.CutPrefix(line, `[projects."`); ok {
				project, _, _ = strings.Cut(p, `"]`)
			}
		case project != "" && strings.ReplaceAll(line, " ", "") == `trust_level="trusted"`:
			trusted[project] = true
		}
	}
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if trusted[d] {
			return true
		}
	}
	return false
}

// claudeTrusts reports whether claude was told to trust dir or a folder
// above it, in ~/.claude.json.
func claudeTrusts(dir string) bool {
	data, err := os.ReadFile(filepath.Join(paths.Home(), ".claude.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if cfg.Projects[d].Trusted {
			return true
		}
	}
	return false
}

// rounds says how often a flow's reviews may send work back.
func rounds(f Flow) string {
	var parts []string
	same := true
	for _, s := range f.Steps {
		if !s.Review {
			continue
		}
		n := s.MaxRounds
		if n == 0 {
			n = DefaultMaxRounds
		}
		parts = append(parts, fmt.Sprintf("%s %d", s.Name, n))
		same = same && strings.HasSuffix(parts[0], fmt.Sprintf(" %d", n))
	}
	switch {
	case len(parts) == 0:
		return "no reviews"
	case same:
		_, n, _ := strings.Cut(parts[0], " ")
		return "at most " + n + " rounds of review"
	}
	return "rounds of review: " + strings.Join(parts, ", ")
}

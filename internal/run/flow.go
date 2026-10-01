// Package run hands one task through agents in roles: a flow says which
// roles there are and in what order they work, and a runner, one hidden
// pane per run, sends each its part, waits for the reply and decides what
// comes next from the reply's last line (DONE, VERDICT: APPROVE or
// CHANGES, QUESTION(@role): ...). The agents share one worktree and hand
// over through files; everything the run knows is in its directory,
// .mad/runs/<name>/ in that worktree.
//
// Flows are JSON files: mad's own (built in, see flows/), the user's in
// ~/.config/mad/flows/ and a project's in .mad/flows/, a later one taking
// the place of an earlier one of the same name.
package run

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Text is a prompt in a flow file: one string, or its lines in a list.
type Text string

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Text(s)
		return nil
	}
	var lines []string
	if err := json.Unmarshal(b, &lines); err != nil {
		return errors.New("a prompt is a string or a list of lines")
	}
	*t = Text(strings.Join(lines, "\n"))
	return nil
}

// MarshalJSON writes a prompt of several lines as their list, which reads
// better in a file than one string full of \n.
func (t Text) MarshalJSON() ([]byte, error) {
	if !strings.Contains(string(t), "\n") {
		return json.Marshal(string(t))
	}
	return json.Marshal(strings.Split(string(t), "\n"))
}

// Role is a part in a flow, played by one agent for the whole run.
type Role struct {
	Name  string `json:"name"`            // designer, builder, reviewer
	Label string `json:"label,omitempty"` // what the sidebar calls it; Name by default
	// Agent plays it: claude:opus, codex, codex:gpt-6-sol, with @effort
	// to set how hard it thinks (claude:opus@high).
	Agent string `json:"agent"`
	// ReadOnly: the role looks and changes nothing (a reviewer).
	ReadOnly bool `json:"read_only,omitempty"`

	// Kind, Model and Effort are Agent read.
	Kind   string `json:"-"`
	Model  string `json:"-"`
	Effort string `json:"-"`
}

// Step is one piece of work for a role.
type Step struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Role  string `json:"role"`
	// Prompt is the step's message, Again the one when it comes round
	// again after a review asked for changes (mad has one when it is
	// left out). Placeholders: {{task}}, {{run}} (the run's directory,
	// relative to the worktree), {{base}} (the commit the run started
	// from), {{review}} (what the review found) and {{round}}. How to end
	// the reply mad adds itself.
	Prompt Text `json:"prompt"`
	Again  Text `json:"again,omitempty"`
	// Review: the reply ends in a verdict; on CHANGES the flow goes back
	// to step Back, at most MaxRounds times (3 by default).
	Review    bool   `json:"review,omitempty"`
	Back      string `json:"back,omitempty"`
	MaxRounds int    `json:"max_rounds,omitempty"`
	// Ask are the roles the step may put a question to.
	Ask []string `json:"ask,omitempty"`
	// Gate, when the run asks for gates, stops after this step for you.
	Gate bool `json:"gate,omitempty"`
}

// Flow is a run's template.
type Flow struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Roles       []Role `json:"roles"`
	Steps       []Step `json:"steps"`
	// Source is where it was read: "built in", or its file.
	Source string `json:"-"`
}

func (f Flow) Role(name string) (Role, bool) {
	for _, r := range f.Roles {
		if r.Name == name {
			return r, true
		}
	}
	return Role{}, false
}

func (f Flow) StepIndex(name string) int {
	for i, s := range f.Steps {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// Gates are the steps a run may stop after for you.
func (f Flow) Gates() []string {
	var out []string
	for _, s := range f.Steps {
		if s.Gate {
			out = append(out, s.Name)
		}
	}
	return out
}

// RoleLabel is what role name is called in f.
func (f Flow) RoleLabel(name string) string {
	if r, ok := f.Role(name); ok && r.Label != "" {
		return r.Label
	}
	return name
}

// resolved fills in what a flow leaves to defaults: labels, and the
// agent read.
func (f Flow) resolved() Flow {
	out := f
	out.Roles = append([]Role(nil), f.Roles...)
	out.Steps = append([]Step(nil), f.Steps...)
	for i, r := range out.Roles {
		if r.Label == "" {
			out.Roles[i].Label = r.Name
		}
		c := ParseChoice(r.Agent)
		out.Roles[i].Kind, out.Roles[i].Model, out.Roles[i].Effort = c.Kind, c.Model, c.Effort
	}
	for i, s := range out.Steps {
		if s.Label == "" {
			out.Steps[i].Label = s.Name
		}
	}
	return out
}

// WithAgents is f with its roles played by the agents chosen for them,
// role name → "kind", "kind:model", either with "@effort".
func (f Flow) WithAgents(agents map[string]string) Flow {
	out := f
	out.Roles = append([]Role(nil), f.Roles...)
	for i, r := range out.Roles {
		if s, ok := agents[r.Name]; ok && s != "" {
			c := ParseChoice(s)
			out.Roles[i].Agent = s
			out.Roles[i].Kind, out.Roles[i].Model, out.Roles[i].Effort = c.Kind, c.Model, c.Effort
		}
	}
	return out
}

//go:embed flows/*.json
var builtinFS embed.FS

var builtin struct {
	once  sync.Once
	flows []Flow
}

// Builtin are the flows mad comes with, the default first.
func Builtin() []Flow {
	builtin.once.Do(func() { builtin.flows = readBuiltin() })
	return append([]Flow(nil), builtin.flows...)
}

func readBuiltin() []Flow {
	var out []Flow
	for _, name := range []string{"design-impl-review", "impl-review"} {
		data, err := builtinFS.ReadFile("flows/" + name + ".json")
		if err != nil {
			panic(err)
		}
		f, err := Parse(data)
		if err != nil {
			panic(fmt.Sprintf("built-in flow %s: %v", name, err))
		}
		f.Source = "built in"
		out = append(out, f)
	}
	return out
}

// UserFlows is where a user keeps flows of their own.
func UserFlows() string { return filepath.Join(paths.ConfigDir(), "flows") }

// ProjectFlows is where a project keeps its flows.
func ProjectFlows(root string) string { return filepath.Join(root, ".mad", "flows") }

// Flows are the flows a run in project root can follow: mad's own, the
// user's, the project's; one of the same name as an earlier one takes its
// place. A file that doesn't read or check is left out, and said so in
// the error.
func Flows(root string) ([]Flow, error) {
	flows := Builtin()
	var errs []error
	for _, dir := range []string{UserFlows(), ProjectFlows(root)} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
		for _, file := range files {
			f, err := ReadFile(file)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			replaced := false
			for i := range flows {
				if flows[i].Name == f.Name {
					flows[i], replaced = f, true
				}
			}
			if !replaced {
				flows = append(flows, f)
			}
		}
	}
	return flows, errors.Join(errs...)
}

// FlowByName finds a flow for project root ("" for no project: the built
// in ones only); "" is the default one.
func FlowByName(root, name string) (Flow, bool) {
	flows := Builtin()
	if root != "" {
		flows, _ = Flows(root)
	}
	if name == "" {
		return flows[0], true
	}
	for _, f := range flows {
		if f.Name == name {
			return f, true
		}
	}
	return Flow{}, false
}

// ReadFile reads and checks a flow file.
func ReadFile(path string) (Flow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Flow{}, err
	}
	f, err := Parse(data)
	if err != nil {
		return Flow{}, fmt.Errorf("%s: %w", paths.Short(path), err)
	}
	f.Source = path
	return f, nil
}

// Parse reads a flow and checks it.
func Parse(data []byte) (Flow, error) {
	var f Flow
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Flow{}, fmt.Errorf("not a flow: %v", strings.TrimPrefix(err.Error(), "json: "))
	}
	if err := Check(f); err != nil {
		return Flow{}, err
	}
	return f.resolved(), nil
}

var (
	nameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	placeholderRe = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)
	placeholders  = map[string]bool{"task": true, "run": true, "base": true, "review": true, "round": true}
)

// Check says everything that is wrong with a flow, or nil: what a runner
// can't follow, or would follow other than its author meant.
func Check(f Flow) error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !nameRe.MatchString(f.Name) {
		bad("name %q: lowercase letters, digits and dashes", f.Name)
	}
	if len(f.Roles) == 0 {
		bad("no roles")
	}
	if len(f.Steps) == 0 {
		bad("no steps")
	}
	roles := map[string]bool{}
	for _, r := range f.Roles {
		switch {
		case !nameRe.MatchString(r.Name):
			bad("role %q: name it with lowercase letters, digits and dashes", r.Name)
		case roles[r.Name]:
			bad("role %s: twice", r.Name)
		}
		roles[r.Name] = true
		c := ParseChoice(r.Agent)
		switch c.Kind {
		case "claude":
			if c.Effort != "" && !contains(claudeEfforts, c.Effort) {
				bad("role %s: claude's effort is one of %s, not %q", r.Name, strings.Join(claudeEfforts, ", "), c.Effort)
			}
		case "codex":
		case "":
			bad("role %s: no agent (claude:opus, claude:sonnet, codex, codex:<model>, …)", r.Name)
		default:
			bad("role %s: agent %q: a run works with claude and codex", r.Name, r.Agent)
		}
	}
	steps := map[string]int{}
	for i, s := range f.Steps {
		at := fmt.Sprintf("step %s", s.Name)
		switch {
		case !nameRe.MatchString(s.Name):
			bad("step %q: name it with lowercase letters, digits and dashes", s.Name)
		case steps[s.Name] > 0:
			bad("%s: twice", at)
		}
		steps[s.Name] = i + 1
		if !roles[s.Role] {
			bad("%s: no role %q", at, s.Role)
		}
		if strings.TrimSpace(string(s.Prompt)) == "" {
			bad("%s: no prompt", at)
		}
		for _, text := range []Text{s.Prompt, s.Again} {
			for _, m := range placeholderRe.FindAllStringSubmatch(string(text), -1) {
				if !placeholders[m[1]] {
					bad("%s: unknown placeholder {{%s}} (there are {{task}}, {{run}}, {{base}}, {{review}}, {{round}})", at, m[1])
				}
			}
		}
		for _, q := range s.Ask {
			switch {
			case !roles[q]:
				bad("%s: asks %q, which is no role", at, q)
			case q == s.Role:
				bad("%s: a role can't ask itself", at)
			}
		}
		switch {
		case s.Review && s.Back == "":
			bad("%s: a review needs back: the step its changes go back to", at)
		case s.Review && steps[s.Back] == 0:
			bad("%s: back %q is no step before it", at, s.Back)
		case !s.Review && (s.Back != "" || s.MaxRounds != 0):
			bad("%s: back and max_rounds are for review steps", at)
		}
		if s.MaxRounds < 0 {
			bad("%s: max_rounds below 0", at)
		}
		if r, ok := f.Role(s.Role); ok && r.ReadOnly && !s.Review {
			bad("%s: its role %s is read only, so it can only review", at, s.Role)
		}
	}
	return errors.Join(errs...)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// render fills a step's placeholders.
func render(tpl string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		if v, ok := vars[placeholderRe.FindStringSubmatch(m)[1]]; ok {
			return v
		}
		return m
	})
}

// What a step's message gets when the flow has no Again for it: the step
// changes go back to, and a review that comes round again.
const (
	defaultFix = `Review findings (round {{round}}):
{{review}}

Fix them one by one; where you disagree, say why.`
	defaultRereview = `The work has been changed after your last review. Review it again, git diff {{base}}: are those problems solved, and are there new ones? At most 5.`
)

// ending is what mad adds to every message of step s: how to end the
// reply, so the runner knows what comes next.
func (f Flow) ending(s Step) string {
	var b strings.Builder
	if len(s.Ask) > 0 {
		var who []string
		for _, q := range s.Ask {
			who = append(who, "the "+f.RoleLabel(q)+fmt.Sprintf(" (@%s)", q))
		}
		fmt.Fprintf(&b, "\n\nIf something you depend on is unclear, you may ask %s: make the last line QUESTION(@role): your question, and stop to wait for the answer.",
			strings.Join(who, " or "))
	}
	if s.Review {
		b.WriteString("\n\nMake the last line just VERDICT: APPROVE or VERDICT: CHANGES.")
	} else {
		b.WriteString("\n\nWhen done, sum up in at most 3 lines, and make the last line just DONE.")
	}
	return b.String()
}

// Marshal writes f as a flow file says it: what its author left to
// defaults left out.
func Marshal(f Flow) ([]byte, error) {
	out := f
	out.Roles = append([]Role(nil), f.Roles...)
	out.Steps = append([]Step(nil), f.Steps...)
	for i, r := range out.Roles {
		if r.Label == r.Name {
			out.Roles[i].Label = ""
		}
	}
	for i, s := range out.Steps {
		if s.Label == s.Name {
			out.Steps[i].Label = ""
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

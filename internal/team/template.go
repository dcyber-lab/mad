// Package team starts agents in roles together, in a channel with a lead:
// a team stands, talks on the bus and hands work over with handoffs,
// where a run (package run) pushes one task through steps and hands over
// through files. A template names the roles; Start spawns them, puts them
// in a new channel, makes one the lead and posts the goal to it. Each
// agent's part is told to it through mad's MCP server instructions
// (Brief), so it holds across sleep, restarts and rebuilds.
//
// Templates are JSON files: mad's own (built in, see teams/), the user's
// in ~/.config/mad/teams/ and a project's in .mad/teams/, a later one
// taking the place of an earlier one of the same name. The teams that
// run are kept in bus/teams.json.
package team

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
	"github.com/dcyber-lab/mad/internal/run"
)

// Limits: how many roles a team has, and how many teams run at once.
const (
	MaxRoles = 6
	MaxTeams = 3
)

// Role is a part in a team: a run's role (name, agent, read_only), who
// leads, and what it is told about its part.
type Role struct {
	run.Role
	Lead  bool     `json:"lead,omitempty"`
	Brief run.Text `json:"brief,omitempty"`
}

// Template is a team's definition.
type Template struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Topic       run.Text `json:"topic,omitempty"` // the channel's; {{goal}}
	Roles       []Role   `json:"roles"`
	// Kickoff is what the person posts to the lead to start; {{goal}}.
	Kickoff run.Text `json:"kickoff,omitempty"`
	// Source is where it was read: "built in", or its file.
	Source string `json:"-"`
}

// LeadRole is the role that leads.
func (t Template) LeadRole() (Role, bool) {
	for _, r := range t.Roles {
		if r.Lead {
			return r, true
		}
	}
	return Role{}, false
}

// resolved fills in the agent each role's choice names.
func (t Template) resolved() Template {
	out := t
	out.Roles = append([]Role(nil), t.Roles...)
	for i := range out.Roles {
		out.Roles[i].setAgent(out.Roles[i].Agent)
	}
	return out
}

func (r *Role) setAgent(s string) {
	c := run.ParseChoice(s)
	r.Agent, r.Kind, r.Model, r.Effort = s, c.Kind, c.Model, c.Effort
}

// WithAgents is t with its roles played by the agents chosen for them,
// role name → "kind", "kind:model", either with "@effort".
func (t Template) WithAgents(agents map[string]string) Template {
	out := t
	out.Roles = append([]Role(nil), t.Roles...)
	for i := range out.Roles {
		if s := agents[out.Roles[i].Name]; s != "" {
			out.Roles[i].setAgent(s)
		}
	}
	return out
}

//go:embed teams/*.json
var builtinFS embed.FS

var builtin struct {
	once      sync.Once
	templates []Template
}

// Builtin are the templates mad comes with, the default first.
func Builtin() []Template {
	builtin.once.Do(func() {
		for _, name := range []string{"dev"} {
			data, err := builtinFS.ReadFile("teams/" + name + ".json")
			if err != nil {
				panic(err)
			}
			t, err := Parse(data)
			if err != nil {
				panic(fmt.Sprintf("built-in team %s: %v", name, err))
			}
			t.Source = "built in"
			builtin.templates = append(builtin.templates, t)
		}
	})
	return append([]Template(nil), builtin.templates...)
}

// UserDir is where a user keeps templates of their own.
func UserDir() string { return filepath.Join(paths.ConfigDir(), "teams") }

// ProjectDir is where a project keeps its templates.
func ProjectDir(root string) string { return filepath.Join(root, ".mad", "teams") }

// Templates are the templates a team in project root can follow: mad's
// own, the user's, the project's. A file that doesn't read or check is
// left out, and said so in the error.
func Templates(root string) ([]Template, error) {
	list := Builtin()
	dirs := []string{UserDir()}
	if root != "" {
		dirs = append(dirs, ProjectDir(root))
	}
	var errs []error
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
		for _, file := range files {
			t, err := ReadFile(file)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			replaced := false
			for i := range list {
				if list[i].Name == t.Name {
					list[i], replaced = t, true
				}
			}
			if !replaced {
				list = append(list, t)
			}
		}
	}
	return list, errors.Join(errs...)
}

// ByName finds a template for project root; "" is the default one.
func ByName(root, name string) (Template, bool) {
	list, _ := Templates(root)
	if name == "" {
		return list[0], true
	}
	for _, t := range list {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// ReadFile reads and checks a template file.
func ReadFile(path string) (Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Template{}, err
	}
	t, err := Parse(data)
	if err != nil {
		return Template{}, fmt.Errorf("%s: %w", paths.Short(path), err)
	}
	t.Source = path
	return t, nil
}

// Parse reads a template and checks it.
func Parse(data []byte) (Template, error) {
	var t Template
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Template{}, fmt.Errorf("not a team: %v", strings.TrimPrefix(err.Error(), "json: "))
	}
	t = t.resolved()
	if err := Check(t); err != nil {
		return Template{}, err
	}
	return t, nil
}

var (
	nameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	placeholderRe = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)
)

// Check says everything that is wrong with a template, or nil.
func Check(t Template) error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !nameRe.MatchString(t.Name) {
		bad("name %q: lowercase letters, digits and dashes", t.Name)
	}
	switch {
	case len(t.Roles) == 0:
		bad("no roles")
	case len(t.Roles) > MaxRoles:
		bad("%d roles: a team has at most %d", len(t.Roles), MaxRoles)
	}
	names, leads := map[string]bool{}, 0
	for _, r := range t.Roles {
		switch {
		case !nameRe.MatchString(r.Name):
			bad("role %q: name it with lowercase letters, digits and dashes", r.Name)
		case names[r.Name]:
			bad("role %s: twice", r.Name)
		}
		names[r.Name] = true
		if r.Lead {
			leads++
		}
		c := run.ParseChoice(r.Agent)
		switch c.Kind {
		case "claude", "codex":
			if c.Effort != "" && !contains(run.Efforts(run.Choice{Kind: c.Kind, Model: c.Model}), c.Effort) {
				bad("role %s: effort %q is not one of %s's", r.Name, c.Effort, c.Kind)
			}
		case "":
			bad("role %s: no agent (claude:opus, claude:sonnet, codex, codex:<model>, …)", r.Name)
		default:
			bad("role %s: agent %q: a team works with claude and codex", r.Name, r.Agent)
		}
		if r.Lead && r.ReadOnly {
			bad("role %s: the lead can't be read only", r.Name)
		}
	}
	switch {
	case leads == 0 && len(t.Roles) > 0:
		bad("no lead: mark one role with \"lead\": true")
	case leads > 1:
		bad("%d leads: exactly one role leads", leads)
	}
	for _, text := range []run.Text{t.Topic, t.Kickoff} {
		for _, m := range placeholderRe.FindAllStringSubmatch(string(text), -1) {
			if m[1] != "goal" {
				bad("unknown placeholder {{%s}} (there is {{goal}})", m[1])
			}
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

// render fills a template text's {{goal}}.
func render(tpl, goal string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		if placeholderRe.FindStringSubmatch(m)[1] == "goal" {
			return goal
		}
		return m
	})
}

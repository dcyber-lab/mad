package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// RoleRec is a role of a running team. It keeps what the template said
// then (Spec, Brief): an agent's brief reads from here, so the team goes
// on as it was started when the template file changes.
type RoleRec struct {
	Role     string `json:"role"`
	Agent    string `json:"agent"` // the agent's id; "" until it is started
	Spec     string `json:"spec"`  // claude:opus@high, codex
	Lead     bool   `json:"lead,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Brief    string `json:"brief,omitempty"`
}

// Record is a team that runs: bus/teams.json lists them.
type Record struct {
	Name       string    `json:"name"`
	Template   string    `json:"template"`
	Channel    string    `json:"channel"`
	Project    string    `json:"project"`
	Branch     string    `json:"branch,omitempty"`
	Permission string    `json:"permission"`
	CreatedAt  time.Time `json:"created_at"`
	Roles      []RoleRec `json:"roles"`
}

// Lead is the record's lead role.
func (r Record) Lead() (RoleRec, bool) {
	for _, x := range r.Roles {
		if x.Lead {
			return x, true
		}
	}
	return RoleRec{}, false
}

// Replaceable in tests.
var (
	spawn = drive.Spawn
	kill  = deck.KillAgent
	sleep = deck.SleepAgent
	now   = time.Now
)

type file struct {
	Teams []Record `json:"teams"`
}

func recordsFile() string { return filepath.Join(paths.StateDir(), "bus", "teams.json") }

// update runs fn on the records under a lock, saving them when fn says
// to (returns true).
func update(fn func(*file) (bool, error)) error {
	path := recordsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	var f file
	if err := paths.ReadJSON(path, &f); err != nil {
		return err
	}
	save, err := fn(&f)
	if err != nil || !save {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(path, data)
}

// Running lists the teams that run, as agents in st say they do: a role
// whose agent was removed by hand drops out of its team (and its
// memberships), and a team without agents is gone.
func Running(st *state.State) ([]Record, error) {
	if st == nil {
		var err error
		if st, err = state.Load(); err != nil {
			return nil, err
		}
	}
	var out []Record
	var gone, emptied []string
	var fresh *state.State
	err := update(func(f *file) (bool, error) {
		changed := false
		var kept []Record
		for _, r := range f.Teams {
			var roles []RoleRec
			for _, x := range r.Roles {
				if x.Agent != "" {
					if _, a := st.FindAgent(x.Agent); a == nil && !exists(&fresh, x.Agent) {
						gone = append(gone, x.Agent)
						changed = true
						continue
					}
				}
				roles = append(roles, x)
			}
			if len(roles) == 0 && len(r.Roles) > 0 {
				changed = true
				emptied = append(emptied, r.Channel)
				continue
			}
			r.Roles = roles
			kept = append(kept, r)
		}
		f.Teams = kept
		out = append([]Record(nil), kept...)
		return changed, nil
	})
	for _, id := range gone {
		bus.Forget(id)
	}
	for _, ch := range emptied {
		_ = bus.Archive(ch)
	}
	return out, err
}

// exists looks for agent id in the state as it is now (read once, into
// *fresh): a snapshot taken before an agent was started would drop it.
// An unreadable state counts as the agent being there.
func exists(fresh **state.State, id string) bool {
	if *fresh == nil {
		var err error
		if *fresh, err = state.Load(); err != nil {
			*fresh = nil
			return true
		}
	}
	_, a := (*fresh).FindAgent(id)
	return a != nil
}

// Find finds the running team called name.
func Find(st *state.State, name string) (Record, bool, error) {
	list, err := Running(st)
	if err != nil {
		return Record{}, false, err
	}
	for _, r := range list {
		if r.Name == name {
			return r, true, nil
		}
	}
	return Record{}, false, nil
}

// Options says what team to start.
type Options struct {
	Project    string // a directory in the project
	Template   string // "" for the default
	Name       string // the team's and its channel's; the goal's first words by default
	Goal       string
	Permission string // run.PermAuto by default
	Branch     string // all roles work in this branch's worktree; none: the project's directory
	// Agents choose another agent for a role: role name → "claude:opus@high".
	Agents map[string]string
	Kinds  []agent.Kind
}

// Started is a team that was started, and what to tell the person.
type Started struct {
	Record
	// Notes are things that need the person (an agent that came up asking
	// something) or did not go through (the kickoff).
	Notes []string
}

// Start starts a team: its channel, its agents, the lead, and the goal
// posted to the lead. It spawns the roles one after another and returns
// when all can take a message; on failure what was started is removed.
func Start(o Options) (*Started, error) {
	root, _ := paths.ProjectRoot(paths.Expand(o.Project))
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", o.Project)
	}
	goal := strings.TrimSpace(o.Goal)
	if goal == "" {
		return nil, errors.New("the team needs a goal")
	}
	tpl, ok := ByName(root, o.Template)
	if !ok {
		return nil, fmt.Errorf("no team template %q (mad team ls)", o.Template)
	}
	for role, s := range o.Agents {
		if !hasRole(tpl, role) {
			return nil, fmt.Errorf("team %s has no role %q", tpl.Name, role)
		}
		if k := run.ParseChoice(s).Kind; k != "claude" && k != "codex" {
			return nil, fmt.Errorf("%s: unknown agent %q", role, s)
		}
	}
	tpl = tpl.WithAgents(o.Agents)
	if err := Check(tpl); err != nil {
		return nil, err
	}
	perm := o.Permission
	switch {
	case perm == "":
		perm = run.PermAuto
	case !contains(run.Permissions, perm):
		return nil, fmt.Errorf("permission is one of %s, not %q", strings.Join(run.Permissions, ", "), perm)
	}
	branch := strings.TrimSpace(o.Branch)
	dir := root
	if branch != "" {
		if err := git.ValidBranch(branch); err != nil {
			return nil, err
		}
		dir = git.WorktreeDir(root, branch)
	}

	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	teams, err := Running(st)
	if err != nil {
		return nil, err
	}
	if len(teams) >= MaxTeams {
		return nil, fmt.Errorf("%d teams run already (the most there are at once); remove one first: mad team rm NAME", len(teams))
	}
	name, err := teamName(o.Name, goal, teams)
	if err != nil {
		return nil, err
	}
	if branch != "" {
		if err := git.AddWorktree(root, branch, dir); err != nil {
			return nil, err
		}
	}

	rec := Record{Name: name, Template: tpl.Name, Channel: name, Project: root, Branch: branch, Permission: perm, CreatedAt: now()}
	for _, r := range tpl.Roles {
		rec.Roles = append(rec.Roles, RoleRec{Role: r.Name, Spec: r.Agent, Lead: r.Lead, ReadOnly: r.ReadOnly, Brief: strings.TrimSpace(string(r.Brief))})
	}
	topic := strings.TrimSpace(render(string(tpl.Topic), goal))
	if _, err := bus.Create(name, topic, nil, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		return nil, err
	}
	// The record first: an agent's brief is read from it as its MCP server
	// starts, with the agent.
	if err := update(func(f *file) (bool, error) { f.Teams = append(f.Teams, rec); return true, nil }); err != nil {
		_ = bus.Archive(name)
		return nil, err
	}
	out := &Started{Record: rec}
	fail := func(err error) (*Started, error) {
		_ = Remove(name)
		return nil, err
	}

	taken := map[string]bool{}
	if p := st.FindProject(root); p != nil {
		for _, a := range p.Agents {
			taken[a.Name] = true
		}
	}
	var ids []string
	leadID, lead := "", ""
	for i, r := range tpl.Roles {
		agentName := r.Name
		if taken[agentName] {
			agentName = r.Name + "-" + name
		}
		args := append(r.Args(perm), r.WorktreeArgs(branchDir(branch, dir))...)
		a, err := spawn(drive.Spec{Kind: r.Kind, Model: r.Model, Name: agentName, Dir: dir, Args: args, Role: r.Name, Team: name}, o.Kinds)
		if a == nil {
			return fail(fmt.Errorf("starting the %s: %w", r.Name, err))
		}
		if err != nil { // it came up asking something: the person must answer
			out.Notes = append(out.Notes, fmt.Sprintf("the %s asks you something at its start: %v", r.Name, err))
		}
		rec.Roles[i].Agent = a.ID
		out.Record = rec
		if uerr := setAgent(name, r.Name, a.ID); uerr != nil {
			// The record lacks it, and so would a removal: do it here.
			_ = kill(a.ID)
			_ = state.Update(func(st *state.State) error { st.RemoveAgent(a.ID); return nil })
			return fail(uerr)
		}
		ids = append(ids, a.ID)
		if r.Lead {
			leadID, lead = a.ID, drive.Label(a)
		}
	}
	if err := bus.Invite(name, ids); err != nil {
		return fail(err)
	}
	if err := bus.SetLead(name, leadID); err != nil {
		return fail(err)
	}
	kick := strings.TrimSpace(render(string(tpl.Kickoff), goal))
	if kick == "" {
		kick = "Goal: " + goal
	}
	if _, err := bus.Post(name, bus.From{ID: bus.You, Label: bus.You}, "@"+lead+" "+kick, 0); err != nil {
		out.Notes = append(out.Notes, "the kickoff was not posted: "+err.Error())
	}
	return out, nil
}

// branchDir is dir when the roles work in a worktree, "" when they work
// in the project's own directory.
func branchDir(branch, dir string) string {
	if branch == "" {
		return ""
	}
	return dir
}

func hasRole(t Template, name string) bool {
	for _, r := range t.Roles {
		if r.Name == name {
			return true
		}
	}
	return false
}

// teamName is the name for a new team: name if given (and free), else
// the goal's first words, made free.
func teamName(name, goal string, teams []Record) (string, error) {
	taken := func(n string) bool {
		for _, t := range teams {
			if t.Name == n {
				return true
			}
		}
		_, exists, _ := bus.Find(n)
		return exists
	}
	if name = strings.TrimSpace(name); name != "" {
		if !nameRe.MatchString(name) {
			return "", fmt.Errorf("team name %q: lowercase letters, digits and dashes", name)
		}
		if taken(name) {
			return "", fmt.Errorf("a team or channel called %s exists; pick another name", name)
		}
		return name, nil
	}
	base := strings.TrimPrefix(run.BranchFor(goal, now()), "run/")
	if !nameRe.MatchString(base) {
		base = "team"
	}
	name = base
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name, nil
}

// setAgent records the agent of a team's role.
func setAgent(name, role, id string) error {
	return update(func(f *file) (bool, error) {
		for i := range f.Teams {
			if f.Teams[i].Name != name {
				continue
			}
			for j := range f.Teams[i].Roles {
				if f.Teams[i].Roles[j].Role == role {
					f.Teams[i].Roles[j].Agent = id
					return true, nil
				}
			}
		}
		return false, fmt.Errorf("team %s is gone", name)
	})
}

// Sleep puts every agent of team name to sleep: it frees what they hold,
// and a message wakes one. It says how many slept, and which would not.
func Sleep(name string) (int, error) {
	rec, ok, err := Find(nil, name)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("no team %q (mad team ls)", name)
	}
	n := 0
	var errs []error
	for _, x := range rec.Roles {
		if x.Agent == "" {
			continue
		}
		slept, err := sleep(x.Agent, false)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", x.Role, err))
		}
		if slept {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Remove ends team name: its agents are killed and removed, forgotten by
// the bus, its channel archived and its record deleted.
func Remove(name string) error {
	rec, ok, err := Find(nil, name)
	if err != nil {
		return err
	}
	if !ok {
		// A record that never got its first agent is still there to clear.
		_ = update(func(f *file) (bool, error) { return dropRecord(f, name), nil })
		return fmt.Errorf("no team %q (mad team ls)", name)
	}
	var errs []error
	for _, x := range rec.Roles {
		if x.Agent == "" {
			continue
		}
		id := x.Agent
		if err := state.Update(func(st *state.State) error { st.RemoveAgent(id); return nil }); err != nil {
			errs = append(errs, err)
		}
		status.RemoveHook(id)
		queue.Forget(id)
		if err := kill(id); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", x.Role, err))
		}
		bus.Forget(id)
	}
	if rec.Channel != "" {
		if err := bus.Archive(rec.Channel); err != nil {
			errs = append(errs, err)
		}
	}
	if err := update(func(f *file) (bool, error) { return dropRecord(f, name), nil }); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func dropRecord(f *file, name string) bool {
	for i, t := range f.Teams {
		if t.Name == name {
			f.Teams = append(f.Teams[:i], f.Teams[i+1:]...)
			return true
		}
	}
	return false
}

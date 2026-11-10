package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/create"
	"github.com/dcyber-lab/mad/internal/creator"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

// autoPerHour is how many agents or teams may be started in an hour without
// asking (agents_create_without_asking); past it each asks again.
var autoPerHour = 5

var agentNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// mcpProject is the project ref names (its name or its path), or the
// project agent id is in when ref is empty.
func mcpProject(st *state.State, id, ref string) (*state.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if p, _ := st.FindAgent(id); p != nil {
			return p, nil
		}
		return nil, errors.New("give a project: this agent is in none")
	}
	var names []string
	for _, p := range st.Projects {
		if strings.EqualFold(p.Name, ref) || p.Path == paths.Expand(ref) {
			return p, nil
		}
		names = append(names, p.Name)
	}
	return nil, fmt.Errorf("no project %q in this deck (projects: %s)", ref, strings.Join(names, ", "))
}

// mcpKind checks a kind an agent can start for a message: one of the
// deck's, with a shell excluded (it takes no message).
func mcpKind(kind string) error {
	var names []string
	for _, k := range kinds() {
		if k.Name == kind && kind != "shell" {
			return nil
		}
		if k.Name != "shell" {
			names = append(names, k.Name)
		}
	}
	return fmt.Errorf("no agent kind %q (kinds: %s)", kind, strings.Join(names, ", "))
}

func cleanModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, "-") || strings.ContainsAny(model, " \t\n") {
		return "", fmt.Errorf("model %q is not a model name", model)
	}
	return model, nil
}

// mcpFromChannel checks the channel an agent says it is answering in (the
// bus does not keep track of that): one that exists and the agent is in.
func mcpFromChannel(id, ref string) (string, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if ref == "" {
		return "", nil
	}
	c, ok, _ := bus.Find(ref)
	if !ok || strings.HasPrefix(c.ID, "dm.") {
		return "", fmt.Errorf("no channel %q for from_channel: the one you are answering in (bus_who)", ref)
	}
	if !containsStr(c.Members, id) {
		return "", fmt.Errorf("you are not in #%s", ref)
	}
	return c.ID, nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// mcpAgentNew asks the person to let agent id start an agent with its
// first message, then starts it.
func mcpAgentNew(ctx context.Context, id, kind, model, project, name, message, channel, from string) (string, error) {
	_, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	if from, err = mcpFromChannel(id, from); err != nil {
		return "", err
	}
	if kind, model = strings.TrimSpace(kind), strings.TrimSpace(model); strings.Contains(kind, ":") && model == "" {
		kind, model, _ = strings.Cut(kind, ":")
	}
	if err := mcpKind(kind); err != nil {
		return "", err
	}
	if model, err = cleanModel(model); err != nil {
		return "", err
	}
	p, err := mcpProject(st, id, project)
	if err != nil {
		return "", err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("message: what the new agent is to do, as its first message")
	}
	if name = strings.TrimSpace(name); name != "" {
		if !agentNameRe.MatchString(name) {
			return "", fmt.Errorf("name %q: lowercase letters, digits and dashes", name)
		}
		for _, a := range st.OrderedAgents() {
			if strings.EqualFold(drive.Label(a), name) {
				return "", fmt.Errorf("an agent called %s exists; pick another name", name)
			}
		}
	} else {
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		name = kind + "-" + hex.EncodeToString(suffix)
	}
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if channel != "" {
		if c, ok, _ := bus.Find(channel); !ok || strings.HasPrefix(c.ID, "dm.") {
			return "", fmt.Errorf("no channel %q: bus_who lists them, bus_channel creates one", channel)
		}
	}
	return mcpCreateRun(ctx, id, from, create.Spec{Type: create.TypeAgent, Name: name, Project: p.Path, ProjectName: p.Name,
		Kind: kind, Model: model, Message: message, Channel: channel})
}

// mcpRole is a role as team_start takes it.
type mcpRole struct {
	Role, Kind, Model string
	Lead              bool
}

// mcpTeamStart asks the person to let agent id start a team, then starts
// it through the same path as the page's POST /api/team.
func mcpTeamStart(ctx context.Context, id, name, goal, project, tmpl, from string, roles []mcpRole) (string, error) {
	_, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	if from, err = mcpFromChannel(id, from); err != nil {
		return "", err
	}
	if goal = strings.TrimSpace(goal); goal == "" {
		return "", errors.New("goal: what the team is to achieve")
	}
	p, err := mcpProject(st, id, project)
	if err != nil {
		return "", err
	}
	root, _ := paths.ProjectRoot(p.Path)
	tmpl = strings.TrimSpace(tmpl)
	spec := create.Spec{Type: create.TypeTeam, Project: p.Path, ProjectName: p.Name, Goal: goal}
	switch {
	case tmpl != "" && len(roles) > 0:
		return "", errors.New("give a template or roles, not both")
	case len(roles) > 0:
		var tr []team.Role
		leads := 0
		for _, r := range roles {
			kind := strings.TrimSpace(r.Kind)
			model, err := cleanModel(r.Model)
			if err != nil {
				return "", fmt.Errorf("role %s: %v", r.Role, err)
			}
			agentSpec := kind
			if model != "" {
				agentSpec += ":" + model
			}
			if r.Lead {
				leads++
			}
			tr = append(tr, team.Role{Role: madrun.Role{Name: strings.TrimSpace(r.Role), Agent: agentSpec}, Lead: r.Lead})
		}
		if leads == 0 { // the first leads unless told otherwise
			tr[0].Lead = true
		}
		tpl := team.Template{Name: "custom", Topic: "{{goal}}", Roles: tr}
		if err := team.Check(tpl); err != nil {
			return "", err
		}
		spec.Template, spec.Topic = "", "{{goal}}"
		spec.Roles = specRoles(tpl)
	default:
		// Read now, so the card lists what the team will be and it starts as
		// listed, whatever the file says later.
		t, ok := team.ByName(root, tmpl)
		if !ok {
			var names []string
			list, _ := team.Templates(root)
			for _, t := range list {
				names = append(names, t.Name)
			}
			return "", fmt.Errorf("no team template %q (templates: %s)", tmpl, strings.Join(names, ", "))
		}
		spec.Template, spec.Topic, spec.Kickoff = t.Name, string(t.Topic), string(t.Kickoff)
		spec.Roles = specRoles(t)
	}
	if spec.Name, err = team.CheckName(name, goal); err != nil {
		return "", err
	}
	return mcpCreateRun(ctx, id, from, spec)
}

// specRoles are template t's roles as a request lists them.
func specRoles(t team.Template) []create.Role {
	var out []create.Role
	for _, r := range t.Roles {
		c := madrun.ParseChoice(r.Agent)
		out = append(out, create.Role{Role: r.Name, Kind: c.Kind, Model: c.Model, Effort: c.Effort, Lead: r.Lead, ReadOnly: r.ReadOnly, Brief: strings.TrimSpace(string(r.Brief))})
	}
	return out
}

// mcpCreateRun asks the person for spec, and returns at once: the tool call
// does not wait (it would hold the agent's whole mad server). The window
// starts what is allowed and tells the agent by DM (package creator); a
// spent budget refuses now. With the deck's agents_create_without_asking it
// starts at once, up to autoPerHour an hour, then asks. from is the channel
// the agent is answering in.
func mcpCreateRun(_ context.Context, id, from string, spec create.Spec) (string, error) {
	me, _, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	if !bus.SpendOK(time.Now()) {
		return "", bus.ErrSpent
	}
	if cfg, _ := deck.LoadConfig(); cfg.CreateWithoutAsking && create.AutoSince(time.Now().Add(-time.Hour)) < autoPerHour {
		r, err := create.AddAuto(me.Label, id, from, spec)
		if err != nil {
			return "", err
		}
		// A team takes minutes to come up and this server is serial: start it
		// in the background and tell the agent when it is done, as for an
		// allowed request.
		creator.Go(func() { creator.Run(r) })
		return mcpJSON(map[string]any{
			"status":  "creating",
			"request": r.ID,
			"item":    "create:" + r.ID,
			"say":     "starting it; you'll be told by a message from mad when it is ready (or why not). End your turn now.",
		})
	}
	r, err := create.Add(me.Label, id, from, spec)
	if err != nil {
		return "", err
	}
	_ = poke.Send(poke.Poll) // the window and the deck show it now
	return mcpJSON(map[string]any{
		"status":  "asked",
		"item":    "create:" + r.ID,
		"request": r.ID,
		"say":     "asked the person (request " + r.ID + "); you'll be told by a message from mad when they answer, within about 10 minutes. End your turn now; do not ask again.",
	})
}

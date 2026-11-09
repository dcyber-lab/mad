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
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

// Replaceable in tests.
var (
	startTeam     = team.Start
	createTimeout = 10 * time.Minute
	createPoll    = 500 * time.Millisecond
)

const (
	createDeclined = "the person declined"
	createNoAnswer = "the person hasn't answered yet"
)

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
			spec.Roles = append(spec.Roles, create.Role{Role: strings.TrimSpace(r.Role), Kind: kind, Model: model, Lead: r.Lead})
		}
		if leads == 0 { // the first leads unless told otherwise
			tr[0].Lead, spec.Roles[0].Lead = true, true
		}
		if err := team.Check(team.Template{Name: "custom", Topic: "{{goal}}", Roles: tr}); err != nil {
			return "", err
		}
	default:
		t, ok := team.ByName(root, tmpl)
		if !ok {
			var names []string
			list, _ := team.Templates(root)
			for _, t := range list {
				names = append(names, t.Name)
			}
			return "", fmt.Errorf("no team template %q (templates: %s)", tmpl, strings.Join(names, ", "))
		}
		spec.Template = t.Name
	}
	if spec.Name, err = team.CheckName(name, goal); err != nil {
		return "", err
	}
	return mcpCreateRun(ctx, id, from, spec)
}

// mcpCreateRun gets the person's word for spec (unless the deck's
// agents_create_without_asking is on) and carries it out. A spent budget
// refuses it at once, before the person is asked. from is the channel the
// agent is answering in: the outcome is told there too.
func mcpCreateRun(ctx context.Context, id, from string, spec create.Spec) (string, error) {
	me, _, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	if !bus.SpendOK(time.Now()) {
		return "", bus.ErrSpent
	}
	reqID := ""
	// end records how the request ended, and tells the channel it came from.
	end := func(status string, res create.Result, event, text string) {
		if reqID == "" {
			return
		}
		_ = create.Finish(reqID, status, res)
		if from != "" {
			_ = bus.Event(from, event, "create:"+reqID, text)
		}
		_ = poke.Send(poke.Poll)
	}
	if cfg, _ := deck.LoadConfig(); !cfg.CreateWithoutAsking {
		r, err := create.Add(me.Label, id, from, spec)
		if err != nil {
			return "", err
		}
		reqID = r.ID
		_ = poke.Send(poke.Poll) // the window and the deck show it now
		deadline := time.NewTimer(createTimeout)
		defer deadline.Stop()
		tick := time.NewTicker(createPoll)
		defer tick.Stop()
		for answered := false; !answered; {
			select {
			case <-ctx.Done():
				end(create.Expired, create.Result{}, bus.EventCreateExpired, "You did not confirm in time; nothing was created.")
				return "", ctx.Err()
			case <-deadline.C:
				end(create.Expired, create.Result{}, bus.EventCreateExpired, "You did not confirm in time; nothing was created.")
				return createNoAnswer, nil
			case <-tick.C:
				cur, err := create.Get(r.ID)
				switch {
				case err != nil:
					return createNoAnswer, nil // taken back
				case cur.Status == create.Declined:
					if from != "" {
						_ = bus.Event(from, bus.EventCreateDeclined, "create:"+reqID, "You declined; nothing was created.")
					}
					return createDeclined, nil
				case cur.Status == create.Changes:
					if from != "" {
						_ = bus.Event(from, bus.EventCreateChanges, "create:"+reqID, "You asked for changes: "+cur.Reply)
					}
					return "the person wants changes: " + cur.Reply, nil
				case cur.Status == create.Allowed:
					answered = true
				case cur.Status != create.Waiting:
					return createNoAnswer, nil
				}
			}
		}
		if !bus.SpendOK(time.Now()) { // while it waited
			end(create.Failed, create.Result{Reason: bus.ErrSpent.Error()}, bus.EventCreateFailed, "Nothing was created: today's budget is spent.")
			return "", bus.ErrSpent
		}
	}
	defer func() { _ = poke.Send(poke.Poll) }()
	var out string
	var res create.Result
	if spec.Type == create.TypeTeam {
		out, res, err = startTeamFor(spec)
	} else {
		out, res, err = startAgentFor(me, spec)
	}
	what := "Agent " + spec.Name
	if spec.Type == create.TypeTeam {
		what = "Team " + spec.Name
	}
	if err != nil {
		end(create.Failed, create.Result{Reason: err.Error()}, bus.EventCreateFailed, what+" was not created: "+err.Error())
		return "", err
	}
	end(create.Created, res, bus.EventCreated, what+" was created.")
	return out, nil
}

func startAgentFor(from bus.From, spec create.Spec) (string, create.Result, error) {
	a, err := spawnAgent(drive.Spec{Kind: spec.Kind, Model: spec.Model, Dir: spec.Project, Name: spec.Name}, kinds())
	if a == nil {
		return "", create.Result{}, err
	}
	res := create.Result{Agent: a.ID}
	out := map[string]any{"agent": map[string]string{"id": a.ID, "label": drive.Label(a)}}
	if err != nil { // it came up asking something: the person must answer
		out["note"] = "it came up asking something the person must answer: " + err.Error()
	}
	label := drive.Label(a)
	var perr error
	if spec.Channel != "" {
		if perr = bus.Join(spec.Channel, a.ID); perr == nil {
			_, perr = bus.Post(spec.Channel, from, "@"+label+" "+spec.Message, 0)
		}
		res.Channel = spec.Channel
	} else {
		res.Channel, _, perr = bus.DM(from, a.ID, spec.Message)
	}
	out["channel"] = res.Channel
	if perr != nil {
		out["warning"] = "the agent was started but its first message was not sent: " + perr.Error()
	}
	text, err := mcpJSON(out)
	return text, res, err
}

func startTeamFor(spec create.Spec) (string, create.Result, error) {
	o := team.Options{Project: spec.Project, Template: spec.Template, Name: spec.Name, Goal: spec.Goal, Kinds: kinds()}
	for _, r := range spec.Roles {
		agentSpec := r.Kind
		if r.Model != "" {
			agentSpec += ":" + r.Model
		}
		o.Roles = append(o.Roles, team.Role{Role: madrun.Role{Name: r.Role, Agent: agentSpec}, Lead: r.Lead})
	}
	got, err := startTeam(o)
	if err != nil {
		return "", create.Result{}, err
	}
	agents := map[string]string{}
	for _, r := range got.Roles {
		agents[r.Role] = r.Agent
	}
	text, err := mcpJSON(map[string]any{"team": got.Name, "channel": got.Channel, "agents": agents, "notes": got.Notes})
	return text, create.Result{Team: got.Name, Channel: got.Channel}, err
}

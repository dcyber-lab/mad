// Package creator carries out what the person answers to an agent's request
// to start an agent or a team (package create), and tells the agent how it
// went. The agent's tool call does not wait: the process that records the
// answer (the window) starts what was allowed, in the background, and says
// the outcome in a DM to the asking agent and, when it was asked in a
// channel, as an event line there.
package creator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/create"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

// Replaceable in tests.
var (
	Spawn     = drive.Spawn
	StartTeam = team.Start
	// Kinds are the agent kinds the deck knows.
	Kinds = func() []agent.Kind {
		if k, _ := agent.Load(); k != nil {
			return k
		}
		return agent.Builtin()
	}
	// Go runs the follow-up of an answer: in the background, since a team
	// takes a while to come up.
	Go = func(f func()) { go f() }
)

// Answer records the person's answer to request id and follows it up in
// the background: starts what was allowed, tells the agent.
func Answer(id string, allow bool, reply string) error {
	if err := create.Answer(id, allow, reply); err != nil {
		return err
	}
	Go(func() { Settle(id) })
	return nil
}

// Settle carries out the answer given to request id.
func Settle(id string) {
	r, err := create.Get(id)
	if err != nil {
		return
	}
	switch r.Status {
	case create.Declined, create.Changes:
		conclude(r)
	case create.Allowed:
		_, _, _ = Run(r)
	}
}

// Sweep ends the requests nobody will answer: one that waited longer than
// create.Expiry, one whose agent is gone from the deck (it must not start
// anything), and an allowed one that never finished starting (mad was
// closed on it). The agent, if there, is told.
func Sweep(st *state.State) {
	for _, r := range create.All() {
		_, asker := st.FindAgent(r.ByID)
		switch {
		case r.Status == create.Waiting && (asker == nil || time.Since(r.At) > create.Expiry):
			end(r, create.Expired, create.Result{})
		case r.Status == create.Allowed && time.Since(r.AnsweredAt) > create.Interrupted:
			end(r, create.Failed, create.Result{Reason: "mad was closed while it started"})
		}
	}
}

// end ends r as status and tells of it.
func end(r create.Request, status string, res create.Result) {
	if err := create.Finish(r.ID, status, res); err != nil {
		return
	}
	if got, err := create.Get(r.ID); err == nil && got.Status == status {
		conclude(got)
	}
}

// Run starts what request r (allowed) asks for, as the asking agent, and
// ends it as created or failed. It returns what the tool reports (JSON
// with an outcome) for a request made without asking, which is not told
// by DM. An agent that is gone starts nothing.
func Run(r create.Request) (create.Result, string, error) {
	st, err := state.Load()
	if err != nil {
		end(r, create.Failed, create.Result{Reason: err.Error()})
		return create.Result{}, "", err
	}
	_, a := st.FindAgent(r.ByID)
	if a == nil {
		end(r, create.Expired, create.Result{Reason: "the agent that asked is gone"})
		return create.Result{}, "", fmt.Errorf("the agent that asked is gone: nothing was created")
	}
	if !bus.SpendOK(time.Now()) { // while it waited
		end(r, create.Failed, create.Result{Reason: bus.ErrSpent.Error()})
		return create.Result{}, "", bus.ErrSpent
	}
	by := bus.From{ID: a.ID, Label: drive.Label(a), Kind: a.Kind}
	var res create.Result
	var out map[string]any
	if r.Spec.Type == create.TypeTeam {
		res, out, err = startTeam(by, r.Spec)
	} else {
		res, out, err = startAgent(by, r.Spec)
	}
	if err != nil {
		end(r, create.Failed, create.Result{Reason: err.Error()})
		return create.Result{}, "", err
	}
	end(r, create.Created, res)
	out["status"] = create.Created
	out["say"] = created(r.Spec, res)
	b, _ := json.MarshalIndent(out, "", "  ")
	return res, string(b), nil
}

func startAgent(by bus.From, spec create.Spec) (create.Result, map[string]any, error) {
	a, err := Spawn(drive.Spec{Kind: spec.Kind, Model: spec.Model, Dir: spec.Project, Name: spec.Name}, Kinds())
	if a == nil {
		return create.Result{}, nil, err
	}
	label := drive.Label(a)
	res := create.Result{Agent: a.ID}
	out := map[string]any{"agent": map[string]string{"id": a.ID, "label": label}}
	if err != nil { // it came up asking something: the person must answer
		out["note"] = "it came up asking something the person must answer: " + err.Error()
	}
	var perr error
	if spec.Channel != "" {
		if perr = bus.Join(spec.Channel, a.ID); perr == nil {
			_, perr = bus.Post(spec.Channel, by, "@"+label+" "+spec.Message, 0)
		}
		res.Channel = spec.Channel
	} else {
		res.Channel, _, perr = bus.DM(by, a.ID, spec.Message)
	}
	out["channel"] = res.Channel
	if perr != nil {
		out["warning"] = "the agent was started but its first message was not sent: " + perr.Error()
	}
	return res, out, nil
}

func startTeam(by bus.From, spec create.Spec) (create.Result, map[string]any, error) {
	got, err := StartTeam(team.Options{Project: spec.Project, Name: spec.Name, Goal: spec.Goal, Custom: Template(spec), By: &by, Kinds: Kinds()})
	if err != nil {
		return create.Result{}, nil, err
	}
	agents := map[string]string{}
	for _, r := range got.Roles {
		agents[r.Role] = r.Agent
	}
	return create.Result{Team: got.Name, Channel: got.Channel}, map[string]any{"team": got.Name, "channel": got.Channel, "agents": agents, "notes": got.Notes}, nil
}

// Template is the team template a team request names, as the card listed it.
func Template(spec create.Spec) *team.Template {
	t := team.Template{Name: spec.Template, Topic: run.Text(spec.Topic), Kickoff: run.Text(spec.Kickoff)}
	if t.Name == "" {
		t.Name = "custom"
	}
	for _, r := range spec.Roles {
		agentSpec := r.Kind
		if r.Model != "" {
			agentSpec += ":" + r.Model
		}
		if r.Effort != "" {
			agentSpec += "@" + r.Effort
		}
		t.Roles = append(t.Roles, team.Role{Role: run.Role{Name: r.Role, Agent: agentSpec, ReadOnly: r.ReadOnly}, Lead: r.Lead, Brief: run.Text(r.Brief)})
	}
	return &t
}

func what(spec create.Spec) string {
	if spec.Type == create.TypeTeam {
		return "team " + spec.Name
	}
	return "agent " + spec.Name
}

func created(spec create.Spec, res create.Result) string {
	switch {
	case res.Team != "":
		return fmt.Sprintf("team %s was created (channel #%s)", spec.Name, res.Channel)
	}
	return fmt.Sprintf("agent %s was created (id %s, %s)", spec.Name, res.Agent, res.Channel)
}

// conclude says how r ended: an event line in the channel it was asked in,
// and a DM to the agent (not for one made without asking: its tool call
// had the result).
func conclude(r create.Request) {
	ref := "create:" + r.ID
	data := map[string]string{"type": r.Spec.Type, "name": r.Spec.Name}
	for k, v := range map[string]string{"agent": r.Result.Agent, "team": r.Result.Team, "channel": r.Result.Channel, "reason": r.Result.Reason, "words": r.Reply} {
		if v != "" {
			data[k] = v
		}
	}
	var event, line, say string
	switch r.Status {
	case create.Created:
		event, line, say = bus.EventCreated, strings.ToUpper(what(r.Spec)[:1])+what(r.Spec)[1:]+" was created.", created(r.Spec, r.Result)
	case create.Declined:
		event, line, say = bus.EventCreateDeclined, "You declined; nothing was created.", "the person declined"
	case create.Changes:
		event, line, say = bus.EventCreateChanges, "You asked for changes: "+r.Reply, "the person wants changes: "+r.Reply
	case create.Expired:
		event, line, say = bus.EventCreateExpired, "You did not confirm in time; nothing was created.", "the person hasn't answered, and the request expired: nothing was created"
		if r.Result.Reason != "" {
			line, say = "Nothing was created: "+r.Result.Reason+".", "nothing was created: "+r.Result.Reason
		}
	case create.Failed:
		event, line, say = bus.EventCreateFailed, "Nothing was created: "+r.Result.Reason, "it was allowed but failed: "+r.Result.Reason
	default:
		return
	}
	if r.From != "" {
		_ = bus.Event(r.From, event, ref, line, data)
	}
	if !r.Auto {
		out, _ := json.Marshal(map[string]any{"status": r.Status, "request": r.ID, "say": say, "data": data})
		_, _ = bus.TellAgent(r.ByID, fmt.Sprintf("[mad] your request %s to create %s: %s\n%s", r.ID, what(r.Spec), say, out))
	}
	_ = poke.Send(poke.Poll)
}

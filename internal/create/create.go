// Package create holds the requests of agents to start an agent or a team
// (the MCP tools agent_new and team_start) while the person has not
// answered. A request is a file in the state directory: the agent's MCP
// server waits on it in one process, the window or the deck's inbox shows
// it and records the answer in another.
package create

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// What a request asks for.
const (
	TypeAgent = "agent"
	TypeTeam  = "team"
)

// Where a request stands: Waiting for the person; Allowed (being created);
// then how it ended: Created, Declined, Changes (declined with words that
// went back to the agent), Expired (no answer) or Failed (allowed, but it
// did not start).
const (
	Waiting  = "waiting"
	Allowed  = "allowed"
	Created  = "created"
	Declined = "declined"
	Changes  = "changes"
	Expired  = "expired"
	Failed   = "failed"
)

// Expiry is how long a request waits: its agent gives up on it sooner
// (about ten minutes), so one older was left by an agent that died.
const Expiry = 15 * time.Minute

// Keep is how long a request that ended is kept, for a card to show how.
const Keep = 24 * time.Hour

// ErrGone: no such request waits (answered already, or given up on).
var ErrGone = errors.New("that was answered already")

// Role is a role of a team to start.
type Role struct {
	Role  string `json:"role"`
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`
	Lead  bool   `json:"lead,omitempty"`
}

// Spec is what would be created, in the words a card lists it with.
type Spec struct {
	Type string `json:"type"` // TypeAgent or TypeTeam
	Name string `json:"name,omitempty"`
	// Project is its path; ProjectName what the deck calls it.
	Project     string `json:"project"`
	ProjectName string `json:"project_name,omitempty"`
	// An agent: its kind, model, first message and the channel it joins.
	Kind    string `json:"kind,omitempty"`
	Model   string `json:"model,omitempty"`
	Message string `json:"message,omitempty"`
	Channel string `json:"channel,omitempty"`
	// A team: its goal, the template it follows, or its own roles.
	Goal     string `json:"goal,omitempty"`
	Template string `json:"template,omitempty"`
	Roles    []Role `json:"roles,omitempty"`
}

// Request is a Spec asked for by an agent.
type Request struct {
	ID   string    `json:"id"`
	By   string    `json:"by"`    // the asking agent's label
	ByID string    `json:"by_id"` // and its id
	At   time.Time `json:"at"`
	// From is the channel the agent was answering in, "" for none: its view
	// shows the card there.
	From   string `json:"from,omitempty"`
	Status string `json:"status"`
	// Reply is what the person said with Changes.
	Reply string `json:"reply,omitempty"`
	// Result is what was created (Created) or why not (Failed).
	Result Result    `json:"result,omitempty"`
	DoneAt time.Time `json:"done_at,omitzero"`
	Spec   Spec      `json:"spec"`
}

// Result is how an allowed request went: the new agent's id, or the team's
// name, its channel and its agents; the channel an agent joined; why it
// failed.
type Result struct {
	Agent   string `json:"agent,omitempty"`
	Team    string `json:"team,omitempty"`
	Channel string `json:"channel,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Outcome is how a request stands, for a card's result line: status is one
// of the statuses above ("creating" for Allowed).
type Outcome struct {
	Status string `json:"status"`
	Result
	Reply string    `json:"reply,omitempty"`
	At    time.Time `json:"at,omitzero"`
}

// Outcome is how r stands.
func (r Request) Outcome() Outcome {
	st := r.Status
	if st == Allowed {
		st = "creating"
	}
	return Outcome{Status: st, Result: r.Result, Reply: r.Reply, At: r.DoneAt}
}

func dir() string { return filepath.Join(paths.StateDir(), "create") }

func path(id string) string { return filepath.Join(dir(), id+".json") }

// valid: ids are made here, so nothing else names a file.
func valid(id string) bool {
	if len(id) != 10 || !strings.HasPrefix(id, "c-") {
		return false
	}
	_, err := hex.DecodeString(id[2:])
	return err == nil
}

func write(r Request) error {
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(path(r.ID), b)
}

// Add queues spec as asked for by agent byID (labeled by), answering in
// channel from ("" for none).
func Add(by, byID, from string, spec Spec) (Request, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	r := Request{ID: "c-" + hex.EncodeToString(b), By: by, ByID: byID, From: from, At: time.Now().UTC().Truncate(time.Millisecond), Status: Waiting, Spec: spec}
	return r, write(r)
}

// Get is request id as it stands.
func Get(id string) (Request, error) {
	var r Request
	if !valid(id) {
		return r, ErrGone
	}
	if err := paths.ReadJSON(path(id), &r); err != nil || r.ID != id {
		return Request{}, ErrGone
	}
	return r, nil
}

// Answer records the person's answer to a waiting request: allow, or deny,
// with words (reply) that tell the agent what to change.
func Answer(id string, allow bool, reply string) error {
	r, err := Get(id)
	if err != nil || r.Status != Waiting {
		return ErrGone
	}
	switch reply = strings.TrimSpace(reply); {
	case allow:
		r.Status = Allowed
	case reply != "":
		r.Status, r.Reply, r.DoneAt = Changes, reply, time.Now().UTC()
	default:
		r.Status, r.DoneAt = Declined, time.Now().UTC()
	}
	return write(r)
}

// Finish records how a request that was not waiting any more ended: Created
// or Failed after Allowed, Expired from Waiting. It does nothing to one
// that ended already.
func Finish(id, status string, res Result) error {
	r, err := Get(id)
	if err != nil {
		return err
	}
	if r.Status != Waiting && r.Status != Allowed || r.Status == Allowed && status == Expired {
		return nil
	}
	r.Status, r.Result, r.DoneAt = status, res, time.Now().UTC()
	return write(r)
}

// Remove drops request id.
func Remove(id string) {
	if valid(id) {
		_ = os.Remove(path(id))
	}
}

// All is the requests there are: those waiting and those that ended in the
// last Keep, oldest first. One that waited longer than Expiry was left by
// an agent that died: it ends as Expired.
func All() []Request {
	ents, _ := os.ReadDir(dir())
	var out []Request
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".json")
		r, err := Get(id)
		if err != nil {
			continue
		}
		if r.Status == Waiting && time.Since(r.At) > Expiry {
			_ = Finish(id, Expired, Result{})
			r, _ = Get(id)
		}
		if r.Status != Waiting && r.Status != Allowed && time.Since(r.DoneAt) > Keep || r.Status == Allowed && time.Since(r.At) > Keep {
			Remove(id)
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// List is the requests that wait on the person, oldest first.
func List() []Request {
	var out []Request
	for _, r := range All() {
		if r.Status == Waiting {
			out = append(out, r)
		}
	}
	return out
}

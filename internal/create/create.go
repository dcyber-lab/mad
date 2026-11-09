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

// Where a request stands.
const (
	Waiting  = "waiting"
	Allowed  = "allowed"
	Declined = "declined"
)

// Expiry is how long a request is listed: its agent gives up on it sooner
// (about ten minutes), so one older was left by an agent that died.
const Expiry = 15 * time.Minute

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
	ID     string    `json:"id"`
	By     string    `json:"by"`    // the asking agent's label
	ByID   string    `json:"by_id"` // and its id
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Spec   Spec      `json:"spec"`
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

// Add queues spec as asked for by agent byID (labeled by).
func Add(by, byID string, spec Spec) (Request, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	r := Request{ID: "c-" + hex.EncodeToString(b), By: by, ByID: byID, At: time.Now().UTC().Truncate(time.Millisecond), Status: Waiting, Spec: spec}
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

// Answer records the person's answer to a waiting request.
func Answer(id string, allow bool) error {
	r, err := Get(id)
	if err != nil || r.Status != Waiting {
		return ErrGone
	}
	r.Status = Declined
	if allow {
		r.Status = Allowed
	}
	return write(r)
}

// Remove drops request id.
func Remove(id string) {
	if valid(id) {
		_ = os.Remove(path(id))
	}
}

// List is the requests that wait on the person, oldest first. One left
// from longer than Expiry ago is dropped.
func List() []Request {
	ents, _ := os.ReadDir(dir())
	var out []Request
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".json")
		r, err := Get(id)
		switch {
		case err != nil:
		case time.Since(r.At) > Expiry:
			Remove(id)
		case r.Status == Waiting:
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

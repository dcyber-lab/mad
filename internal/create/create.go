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
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// What a request asks for.
const (
	TypeAgent = "agent"
	TypeTeam  = "team"
)

// Where a request stands: Waiting for the person; Allowed (the person
// said yes) and Creating (a process claimed it and is starting it);
// then how it ended: Created, Declined, Changes (declined with words that
// went back to the agent), Expired (no answer) or Failed (allowed, but it
// did not start).
const (
	Waiting  = "waiting"
	Allowed  = "allowed"
	Creating = "creating" // claimed by the process that starts it
	Created  = "created"
	Declined = "declined"
	Changes  = "changes"
	Expired  = "expired"
	Failed   = "failed"
)

// Expiry is how long a request waits for the person.
const Expiry = 10 * time.Minute

// Interrupted is how long an allowed request may take to start before it
// is taken for one that mad was closed on.
const Interrupted = 15 * time.Minute

// Keep is how long a request that ended is kept, for a card to show how.
const Keep = 24 * time.Hour

// ErrGone: no such request waits (answered already, or given up on).
var ErrGone = errors.New("that was answered already")

// Role is a role of a team to start, as it will be: the card lists it and
// the team starts with exactly these.
type Role struct {
	Role     string `json:"role"`
	Kind     string `json:"kind"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Lead     bool   `json:"lead,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	NoEdits  bool   `json:"no_edits,omitempty"`
	// Brief is what the role is told about its part.
	Brief string `json:"brief,omitempty"`
	// Skill is the skill it works by.
	Skill string `json:"skill,omitempty"`
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
	Goal string `json:"goal,omitempty"`
	// Template is the template's name, as read when the request was made;
	// Roles are its roles (or the agent's own), Topic and Kickoff its texts
	// ({{goal}} in them is the goal): all resolved then, so the team starts
	// as the card said even if the template's file changes.
	Template string `json:"template,omitempty"`
	Roles    []Role `json:"roles,omitempty"`
	Topic    string `json:"topic,omitempty"`
	Kickoff  string `json:"kickoff,omitempty"`
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
	// Auto: made without asking (agents_create_without_asking), so it was
	// never on the desk.
	Auto bool `json:"auto,omitempty"`
	// AnsweredAt is when the person allowed it.
	AnsweredAt time.Time `json:"answered_at,omitzero"`
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
	// Note is something the agent should know of a request that went through:
	// the new agent came up asking something, its first message did not go.
	Note string `json:"note,omitempty"`
	// Agents are a team's agents by role.
	Agents []RoleAgent `json:"agents,omitempty"`
}

// RoleAgent is the agent that plays a role of a team.
type RoleAgent struct {
	Role  string `json:"role"`
	Agent string `json:"agent"`
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
		st = Creating
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

// lock holds the lock of the requests while fn runs: every read-modify-write
// of a request is under it, since several processes (the window, the deck,
// the agents' servers) answer, claim and sweep them.
func lock(fn func() error) error {
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(dir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

// modify runs fn on request id under the lock and writes it when fn says
// to. A request that is not there is ErrGone.
func modify(id string, fn func(*Request) (bool, error)) error {
	return lock(func() error {
		r, err := Get(id)
		if err != nil {
			return err
		}
		save, err := fn(&r)
		if err != nil || !save {
			return err
		}
		return write(r)
	})
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
	r, err := newRequest(by, byID, from, spec)
	if err != nil {
		return r, err
	}
	return r, lock(func() error { return write(r) })
}

func newRequest(by, byID, from string, spec Spec) (Request, error) {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	return Request{ID: "c-" + hex.EncodeToString(b), By: by, ByID: byID, From: from, At: time.Now().UTC().Truncate(time.Millisecond), Status: Waiting, Spec: spec}, err
}

// AddAuto records a request that is carried out without asking: Allowed
// from the start, never waiting.
func AddAuto(by, byID, from string, spec Spec) (Request, error) {
	r, err := newRequest(by, byID, from, spec)
	r.Auto, r.Status, r.AnsweredAt = true, Allowed, r.At
	if err != nil {
		return r, err
	}
	return r, lock(func() error { return write(r) })
}

// AutoSince counts the requests carried out without asking since t.
func AutoSince(t time.Time) int {
	n := 0
	for _, r := range All() {
		if r.Auto && r.At.After(t) {
			n++
		}
	}
	return n
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
// with words (reply) that tell the agent what to change. A request that is
// not waiting any more (answered, expired) is ErrGone.
func Answer(id string, allow bool, reply string) error {
	err := modify(id, func(r *Request) (bool, error) {
		if r.Status != Waiting {
			return false, ErrGone
		}
		switch reply = strings.TrimSpace(reply); {
		case allow:
			r.Status, r.AnsweredAt = Allowed, time.Now().UTC()
		case reply != "":
			r.Status, r.Reply, r.DoneAt = Changes, reply, time.Now().UTC()
		default:
			r.Status, r.DoneAt = Declined, time.Now().UTC()
		}
		return true, nil
	})
	if err != nil {
		return ErrGone
	}
	return nil
}

// Claim takes an allowed request to start it: Allowed becomes Creating, for
// one caller only. It is false when it is not allowed (any more), or
// another process claimed it.
func Claim(id string) (Request, bool) {
	var got Request
	ok := false
	_ = modify(id, func(r *Request) (bool, error) {
		if r.Status != Allowed {
			return false, nil
		}
		r.Status, r.AnsweredAt = Creating, time.Now().UTC()
		got, ok = *r, true
		return true, nil
	})
	return got, ok
}

// Finish ends request id as status, if it still stands at from, the status
// the caller saw: a compare-and-set, so an end meant for a request that
// was answered meanwhile (a sweep's expiry, an Allow landing with it) does
// not happen. It reports whether it did.
func Finish(id, from, status string, res Result) (bool, error) {
	done := false
	err := modify(id, func(r *Request) (bool, error) {
		if r.Status != from {
			return false, nil
		}
		r.Status, r.Result, r.DoneAt = status, res, time.Now().UTC()
		done = true
		return true, nil
	})
	return done, err
}

// Remove drops request id.
func Remove(id string) {
	if valid(id) {
		_ = os.Remove(path(id))
	}
}

// All is the requests there are: those waiting and those that ended in the
// last Keep, oldest first. Ending the ones that waited too long or lost
// their agent is creator.Sweep's.
func All() []Request {
	ents, _ := os.ReadDir(dir())
	var out []Request
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".json")
		r, err := Get(id)
		if err != nil {
			continue
		}
		if r.Status != Waiting && r.Status != Allowed && r.Status != Creating && time.Since(r.DoneAt) > Keep || time.Since(r.At) > Keep+Expiry {
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

// Put writes r as it is: for a test to age or end a request.
func Put(r Request) error { return lock(func() error { return write(r) }) }

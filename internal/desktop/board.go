package desktop

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
)

// The task board in the window (bus.Board): its cards with the snapshot,
// and what the person does to a task.

// boardCards is the board as of snap, the agents' statuses as it has them.
func boardCards(st *state.State, snap Snapshot) []bus.Card {
	statuses := map[string]string{}
	for _, p := range snap.Projects {
		for _, a := range p.Agents {
			s := a.Status
			if s == "done" {
				s = "idle" // done is the deck's word for idle and not looked at
			}
			statuses[a.ID] = s
		}
	}
	now := time.Now()
	cards, err := bus.Board(st, statuses, now)
	if err != nil || cards == nil {
		cards = []bus.Card{}
	}
	// A skill an agent saved waits for the person too.
	pending, _ := skill.PendingList()
	for _, p := range pending {
		cards = append(cards, bus.Card{
			Kind: "skill", Title: "/" + p.Name, Column: "waiting-you", Why: "approve",
			OwnerLabel: p.By, Age: now.Sub(p.At), Note: p.Description,
		})
	}
	return cards
}

// boardTask answers POST /api/board/task {id, status, note, owner}: the
// person changes a task. A new owner is told in the task's channel (or a
// DM), which wakes it.
func (s *Server) boardTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID                  string
		Status, Note, Owner *string
	}
	if !decode(w, r, &req) {
		return
	}
	before, ok, err := bus.FindTask(req.ID)
	if err != nil || !ok {
		fail(w, http.StatusNotFound, errors.New("no such task"))
		return
	}
	t, err := bus.SetTask(you, bus.TaskChange{ID: req.ID, Status: req.Status, Note: req.Note, Owner: req.Owner})
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if req.Owner != nil && *req.Owner != "" && *req.Owner != bus.You && *req.Owner != before.Owner {
		if err := tellAgent(t, *req.Owner, fmt.Sprintf("%s is yours now: %s. task_list shows it; keep it current with task_set.", t.ID, t.Title)); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	}
	reply(w, t)
}

// boardNudge answers POST /api/board/nudge {id}: the person asks the
// owner how its task stands.
func (s *Server) boardNudge(w http.ResponseWriter, r *http.Request) {
	var req struct{ ID string }
	if !decode(w, r, &req) {
		return
	}
	t, ok, err := bus.FindTask(req.ID)
	if err != nil || !ok {
		fail(w, http.StatusNotFound, errors.New("no such task"))
		return
	}
	if t.Owner == "" || t.Owner == bus.You {
		fail(w, http.StatusBadRequest, errors.New("it has no agent to ask"))
		return
	}
	if err := tellAgent(t, t.Owner, fmt.Sprintf("how does %s (%s) stand? It is yours on the board: task_list shows it. Finish it, hand it on, or say what blocks it, and keep it current with task_set.", t.ID, t.Title)); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"ok": true})
}

var you = bus.From{ID: bus.You, Label: bus.You}

// tellAgent says text to agent id about task t, as the person: in the
// task's channel, mentioning it, else in a DM.
func tellAgent(t bus.Task, id, text string) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	_, a := st.FindAgent(id)
	if a == nil {
		return errors.New("no such agent")
	}
	if c, ok, _ := bus.Find(t.Channel); ok && t.Channel != "" && !c.Archived {
		_, err = bus.Post(t.Channel, you, "@"+drive.Label(a)+" "+text, 0)
		return err
	}
	_, _, err = bus.DM(you, id, text)
	return err
}

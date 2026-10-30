package desktop

import (
	"errors"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
)

// The scheduled strip of the Board page (bus.Timer): the snapshot carries
// the timers, and POST /api/timer is what the person does to them.

// timersOf is the timers for the snapshot; never nil.
func timersOf() []bus.Timer {
	ts, err := bus.Timers()
	if err != nil || ts == nil {
		return []bus.Timer{}
	}
	return ts
}

// timerSet answers POST /api/timer {op, id, every, channel, text, action}:
// op add makes a routine (action "report" posts the channel's progress
// report; else text, with its @mentions), rm deletes a timer (a routine,
// or a check-in), pause and resume stop and go on, fire goes off now.
func (s *Server) timerSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op, ID, Every, Channel, Text, Action string
	}
	if !decode(w, r, &req) {
		return
	}
	now := time.Now()
	var err error
	var out any = map[string]any{"ok": true}
	switch req.Op {
	case "add":
		var t bus.Timer
		t, err = bus.AddRoutine(bus.RoutineSpec{Every: req.Every, Channel: req.Channel, Text: req.Text, Report: req.Action == bus.ActionReport}, now)
		out = t
	case "rm":
		err = bus.RemoveTimer(req.ID, bus.You)
	case "pause", "resume":
		err = bus.PauseTimer(req.ID, req.Op == "pause", now)
	case "fire":
		err = bus.FireTimer(req.ID, now, statusesOf(s.poll.Snapshot()))
	default:
		err = errors.New("op is add, rm, pause, resume or fire")
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	changed()
	reply(w, out)
}

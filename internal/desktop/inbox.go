package desktop

import (
	"errors"
	"net/http"

	"github.com/dcyber-lab/mad/internal/attention"
)

// inboxAct answers POST /api/inbox {key, verb, option, text}: the person
// acts on an inbox item, from the sidebar, the phone, home or the board.
// The page sends the item's key and the act it chose; what that reaches
// (a host, a channel, a skill) is the Desk's to know.
//
//	204: done
//	409: attention.ErrGone: answered already, or no longer waiting; or
//	     attention.ErrChanged: it asks something else now. Its text is
//	     what the person is told.
//	400: attention.ErrNotOffered, or the act failed (its error)
func (s *Server) inboxAct(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key attention.Key `json:"key"`
		attention.Answer
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := s.poll.desk.Act(req.Key, req.Answer); {
	case err == nil:
		changed() // the deck looks again now
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, attention.ErrChanged):
		fail(w, http.StatusConflict, errors.New("It asks something else now: see the new prompt"))
	case errors.Is(err, attention.ErrGone):
		fail(w, http.StatusConflict, errors.New("Already handled: it is not waiting on you any more"))
	default:
		fail(w, http.StatusBadRequest, err)
	}
}

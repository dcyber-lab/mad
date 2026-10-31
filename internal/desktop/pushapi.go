package desktop

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/phone"
	"github.com/dcyber-lab/mad/internal/push"
)

// A browser asks to be told what the deck's notifications say (package
// push): GET /api/push?endpoint= gives mad's key and whether that browser
// is subscribed; POST subscribes it, DELETE forgets it, and POST
// /api/push/test sends it a notification to see.

func (s *Server) pushState(w http.ResponseWriter, r *http.Request) {
	key, err := push.PublicKey()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	on := false
	if ep := r.URL.Query().Get("endpoint"); ep != "" {
		subs, _ := push.List()
		for _, sub := range subs {
			on = on || sub.Endpoint == ep
		}
	}
	reply(w, map[string]any{"key": key, "on": on})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	var sub push.Subscription
	if !decode(w, r, &sub) {
		return
	}
	// Bound to the device that asks, whatever the body says: revoking it
	// ends its notifications too.
	sub.Device = deviceOf(r)
	if err := phone.SubscribeDevice(sub); err != nil {
		if errors.Is(err, phone.ErrDeviceGone) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"on": true})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if err := push.Unsubscribe(r.URL.Query().Get("endpoint")); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, map[string]any{"on": false})
}

func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	m := push.Message{Title: "mad", Body: "Notifications are on: an agent that needs you, or is done, says so here.", Tag: "test"}
	if err := push.Send(ctx, m, r.URL.Query().Get("endpoint")); err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, map[string]any{"sent": true})
}

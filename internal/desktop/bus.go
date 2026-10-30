package desktop

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
)

// The bus in the window (package bus): its channels and DMs with the
// sidebar (in the snapshot), a channel's messages, and the person's posts.

// BusMember is an agent in a channel, as the window draws it.
type BusMember struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// BusChannel is a channel or DM as the sidebar lists it.
type BusChannel struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"` // #name, @agent (a DM with you), a ↔ b
	Topic   string      `json:"topic,omitempty"`
	DM      bool        `json:"dm,omitempty"`
	Yours   bool        `json:"yours,omitempty"` // a DM with you: you may write in it
	Members []BusMember `json:"members"`
	Last    int         `json:"last"`
	Unread  int         `json:"unread,omitempty"`
	Paused  bool        `json:"paused,omitempty"`
	Lead    string      `json:"lead,omitempty"` // the agent that answers for its work
	Team    *BusTeam    `json:"team,omitempty"` // the team it is the channel of (team.go)
}

// busChannels lists the channels, then the DMs, for the snapshot.
func busChannels(st *state.State) []BusChannel {
	out := []BusChannel{}
	chans, err := bus.Channels()
	if err != nil || len(chans) == 0 {
		return out
	}
	yours, _ := bus.YourChannels()
	teams := busTeams(st)
	for _, c := range chans {
		bc := BusChannel{ID: c.ID, Title: "#" + c.ID, Topic: c.Topic, DM: strings.HasPrefix(c.ID, "dm."),
			Last: yours[c.ID].Last, Unread: yours[c.ID].Unread, Paused: c.Paused, Lead: c.Lead, Members: []BusMember{}, Team: teams[c.ID]}
		var names []string
		for _, m := range c.Members {
			if m == bus.You {
				bc.Yours = true
				continue
			}
			if _, a := st.FindAgent(m); a != nil {
				bc.Members = append(bc.Members, BusMember{a.ID, drive.Label(a), a.Kind})
				names = append(names, drive.Label(a))
			}
		}
		if bc.DM {
			bc.Yours = bc.Yours || strings.HasSuffix(c.ID, "."+bus.You) || strings.Contains(c.ID, "."+bus.You+".")
			if bc.Yours {
				bc.Title = "@" + strings.Join(names, ", ")
			} else {
				bc.Title = strings.Join(names, " ↔ ")
			}
		}
		out = append(out, bc)
	}
	sort.SliceStable(out, func(i, j int) bool { return !out[i].DM && out[j].DM })
	return out
}

// busMessages answers GET /api/bus/messages?channel=&since=: a channel's
// messages after since (the last 200 without it).
func (s *Server) busMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ch := q.Get("channel")
	if _, ok, err := bus.Find(ch); err != nil || !ok {
		fail(w, http.StatusNotFound, errors.New("no such channel"))
		return
	}
	since, _ := strconv.Atoi(q.Get("since"))
	limit := 200
	if since <= 0 {
		since = 0
	}
	msgs, last, err := bus.Read(ch, since, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	c, _, _ := bus.Find(ch)
	if msgs == nil {
		msgs = []bus.Message{}
	}
	reply(w, map[string]any{"channel": ch, "messages": msgs, "last": last, "paused": c.Paused})
}

// busPost answers POST /api/bus/post {channel, text}: the person posts,
// in a channel or a DM of theirs.
func (s *Server) busPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel, Text string
		ReplyTo       int `json:"reply_to"` // the seq of the message answered
	}
	if !decode(w, r, &req) {
		return
	}
	me := bus.From{ID: bus.You, Label: bus.You}
	var res bus.PostResult
	var err error
	if strings.HasPrefix(req.Channel, "dm.") {
		ids := strings.Split(strings.TrimPrefix(req.Channel, "dm."), ".")
		if len(ids) != 2 || (ids[0] != bus.You && ids[1] != bus.You) {
			fail(w, http.StatusForbidden, errors.New("a DM between two agents is theirs: read it, or post in a channel"))
			return
		}
		to := ids[0]
		if to == bus.You {
			to = ids[1]
		}
		if _, ok, _ := bus.Find(req.Channel); ok && req.ReplyTo > 0 {
			res, err = bus.Post(req.Channel, me, req.Text, req.ReplyTo) // made: a reply in it
		} else {
			_, res, err = bus.DM(me, to, req.Text)
		}
	} else {
		res, err = bus.Post(req.Channel, me, req.Text, req.ReplyTo)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, res)
}

// busDM answers POST /api/bus/dm {to, text}: the person starts or goes on
// with a DM with an agent; the DM's channel comes back.
func (s *Server) busDM(w http.ResponseWriter, r *http.Request) {
	var req struct{ To, Text string }
	if !decode(w, r, &req) {
		return
	}
	ch, res, err := bus.DM(bus.From{ID: bus.You, Label: bus.You}, req.To, req.Text)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"channel": ch, "seq": res.Seq, "woke": res.Woke})
}

// busCreate answers POST /api/bus/channel {name, topic, members}: a new
// channel, its agents by id.
func (s *Server) busCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name, Topic string
		Members     []string
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := bus.Create(req.Name, req.Topic, req.Members, bus.From{ID: bus.You, Label: bus.You})
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, c)
}

// busRead answers POST /api/bus/read {channel, seq}: the person has read
// it so far.
func (s *Server) busRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string
		Seq     int
	}
	if !decode(w, r, &req) {
		return
	}
	if err := bus.MarkRead(bus.You, req.Channel, req.Seq); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, map[string]any{"ok": true})
}

// busResume answers POST /api/bus/resume {channel}: a paused channel goes on.
func (s *Server) busResume(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel string }
	if !decode(w, r, &req) {
		return
	}
	if err := bus.Resume(req.Channel); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"ok": true})
}

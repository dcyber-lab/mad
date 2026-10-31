package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// Last is the last message as a line of a list ("who: its first line"),
	// LastSeq its number, which the page counts what is new by, and LastAt
	// when it was said (RFC 3339).
	Last    string    `json:"last"`
	LastSeq int       `json:"last_seq"`
	LastAt  time.Time `json:"last_at,omitzero"`
	Unread  int       `json:"unread,omitempty"`
	// Mention: an unread message in it asks you; Listed: it is in the
	// short list (bus.Yours).
	Mention bool     `json:"mention,omitempty"`
	Listed  bool     `json:"listed,omitempty"`
	Paused  bool     `json:"paused,omitempty"`
	Lead    string   `json:"lead,omitempty"` // the agent that answers for its work
	Team    *BusTeam `json:"team,omitempty"` // the team it is the channel of (team.go)
}

// busChannels lists the channels, then the DMs, for the snapshot, as of
// the poller's last look at the bus.
func busChannels(st *state.State, look bus.Look) []BusChannel {
	out := []BusChannel{}
	chans, yours := look.Channels, look.Yours
	if len(chans) == 0 {
		return out
	}
	teams := busTeams(st)
	for _, c := range chans {
		y := yours[c.ID]
		bc := BusChannel{ID: c.ID, Title: "#" + c.ID, Topic: c.Topic, DM: strings.HasPrefix(c.ID, "dm."),
			Last: y.LastLine, LastSeq: y.Last, LastAt: y.LastAt, Unread: y.Unread, Mention: y.Mention, Listed: y.Listed, Paused: c.Paused, Lead: c.Lead,
			Members: []BusMember{}, Team: teams[c.ID]}
		var names []string
		gone := 0 // members no longer in the deck
		members := c.Members
		if bc.DM {
			// A DM's two sides are in its id; forgetting an agent empties
			// its members.
			members = bus.DMSides(c.ID)
		}
		for _, m := range members {
			if m == bus.You {
				bc.Yours = true
				continue
			}
			if _, a := st.FindAgent(m); a != nil {
				bc.Members = append(bc.Members, BusMember{a.ID, drive.Label(a), a.Kind})
				names = append(names, drive.Label(a))
			} else {
				gone++
				names = append(names, m[:min(len(m), 8)])
			}
		}
		if bc.DM {
			bc.Yours = bc.Yours || strings.HasSuffix(c.ID, "."+bus.You) || strings.Contains(c.ID, "."+bus.You+".")
			if !bc.Yours && len(bc.Members) == 0 && gone > 0 {
				continue // between agents both removed: nobody's any more
			}
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
	reply(w, map[string]any{"channel": ch, "messages": msgs, "last": last, "paused": c.Paused, "receipts": busReceipts(ch)})
}

// BusReceipt is how one of the person's messages stands: the agents it
// wakes, each with its state (waiting, delivered, read, replied); none
// when it wakes nobody.
type BusReceipt struct {
	Seq     int             `json:"seq"`
	Targets []BusTargetInfo `json:"targets"`
	Paused  bool            `json:"paused,omitempty"`
}

// BusTargetInfo is an agent a message wakes, by its label.
type BusTargetInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
}

// busReceipts is the receipts of the person's last messages in a channel;
// all of them, each time, so a mark moves as the agent reads. None on an
// error: the log is shown without marks.
func busReceipts(ch string) []BusReceipt {
	out := []BusReceipt{}
	rs, err := bus.Receipts(ch, 20)
	if err != nil || len(rs) == 0 {
		return out
	}
	st, _ := state.Load()
	for _, r := range rs {
		br := BusReceipt{Seq: r.Seq, Paused: r.Paused, Targets: []BusTargetInfo{}}
		for _, t := range r.Targets {
			name := t.ID[:min(len(t.ID), 8)]
			if st != nil {
				if _, a := st.FindAgent(t.ID); a != nil {
					name = drive.Label(a)
				}
			}
			br.Targets = append(br.Targets, BusTargetInfo{t.ID, name, t.State})
		}
		out = append(out, br)
	}
	return out
}

// busPicture is a picture sent with a post, as /api/send takes one: its
// bytes in base64, without a data: prefix. What it is comes from the
// bytes; the mime is not believed.
type busPicture struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
}

// maxBusBody bounds a post with pictures: bus.MaxImages of
// bus.MaxImageBytes in base64 (4 bytes for 3), and a megabyte for the rest.
const maxBusBody = bus.MaxImages*(bus.MaxImageBytes/3+1)*4 + 1<<20

// decodeBus reads a post's JSON body, which may hold pictures: bigger than
// decode allows.
func decodeBus(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBusBody)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// pictureBytes takes a post's pictures from base64, and keeps none of
// them: how many, and how big each is claimed to be, are checked before
// anything is decoded, and each is decoded and read for its type in turn.
// What it returns is stored (bus.StoreImages) only when the post goes on.
func pictureBytes(in []busPicture) ([][]byte, error) {
	if len(in) > bus.MaxImages {
		return nil, bus.TooManyImages()
	}
	for i, p := range in {
		if base64.StdEncoding.DecodedLen(len(p.Data)) > bus.MaxImageBytes+2 {
			return nil, fmt.Errorf("image %d is over the %d MB limit", i+1, bus.MaxImageBytes>>20)
		}
	}
	var datas [][]byte
	for i, p := range in {
		b, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return nil, fmt.Errorf("image %d is not base64", i+1)
		}
		if _, err := bus.CheckImage(b); err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		datas = append(datas, b)
	}
	return datas, nil
}

// keepPictures stores pictures that passed pictureBytes, for a post about
// to be made; nil for none.
func keepPictures(w http.ResponseWriter, datas [][]byte) ([]bus.Image, bool) {
	imgs, err := bus.StoreImages(datas)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return nil, false
	}
	return imgs, true
}

// postFailed answers a refused post: forbidden for another's DM, else 400.
func postFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, bus.ErrTheirDM) {
		fail(w, http.StatusForbidden, err)
		return
	}
	fail(w, http.StatusBadRequest, err)
}

// busImage answers GET /api/bus/image?id=: a picture of a message, by the
// hash that names it.
func (s *Server) busImage(w http.ResponseWriter, r *http.Request) {
	path, mime, ok := bus.ImagePath(r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

// busPost answers POST /api/bus/post {channel, text, images}: the person
// posts, in a channel or a DM of theirs.
func (s *Server) busPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel, Text string
		ReplyTo       int          `json:"reply_to"` // the seq of the message answered
		Images        []busPicture `json:"images"`
	}
	if !decodeBus(w, r, &req) {
		return
	}
	// The target first, then the pictures: nothing is kept for a post that
	// can't be made.
	if err := bus.CheckReply(req.Channel); err != nil {
		postFailed(w, err)
		return
	}
	datas, err := pictureBytes(req.Images)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	imgs, ok := keepPictures(w, datas)
	if !ok {
		return
	}
	res, err := bus.ReplyImages(req.Channel, req.Text, req.ReplyTo, imgs)
	switch {
	case errors.Is(err, bus.ErrTheirDM):
		fail(w, http.StatusForbidden, err)
		return
	case err != nil:
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, res)
}

// busDM answers POST /api/bus/dm {to, text, images}: the person starts or
// goes on with a DM with an agent; the DM's channel comes back.
func (s *Server) busDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To, Text string
		Images   []busPicture `json:"images"`
	}
	if !decodeBus(w, r, &req) {
		return
	}
	if err := bus.CheckDM(bus.From{ID: bus.You, Label: bus.You}, req.To); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	datas, err := pictureBytes(req.Images)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	imgs, ok := keepPictures(w, datas)
	if !ok {
		return
	}
	ch, res, err := bus.DMImages(bus.From{ID: bus.You, Label: bus.You}, req.To, req.Text, imgs)
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

package desktop

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/live"
	"github.com/dcyber-lab/mad/internal/state"
)

// The window draws an agent's conversation from its transcript, in place
// of its terminal: the page asks for what was written after the offset it
// has, and gets it as entries (discover.Chatter). Reading it starts
// nothing: an agent asleep or stopped stays so.

const (
	// chatTail bounds a first look at a long transcript: its last bytes.
	chatTail = 8 << 20
	// chatMax bounds the entries a first look sends: the latest ones.
	chatMax = 600
)

// Chat answers one look: the entries written after the offset asked for.
type Chat struct {
	File    string           `json:"file"`   // which transcript; another one starts over
	Offset  int64            `json:"offset"` // where the next look starts
	Entries []discover.Entry `json:"entries"`
	// Reset: the entries start the conversation over (another session,
	// a rewritten file); Earlier: ones before them were left out.
	Reset   bool `json:"reset,omitempty"`
	Earlier bool `json:"earlier,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, err := state.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, a := st.FindAgent(q.Get("id"))
	if a == nil {
		http.Error(w, "no such agent", http.StatusNotFound)
		return
	}
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	c, err := readChat(r.Context(), p, a, q.Get("file"), from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c)
}

// live passes on the deck's look at what agent id's model is writing now
// (package live), once it changed after version since. The page asks
// again as soon as it has an answer.
func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	m, err := live.Fetch(r.Context(), q.Get("id"), since)
	if errors.Is(err, live.ErrNoHub) {
		// Nothing streams. Late, or the page asks again at once.
		select {
		case <-time.After(10 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m)
}

// transcriptOf finds agent a's transcript: its path, and the key the page
// knows it by; "" when it has none yet, or its kind keeps none.
func transcriptOf(ctx context.Context, p *state.Project, a *state.Agent) (conv discover.Conversations, path, key string) {
	conv, ok := discover.Conversation(a.Kind)
	if !ok {
		return nil, "", ""
	}
	sid := a.SessionID
	if sid == "" {
		// One the agent names itself (codex) the deck learns once a turn
		// ends; its calls named it from the first.
		if m, err := live.Fetch(ctx, a.ID, -1); err == nil {
			sid = m.Session
		}
	}
	if sid == "" {
		sid = a.ID
	}
	paths := conv.Transcripts(p.Dir(a), sid)
	if len(paths) == 0 {
		return conv, "", "" // nothing written yet
	}
	sum := sha256.Sum256([]byte(paths[0]))
	return conv, paths[0], hex.EncodeToString(sum[:8])
}

// readChat reads agent a's transcript from offset from of file (a key
// from an earlier answer), or from the start of the one it has now.
func readChat(ctx context.Context, p *state.Project, a *state.Agent, file string, from int64) (Chat, error) {
	out := Chat{Entries: []discover.Entry{}}
	conv, path, key := transcriptOf(ctx, p, a)
	if path == "" {
		return out, nil
	}
	out.File = key
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return out, err
	}
	if file != out.File || from < 0 || from > fi.Size() {
		from, out.Reset = 0, true
	}
	if from == 0 && fi.Size() > chatTail {
		from, out.Earlier = fi.Size()-chatTail, true
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return out, err
	}
	br := bufio.NewReaderSize(f, 256<<10)
	out.Offset = from
	if out.Earlier {
		// The tail starts mid-line: from the next one.
		skip, err := br.ReadBytes('\n')
		if err != nil {
			return out, nil
		}
		out.Offset += int64(len(skip))
	}
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			break // a partial last line waits for its newline
		}
		at := out.Offset
		out.Offset += int64(len(line))
		entries := conv.Chat(line)
		// An image is fetched by where it is: its line, entry and place.
		for i := range entries {
			for n := range entries[i].Images {
				entries[i].Images[n].URL = "/api/image?" + url.Values{"id": {a.ID}, "file": {key},
					"at": {strconv.FormatInt(at, 10)}, "e": {strconv.Itoa(i)}, "n": {strconv.Itoa(n)}}.Encode()
			}
		}
		out.Entries = append(out.Entries, entries...)
	}
	if out.Reset && len(out.Entries) > chatMax {
		out.Entries, out.Earlier = out.Entries[len(out.Entries)-chatMax:], true
	}
	return out, nil
}

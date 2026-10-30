package live

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
)

// The pi tap. pi has no API mad could stand in front of for every
// provider it signs in to, but it takes extensions: a pi agent is started
// with one of mad's (piExtension), which posts the message being written
// here (/p/<agent id>) as it grows.

const piPrefix = "/p/"

// piExtension is the extension: pi loads TypeScript, and this is plain
// JavaScript too. A snapshot goes at most every 50ms, and when the message
// is done; one late or out of order is told by its seq.
const piExtension = `// mad's pi extension (written by mad stream): the message the model is
// writing goes to the deck as it grows, for the desktop window to show.
export default function (pi) {
  const url = process.env.MAD_LIVE_URL;
  const token = process.env.MAD_LIVE_TOKEN;
  if (!url) return;
  let seq = 0, pending = null, timer = null;
  const flush = () => {
    timer = null;
    if (!pending) return;
    const body = JSON.stringify(pending);
    pending = null;
    fetch(url, { method: "POST", headers: { "Content-Type": "application/json", Authorization: "Bearer " + token }, body }).catch(() => {});
  };
  const send = (message, done) => {
    if (!message || message.role !== "assistant") return;
    pending = { id: String(message.timestamp), seq: ++seq, done, content: message.content || [] };
    if (done) { clearTimeout(timer); flush(); } else if (!timer) timer = setTimeout(flush, 50);
  };
  pi.on("message_start", (e) => send(e.message, false));
  pi.on("message_update", (e) => send(e.message, false));
  pi.on("message_end", (e) => send(e.message, true));
}
`

func piExtensionFile() string { return filepath.Join(paths.StateDir(), "pi-live.ts") }

// piSnapshot is what the extension posts: the message as it is so far.
type piSnapshot struct {
	ID      string `json:"id"`
	Seq     int64  `json:"seq"`
	Done    bool   `json:"done"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"content"`
}

func (h *Hub) pi(token string) http.Handler {
	seqs := map[string]int64{} // agent → the last snapshot's seq, under h.mu
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := strings.TrimPrefix(r.URL.Path, piPrefix)
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Method != http.MethodPost || !agentIDRe.MatchString(agent) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var s piSnapshot
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&s) != nil || s.ID == "" {
			http.Error(w, "bad snapshot", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		cur := h.msgs[agent]
		stale := cur != nil && cur.ID == s.ID && (cur.Done || s.Seq <= seqs[agent])
		if !stale {
			seqs[agent] = s.Seq
		}
		h.mu.Unlock()
		if stale {
			return
		}
		if cur == nil || cur.ID != s.ID {
			h.Begin(agent, s.ID, "")
		}
		h.change(agent, s.ID, func(m *Message) {
			m.Blocks = m.Blocks[:0]
			for _, c := range s.Content {
				b := Block{Kind: c.Type, Text: c.Text + c.Thinking, Done: s.Done}
				if c.Type == "toolCall" {
					b.Kind, b.Tool, b.ID, b.Text = "tool", c.Name, c.ID, string(c.Arguments)
				}
				m.Blocks = append(m.Blocks, b)
			}
			m.Done = s.Done
		})
	})
}

// writePiExtension puts the extension where pi agents are pointed at.
func writePiExtension() error {
	return os.WriteFile(piExtensionFile(), []byte(piExtension), 0o644)
}

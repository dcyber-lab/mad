package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Agents in window mode run headless under mad host (package host); the
// window draws them from what the host says, and answers through it. The
// page puts an agent it opens in window mode (/api/mode) unless it is
// busy as its TUI; a message to one asleep starts its host (/api/send).

// statusOf is agent id's status as the window last saw it.
func (s *Server) statusOf(id string) string {
	for _, p := range s.poll.Snapshot().Projects {
		for _, a := range p.Agents {
			if a.ID == id {
				return a.Status
			}
		}
	}
	return ""
}

// mode puts agent id in window mode (mode=window) or back in its TUI
// (mode=tui); one that is working or asking something is left as it is.
func (s *Server) mode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, mode := q.Get("id"), q.Get("mode")
	st, err := state.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, a := st.FindAgent(id)
	switch {
	case a == nil:
		http.Error(w, "no such agent", http.StatusNotFound)
		return
	case mode == "tui":
		mode = ""
	case mode != state.ModeWindow || !deck.Headless[a.Kind]:
		http.Error(w, "no such mode for "+a.Kind, http.StatusBadRequest)
		return
	}
	if err := s.switchable(id, a.Mode, mode); err != nil {
		if errors.Is(err, errSame) {
			// Runs so already: the state is told, nothing restarted.
			err = nil
			if a.Mode != mode {
				err = state.Update(func(st *state.State) error {
					if _, a := st.FindAgent(id); a != nil {
						a.Mode = mode
					}
					return nil
				})
			}
			if err == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := deck.SetMode(id, mode, s.kinds); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errSame = errors.New("it runs so already")

// switchable says whether agent id, set to run as was, can be restarted
// to run as mode: errSame when it runs so already, or why not now (it
// works or asks something, or holds a draft of yours in its TUI).
func (s *Server) switchable(id, was, mode string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	pane, ok := tmux.FindPane(panes, id)
	alive := ok && !pane.Dead
	if !alive && was == mode || alive && deck.RunsHost(pane, id) == (mode == state.ModeWindow) {
		return errSame
	}
	if st := s.statusOf(id); alive && (st == status.Running || st == status.Waiting) {
		return errors.New("it is busy in its TUI: switched once it is done")
	}
	// Restarting its TUI would lose what you typed there and did not send.
	if alive && mode == state.ModeWindow && drive.Draft(tmux.CaptureStyled(pane.ID)) != "" {
		return errors.New("it holds a draft of yours in its TUI")
	}
	return nil
}

// send gives agent id in window mode the message in the body, starting
// its host first if it does not run. The body is the text, or JSON with
// the text and images ({"text", "images": [{"mime", "data"}]}).
func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	cmd := host.Command{Type: "send"}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "application/json" {
		var m struct {
			Text   string       `json:"text"`
			Images []host.Image `json:"images"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cmd.Text, cmd.Images = m.Text, m.Images
	} else {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cmd.Text = string(body)
	}
	if err := s.ensureHost(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := host.Do(id, cmd); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ensureHost starts agent id's host unless it runs, and waits for it.
func (s *Server) ensureHost(ctx context.Context, id string) error {
	if conn, err := host.Dial(id); err == nil {
		conn.Close()
		return nil
	}
	st, err := state.Load()
	if err != nil {
		return err
	}
	p, a := st.FindAgent(id)
	if a == nil {
		return errors.New("no such agent")
	}
	if !deck.Windowed(a) {
		return errors.New("it is not in window mode")
	}
	if err := drive.EnsureDeck(p.Path); err != nil {
		return err
	}
	// Its TUI runs yet: restarted as its host when free.
	switch err := s.switchable(id, "", state.ModeWindow); {
	case errors.Is(err, errSame):
	case err != nil:
		return err
	default:
		if err := deck.SetMode(id, state.ModeWindow, s.kinds); err != nil {
			return err
		}
	}
	if err := deck.EnsureAgent(st, id, s.kinds); err != nil {
		return err
	}
	for range 100 {
		if conn, err := host.Dial(id); err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("its host did not start")
}

// hostOff is what the page is told while agent's host does not run.
var hostOff = []byte(`{"type":"state","state":{"status":"off","requests":[]}}`)

// hostWS relays a websocket to agent id's host: its states to the page,
// the page's commands to it. While the host does not run, the page hears
// so, and the host is looked for again each second.
func (s *Server) hostWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Origins})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	conns := make(chan net.Conn, 1)
	go func() {
		defer close(conns)
		for ctx.Err() == nil {
			conn, err := host.Dial(id)
			if err != nil {
				_ = c.Write(ctx, websocket.MessageText, hostOff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}
			conns <- conn
			sc := bufio.NewScanner(conn)
			sc.Buffer(make([]byte, 64<<10), 64<<20)
			for sc.Scan() {
				if c.Write(ctx, websocket.MessageText, sc.Bytes()) != nil {
					cancel()
					break
				}
			}
			conn.Close()
		}
	}()

	var conn net.Conn
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			return
		}
		// The latest connection, if there is one by now.
	drain:
		for {
			select {
			case nc, ok := <-conns:
				if !ok {
					return
				}
				conn = nc
			default:
				break drain
			}
		}
		var cmd host.Command
		if conn == nil || json.Unmarshal(data, &cmd) != nil {
			msg, _ := json.Marshal(host.Line{Type: "error", Error: "it does not run: send it a message to start it"})
			_ = c.Write(ctx, websocket.MessageText, msg)
			continue
		}
		if _, err := conn.Write(append(data, '\n')); err != nil {
			conn = nil
		}
	}
}

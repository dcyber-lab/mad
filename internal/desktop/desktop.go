// Package desktop serves the deck to a window outside the terminal: a
// sidebar drawn as a web page, and each agent's real TUI in a terminal
// emulator (xterm.js) attached to its pane in mad's tmux server. The tmux
// server stays where agents run; the window is one more client of it, so
// the terminal deck and the window can be open at the same time.
//
// The server listens on 127.0.0.1 only and answers nothing without the
// token it was started with: what it serves is a shell on this machine.
package desktop

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
)

//go:embed web
var webFS embed.FS

// Server is the desktop window's backend.
type Server struct {
	Token string
	// Origins are host:port the page is opened from besides its own, its
	// websockets taken from too: the phone's, through tailscale serve.
	Origins []string
	kinds   []agent.Kind
	poll    *poller
	looks   *looks
	cmds    *answers[discover.Command]
	hist    historyCache
	// fileList: what @ offers in a message; modelList: what a new
	// chat can start on.
	fileList  fileCache
	modelList *answers[discover.Model]
}

// New makes a server with a fresh random token.
func New(kinds []agent.Kind) *Server {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	l := newLooks()
	return &Server{
		Token: hex.EncodeToString(b), kinds: kinds, poll: newPoller(kinds, l), looks: l,
		// Through the vars, which tests replace.
		cmds: newAnswers(func(ctx context.Context, k, d string) ([]discover.Command, error) {
			return findCommands(ctx, k, d)
		}),
		modelList: newAnswers(func(ctx context.Context, k, d string) ([]discover.Model, error) {
			return findModels(ctx, k, d)
		}),
	}
}

// Handler routes the page, its assets and the API.
func (s *Server) Handler() http.Handler {
	web, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	// The page and its assets carry nothing secret; the token arrives in
	// the URL's fragment, which the browser never sends.
	mux.Handle("GET /", http.FileServerFS(web))
	mux.Handle("GET /api/deck", s.auth(http.HandlerFunc(s.deck)))
	mux.Handle("GET /api/chat", s.auth(http.HandlerFunc(s.chat)))
	mux.Handle("GET /api/live", s.auth(http.HandlerFunc(s.live)))
	mux.Handle("GET /api/commands", s.auth(http.HandlerFunc(s.commands)))
	mux.Handle("POST /api/mode", s.auth(http.HandlerFunc(s.mode)))
	mux.Handle("POST /api/send", s.auth(http.HandlerFunc(s.send)))
	mux.Handle("GET /api/host", s.auth(http.HandlerFunc(s.hostWS)))
	mux.Handle("POST /api/seen", s.auth(http.HandlerFunc(s.seen)))
	mux.Handle("POST /api/queue", s.auth(http.HandlerFunc(s.queueAdd)))
	mux.Handle("DELETE /api/queue", s.auth(http.HandlerFunc(s.queueRemove)))
	mux.Handle("POST /api/queue/pause", s.auth(http.HandlerFunc(s.queuePause)))
	mux.Handle("GET /api/projects/suggest", s.auth(http.HandlerFunc(s.suggest)))
	mux.Handle("POST /api/projects", s.auth(http.HandlerFunc(s.projectAdd)))
	mux.Handle("DELETE /api/projects", s.auth(http.HandlerFunc(s.projectRemove)))
	mux.Handle("POST /api/agents", s.auth(http.HandlerFunc(s.agentAdd)))
	mux.Handle("PATCH /api/agents", s.auth(http.HandlerFunc(s.agentRename)))
	mux.Handle("DELETE /api/agents", s.auth(http.HandlerFunc(s.agentRemove)))
	mux.Handle("POST /api/agents/stop", s.auth(http.HandlerFunc(s.agentStop)))
	mux.Handle("POST /api/terminal", s.auth(http.HandlerFunc(s.terminal)))
	mux.Handle("GET /api/history", s.auth(http.HandlerFunc(s.history)))
	mux.Handle("GET /api/diff", s.auth(http.HandlerFunc(s.diff)))
	mux.Handle("GET /api/files", s.auth(http.HandlerFunc(s.files)))
	mux.Handle("GET /api/models", s.auth(http.HandlerFunc(s.models)))
	mux.Handle("GET /api/image", s.auth(http.HandlerFunc(s.image)))
	mux.Handle("GET /api/file", s.auth(http.HandlerFunc(s.file)))
	mux.Handle("GET /api/phone", s.auth(http.HandlerFunc(s.phone)))
	mux.Handle("POST /api/phone", s.auth(http.HandlerFunc(s.phone)))
	mux.Handle("DELETE /api/phone", s.auth(http.HandlerFunc(s.phone)))
	mux.Handle("GET /api/phone/qr", s.auth(http.HandlerFunc(s.phoneQR)))
	return headers(mux)
}

// auth refuses requests without the token. Browsers let any page open a
// websocket to localhost, so this check, not the Origin, is what keeps
// other sites out; the Origin is checked too by the websocket accept.
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// csp lets the page run its own scripts and nothing else. It draws what
// agents wrote, and a script slipped in there would have the token: a
// shell on this machine. Its markdown is sanitized too (app.js).
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// headers: nothing is cached, and the page is held to csp.
func headers(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", csp)
		h.ServeHTTP(w, r)
	})
}

func (s *Server) deck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.poll.Snapshot())
}

// Serve listens on a free port of 127.0.0.1, calls ready with the page's
// URL (token included) once it accepts connections, and serves until ctx
// ends.
func (s *Server) Serve(ctx context.Context, addr string, ready func(url string)) error {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go s.poll.Run(ctx)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if ready != nil {
		ready("http://" + ln.Addr().String() + "/#token=" + s.Token)
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

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
)

//go:embed web
var webFS embed.FS

// Server is the desktop window's backend.
type Server struct {
	Token string
	kinds []agent.Kind
	poll  *poller
	terms *terms
}

// New makes a server with a fresh random token.
func New(kinds []agent.Kind) *Server {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	t := newTerms()
	return &Server{Token: hex.EncodeToString(b), kinds: kinds, poll: newPoller(kinds, t), terms: t}
}

// Handler routes the page, its assets and the API.
func (s *Server) Handler() http.Handler {
	web, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	// The page and its assets carry nothing secret; the token arrives in
	// the URL's fragment, which the browser never sends.
	mux.Handle("GET /", http.FileServerFS(web))
	mux.Handle("GET /api/deck", s.auth(http.HandlerFunc(s.deck)))
	mux.Handle("GET /api/term", s.auth(http.HandlerFunc(s.term)))
	return noCache(mux)
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

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
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
		s.terms.CloseAll()
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

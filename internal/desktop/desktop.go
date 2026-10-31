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
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/phone"
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
	// interrupts: when each agent was last interrupted (/api/interrupt).
	interrupts interrupts
	// wins: the app windows open on this server (window_life.go).
	wins windows
	// revokeTick is how often a device's requests look for its revoking
	// (revokeEvery); set before serving.
	revokeTick time.Duration
}

// New makes a server with a fresh random token.
func New(kinds []agent.Kind) *Server {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	l := newLooks()
	return &Server{
		Token: hex.EncodeToString(b), kinds: kinds, poll: newPoller(kinds, l), looks: l, revokeTick: revokeEvery,
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
	mux.Handle("GET /api/window", s.auth(http.HandlerFunc(s.window)))
	mux.Handle("GET /api/window/state", s.auth(http.HandlerFunc(s.windowState)))
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
	mux.Handle("POST /api/interrupt", s.auth(http.HandlerFunc(s.interrupt)))
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
	mux.Handle("POST /api/pair", http.HandlerFunc(s.pair))
	mux.Handle("GET /api/phone/devices", s.auth(http.HandlerFunc(s.phoneDevices)))
	mux.Handle("DELETE /api/phone/devices", s.auth(http.HandlerFunc(s.phoneDevices)))
	mux.Handle("GET /api/phone/qr", s.auth(http.HandlerFunc(s.phoneQR)))
	mux.Handle("GET /api/push", s.auth(http.HandlerFunc(s.pushState)))
	mux.Handle("POST /api/push", s.auth(http.HandlerFunc(s.pushSubscribe)))
	mux.Handle("DELETE /api/push", s.auth(http.HandlerFunc(s.pushUnsubscribe)))
	mux.Handle("POST /api/push/test", s.auth(http.HandlerFunc(s.pushTest)))
	mux.Handle("GET /api/bus/messages", s.auth(http.HandlerFunc(s.busMessages)))
	mux.Handle("GET /api/bus/image", s.auth(http.HandlerFunc(s.busImage)))
	mux.Handle("POST /api/bus/post", s.auth(http.HandlerFunc(s.busPost)))
	mux.Handle("POST /api/bus/dm", s.auth(http.HandlerFunc(s.busDM)))
	mux.Handle("POST /api/bus/channel", s.auth(http.HandlerFunc(s.busCreate)))
	mux.Handle("POST /api/bus/read", s.auth(http.HandlerFunc(s.busRead)))
	mux.Handle("POST /api/bus/resume", s.auth(http.HandlerFunc(s.busResume)))
	mux.Handle("POST /api/bus/report", s.auth(http.HandlerFunc(s.busReport)))
	mux.Handle("POST /api/bus/lead", s.auth(http.HandlerFunc(s.busLead)))
	mux.Handle("POST /api/budget", s.auth(http.HandlerFunc(s.budgetSet)))
	mux.Handle("POST /api/inbox", s.auth(http.HandlerFunc(s.inboxAct)))
	mux.Handle("GET /api/team/templates", s.auth(http.HandlerFunc(s.teamTemplates)))
	mux.Handle("POST /api/team", s.auth(http.HandlerFunc(s.teamDo)))
	mux.Handle("POST /api/timer", s.auth(http.HandlerFunc(s.timerSet)))
	mux.Handle("POST /api/board/task", s.auth(http.HandlerFunc(s.boardTask)))
	mux.Handle("POST /api/board/nudge", s.auth(http.HandlerFunc(s.boardNudge)))
	mux.Handle("GET /api/skills", s.auth(http.HandlerFunc(s.skills)))
	mux.Handle("POST /api/skills/approve", s.auth(http.HandlerFunc(s.skillApprove)))
	mux.Handle("POST /api/skills/discard", s.auth(http.HandlerFunc(s.skillDiscard)))
	return headers(mux)
}

// auth refuses requests without a token. Browsers let any page open a
// websocket to localhost, so this check, not the Origin, is what keeps
// other sites out; the Origin is checked too by the websocket accept.
// The token is a paired device's (phone.Pair), or the master one, which
// the local window and phones paired before devices hold: not through a
// Cloudflare Tunnel, where only a paired device gets in. A device
// revoked meanwhile has its requests in flight cancelled.
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, inURL := requestToken(r)
		// A token in a URL is for direct local requests (old pages, the
		// window, the app's first way): through a tunnel it would be in
		// its logs, so there it is refused, as the master token is.
		if inURL && viaCloudflare(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1 && !viaCloudflare(r) {
			h.ServeHTTP(w, r)
			return
		}
		dev, ok := phone.DeviceFor(got)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithCancel(context.WithValue(r.Context(), deviceKey{}, dev.ID))
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			watchDevice(ctx, cancel, got, s.revokeTick)
		}()
		// The watch ends with the request, and is gone before it returns.
		defer func() {
			cancel()
			<-watched
		}()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// deviceKey carries the id of the paired device a request came from; the
// master token's requests have none.
type deviceKey struct{}

func deviceOf(r *http.Request) string {
	id, _ := r.Context().Value(deviceKey{}).(string)
	return id
}

// requestToken is the token a request carries, and says whether it came
// in the URL: an Authorization: Bearer header first, else the websocket
// subprotocol "mad.token.<token>" (a browser can't set headers there; the
// server selects "mad" and never echoes it), else ?token=.
func requestToken(r *http.Request) (token string, inURL bool) {
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return b, false
	}
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			if t, ok := strings.CutPrefix(strings.TrimSpace(p), "mad.token."); ok {
				return t, false
			}
		}
	}
	return r.URL.Query().Get("token"), true
}

// wsSubprotocols are what a websocket accept selects from: "mad" only, so
// the subprotocol that carries the token is not echoed back.
var wsSubprotocols = []string{"mad"}

// viaCloudflare says whether the request came through a Cloudflare
// Tunnel: cloudflared's edge adds these, and a caller can only add them
// to be refused.
func viaCloudflare(r *http.Request) bool {
	return r.Header.Get("Cf-Connecting-Ip") != "" || r.Header.Get("Cf-Ray") != ""
}

// revokeEvery is how often a device's open requests look for its revoking.
const revokeEvery = time.Second

// watchDevice calls cancel once the device holding token is revoked, so
// its websockets and streams end within a tick, not at its next request.
func watchDevice(ctx context.Context, cancel context.CancelFunc, token string, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !phone.Holds(token) {
				cancel()
				return
			}
		}
	}
}

// csp lets the page run its own scripts and nothing else. It draws what
// agents wrote, and a script slipped in there would have the token: a
// shell on this machine. Its markdown is sanitized too (app.js).
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
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
	// Pictures of messages that were cut from their logs, or never sent.
	go func() { _, _ = bus.CollectImages(bus.ImageGCAge) }()
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

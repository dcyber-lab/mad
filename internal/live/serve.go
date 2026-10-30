package live

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// The hub runs in the deck as `mad stream`, on 127.0.0.1. It says where
// in a file of the state directory, with the token that /live/ wants: what
// agents write is theirs to read, not any local process's.

// info is what the file says.
type info struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
}

func file() string { return filepath.Join(paths.StateDir(), "stream.json") }

func readInfo() (info, bool) {
	var in info
	data, err := os.ReadFile(file())
	if err != nil || json.Unmarshal(data, &in) != nil || in.Addr == "" {
		return info{}, false
	}
	return in, true
}

// longPoll is how long a look at /live/ waits for a change.
const longPoll = 25 * time.Second

// Serve runs the hub until ctx ends. It takes the address it had before
// when it can: agents started then still send their calls there, and a
// restarted hub (the deck keeps it running) is where they left it.
func Serve(ctx context.Context) error {
	var ln net.Listener
	var err error
	prev, ok := readInfo()
	if ok {
		ln, _ = net.Listen("tcp", prev.Addr)
	}
	in := info{Token: prev.Token} // pi agents post with it (Env)
	if ln == nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
		in.Token = ""
	}
	if in.Token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		in.Token = hex.EncodeToString(b)
	}
	in.Addr = ln.Addr().String()

	h := NewHub()
	mux := http.NewServeMux()
	mux.Handle(agentPrefix, h.anthropic(anthropicUpstream(in.Addr)))
	mux.Handle(codexPrefix, h.openai(chatgptCodex, openaiAPI))
	mux.Handle(piPrefix, h.pi(in.Token))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /live/{agent}", func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(in.Token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		wctx, cancel := context.WithTimeout(r.Context(), longPoll)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.Wait(wctx, r.PathValue("agent"), since))
	})

	data, _ := json.Marshal(in)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	if err := writePiExtension(); err != nil {
		return err
	}
	tmp := file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// Left when it ends: the next one takes the address again (agents
	// still point there), and agents started meanwhile see it does not
	// answer (Env).
	if err := os.Rename(tmp, file()); err != nil {
		return err
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// anthropicUpstream is where claude's calls go on to: where they went
// before the deck pointed them here, else the API.
func anthropicUpstream(self string) *url.URL {
	if u, err := url.Parse(os.Getenv("ANTHROPIC_BASE_URL")); err == nil && u.Scheme != "" &&
		u.Host != self && !strings.HasPrefix(u.Path, agentPrefix) {
		return u
	}
	return &url.URL{Scheme: "https", Host: "api.anthropic.com"}
}

// Env is what agent id of kind starts with to be followed live: a claude
// agent sends its calls through the hub, a pi agent's extension (Vars)
// posts to it. None when the hub does not answer, as an agent pointed at
// no proxy could not work, nor when claude talks to another cloud's API.
func Env(kind, id string) []string {
	if kind != "claude" && kind != "pi" || !agentIDRe.MatchString(id) {
		return nil
	}
	if kind == "claude" && (os.Getenv("CLAUDE_CODE_USE_BEDROCK") != "" || os.Getenv("CLAUDE_CODE_USE_VERTEX") != "") {
		return nil
	}
	in, ok := readInfo()
	if !ok || !alive(in.Addr) {
		return nil
	}
	if kind == "pi" {
		return []string{"MAD_LIVE_URL=http://" + in.Addr + piPrefix + id, "MAD_LIVE_TOKEN=" + in.Token}
	}
	return []string{"ANTHROPIC_BASE_URL=http://" + in.Addr + agentPrefix + id}
}

// Vars are the command placeholders agent id of kind starts with to be
// followed live: {codex_live} points codex's calls at the hub (unless the
// user sends them somewhere of their own), {pi_live} loads mad's pi
// extension. Empty when the hub does not answer.
func Vars(kind, id string) map[string]string {
	vars := map[string]string{"{codex_live}": "", "{pi_live}": ""}
	if kind != "codex" && kind != "pi" || !agentIDRe.MatchString(id) {
		return vars
	}
	if kind == "codex" && (os.Getenv("OPENAI_BASE_URL") != "" || codexBaseURLSet()) {
		return vars
	}
	in, ok := readInfo()
	if !ok || !alive(in.Addr) {
		return vars
	}
	switch kind {
	case "codex":
		vars["{codex_live}"] = "-c " + paths.ShellQuote(`openai_base_url="http://`+in.Addr+codexPrefix+id+`"`)
	case "pi":
		vars["{pi_live}"] = "-e " + paths.ShellQuote(piExtensionFile())
	}
	return vars
}

var baseURLKey = regexp.MustCompile(`(?m)^\s*openai_base_url\s*=`)

// codexBaseURLSet reports whether codex's config.toml sets openai_base_url.
func codexBaseURLSet() bool {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home = filepath.Join(paths.Home(), ".codex")
	}
	data, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	return baseURLKey.Match(data)
}

func alive(addr string) bool {
	c := http.Client{Timeout: 500 * time.Millisecond}
	res, err := c.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// ErrNoHub: the deck's hub is not running.
var ErrNoHub = errors.New("mad stream is not running")

// Fetch asks the deck's hub for agent's message, once it changed after
// version since; it waits up to the hub's long poll.
func Fetch(ctx context.Context, agent string, since int64) (Message, error) {
	in, ok := readInfo()
	if !ok {
		return Message{}, ErrNoHub
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("http://%s/live/%s?since=%d", in.Addr, url.PathEscape(agent), since), nil)
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+in.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Message{}, ErrNoHub
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("mad stream: %s", res.Status)
	}
	var m Message
	return m, json.NewDecoder(res.Body).Decode(&m)
}

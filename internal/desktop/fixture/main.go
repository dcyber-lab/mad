// Command fixture serves mad's real page with canned data, for UI tests
// (the simulator's XCUITest suite): no agents run, no state is read, and
// nothing is written. It is not part of the mad binary.
//
//	go run ./internal/desktop/fixture -addr 127.0.0.1:0 -scenario edge-home
//
// It prints "ready http://127.0.0.1:PORT" once it listens. It accepts any
// token. What the page calls on load and in its flows is answered from the
// scenario (testdata/NAME.json); the rest of /api/ answers 204.
//
// A scenario is {"deck", "chat", "messages"}:
//
//   - deck is /api/deck as the page reads it, over defaults (the state
//     table, an empty budget and so on); "working" is the running agents'
//     ids unless given, and inbox.needs is the count of its items.
//   - chat maps an agent id to its /api/chat answer.
//   - messages maps a channel id to its messages, as /api/bus/messages
//     gives them (seq, at, from, kind, text, images…).
//
// A string value of a time key (at, since, last_at, reset_at) that reads
// as a signed duration ("-5m", "+2h10m") becomes that long from now, so a
// scenario does not age.
//
// /api/pair: a code of 32 a's pairs (200), 32 b's is refused (403), 32 c's
// fails once (503) and then pairs (so Retry works), and a token that is
// not 32 hex digits is a bad request (400).
//
// /api/bus/image?id= answers a small PNG whose color is the id's.
//
// Scenarios (testdata/): edge-home, empty, many-channels, channel-fold, channel-quiet, team-members, long-title,
// usage and pairing; the files say what is in each.
//
// What the page gives a test to find (web/): a list row is a button
// (role="button") labeled with its name; a character is role="img" labeled
// "working", "needs you", "idle" or "stopped" (a group's tile says the same
// of its members); a red dot is role="img" labeled "needs you"; the title's
// ▾ is a button labeled "切换 Mac", ⋯ "更多", search "搜索" and + "新建"; a
// folded message has buttons whose text is "Show more" and "Show less".
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/desktop"
	"github.com/dcyber-lab/mad/internal/status"
)

//go:embed testdata/*.json
var scenarioFS embed.FS

// scenario is one file of testdata.
type scenario struct {
	Deck     map[string]any               `json:"deck"`
	Chat     map[string]json.RawMessage   `json:"chat"`
	Messages map[string][]json.RawMessage `json:"messages"`
	// Receipts is a channel's receipts under the person's messages, Paused whether it is paused (/api/bus/messages).
	Receipts map[string]json.RawMessage `json:"receipts"`
	Paused   map[string]bool            `json:"paused"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "where to listen (loopback only; never mad's own port)")
	name := flag.String("scenario", "edge-home", "the scenario in testdata/")
	web := flag.String("web", "", "serve the page from this directory instead of the embedded copy")
	flag.Parse()
	h, err := newHandler(*name, *web)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
	ln, err := listen(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
	fmt.Printf("ready http://%s\n", ln.Addr())
	if err := http.Serve(ln, h); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

// listen refuses what is not loopback and mad's own port: the fixture has
// no token.
func listen(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, errors.New("only a loopback address: the fixture takes any token")
	}
	// The port as a number, so 07484 and +7484 are 7484 too; a name for a port is not taken.
	n, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("port %q is not a number", port)
	}
	if n == 7484 {
		return nil, errors.New("7484 is the deck's")
	}
	return net.Listen("tcp", addr)
}

// load reads scenario name.
func load(name string) (*scenario, error) {
	if strings.ContainsAny(name, "/\\.") || name == "" {
		return nil, fmt.Errorf("no scenario %q", name)
	}
	b, err := scenarioFS.ReadFile("testdata/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("no scenario %q (%s)", name, strings.Join(names(), ", "))
	}
	var s scenario
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("scenario %s: %v", name, err)
	}
	return &s, nil
}

// names lists the scenarios.
func names() []string {
	es, _ := fs.ReadDir(scenarioFS, "testdata")
	var out []string
	for _, e := range es {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

// newHandler is the fixture server for scenario name; web is a directory
// to serve the page from, or "" for the embedded page.
func newHandler(name, web string) (http.Handler, error) {
	scn, err := load(name)
	if err != nil {
		return nil, err
	}
	var page fs.FS = desktop.WebFS()
	if web != "" {
		page = os.DirFS(web)
	}
	f := &fixture{scn: scn}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(page))
	mux.HandleFunc("GET /api/deck", f.deck)
	mux.HandleFunc("GET /api/chat", f.chat)
	mux.HandleFunc("GET /api/live", f.live)
	mux.HandleFunc("GET /api/bus/messages", f.messages)
	mux.HandleFunc("GET /api/bus/image", f.image)
	mux.HandleFunc("POST /api/bus/post", f.post)
	mux.HandleFunc("POST /api/pair", f.pair)
	mux.HandleFunc("POST /api/budget", f.budget)
	for _, p := range []string{"commands", "models", "phone/devices", "projects/suggest", "history", "files"} {
		mux.HandleFunc("GET /api/"+p, func(w http.ResponseWriter, r *http.Request) { reply(w, []any{}) })
	}
	mux.HandleFunc("GET /api/phone", func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]any{"on": false}) })
	mux.HandleFunc("GET /api/push", func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]any{"key": "", "on": false}) })
	// Anything else of the API answers nothing, harmlessly.
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(m+" /api/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	return desktop.WithHeaders(mux), nil
}

type fixture struct {
	scn *scenario
	// spend: the day's budget after the person raised or lifted it (/api/budget); nil until then.
	spendMu sync.Mutex
	spend   map[string]any
	// pairTried: the codes that failed once already (32 c's).
	mu        sync.Mutex
	pairTried map[string]bool
	// posted: what the person said through /api/bus/post, by channel; they follow the scenario's messages.
	posted map[string][]json.RawMessage
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// deck is the snapshot: defaults, then the scenario's deck over them.
func (f *fixture) deck(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	d := map[string]any{
		"projects": []any{}, "bus": []any{}, "board": []any{}, "timers": []any{}, "teams": []any{},
		"quota":   map[string]any{},
		"spend":   map[string]any{"day": now.Format("2006-01-02"), "usd": 0, "turns": 0, "budget_usd": 20, "budget_turns": 150, "open_today": false, "closed": false, "part": 0},
		"inbox":   map[string]any{"needs": 0, "items": []any{}},
		"states":  status.Looks,
		"machine": "Fixture Mac",
		"at":      now,
	}
	for k, v := range f.scn.Deck {
		d[k] = v
	}
	f.spendMu.Lock()
	if f.spend != nil {
		d["spend"] = f.spend
	}
	f.spendMu.Unlock()
	relative(d, now)
	if _, ok := f.scn.Deck["working"]; !ok {
		d["working"] = running(d)
	}
	if in, ok := d["inbox"].(map[string]any); ok {
		if _, has := in["needs"]; !has {
			items, _ := in["items"].([]any)
			in["needs"] = len(items)
		}
	}
	reply(w, d)
}

// running lists the ids of the running agents of the deck.
func running(d map[string]any) []string {
	out := []string{}
	ps, _ := d["projects"].([]any)
	for _, p := range ps {
		as, _ := p.(map[string]any)["agents"].([]any)
		for _, a := range as {
			m, _ := a.(map[string]any)
			if m["status"] == "running" {
				id, _ := m["id"].(string)
				out = append(out, id)
			}
		}
	}
	return out
}

var offset = regexp.MustCompile(`^[+-]\d+(\.\d+)?(ns|us|ms|s|m|h)([0-9.]+(ns|us|ms|s|m|h))*$`)

// timeKeys are the keys whose duration strings are made times.
var timeKeys = map[string]bool{"at": true, "since": true, "last_at": true, "reset_at": true, "next_at": true}

// relative turns the signed durations at time keys, anywhere in v, into
// times from now.
func relative(v any, now time.Time) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && timeKeys[k] && offset.MatchString(s) {
				if d, err := time.ParseDuration(strings.TrimPrefix(s, "+")); err == nil {
					x[k] = now.Add(d).Format(time.RFC3339)
				}
				continue
			}
			relative(e, now)
		}
	case []any:
		for _, e := range x {
			relative(e, now)
		}
	}
}

// chat answers an agent's transcript: the scenario's, or an empty one.
func (f *fixture) chat(w http.ResponseWriter, r *http.Request) {
	if raw, ok := f.scn.Chat[r.URL.Query().Get("id")]; ok {
		// A client that has read it (its file, from the end) is told there is nothing new, as the
		// real one does, so a poll does not add the entries again.
		var seen struct {
			File   string `json:"file"`
			Offset int64  `json:"offset"`
		}
		q := r.URL.Query()
		from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
		if json.Unmarshal(raw, &seen) == nil && seen.File != "" && q.Get("file") == seen.File && from >= seen.Offset {
			reply(w, map[string]any{"file": seen.File, "offset": seen.Offset, "entries": []any{}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	reply(w, map[string]any{"file": "fixture", "offset": 0, "entries": []any{}})
}

// live holds the page's long poll, as the real one does, then says there
// is nothing: the page asks again.
func (f *fixture) live(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(10 * time.Second):
	case <-r.Context().Done():
	}
	w.WriteHeader(http.StatusNoContent)
}

// messages answers a channel's messages after since.
func (f *fixture) messages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ch := q.Get("channel")
	since, _ := strconv.Atoi(q.Get("since"))
	out := []any{}
	last := 0
	now := time.Now()
	f.mu.Lock()
	all := append(append([]json.RawMessage{}, f.scn.Messages[ch]...), f.posted[ch]...)
	f.mu.Unlock()
	for _, raw := range all {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		seq, _ := m["seq"].(float64)
		if int(seq) > last {
			last = int(seq)
		}
		if int(seq) <= since {
			continue
		}
		relative(m, now)
		out = append(out, m)
	}
	var receipts any = []any{}
	if raw, ok := f.scn.Receipts[ch]; ok {
		receipts = raw
	}
	reply(w, map[string]any{"channel": ch, "messages": out, "last": last, "paused": f.scn.Paused[ch], "receipts": receipts})
}

// post takes the person's message into the channel, as the next one, so a native client can be seen sending.
func (f *fixture) post(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.Channel == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	seq := 0
	for _, raw := range append(append([]json.RawMessage{}, f.scn.Messages[in.Channel]...), f.posted[in.Channel]...) {
		var m struct {
			Seq int `json:"seq"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Seq > seq {
			seq = m.Seq
		}
	}
	seq++
	raw, _ := json.Marshal(map[string]any{"seq": seq, "at": "-0s", "from": map[string]any{"id": "you", "label": "you"}, "kind": "text", "text": in.Text})
	if f.posted == nil {
		f.posted = map[string][]json.RawMessage{}
	}
	f.posted[in.Channel] = append(f.posted[in.Channel], raw)
	reply(w, map[string]any{"seq": seq, "woke": []string{}})
}

// image answers a small PNG, its color the id's.
func (f *fixture) image(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	n := h.Sum32()
	c := color.RGBA{uint8(90 + n%140), uint8(90 + n>>8%140), uint8(90 + n>>16%140), 255}
	img := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			if (x/20+y/20)%2 == 0 {
				img.Set(x, y, c)
			} else {
				img.Set(x, y, color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255})
			}
		}
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, img)
}

// budget answers POST /api/budget {op, usd} as the real one does: raise adds to the day's
// dollars (and, if the day is then not spent, opens it again); open lifts it for the day.
func (f *fixture) budget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op  string
		USD float64
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || (req.Op != "raise" && req.Op != "open") {
		http.Error(w, "op is raise or open", http.StatusBadRequest)
		return
	}
	f.spendMu.Lock()
	defer f.spendMu.Unlock()
	if f.spend == nil {
		f.spend = map[string]any{"day": time.Now().Format("2006-01-02"), "usd": 0.0, "turns": 0, "budget_usd": 20.0, "budget_turns": 150, "open_today": false, "closed": false, "part": 0.0}
		if sp, ok := f.scn.Deck["spend"].(map[string]any); ok {
			for k, v := range sp {
				f.spend[k] = v
			}
		}
	}
	num := func(k string) float64 { v, _ := f.spend[k].(float64); return v }
	switch req.Op {
	case "raise":
		f.spend["budget_usd"] = num("budget_usd") + req.USD
	case "open":
		f.spend["open_today"] = true
	}
	part := 0.0
	if b := num("budget_usd"); b > 0 {
		part = num("usd") / b
	}
	f.spend["part"] = part
	f.spend["hot"] = part >= 0.8
	f.spend["closed"] = part >= 1 && f.spend["open_today"] != true
	reply(w, f.spend)
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// pair answers as the pairing contract in the package comment says.
func (f *fixture) pair(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, Name, Token string }
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if json.NewDecoder(r.Body).Decode(&in) != nil || !hex32.MatchString(in.Token) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch in.Code {
	case strings.Repeat("a", 32):
		reply(w, map[string]string{"id": "fixture-device", "machine": "Fixture Mac"})
	case strings.Repeat("c", 32):
		f.mu.Lock()
		again := f.pairTried[in.Token]
		if f.pairTried == nil {
			f.pairTried = map[string]bool{}
		}
		f.pairTried[in.Token] = true
		f.mu.Unlock()
		if !again {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		reply(w, map[string]string{"id": "fixture-device", "machine": "Fixture Mac"})
	default:
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}

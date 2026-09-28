// Package summarize is the optional model layer of the resume brief, as
// plugins. mad sends a Request (the turns since your baseline, verbatim,
// and the facts it collected) and gets back candidates: a summary, open
// questions, possible decisions, a suggested bookmark. Candidates are
// shown as such; only you turn one into a decision or a bookmark.
//
// A plugin is anything that implements Plugin. Two are built in:
//
//	"claude"  runs `claude -p` (default model haiku) with your Claude Code
//	          login, no tools, no settings, no saved session
//	"command" runs any executable: the Request as JSON on stdin, a
//	          Response as JSON on stdout
//
// Nothing runs unless config.json turns it on for the project.
package summarize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Version of the Request/Response protocol, for command plugins.
const Version = 1

type Turn struct {
	Ref   string    `json:"ref"` // T1, T2… sources cite these
	At    time.Time `json:"at"`
	You   string    `json:"you"`
	Reply string    `json:"reply,omitempty"`
	Cmds  int       `json:"commands,omitempty"`
}

type Decision struct {
	ID     string `json:"id"`
	Choice string `json:"choice"`
	Scope  string `json:"scope,omitempty"`
}

// Request is everything a plugin gets. It holds your words and the
// agent's; nothing else from the machine.
type Request struct {
	Version   int        `json:"version"`
	Goal      string     `json:"goal"`
	Bookmark  string     `json:"bookmark,omitempty"`
	Decisions []Decision `json:"decisions,omitempty"`
	Facts     []string   `json:"facts,omitempty"` // collected by mad, with what they rest on
	Turns     []Turn     `json:"turns"`
	Omitted   int        `json:"omitted_turns,omitempty"` // older turns in the window not sent
}

type Sourced struct {
	Text   string `json:"text"`
	Source string `json:"source"` // a turn ref
}

type DecisionCandidate struct {
	Choice   string `json:"choice"`
	Why      string `json:"why,omitempty"`
	Rejected string `json:"rejected,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Source   string `json:"source"`
	Quote    string `json:"quote"` // the user's own words it rests on, verbatim
	// Quoted is set by mad, not the plugin: the quote is really there.
	Quoted bool `json:"-"`
}

type Response struct {
	Summary   string              `json:"summary"`
	Open      []Sourced           `json:"open_questions"`
	Decisions []DecisionCandidate `json:"decision_candidates"`
	Bookmark  *Sourced            `json:"suggested_bookmark,omitempty"`
}

// Result is a Response checked against its Request, with where it came from.
type Result struct {
	Response
	Plugin  string    `json:"plugin"`
	Model   string    `json:"model,omitempty"`
	CostUSD float64   `json:"cost_usd,omitempty"`
	At      time.Time `json:"at"`
	Dropped int       `json:"dropped"` // items citing turns that weren't sent
	Cached  bool      `json:"-"`
}

type Plugin interface {
	Name() string
	Summarize(ctx context.Context, r Request) (Response, Meta, error)
}

// Meta is what a plugin reports about a run.
type Meta struct {
	Model               string
	CostUSD             float64
	APIms, Turns        int
	InTokens, OutTokens int
}

// Config is the "brief" section of config.json:
//
//	"brief": {"summarizer": "claude", "model": "haiku",
//	          "projects": ["~/code/api"], "max_usd": 0.05}
//	"brief": {"summarizer": "command", "command": "my-summarizer --fast",
//	          "projects": ["*"]}
type Config struct {
	Summarizer string   `json:"summarizer"` // "" off, "claude", "command"
	Model      string   `json:"model,omitempty"`
	Command    string   `json:"command,omitempty"`
	Projects   []string `json:"projects,omitempty"` // allowed project paths; "*" all
	MaxChars   int      `json:"max_input_chars,omitempty"`
	Timeout    int      `json:"timeout_seconds,omitempty"`
	MaxUSD     float64  `json:"max_usd,omitempty"`
	// Auto summarizes in the background when a turn ends while you are
	// elsewhere, so the brief has it when you come back. On unless false.
	Auto *bool `json:"auto,omitempty"`
	// DailyUSD caps what summaries spend in a day, cache hits free.
	DailyUSD float64 `json:"daily_usd,omitempty"`
}

// Allowed reports whether cfg sends project's conversations anywhere.
func (c Config) Allowed(project string) bool {
	if c.Summarizer == "" {
		return false
	}
	for _, p := range c.Projects {
		if p == "*" || paths.Expand(p) == project {
			return true
		}
	}
	return false
}

func (c Config) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 90 * time.Second
	}
	return time.Duration(c.Timeout) * time.Second
}

// Automatic reports whether summaries run on their own.
func (c Config) Automatic() bool { return c.Auto == nil || *c.Auto }

func (c Config) daily() float64 {
	if c.DailyUSD <= 0 {
		return 1
	}
	return c.DailyUSD
}

// MaxInput is how many characters of turns a request carries.
func (c Config) MaxInput() int {
	if c.MaxChars <= 0 {
		return 60000
	}
	return c.MaxChars
}

// registry maps summarizer names to constructors; plugins register here.
var registry = map[string]func(Config) (Plugin, error){}

// Register adds a plugin kind. The built-ins register themselves.
func Register(name string, make func(Config) (Plugin, error)) { registry[name] = make }

// Names lists the registered plugin kinds.
func Names() []string {
	var out []string
	for n := range registry {
		out = append(out, n)
	}
	return out
}

// New builds the configured plugin.
func New(c Config) (Plugin, error) {
	make, ok := registry[c.Summarizer]
	if !ok {
		return nil, fmt.Errorf("brief: no summarizer %q (have %s)", c.Summarizer, strings.Join(Names(), ", "))
	}
	return make(c)
}

// Run summarizes r with p, from the cache when the same request was
// answered before, and checks the answer against the request.
func Run(p Plugin, c Config, r Request) (Result, error) {
	r.Version = Version
	key := cacheKey(p.Name(), c.Model, r)
	var cached Result
	if err := paths.ReadJSON(cacheFile(key), &cached); err == nil && !cached.At.IsZero() {
		cached.Cached = true
		check(&cached, r)
		return cached, nil
	}
	if spent := Spent(time.Now()); spent >= c.daily() {
		return Result{}, fmt.Errorf("daily budget reached ($%.2f of $%.2f); raise brief.daily_usd", spent, c.daily())
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	resp, meta, err := p.Summarize(ctx, r)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, fmt.Errorf("%s timed out after %s", p.Name(), c.timeout())
	}
	if err != nil {
		return Result{}, err
	}
	res := Result{Response: resp, Plugin: p.Name(), Model: meta.Model, CostUSD: meta.CostUSD, At: time.Now()}
	addSpent(time.Now(), meta.CostUSD)
	check(&res, r)
	if data, err := json.Marshal(res); err == nil {
		_ = paths.WriteFileAtomic(cacheFile(key), data)
	}
	return res, nil
}

// check drops what cites turns that weren't sent, and marks decision
// candidates whose quote is really in the cited turn. A valid citation
// doesn't make an inference right; it only makes it checkable.
func check(res *Result, r Request) {
	you := map[string]string{}
	for _, t := range r.Turns {
		you[t.Ref] = t.You
	}
	valid := func(ref string) bool { _, ok := you[ref]; return ok }
	var open []Sourced
	for _, o := range res.Open {
		if valid(o.Source) {
			open = append(open, o)
		} else {
			res.Dropped++
		}
	}
	res.Open = open
	var decs []DecisionCandidate
	for _, d := range res.Decisions {
		if !valid(d.Source) {
			res.Dropped++
			continue
		}
		q := strings.TrimSpace(d.Quote)
		if len([]rune(q)) < 8 { // "ok", "开mr": too little to stand for a decision
			res.Dropped++
			continue
		}
		d.Quoted = q != "" && strings.Contains(you[d.Source], q)
		decs = append(decs, d)
	}
	res.Decisions = decs
	if res.Bookmark != nil && !valid(res.Bookmark.Source) {
		res.Bookmark = nil
		res.Dropped++
	}
}

func cacheKey(plugin, model string, r Request) string {
	// The conversation, not the facts: those change with every file saved in
	// the checkout, and the brief shows them itself anyway.
	r.Facts = nil
	data, _ := json.Marshal(struct {
		P, M, I string
		R       Request
	}{plugin, model, Instructions, r})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

func cacheFile(key string) string { return filepath.Join(paths.StateDir(), "briefs", key+".json") }

func spendFile(day time.Time) string {
	return filepath.Join(paths.StateDir(), "briefs", "spend-"+day.Format("2006-01-02")+".json")
}

// Spent is what summaries cost on day.
func Spent(day time.Time) float64 {
	var s struct{ USD float64 }
	_ = paths.ReadJSON(spendFile(day), &s)
	return s.USD
}

func addSpent(day time.Time, usd float64) {
	s := struct{ USD float64 }{Spent(day) + usd}
	if data, err := json.Marshal(s); err == nil {
		_ = paths.WriteFileAtomic(spendFile(day), data)
	}
}

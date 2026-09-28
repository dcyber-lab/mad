package resume

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Input is what a brief is built from, collected up to one cutoff.
type Input struct {
	Dir      string
	Session  string // the agent's session now
	Title    string
	Events   []Event // the whole transcript; nil when not collected
	Followed bool    // the kind has a transcript reader
	Question string  // what the agent is waiting on you for, verbatim
	Shared   int     // other agents in the same directory
	Now      time.Time
}

// Item styles, most urgent first.
const (
	Block = "block" // it waits for you
	Fail  = "fail"
	Warn  = "warn" // evidence doesn't cover the current code
	OK    = "ok"
	Info  = "info"
	Claim = "claim" // the agent's words
)

type Item struct {
	Style string
	Text  string
	Sub   string
	Tag   string // what it rests on
}

// Levels: how much the brief opens by itself.
const (
	Quiet     = "quiet"
	Progress  = "progress"
	Attention = "attention"
)

// Brief is a view as of Cutoff. It is rebuilt from the context and the
// collected facts, never from an earlier brief.
type Brief struct {
	Cutoff     Checkpoint
	Events     int    // transcript events up to the cutoff
	Base       string // "caught up", "since you left", ""
	BaseAt     time.Time
	Goal       string
	Decisions  []Decision
	Anchor     *Anchor
	LastYou    string // your last instruction, when there is no bookmark
	Turns      []Turn // the exchanges shown, oldest first
	Window     []Turn // every exchange since the baseline (all of them without one)
	Before     bool   // nothing new since the baseline: the turns shown came before it
	Earlier    int    // turns in the window not shown
	ClaimLast  bool   // the last reply claims success
	Shared     int    // other agents working in the same directory
	Items      []Item
	Needs      []string
	Gaps       []string
	Level      string
	CurrentN   int
	StaleTests bool
}

var claimRe = regexp.MustCompile(`(?i)\b(pass(es|ed)?|done|fixed|complete[ds]?|works|all good|ready)\b|完成|通过|修好|搞定`)

// Build puts the brief together. c must have observed the current
// snapshot already.
func Build(c *Context, in Input) Brief {
	b := Brief{Goal: in.Title, Decisions: c.Active(), Anchor: c.Anchor}
	cur, haveCur := c.Current()
	b.Cutoff = Checkpoint{At: in.Now, Snap: cur.N, Session: in.Session}
	b.CurrentN = cur.N
	b.Events = len(in.Events)

	base := Checkpoint{}
	switch {
	case c.CaughtUp != nil:
		base, b.Base = *c.CaughtUp, "caught up"
	case c.Left != nil:
		base, b.Base = *c.Left, "since you left"
	}
	b.BaseAt = base.At

	if !in.Followed {
		b.Gaps = append(b.Gaps, "no transcript reader for this kind: messages and commands aren't collected")
	}
	if base.Session != "" && in.Session != "" && base.Session != in.Session {
		b.Gaps = append(b.Gaps, "the session changed since (/clear, fork): what came before belongs to the old one")
	}
	if cur.Moving {
		b.Gaps = append(b.Gaps, "the workspace changed while it was read: the file list is an observation")
	}

	var fresh []Event
	var lastYouAll string
	for _, e := range in.Events {
		if e.Kind == You && !trivial(e.Text) {
			lastYouAll = e.Text
		}
		if e.At.After(base.At) && !e.At.After(in.Now) {
			fresh = append(fresh, e)
		}
	}
	if c.Anchor == nil {
		b.LastYou = firstLine(lastYouAll)
	}

	// 1. What blocks you.
	if in.Question != "" {
		b.Items = append(b.Items, Item{Style: Block, Text: "agent asks: “" + in.Question + "”", Tag: "waiting on you"})
		b.Needs = append(b.Needs, "answer the agent: "+in.Question)
	}

	// 2. What bears on the evidence: test runs and whether they cover now.
	var lastTest *Event
	for i := range in.Events {
		if in.Events[i].IsTest() && !in.Events[i].At.After(in.Now) {
			lastTest = &in.Events[i]
		}
	}
	if lastTest != nil {
		e := *lastTest
		on, bound := c.testSnap(e)
		where := "code version unknown"
		if bound {
			where = fmt.Sprintf("on S%d", on)
		}
		when := e.At.Local().Format("15:04")
		switch {
		case !e.Done:
			b.Items = append(b.Items, Item{Style: Info, Text: "`" + short(e.TestPart()) + "` started " + when + ", no result read yet", Tag: "exec record"})
		case !e.ExitKnown:
			b.Items = append(b.Items, Item{Style: Info, Text: "`" + short(e.TestPart()) + "` ran " + when + ", outcome not recorded", Tag: "exec record"})
		case e.Exit != 0:
			b.Items = append(b.Items, Item{Style: Fail, Text: fmt.Sprintf("`%s` failed (exit %d) %s", short(e.TestPart()), e.Exit, where), Sub: "at " + when, Tag: "exec record"})
			b.Needs = append(b.Needs, "the last test run failed")
		default:
			b.Items = append(b.Items, Item{Style: OK, Text: fmt.Sprintf("`%s` exited 0 %s", short(e.TestPart()), where), Sub: "at " + when + " · exit 0 is not coverage", Tag: "exec record"})
			switch {
			case bound && haveCur && on != cur.N:
				b.StaleTests = true
				b.Items = append(b.Items, Item{Style: Warn, Text: fmt.Sprintf("code changed after that: now S%d, no test run on it", cur.N), Tag: "workspace"})
				b.Needs = append(b.Needs, fmt.Sprintf("S%d has no test run: re-run, or accept that", cur.N))
			}
		}
	} else if in.Followed {
		for _, e := range fresh {
			if e.Kind == Agent && claimRe.MatchString(e.Text) && strings.Contains(strings.ToLower(e.Text), "test") {
				b.Items = append(b.Items, Item{Style: Warn, Text: "the agent mentions tests, but no test run is on record", Tag: "claim vs record"})
				break
			}
		}
	}

	news := len(fresh) > 0 || base.At.IsZero()
	// 3. Progress: the workspace, what you said, what the agent says.
	if haveCur {
		var files []string
		basis := "since " + b.Base
		if old, ok := c.snap(base.Snap); ok && base.Snap != 0 {
			files = Changed(in.Dir, old, cur)
		} else {
			for p := range cur.Files {
				files = append(files, p)
			}
			basis = "vs HEAD (no earlier snapshot)"
		}
		if len(files) > 0 {
			news = true
			text := plural(len(files), "file") + " changed in the workspace"
			if in.Shared > 0 {
				text += fmt.Sprintf(" (checkout shared with %s)", plural(in.Shared, "other agent"))
			}
			b.Items = append(b.Items, Item{Style: Info, Text: text, Sub: list(files, 3), Tag: basis + " · not attributed"})
		}
	}
	// The exchanges themselves, verbatim: what you asked, what it answered.
	all := turns(in.Events, in.Now)
	var shown []Turn
	for _, t := range all {
		if !base.At.IsZero() && t.At.After(base.At) {
			shown = append(shown, t)
		}
	}
	if base.At.IsZero() {
		shown = all // no baseline: the latest ones, below
	}
	if len(shown) == 0 && !base.At.IsZero() && len(all) > 0 {
		// Nothing new since the baseline: the last exchanges before it, marked
		// as such, so the brief still says where things were left.
		shown, b.Before = all[max(len(all)-maxTurns, 0):], true
	}
	b.Window = shown
	if len(shown) > maxTurns {
		b.Earlier = len(shown) - maxTurns
		shown = shown[len(shown)-maxTurns:]
	}
	b.Turns = shown
	if n := len(shown); n > 0 && claimRe.MatchString(shown[n-1].Reply) {
		b.ClaimLast = true
	}

	if c.Anchor != nil {
		b.Needs = append(b.Needs, "your bookmark: "+c.Anchor.Text)
	}
	b.Shared = in.Shared
	switch {
	case in.Question != "" || b.StaleTests || containsStyle(b.Items, Fail):
		b.Level = Attention
	case news && (len(b.Items) > 0 || len(b.Turns) > 0):
		b.Level = Progress
	default:
		b.Level = Quiet
	}
	return b
}

// testSnap is the snapshot a test run saw: the same one in force when it
// started and when its result came back. Otherwise it is unknown.
func (c *Context) testSnap(e Event) (int, bool) {
	s1, ok1 := c.SnapAt(e.At)
	end := e.End
	if end.IsZero() {
		end = e.At
	}
	s2, ok2 := c.SnapAt(end)
	if !ok1 || !ok2 || s1.N != s2.N {
		return 0, false
	}
	return s1.N, true
}

// Fresh counts transcript events and snapshots after the cutoff: what came
// in while the brief was open, not shown in it.
func Fresh(b Brief, c *Context, events []Event) int {
	n := 0
	if cur, ok := c.Current(); ok && cur.N > b.Cutoff.Snap {
		n += cur.N - b.Cutoff.Snap
	}
	for _, e := range events {
		if e.At.After(b.Cutoff.At) {
			n++
		}
	}
	return n
}

func containsStyle(items []Item, st string) bool {
	for _, it := range items {
		if it.Style == st {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}

func short(cmd string) string {
	cmd = cdRe.ReplaceAllString(firstLine(cmd), "")
	if r := []rune(cmd); len(r) > 40 {
		cmd = string(r[:40]) + "…"
	}
	return cmd
}

func list(files []string, n int) string {
	if len(files) <= n {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:n], ", ") + fmt.Sprintf(" +%d", len(files)-n)
}

func plural(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}

const maxTurns = 3

// Turn is one exchange: a message of yours and what the agent did and
// said until your next one.
type Turn struct {
	At    time.Time
	You   string
	Reply string // the agent's last text in the turn: usually its answer
	Cmds  int
	Tests int
}

func turns(evs []Event, now time.Time) []Turn {
	var out []Turn
	for _, e := range evs {
		if e.At.After(now) {
			break
		}
		if e.Kind == You {
			out = append(out, Turn{At: e.At, You: e.Text})
			continue
		}
		if len(out) == 0 {
			continue
		}
		t := &out[len(out)-1]
		switch e.Kind {
		case Agent:
			t.Reply = e.Text
		case Cmd:
			t.Cmds++
			if e.IsTest() {
				t.Tests++
			}
		}
	}
	return out
}

// trivial: too short to stand for what you were after ("ok", "说中文").
func trivial(s string) bool { return len([]rune(strings.TrimSpace(s))) < 8 }

var cdRe = regexp.MustCompile(`^cd\s+("[^"]*"|'[^']*'|\S+)\s*&&\s*`)

// Short is a command as shown: its first line, without a leading cd.
func Short(cmd string) string { return short(cmd) }

package bus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
)

// Timers: one record for both kinds of scheduled word. A check-in is a
// timer of kind once, booked by an agent for itself ("wake me in 20m with
// this note"); a routine is kind routine, the person's: a text or the
// channel's progress report on a rhythm. Both fire the same way, as a post
// from mad of kind "timer" that the guards treat like a nudge (it wakes by
// mention, counts toward the quiet budget, waits for the agent's minute
// and is not posted in a paused channel); see CheckTimers. The timers are
// in bus/timers.jsonl.

// Kinds of timer, and what a routine does.
const (
	TimerOnce    = "once"
	TimerRoutine = "routine"

	ActionPost   = "post"
	ActionReport = "report"
)

// Limits and tunables, replaceable in tests.
var (
	maxCheckins = 3  // open check-ins per agent
	maxRoutines = 20 // routines in the deck
	minEvery    = 15 * time.Minute
	// heldFor is how long a check-in that cannot fire is kept trying; then
	// it is dropped and shows on the board.
	heldFor = 24 * time.Hour
	// timerKeep is how long a check-in that was dropped stays in the file
	// (for the board's card).
	timerKeep = 24 * time.Hour
	noteRunes = 600
)

// Timer is one scheduled word.
type Timer struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // once or routine
	// By is the agent that booked a check-in, or "you".
	By string `json:"by"`
	// At is when a check-in is due; NextAt is when the timer fires next
	// (a check-in's At, again).
	At     time.Time `json:"at,omitzero"`
	NextAt time.Time `json:"next_at,omitzero"`
	// Every is a routine's schedule, as ParseSchedule reads it.
	Every   string `json:"every,omitempty"`
	Channel string `json:"channel"`
	// Text is what a routine posts; a check-in's note is in Note.
	Text   string `json:"text,omitempty"`
	Action string `json:"action,omitempty"` // post or report
	// Mentions are the ids a routine's text mentions ("*" for @all).
	Mentions []string `json:"mentions,omitempty"`
	// Note is a check-in's note to its agent.
	Note   string `json:"note,omitempty"`
	Paused bool   `json:"paused,omitempty"`
	// Held says why a check-in is not firing: paused (its channel),
	// budget, stopped (its agent), error, or expired (dropped after
	// heldFor; kept for the board). HeldAt is when it began.
	Held   string    `json:"held,omitempty"`
	HeldAt time.Time `json:"held_at,omitzero"`
	// Why says it in words ("held: #dev paused"); for a routine it is what
	// became of its last slot (missed, paused by itself).
	Why       string    `json:"why,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	LastFired time.Time `json:"last_fired,omitzero"`
	Fired     int       `json:"fired,omitempty"`
	Missed    int       `json:"missed,omitempty"`
}

func timerFile() string { return filepath.Join(dir(), "timers.jsonl") }

func timerArchive() string { return filepath.Join(dir(), "archive", "timers.jsonl") }

func newTimerID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "r-" + hex.EncodeToString(b)
}

// loadTimers reads the timers, oldest first; a line that does not parse is
// skipped.
func loadTimers() ([]Timer, error) {
	data, err := os.ReadFile(timerFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Timer
	for _, line := range strings.Split(string(data), "\n") {
		var t Timer
		if line != "" && json.Unmarshal([]byte(line), &t) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func timerLines(ts []Timer) ([]byte, error) {
	var b strings.Builder
	for _, t := range ts {
		line, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// changeTimers runs fn on the timers under the lock and saves them, along
// with archiving the ones fn returns as finished. It does not write when
// nothing changed.
func changeTimers(fn func(ts *[]Timer) (finished []Timer, err error)) error {
	return locked(func() error {
		ts, err := loadTimers()
		if err != nil {
			return err
		}
		before, _ := timerLines(ts)
		fin, err := fn(&ts)
		if err != nil {
			return err
		}
		data, err := timerLines(ts)
		if err != nil {
			return err
		}
		if len(fin) > 0 {
			old, err := timerLines(fin)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(timerArchive()), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(timerArchive(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, err = f.Write(old)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
		if string(before) == string(data) {
			return nil
		}
		return writeAtomic(timerFile(), data)
	})
}

func findTimer(ts []Timer, id string) *Timer {
	for i := range ts {
		if ts[i].ID == id {
			return &ts[i]
		}
	}
	return nil
}

// open tells whether a check-in is still to fire (or being held to).
func (t Timer) openCheckin() bool { return t.Kind == TimerOnce && t.Held != "expired" }

// Timers lists all the timers, oldest first.
func Timers() ([]Timer, error) { return loadTimers() }

// Checkins lists the open check-ins of agent id, soonest first; an empty id
// lists everyone's.
func Checkins(id string) ([]Timer, error) {
	ts, err := loadTimers()
	if err != nil {
		return nil, err
	}
	var out []Timer
	for _, t := range ts {
		if t.openCheckin() && (id == "" || t.By == id) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NextAt.Before(out[j].NextAt) })
	return out, nil
}

// channelOK checks that channel ch exists and is not archived.
func channelOK(ch string) error {
	c, ok, err := Find(ch)
	if err != nil {
		return err
	}
	if !ok || c.Archived {
		return fmt.Errorf("no channel #%s", ch)
	}
	return nil
}

// AddCheckin books a check-in for agent by: at the time spec says ("in
// 20m", "at 15:00"), mad posts note to it in channel (by default its DM
// with the person) and wakes it. At most maxCheckins are open per agent,
// and the time is minAhead to maxAhead from at.
func AddCheckin(by, spec, channel, note string, at time.Time) (Timer, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return Timer{}, errors.New("a check-in needs a note: what to look at, and why")
	}
	if utf8.RuneCountInString(note) > noteRunes {
		return Timer{}, fmt.Errorf("the note is %d characters; keep it under %d", utf8.RuneCountInString(note), noteRunes)
	}
	st, err := state.Load()
	if err != nil {
		return Timer{}, err
	}
	a := findIn(st, by)
	if a == nil {
		return Timer{}, errors.New("this agent is no longer in the deck")
	}
	if a.Run != "" {
		return Timer{}, errors.New("an agent in a run books no check-ins")
	}
	sc, err := ParseSchedule(spec, at)
	if err != nil {
		return Timer{}, err
	}
	if !sc.Once {
		return Timer{}, errors.New("a check-in happens once: give in (20m) or at (15:00); a routine is the person's")
	}
	if d := sc.At.Sub(at); d < minAhead || d > maxAhead {
		return Timer{}, fmt.Errorf("a check-in is %s to %s from now, not %s", dur(minAhead), dur(maxAhead), dur(d))
	}
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if channel == "" {
		channel = DMChannel(by, You)
	}
	if isDM(channel) {
		if !dmMember(channel, by) {
			return Timer{}, fmt.Errorf("#%s is not your conversation", channel)
		}
	} else if err := channelOK(channel); err != nil {
		return Timer{}, err
	}
	t := Timer{ID: newTimerID(), Kind: TimerOnce, By: by, At: sc.At, NextAt: sc.At, Channel: channel, Note: note, CreatedAt: at}
	err = changeTimers(func(ts *[]Timer) ([]Timer, error) {
		var mine []string
		for _, x := range *ts {
			if x.openCheckin() && x.By == by {
				mine = append(mine, fmt.Sprintf("%s (%s: %s)", x.ID, x.At.Format("15:04"), firstLine(x.Note, 40)))
			}
		}
		if len(mine) >= maxCheckins {
			return nil, fmt.Errorf("you have %d open check-ins: %s; cancel one you no longer need (bus_checkin cancel)", len(mine), strings.Join(mine, ", "))
		}
		*ts = append(*ts, t)
		return nil, nil
	})
	if err != nil {
		return Timer{}, err
	}
	poked()
	return t, nil
}

// RoutineSpec is what a person sets a routine to.
type RoutineSpec struct {
	Every   string // the schedule
	Channel string
	Text    string
	// Report posts the channel's progress report instead of Text.
	Report bool
}

// AddRoutine makes a routine, the person's: at most maxRoutines, and an
// interval of at least minEvery. Its text's @mentions are the agents it
// wakes; they must exist.
func AddRoutine(sp RoutineSpec, at time.Time) (Timer, error) {
	sc, err := ParseSchedule(sp.Every, at)
	if err != nil {
		return Timer{}, err
	}
	if sc.Once {
		return Timer{}, errors.New("a routine repeats: daily, weekdays, weekends, days such as mon,wed, or every 30m; a one-off is an agent's check-in")
	}
	if sc.Clock < 0 && sc.Every < minEvery {
		return Timer{}, fmt.Errorf("every must be at least %s", dur(minEvery))
	}
	ch := strings.TrimPrefix(strings.TrimSpace(sp.Channel), "#")
	if err := channelOK(ch); err != nil {
		return Timer{}, err
	}
	text := strings.TrimSpace(sp.Text)
	t := Timer{ID: newTimerID(), Kind: TimerRoutine, By: You, Every: strings.Join(strings.Fields(strings.ToLower(sp.Every)), " "), Channel: ch, Text: text, Action: ActionPost, CreatedAt: at}
	switch {
	case sp.Report:
		t.Action, t.Text = ActionReport, ""
	case text == "":
		return Timer{}, errors.New("a routine needs a text to post, or the progress report")
	case utf8.RuneCountInString(text) > 2000:
		return Timer{}, errors.New("the text is too long for a routine")
	default:
		st, err := state.Load()
		if err != nil {
			return Timer{}, err
		}
		if t.Mentions, err = mentionsIn(st, text); err != nil {
			return Timer{}, err
		}
		if who := unknownMention(st, text); who != "" {
			return Timer{}, fmt.Errorf("no agent @%s: bus_who lists them", who)
		}
	}
	t.NextAt = sc.Next(at)
	if t.NextAt.IsZero() {
		return Timer{}, errors.New("that schedule never fires")
	}
	err = changeTimers(func(ts *[]Timer) ([]Timer, error) {
		n := 0
		for _, x := range *ts {
			if x.Kind == TimerRoutine {
				n++
			}
		}
		if n >= maxRoutines {
			return nil, fmt.Errorf("there are %d routines already; remove one (mad routine rm)", n)
		}
		*ts = append(*ts, t)
		return nil, nil
	})
	if err != nil {
		return Timer{}, err
	}
	poked()
	return t, nil
}

// RemoveTimer deletes timer id: a routine, or a check-in. An agent may
// remove only its own check-ins; by is its id, or "you".
func RemoveTimer(id, by string) error {
	err := changeTimers(func(ts *[]Timer) ([]Timer, error) {
		t := findTimer(*ts, id)
		if t == nil {
			return nil, fmt.Errorf("no timer %s", id)
		}
		if by != You && (t.Kind != TimerOnce || t.By != by) {
			return nil, fmt.Errorf("%s is not your check-in", id)
		}
		var kept []Timer
		for _, x := range *ts {
			if x.ID != id {
				kept = append(kept, x)
			}
		}
		*ts = kept
		return nil, nil
	})
	if err == nil {
		poked()
	}
	return err
}

// PauseTimer pauses a routine, or lets it go on (from its next slot after
// at).
func PauseTimer(id string, paused bool, at time.Time) error {
	err := changeTimers(func(ts *[]Timer) ([]Timer, error) {
		t := findTimer(*ts, id)
		if t == nil || t.Kind != TimerRoutine {
			return nil, fmt.Errorf("no routine %s", id)
		}
		if t.Paused == paused {
			return nil, nil
		}
		t.Paused = paused
		t.Why = ""
		if !paused {
			sc, err := ParseSchedule(t.Every, at)
			if err != nil {
				return nil, err
			}
			t.NextAt = sc.Next(at)
		}
		return nil, nil
	})
	if err == nil {
		poked()
	}
	return err
}

// dropCheckins removes agent id's check-ins.
func dropCheckins(id string) error {
	return changeTimers(func(ts *[]Timer) ([]Timer, error) {
		var kept []Timer
		for _, t := range *ts {
			if t.Kind != TimerOnce || t.By != id {
				kept = append(kept, t)
			}
		}
		*ts = kept
		return nil, nil
	})
}

// atRe finds the @names in a text.
var atRe = regexp.MustCompile(`(^|[\s(\[{<"'*,;])@([A-Za-z0-9][A-Za-z0-9_-]*)`)

// unknownMention is the first @name in text that is no agent, @all or @you,
// or "".
func unknownMention(st *state.State, text string) string {
	mask := codeMask(text)
	for _, m := range atRe.FindAllStringSubmatchIndex(text, -1) {
		at := m[4] - 1 // the @
		name := text[m[4]:m[5]]
		if mask[at] || strings.EqualFold(name, "all") || strings.EqualFold(name, You) {
			continue
		}
		end := len(text) // up to the next @, for a label of several words
		if k := strings.IndexByte(text[at+1:], '@'); k >= 0 {
			end = at + 1 + k
		}
		if ids, _ := mentionsIn(st, text[at:end]); len(ids) == 0 {
			return name
		}
	}
	return ""
}

// goneMention is the label of an agent that routine t's text mentions and
// that is no longer in the deck, or "".
func goneMention(st *state.State, t Timer) string {
	for _, id := range t.Mentions {
		if id != "*" && findIn(st, id) == nil {
			return drive.ShortID(id)
		}
	}
	return ""
}

// firing is one timer that goes out in this round.
type firing struct {
	t        Timer
	channel  string
	text     string
	mentions []string
	report   func() string // the report line to post, for a report routine
	wake     string        // in a DM, the agent it is for
}

// send posts the firing.
func (f firing) send() error {
	if f.report != nil {
		line := f.report()
		if line == "" {
			return nil
		}
		return PostSystem(f.channel, line)
	}
	sp := postSpec{channel: f.channel, from: madFrom, kind: "timer", text: f.text, mentions: f.mentions}
	if parts := strings.Split(f.channel, "."); isDM(f.channel) && len(parts) == 3 {
		sp.dm, sp.wake = parts[1:], f.wake
	}
	_, err := post(sp)
	return err
}

// label is how agent id is named now.
func labelOf(st *state.State, id string) string {
	if a := findIn(st, id); a != nil {
		return drive.Label(a)
	}
	return drive.ShortID(id)
}

// CheckTimers fires the timers due at time at; the sidebar runs it every
// minute, after CheckFollowUps. Everything is measured from the file, so
// it holds across restarts, and it is safe to run twice in a minute: a
// timer that fired is gone or moved on.
//
//   - A check-in fires however late, saying how late. Held when its channel
//     is paused, the day's budget is spent (SpendOK) or its agent is
//     stopped, it tries again each minute and fires when it can; after
//     heldFor it is dropped and the board shows it as waiting for you
//     (its Held says why). A check-in whose agent is gone is deleted.
//   - A routine fires at its slot; more than an hour late (the deck was
//     down) it is skipped, and a slot it cannot post to (a paused channel,
//     the budget spent) is missed: Missed counts them. It posts to a
//     stopped agent anyway, and wakes whom it can. A routine whose
//     @agent is gone pauses itself, and the person is told once.
//
// Fired timers are posts of kind "timer" from mad; a report routine posts
// the channel's report line instead, which wakes no one.
func CheckTimers(at time.Time, statuses map[string]string) error {
	_, err := runTimers(at, statuses, "")
	return err
}

// FireTimer fires timer id now, whatever its time, and tells why when it
// could not go out.
func FireTimer(id string, at time.Time, statuses map[string]string) error {
	refused, err := runTimers(at, statuses, id)
	if err != nil {
		return err
	}
	if why := refused[id]; why != "" {
		return errors.New(why)
	}
	return nil
}

// runTimers is CheckTimers, or, with only set, the forced firing of that
// timer alone. It returns why a forced one did not go out.
func runTimers(at time.Time, statuses map[string]string, only string) (map[string]string, error) {
	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	cl, err := loadChannels()
	if err != nil {
		return nil, err
	}
	force := only != ""
	refused := map[string]string{}
	var fires []firing
	var told []string
	err = changeTimers(func(tsp *[]Timer) ([]Timer, error) {
		var kept []Timer
		found := false
		for _, t := range *tsp {
			if force && t.ID != only {
				kept = append(kept, t)
				continue
			}
			found = true
			keep, f := planTimer(&t, st, cl, statuses, at, force, refused, &told)
			if f != nil {
				fires = append(fires, *f)
			}
			if keep {
				kept = append(kept, t)
			}
		}
		if force && !found {
			refused[only] = "no timer " + only
		}
		*tsp = kept
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	for _, msg := range told {
		tellPerson(notify.FollowUp, msg)
	}
	// Post outside the timers' lock; a failure is recorded on the timer.
	for _, f := range fires {
		perr := f.send()
		if perr != nil && force {
			refused[f.t.ID] = perr.Error()
		}
		_ = changeTimers(func(tsp *[]Timer) ([]Timer, error) {
			t := findTimer(*tsp, f.t.ID)
			if t == nil {
				return nil, nil
			}
			if t.Kind == TimerRoutine {
				if perr != nil {
					t.Fired, t.Missed, t.Why = t.Fired-1, t.Missed+1, "missed: "+perr.Error()
				}
				return nil, nil
			}
			if perr != nil {
				if t.Held == "" {
					t.HeldAt = at
				}
				t.Held, t.Why = "error", "held: "+perr.Error()
				return nil, nil
			}
			done := *t
			done.Fired, done.LastFired, done.Held = 1, at, ""
			var kept []Timer
			for _, x := range *tsp {
				if x.ID != t.ID {
					kept = append(kept, x)
				}
			}
			*tsp = kept
			return []Timer{done}, nil
		})
	}
	if len(fires) > 0 {
		poked()
	}
	return refused, nil
}

// planTimer decides what becomes of t at time at: it reports whether t
// stays in the file (changed in place), and the firing, if one goes out.
func planTimer(t *Timer, st *state.State, cl chanList, statuses map[string]string, at time.Time, force bool, refused map[string]string, told *[]string) (bool, *firing) {
	refuse := func(why string) {
		if force {
			refused[t.ID] = why
		}
	}
	if t.Kind == TimerOnce {
		if findIn(st, t.By) == nil {
			return false, nil // its agent was removed
		}
		if t.Held == "expired" {
			return at.Sub(t.HeldAt) <= timerKeep, nil
		}
	} else if gone := goneMention(st, *t); gone != "" && !t.Paused {
		t.Paused, t.Why = true, "no agent "+gone
		*told = append(*told, fmt.Sprintf("routine %s paused itself: no agent %s", t.ID, gone))
	}
	if t.Paused && !force || !force && t.NextAt.After(at) {
		return true, nil
	}
	late := max(0, at.Sub(t.NextAt))
	var sc Schedule
	if t.Kind == TimerRoutine {
		var err error
		if sc, err = ParseSchedule(t.Every, at); err != nil {
			return true, nil
		}
	}
	// advance moves a routine on to its next slot after the one now.
	advance := func() {
		t.NextAt = sc.Next(at)
		if t.NextAt.IsZero() {
			t.Paused, t.Why = true, "no next time"
		}
	}
	if t.Kind == TimerRoutine && !force && late > reportWindow {
		t.Missed++
		t.Why = fmt.Sprintf("missed: %s late (the deck was down)", dur(late))
		advance()
		return true, nil
	}

	// Why it cannot go out now, if so.
	code, hold := "", ""
	c := cl.find(t.Channel)
	switch {
	case c == nil && !isDM(t.Channel) || c != nil && c.Archived:
		code, hold = "error", fmt.Sprintf("#%s is gone", t.Channel)
	case t.Action == ActionReport:
	case c != nil && c.Paused:
		code, hold = "paused", fmt.Sprintf("#%s paused", t.Channel)
	case !SpendOK(at):
		code, hold = "budget", "budget"
	case t.Kind == TimerOnce && !canNudge(st, statuses, t.By):
		code, hold = "stopped", labelOf(st, t.By)+" stopped"
	}
	if hold != "" {
		refuse("held: " + hold)
		if force {
			return true, nil
		}
		if t.Kind == TimerRoutine {
			t.Missed++
			t.Why = "missed: " + hold
			advance()
			return true, nil
		}
		if t.Held == "" {
			t.HeldAt = at
		}
		t.Held, t.Why = code, "held: "+hold
		if at.Sub(t.HeldAt) > heldFor {
			t.Held, t.HeldAt, t.Why = "expired", at, "dropped: held "+dur(heldFor)+", "+hold
		}
		return true, nil
	}

	f := firing{t: *t, channel: t.Channel}
	if t.Kind == TimerOnce {
		who := labelOf(st, t.By)
		f.mentions, f.wake = []string{t.By}, t.By
		f.text = fmt.Sprintf("[mad] check-in for @%s (booked %s, in %s", who, t.CreatedAt.Format("15:04"), dur(t.At.Sub(t.CreatedAt)))
		if late >= 2*time.Minute {
			f.text += ", late by " + dur(late)
		}
		f.text += "): " + t.Note
		return true, &f // the file drops it once posted
	}
	f.mentions = t.Mentions
	for _, id := range t.Mentions {
		if id != "*" && f.wake == "" {
			f.wake = id
		}
	}
	if isDM(t.Channel) && f.wake == "" {
		for _, p := range strings.Split(t.Channel, ".")[1:] {
			if p != You && p != system {
				f.wake = p
			}
		}
	}
	if t.Action == ActionReport {
		ch := t.Channel
		f.report = func() string { return ReportChannel(st, statuses, at, ch) }
	} else {
		f.text = t.Text
	}
	t.LastFired, t.Fired, t.Why = at, t.Fired+1, ""
	advance()
	return true, &f
}

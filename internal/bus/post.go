package bus

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
)

// minPost is the shortest text an agent may post, in characters.
const minPost = 12

// ackRe matches a post that is only an acknowledgment.
var ackRe = regexp.MustCompile(`(?i)^\s*(ok|okay|thanks|thank you|got it|noted|done|sounds good|👍)[\s[:punct:]]*$`)

// errAck is why an acknowledgment is not posted.
var errAck = errors.New("not posted: a message that only acknowledges needs no reply, and reading one needs no answer; post only to ask or tell someone something")

// mentionsIn finds the agents that text mentions: @label, @id-prefix (8 or
// more characters) and @all (reported as "*"). Ids come in the order of
// their first mention. An agent in a run is an error.
func mentionsIn(st *state.State, text string) ([]string, error) {
	type name struct {
		label string // lower case
		id    string
	}
	var names []name
	for _, a := range st.OrderedAgents() {
		names = append(names, name{asciiLower(drive.Label(a)), a.ID})
	}
	sort.SliceStable(names, func(i, j int) bool { return len(names[i].label) > len(names[j].label) })

	var out []string
	add := func(id string) {
		if !has(out, id) {
			out = append(out, id)
		}
	}
	boundary := func(s string) bool { // s follows a name
		// Names are ASCII, and a Chinese writer types no space after one
		// (@architect你好): only an ASCII character goes on a name.
		r, _ := utf8.DecodeRuneInString(s)
		return s == "" || r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	}
	lower := asciiLower(text) // same byte offsets as text
	code := codeMask(text)
	for i := 0; i < len(text); i++ {
		if text[i] != '@' || code[i] {
			continue
		}
		if i > 0 {
			// An address or a URL is not a mention: an @ follows a space,
			// the start, or an opening mark.
			if r, _ := utf8.DecodeLastRuneInString(text[:i]); !unicode.IsSpace(r) && !strings.ContainsRune("([{<\"'*,;", r) {
				continue
			}
		}
		rest := lower[i+1:]
		var hits []string
		label := ""
		for _, n := range names {
			if n.label != "" && strings.HasPrefix(rest, n.label) && boundary(rest[len(n.label):]) {
				label = n.label // the longest label wins
				for _, m := range names {
					if m.label == n.label {
						hits = append(hits, m.id)
					}
				}
				break
			}
		}
		switch {
		case len(hits) > 1:
			return nil, fmt.Errorf("@%s names %d agents; mention one by @<id>", label, len(hits))
		case len(hits) == 1:
			add(hits[0])
		case strings.HasPrefix(rest, "all") && boundary(rest[3:]):
			add("*")
		default:
			end := 0
			for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9' || rest[end] >= 'a' && rest[end] <= 'f' || rest[end] == '-') {
				end++
			}
			if end >= 8 && boundary(rest[end:]) {
				var ids []string
				for _, n := range names {
					if strings.HasPrefix(n.id, rest[:end]) {
						ids = append(ids, n.id)
					}
				}
				if len(ids) == 1 {
					add(ids[0])
				}
			}
		}
	}
	for _, id := range out {
		if id == "*" {
			continue
		}
		if a := findIn(st, id); a != nil && a.Run != "" {
			return nil, fmt.Errorf("%s is in a run; use mad run note", drive.Label(a))
		}
	}
	return out, nil
}

// asciiLower lowers A-Z only, so that byte offsets in the result are those
// of s (Unicode case mapping changes the length of some letters).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// codeMask marks the bytes of text that are inline code, a fenced code
// block or a URL: an @ there is not a mention.
func codeMask(text string) []bool {
	mask := make([]bool, len(text)+1)
	set := func(from, to int) {
		for i := from; i < to && i < len(text); i++ {
			mask[i] = true
		}
	}
	lineStart := func(i int) bool { return i == 0 || text[i-1] == '\n' }
	for i := 0; i < len(text); {
		switch {
		case text[i] == '`' && lineStart(i) && strings.HasPrefix(text[i:], "```"):
			end := len(text) // up to the closing fence line, or the end
			for j := i + 3; j < len(text); j++ {
				if text[j] == '\n' && strings.HasPrefix(strings.TrimLeft(text[j+1:], " "), "```") {
					end = len(text) // past the closing fence line
					if k := strings.IndexByte(text[j+1:], '\n'); k >= 0 {
						end = j + 1 + k + 1
					}
					break
				}
			}
			set(i, end)
			i = max(end, i+3)
		case text[i] == '`':
			n := 0
			for i+n < len(text) && text[i+n] == '`' {
				n++
			}
			ticks := text[i : i+n]
			if k := strings.Index(text[i+n:], ticks); k >= 0 {
				set(i, i+n+k+n)
				i += n + k + n
			} else {
				i += n
			}
		case strings.HasPrefix(text[i:], "://"):
			from := i
			for from > 0 && (text[from-1] >= 'a' && text[from-1] <= 'z' || text[from-1] >= 'A' && text[from-1] <= 'Z' || text[from-1] == '+') {
				from--
			}
			end := i
			for end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '\t' {
				end++
			}
			set(from, end)
			i = end
		default:
			i++
		}
	}
	return mask
}

func findIn(st *state.State, id string) *state.Agent {
	_, a := st.FindAgent(id)
	return a
}

// postSpec says what to post.
type postSpec struct {
	channel string
	// dm, when set, is the two ids of a DM channel, made on first use; wake
	// is the one of them that is always woken.
	dm      []string
	wake    string
	from    From
	text    string
	replyTo int
	// images are the pictures it carries, kept already (StoreImages).
	images []Image
	// kind is the message's kind when it is not text (handoff, done or
	// system); then mentions are the agents it wakes, as given, and ref
	// the handoff it belongs to.
	kind     string
	mentions []string
	ref      *HandoffRef
	// stored, when set, runs under the bus lock once the message has its
	// seq, to record it elsewhere in the same step.
	stored func(seq int) error
}

// mads tells whether kind is one of mad's own words that the guards hold
// back like a nudge: a nudge, or a timer going off.
func mads(kind string) bool { return kind == "nudge" || kind == "timer" }

// errNudgePaused is why a nudge is not posted: a paused channel waits for a
// person, and mad does not add to it.
var errNudgePaused = errors.New("channel is paused: no nudge")

// errNudgeSpent is why a nudge is not posted: the day's budget for mad's
// own work is spent.
var errNudgeSpent = errors.New("today's budget is spent: no nudge")

// relay tells whether the post carries a handoff: its start, its result or
// a lapse. These always wake their target and are not counted by the
// guards, so a pause cannot swallow the end of a relay.
func (sp postSpec) relay() bool { return sp.kind == "handoff" || sp.kind == "done" || sp.ref != nil }

// dmMember tells whether id is one of the two of DM channel ch.
func dmMember(ch, id string) bool {
	parts := strings.Split(ch, ".")
	return len(parts) == 3 && (parts[1] == id || parts[2] == id)
}

// Post adds text from from to channel, and wakes the agents it mentions.
// The guards: an agent's acknowledgment is refused; after warnAt posts by
// agents with no word from a person it warns, and at pauseAt, or when two
// agents alternate pingPong times, the channel pauses until a person posts
// or resumes it.
func Post(channel string, from From, text string, replyTo int) (PostResult, error) {
	return post(postSpec{channel: channel, from: from, text: text, replyTo: replyTo})
}

// PostImages is Post with pictures, kept already (StoreImages); a message
// may be pictures alone.
func PostImages(channel string, from From, text string, replyTo int, images []Image) (PostResult, error) {
	return post(postSpec{channel: channel, from: from, text: text, replyTo: replyTo, images: images})
}

// DM posts text to agent to in the channel between the two, made on the
// first use; to is always woken. It returns the channel.
func DM(from From, to, text string) (string, PostResult, error) {
	return DMImages(from, to, text, nil)
}

// DMImages is DM with pictures, kept already (StoreImages).
func DMImages(from From, to, text string, images []Image) (string, PostResult, error) {
	if to == "" {
		return "", PostResult{}, errors.New("no one to send to")
	}
	if to == from.ID {
		return "", PostResult{}, errors.New("a message to yourself reaches no one")
	}
	ch := dmID(from.ID, to)
	res, err := post(postSpec{channel: ch, dm: []string{from.ID, to}, wake: to, from: from, text: text, images: images})
	return ch, res, err
}

// ErrTheirDM: the person may read a DM between two agents, not post in it.
var ErrTheirDM = errors.New("a DM between two agents is theirs: read it, or post in a channel")

// Reply posts text as the person in ch, answering message seq (0 for
// none): in a channel, a post; in a DM of theirs, a post in it, made with
// the first one. The window's composer and its inbox both post this way.
func Reply(ch, text string, seq int) (PostResult, error) {
	return ReplyImages(ch, text, seq, nil)
}

// ReplyImages is Reply with pictures, kept already (StoreImages).
func ReplyImages(ch, text string, seq int, images []Image) (PostResult, error) {
	me := From{ID: You, Label: You}
	if !isDM(ch) {
		return PostImages(ch, me, text, seq, images)
	}
	ids := DMSides(ch)
	if len(ids) != 2 || (ids[0] != You && ids[1] != You) {
		return PostResult{}, ErrTheirDM
	}
	to := ids[0]
	if to == You {
		to = ids[1]
	}
	if _, ok, _ := Find(ch); ok && seq > 0 {
		return PostImages(ch, me, text, seq, images)
	}
	_, res, err := DMImages(me, to, text, images)
	return res, err
}

// CheckReply says whether the person may post to ch now, before anything is
// kept for the post: the channel is there, or it is a DM of theirs with an
// agent that is.
func CheckReply(ch string) error {
	if !isDM(ch) {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		if c := cl.find(ch); c == nil || c.Archived {
			return fmt.Errorf("no channel #%s", ch)
		}
		return nil
	}
	ids := DMSides(ch)
	if len(ids) != 2 || (ids[0] != You && ids[1] != You) {
		return ErrTheirDM
	}
	to := ids[0]
	if to == You {
		to = ids[1]
	}
	return CheckDM(From{ID: You, Label: You}, to)
}

// CheckDM says whether from may message agent to: it is one, not itself,
// and not in a run.
func CheckDM(from From, to string) error {
	if to == "" {
		return errors.New("no one to send to")
	}
	if to == from.ID {
		return errors.New("a message to yourself reaches no one")
	}
	st, err := state.Load()
	if err != nil {
		return err
	}
	a := findIn(st, to)
	if a == nil {
		return fmt.Errorf("no agent %q", to)
	}
	if a.Run != "" {
		return fmt.Errorf("%s is in a run; use mad run note", drive.Label(a))
	}
	return nil
}

func post(sp postSpec) (PostResult, error) {
	text := strings.TrimSpace(sp.text)
	if text == "" && len(sp.images) == 0 {
		return PostResult{}, errors.New("nothing to post")
	}
	byAgent := sp.from.ID != You
	if byAgent && sp.kind == "" && (utf8.RuneCountInString(text) < minPost || ackRe.MatchString(text)) {
		return PostResult{}, errAck
	}
	if isDM(sp.channel) && sp.from.ID != system && !dmMember(sp.channel, sp.from.ID) {
		return PostResult{}, fmt.Errorf("#%s is a private conversation of two others; you may not post in it", sp.channel)
	}
	st, err := state.Load()
	if err != nil {
		return PostResult{}, err
	}
	mentions := sp.mentions
	if sp.kind == "" {
		if mentions, err = mentionsIn(st, text); err != nil {
			return PostResult{}, err
		}
	}
	if sp.wake != "" && sp.wake != You {
		a := findIn(st, sp.wake)
		if a == nil {
			return PostResult{}, fmt.Errorf("no agent %q", sp.wake)
		}
		if a.Run != "" {
			return PostResult{}, fmt.Errorf("%s is in a run; use mad run note", drive.Label(a))
		}
	}

	// With the day's budget spent, mad starts no turn by itself: a nudge
	// is not posted, and an agent's words wake no one.
	spent := !SpendOK(now())
	if spent && mads(sp.kind) {
		return PostResult{}, errNudgeSpent
	}
	res := PostResult{Woke: []string{}}
	var woken []string
	paused := false
	err = locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		c := cl.find(sp.channel)
		if c == nil && sp.dm != nil {
			cl.Channels = append(cl.Channels, Channel{ID: sp.channel, Members: []string{}, CreatedBy: sp.from.ID, CreatedAt: now()})
			c = &cl.Channels[len(cl.Channels)-1]
			if err := addMembers(c, nil, sp.dm...); err != nil {
				return err
			}
		}
		if c == nil || c.Archived {
			return fmt.Errorf("no channel #%s", sp.channel)
		}
		if mads(sp.kind) && c.Paused {
			return errNudgePaused
		}
		for _, im := range sp.images {
			if im.Path() == "" { // collected, or never kept
				return fmt.Errorf("a picture of this message is gone: send it again")
			}
		}
		msgs, err := readMsgs(c.ID)
		if err != nil {
			return err
		}
		if parts := strings.Split(c.ID, "."); isDM(c.ID) && len(parts) == 3 {
			// Both sides are members, whatever happened to the list.
			if err := addMembers(c, msgs, parts[1], parts[2]); err != nil {
				return err
			}
		}
		if !isDM(c.ID) { // a mention brings the agent in
			for _, id := range mentions {
				if id != "*" {
					if err := addMembers(c, msgs, id); err != nil {
						return err
					}
				}
			}
		}

		kind := "text"
		if sp.kind != "" {
			kind = sp.kind
		}
		m := Message{From: sp.from, Kind: kind, Text: text, Mentions: mentions, ReplyTo: sp.replyTo, Handoff: sp.ref, Images: sp.images}
		pause := ""
		switch {
		case kind == "system" || sp.relay(): // mad's own line, and a relay, count for nothing
		case !byAgent:
			c.Paused = false
		case c.Paused:
			res.Warning = fmt.Sprintf("#%s is paused until a person posts or resumes it (mad bus resume %s); your message is kept, and no one is woken", c.ID, c.ID)
		default:
			quiet := 1
			for i := len(msgs) - 1; i >= 0 && msgs[i].From.ID != You; i-- {
				if msgs[i].Kind != "system" && msgs[i].Handoff == nil {
					quiet++
				}
			}
			switch {
			case quiet >= pauseAt:
				pause = fmt.Sprintf("Paused: %d posts by agents with no word from a person. It waits for a person: post here, or mad bus resume %s.", quiet, c.ID)
			case pingPongAt(msgs, m):
				pause = fmt.Sprintf("Paused: two agents answered each other %d times in a row. It waits for a person: post here, or mad bus resume %s.", pingPong, c.ID)
			case quiet >= warnAt:
				res.Warning = fmt.Sprintf("%d agent posts in #%s with no word from a person; %d left before it pauses", quiet, c.ID, pauseAt-quiet)
			}
		}
		stored, err := addMsg(c.ID, &msgs, m)
		if err != nil {
			return err
		}
		res.Seq = stored.Seq
		if sp.stored != nil {
			if err := sp.stored(stored.Seq); err != nil {
				return err
			}
		}
		if pause != "" {
			c.Paused = true
			paused = true
			res.Warning = pause
			note := systemMsg(pause)
			note.Event = EventPaused
			if _, err := addMsg(c.ID, &msgs, note); err != nil {
				return err
			}
		}
		if !c.Paused || sp.relay() {
			woken = wakeList(st, *c, sp, mentions)
		}
		if spent && byAgent && sp.kind == "" && len(woken) > 0 {
			woken = nil
			if res.Warning != "" {
				res.Warning += "; "
			}
			res.Warning += spendWarning
		}
		return saveChannels(cl)
	})
	if err != nil {
		return PostResult{}, err
	}
	for _, id := range woken {
		_ = Notify(id)
		if a := findIn(st, id); a != nil {
			res.Woke = append(res.Woke, drive.Label(a))
		}
	}
	poked()
	// What is for the person tells them: a DM to them, an @you, a
	// channel that waits for them.
	switch {
	case !byAgent:
	case paused:
		tell(sp.channel, sp.from, "#"+sp.channel+" is paused: it waits for you")
	case mads(sp.kind): // its "@you" is for the agent to use, not a message for the person
	case sp.wake == You || youRe.MatchString(text):
		tell(sp.channel, sp.from, text)
	}
	return res, nil
}

// youRe finds @you in a message: at the start, or after a space (any Unicode space: \s alone is ASCII) or a
// (half or full width) parenthesis; and not followed by an ASCII letter, digit, _ or -, so "（@you" and
// "@you请看" count and "@you-x" does not. Go has no lookahead, so the end is matched ($ or a character that
// is not one of those). The page's rule is the same (bus.js YOU_MENTION), as native's is; testdata/at-you.json
// is the table all three are held to.
var youRe = regexp.MustCompile(`(?i)(^|[\s\p{Zs}(（])@you($|[^A-Za-z0-9_-])`)

// tell lets the person know about a message for them, as the deck's
// notifications do (on their phone too); replaced in tests.
var tell = func(channel string, from From, text string) {
	cfg, _ := deck.LoadConfig()
	if !cfg.Notify.Wants(notify.Bus) {
		return
	}
	_ = notify.Send(cfg.Notify, notify.Event{Kind: notify.Bus, Project: channelName(channel), Agent: from.Label, Message: text, Channel: channel})
}

// channelName is how a channel is named to the person: #name, or DM.
func channelName(ch string) string {
	if isDM(ch) {
		return "DM"
	}
	return "#" + ch
}

// pingPongAt tells whether m, after msgs, is the pingPong-th post in a row
// alternating strictly between two agents.
func pingPongAt(msgs []Message, m Message) bool {
	if len(msgs) < pingPong-1 {
		return false
	}
	if m.Handoff != nil {
		return false
	}
	run := append(append([]Message(nil), msgs[len(msgs)-(pingPong-1):]...), m)
	for i, x := range run {
		if x.Kind == "system" || x.Handoff != nil || x.From.ID == You || x.From.ID == system {
			return false
		}
		if i > 0 && x.From.ID == run[i-1].From.ID {
			return false
		}
		if i > 1 && x.From.ID != run[i-2].From.ID {
			return false
		}
	}
	return true
}

// wakeList is the agents a post wakes: those mentioned, every member for
// @all, the other side of a DM. Never the author, nor a person, nor an agent
// that is not in the state or is in a run.
func wakeList(st *state.State, c Channel, sp postSpec, mentions []string) []string {
	var out []string
	add := func(id string) {
		a := findIn(st, id)
		if id != sp.from.ID && id != You && a != nil && a.Run == "" && !has(out, id) {
			out = append(out, id)
		}
	}
	if sp.wake != "" {
		add(sp.wake)
	}
	for _, id := range mentions {
		if isDM(c.ID) {
			break
		}
		if id == "*" {
			for _, m := range c.Members {
				add(m)
			}
		} else {
			add(id)
		}
	}
	return out
}

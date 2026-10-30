package bus

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dcyber-lab/mad/internal/drive"
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
		names = append(names, name{strings.ToLower(drive.Label(a)), a.ID})
	}
	sort.SliceStable(names, func(i, j int) bool { return len(names[i].label) > len(names[j].label) })

	var out []string
	add := func(id string) {
		if !has(out, id) {
			out = append(out, id)
		}
	}
	boundary := func(s string) bool { // s follows a name
		r, _ := utf8.DecodeRuneInString(s)
		return s == "" || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	}
	lower := strings.ToLower(text)
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(text[:i]); unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue // an address, not a mention
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
	// kind is the message's kind when it is not text (handoff, done or
	// system); then mentions are the agents it wakes, as given, and ref
	// the handoff it belongs to.
	kind     string
	mentions []string
	ref      *HandoffRef
}

// Post adds text from from to channel, and wakes the agents it mentions.
// The guards: an agent's acknowledgment is refused; after warnAt posts by
// agents with no word from a person it warns, and at pauseAt, or when two
// agents alternate pingPong times, the channel pauses until a person posts
// or resumes it.
func Post(channel string, from From, text string, replyTo int) (PostResult, error) {
	return post(postSpec{channel: channel, from: from, text: text, replyTo: replyTo})
}

// DM posts text to agent to in the channel between the two, made on the
// first use; to is always woken. It returns the channel.
func DM(from From, to, text string) (string, PostResult, error) {
	if to == "" {
		return "", PostResult{}, errors.New("no one to send to")
	}
	if to == from.ID {
		return "", PostResult{}, errors.New("a message to yourself reaches no one")
	}
	ch := dmID(from.ID, to)
	res, err := post(postSpec{channel: ch, dm: []string{from.ID, to}, wake: to, from: from, text: text})
	return ch, res, err
}

func post(sp postSpec) (PostResult, error) {
	text := strings.TrimSpace(sp.text)
	if text == "" {
		return PostResult{}, errors.New("nothing to post")
	}
	byAgent := sp.from.ID != You
	if byAgent && sp.kind == "" && (utf8.RuneCountInString(text) < minPost || ackRe.MatchString(text)) {
		return PostResult{}, errAck
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

	res := PostResult{Woke: []string{}}
	var woken []string
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
		msgs, err := readMsgs(c.ID)
		if err != nil {
			return err
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
		m := Message{From: sp.from, Kind: kind, Text: text, Mentions: mentions, ReplyTo: sp.replyTo, Handoff: sp.ref}
		pause := ""
		switch {
		case kind == "system": // mad's own line counts for nothing
		case !byAgent:
			c.Paused = false
		case c.Paused:
			res.Warning = fmt.Sprintf("#%s is paused until a person posts or resumes it (mad bus resume %s); your message is kept, and no one is woken", c.ID, c.ID)
		default:
			quiet := 1
			for i := len(msgs) - 1; i >= 0 && msgs[i].From.ID != You; i-- {
				if msgs[i].Kind != "system" {
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
		if pause != "" {
			c.Paused = true
			res.Warning = pause
			if _, err := addMsg(c.ID, &msgs, systemMsg(pause)); err != nil {
				return err
			}
		}
		if !c.Paused {
			woken = wakeList(st, *c, sp, mentions)
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
	return res, nil
}

// pingPongAt tells whether m, after msgs, is the pingPong-th post in a row
// alternating strictly between two agents.
func pingPongAt(msgs []Message, m Message) bool {
	if len(msgs) < pingPong-1 {
		return false
	}
	run := append(append([]Message(nil), msgs[len(msgs)-(pingPong-1):]...), m)
	for i, x := range run {
		if x.Kind == "system" || x.From.ID == You || x.From.ID == system {
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

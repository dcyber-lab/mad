package bus

// Receipts: what became of the person's messages. A message wakes the
// agents it @mentions (every member for @all), or the other side of a DM,
// and no one else (wakeList): a plain line in a channel wakes nobody, and
// agents see it only when they read the channel.

// Receipt states of one target, each implying the ones before it.
const (
	RcWaiting   = "waiting"   // not yet delivered to the agent
	RcDelivered = "delivered" // in a digest sent to it
	RcRead      = "read"      // it read the channel past the message
	RcReplied   = "replied"   // it answered the message, or mentioned the person after it
)

// Target is an agent a message wakes, and how far the message got to it.
type Target struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// Receipt is how one of the person's messages stands. Targets is empty
// when it wakes nobody.
type Receipt struct {
	Seq     int      `json:"seq"`
	Targets []Target `json:"targets"`
	// Paused: the channel is paused, so what waits is held until a person
	// resumes it.
	Paused bool `json:"paused,omitempty"`
}

// Receipts says how the person's last n messages of a channel stand (n
// of 0 or less is 20). Each agent's cursor is read once, and the channel's
// messages once. It takes no lock (see YourChannels): a window asks every
// second or two for each channel open in it.
func Receipts(channel string, n int) ([]Receipt, error) {
	if n <= 0 {
		n = 20
	}
	cl, err := loadChannels()
	if err != nil {
		return nil, err
	}
	c := cl.find(channel)
	if c == nil {
		return nil, nil
	}
	msgs, err := readMsgs(channel)
	if err != nil {
		return nil, err
	}
	cursors := map[string]cursor{}
	cursorFor := func(id string) cursor {
		cu, ok := cursors[id]
		if !ok {
			cs, _ := loadCursors(id)
			cu = cs[channel]
			cursors[id] = cu
		}
		return cu
	}
	ans := answersIn(msgs, isDM(channel))
	var out []Receipt
	for i := len(msgs) - 1; i >= 0 && len(out) < n; i-- {
		m := msgs[i]
		if m.From.ID != You || m.Kind == "system" {
			continue
		}
		r := Receipt{Seq: m.Seq, Targets: []Target{}, Paused: c.Paused}
		for _, id := range targetsOf(*c, m) {
			cu := cursorFor(id)
			st := RcWaiting
			switch {
			case ans.answered(i, m, id):
				st = RcReplied
			case cu.Read >= m.Seq:
				st = RcRead
			case cu.Delivered >= m.Seq:
				st = RcDelivered
			}
			r.Targets = append(r.Targets, Target{id, st})
		}
		out = append(out, r)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// targetsOf is the agents message m of channel c wakes, in the order they
// are named; the same as wakeList, without looking at the deck.
func targetsOf(c Channel, m Message) []string {
	var out []string
	add := func(id string) {
		if id != m.From.ID && id != You && !has(out, id) {
			out = append(out, id)
		}
	}
	if isDM(c.ID) {
		for _, id := range DMSides(c.ID) {
			add(id)
		}
		return out
	}
	for _, id := range m.Mentions {
		if id == "*" {
			for _, x := range c.Members {
				add(x)
			}
		} else {
			add(id)
		}
	}
	return out
}

// answers is, for each agent, where in a channel's messages it last
// answered the person: by any message that asks them (@you), or any at all
// in a DM (last), and by a reply to each message (to, by its seq). Made in
// one pass, so a receipt's state is a lookup, not a scan of what followed.
type answers map[string]*agentAnswers

type agentAnswers struct {
	last int
	to   map[int]int
}

func answersIn(msgs []Message, dm bool) answers {
	out := answers{}
	for j, x := range msgs {
		if x.Kind == "system" {
			continue
		}
		a := out[x.From.ID]
		if a == nil {
			a = &agentAnswers{last: -1}
			out[x.From.ID] = a
		}
		if dm || mentionsYou(x.Text) {
			a.last = j
		}
		if x.ReplyTo > 0 {
			if a.to == nil {
				a.to = map[int]int{}
			}
			a.to[x.ReplyTo] = j
		}
	}
	return out
}

// answered tells whether agent id replied to m, the message at index i: a
// message of its own after it that answers it, or asks the person (@you),
// or, in a DM, any.
func (as answers) answered(i int, m Message, id string) bool {
	a := as[id]
	if a == nil {
		return false
	}
	if a.last > i {
		return true
	}
	j, ok := a.to[m.Seq]
	return ok && j > i
}

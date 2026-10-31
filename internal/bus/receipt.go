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
// of 0 or less is 20). Each agent's cursor is read once.
func Receipts(channel string, n int) ([]Receipt, error) {
	if n <= 0 {
		n = 20
	}
	var out []Receipt
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		c := cl.find(channel)
		if c == nil {
			return nil
		}
		msgs, err := readMsgs(channel)
		if err != nil {
			return err
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
				case answered(msgs[i+1:], m, id, isDM(channel)):
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
		return nil
	})
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
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

// answered tells whether, in later, agent id replied to m: a message of
// its own that answers it, or asks the person (@you), or, in a DM, any.
func answered(later []Message, m Message, id string, dm bool) bool {
	for _, x := range later {
		if x.From.ID != id || x.Kind == "system" {
			continue
		}
		if dm || x.ReplyTo == m.Seq || youRe.MatchString(x.Text) {
			return true
		}
	}
	return false
}

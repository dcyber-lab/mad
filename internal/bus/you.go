package bus

// The person reads every channel: there is no membership to count by.

// Yours is how a channel stands for the person: its last message, and
// how many of the agents' they have not read (MarkRead(You, ...)).
type Yours struct {
	Last   int `json:"last"`
	Unread int `json:"unread"`
}

// YourChannels says how each channel stands for the person, by id.
func YourChannels() (map[string]Yours, error) {
	out := map[string]Yours{}
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		cs, err := loadCursors(You)
		if err != nil {
			return err
		}
		for _, c := range cl.Channels {
			if c.Archived {
				continue
			}
			msgs, err := readMsgs(c.ID)
			if err != nil {
				return err
			}
			var y Yours
			for _, m := range msgs {
				y.Last = max(y.Last, m.Seq)
				if m.Seq > cs[c.ID].Read && m.Kind != "system" && m.From.ID != You {
					y.Unread++
				}
			}
			out[c.ID] = y
		}
		return nil
	})
	return out, err
}

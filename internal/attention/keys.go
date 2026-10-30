package attention

import (
	"strconv"
	"time"
)

// The keys, one maker per thing that can wait on the person. A key is
// built from that thing's identity in its own store, never from a
// poller's memory, so the window and the deck (two processes) give one
// thing one key, and two sources of one thing (an ask card and the task
// that carries it) collapse into one item.

// keyRequest: a request an agent's host has open (status.Ask.ID).
func keyRequest(agent, request string) Key { return Key("req:" + agent + "/" + request) }

// keyWait: a TUI-run agent's wait. at is the hook's time: a new wait after
// an answer writes a new hook, so it gets a new key; an agent waiting by
// its screen alone (no hooks) has at zero.
func keyWait(agent string, at time.Time) Key {
	return Key("wait:" + agent + "/" + millis(at))
}

// keyMessage: a message on the bus (an @you, a DM).
func keyMessage(channel string, seq int) Key {
	return Key("msg:" + channel + "#" + strconv.Itoa(seq))
}

func keyPaused(channel string) Key { return Key("paused:" + channel) }
func keyCheckin(timer string) Key  { return Key("checkin:" + timer) }
func keySkill(slug string) Key     { return Key("skill:" + slug) }
func keyTask(id string) Key        { return Key("task:" + id) }

// keyCard: a waiting-you card of a kind the inbox does not know, so it
// shows rather than vanishes.
func keyCard(kind, channel, title string) Key {
	return Key("card:" + kind + ":" + channel + ":" + title)
}

// keyBudget: the day's budget by how it stands: going from near to spent
// is a new item, staying spent is not.
func keyBudget(day string, spent bool) Key {
	if spent {
		return Key("budget:" + day + "/spent")
	}
	return Key("budget:" + day + "/near")
}

// keyFinished: agent's turn that ended at since: its next turn finishing
// is a new item.
func keyFinished(agent string, since time.Time) Key {
	return Key("fin:" + agent + "/" + millis(since))
}

func millis(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.UnixMilli(), 10)
}

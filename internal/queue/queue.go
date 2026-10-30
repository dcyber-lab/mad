// Package queue keeps messages waiting for an agent to be free. The deck
// (the sidebar) types the first one in when the agent's turn is over, so
// you can line up the next thing to do while it works on this one.
//
// Each agent's queue is a file of its own under the state directory,
// changed under a lock: the sidebar, `mad queue` and the desktop window
// all write to it.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Item is one message waiting.
type Item struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	Added time.Time `json:"added"`
	// Bus marks a pointer to the bus (package bus): when it comes up the
	// sidebar composes a digest of what waits there and sends that; Text is
	// only what lists show.
	Bus bool `json:"bus,omitempty"`
}

// Queue is what waits for one agent.
type Queue struct {
	// Paused: nothing is sent until it is resumed.
	Paused bool   `json:"paused,omitempty"`
	Items  []Item `json:"items"`
	// Error is why the last message could not be sent, until one is.
	Error string `json:"error,omitempty"`
}

func Dir() string { return filepath.Join(paths.StateDir(), "queue") }

func path(agentID string) string { return filepath.Join(Dir(), agentID+".json") }

// Load returns agent id's queue; none is an empty one.
func Load(agentID string) (Queue, error) {
	var q Queue
	data, err := os.ReadFile(path(agentID))
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return q, err
	}
	return q, json.Unmarshal(data, &q)
}

// Update changes agent id's queue under its lock. An emptied queue that
// is not paused leaves no file.
func Update(agentID string, fn func(*Queue) error) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(path(agentID)+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	q, err := Load(agentID)
	if err != nil {
		return err
	}
	if err := fn(&q); err != nil {
		return err
	}
	if len(q.Items) == 0 && !q.Paused {
		if err := os.Remove(path(agentID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	tmp := path(agentID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(agentID))
}

// Push adds text at the end of agent id's queue.
func Push(agentID, text string, now time.Time) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, errors.New("nothing to queue")
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	it := Item{ID: hex.EncodeToString(b), Text: text, Added: now}
	return it, Update(agentID, func(q *Queue) error {
		q.Items = append(q.Items, it)
		return nil
	})
}

// PushBus adds a bus item at the end of agent id's queue unless one is
// already pending; it reports whether it did.
func PushBus(agentID, text string, now time.Time) (bool, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	it := Item{ID: hex.EncodeToString(b), Text: text, Added: now, Bus: true}
	added := false
	err := Update(agentID, func(q *Queue) error {
		for _, x := range q.Items {
			if x.Bus {
				return nil
			}
		}
		q.Items = append(q.Items, it)
		added = true
		return nil
	})
	return added, err
}

// Remove drops item itemID; one already gone is no error.
func Remove(agentID, itemID string) error {
	return Update(agentID, func(q *Queue) error {
		for i, it := range q.Items {
			if it.ID == itemID {
				q.Items = append(q.Items[:i], q.Items[i+1:]...)
				break
			}
		}
		return nil
	})
}

// Clear drops every message and the pause.
func Clear(agentID string) error {
	return Update(agentID, func(q *Queue) error {
		*q = Queue{}
		return nil
	})
}

func SetPaused(agentID string, paused bool) error {
	return Update(agentID, func(q *Queue) error {
		q.Paused = paused
		return nil
	})
}

// Take removes the first message to send it, unless the queue is paused
// or empty. Taking before sending means a crash in between loses the
// message rather than typing it in twice.
func Take(agentID string) (Item, bool, error) {
	var it Item
	var ok bool
	err := Update(agentID, func(q *Queue) error {
		if q.Paused || len(q.Items) == 0 {
			return nil
		}
		it, ok = q.Items[0], true
		q.Items = q.Items[1:]
		return nil
	})
	return it, ok, err
}

// TakeWhere is Take for the first message that fn accepts; the ones before
// it stay.
func TakeWhere(agentID string, fn func(Item) bool) (Item, bool, error) {
	var it Item
	var ok bool
	err := Update(agentID, func(q *Queue) error {
		if q.Paused {
			return nil
		}
		for i, x := range q.Items {
			if fn(x) {
				it, ok = x, true
				q.Items = append(append([]Item(nil), q.Items[:i]...), q.Items[i+1:]...)
				return nil
			}
		}
		return nil
	})
	return it, ok, err
}

// PutBack returns a message that could not be sent to the front, with why.
func PutBack(agentID string, it Item, why string) error {
	return Update(agentID, func(q *Queue) error {
		q.Items = append([]Item{it}, q.Items...)
		q.Error = why
		return nil
	})
}

// Sent clears the error of the last attempt.
func Sent(agentID string) error {
	return Update(agentID, func(q *Queue) error {
		q.Error = ""
		return nil
	})
}

// Pending lists the agents with messages to send: not paused, not empty.
func Pending() map[string]int {
	out := map[string]int{}
	all, _ := All()
	for id, q := range all {
		if !q.Paused && len(q.Items) > 0 {
			out[id] = len(q.Items)
		}
	}
	return out
}

// All reads every queue there is, by agent id.
func All() (map[string]Queue, error) {
	ents, err := os.ReadDir(Dir())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Queue{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Queue{}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if q, err := Load(id); err == nil {
			out[id] = q
		}
	}
	return out, nil
}

// Forget removes what is kept for an agent that is gone.
func Forget(agentID string) {
	_ = os.Remove(path(agentID))
	_ = os.Remove(path(agentID) + ".lock")
}

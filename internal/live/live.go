// Package live follows what agents' models write as they write it: the
// text, thinking and tool calls of the message in progress, token by
// token. A transcript has each part once it is finished; this is the part
// before. One process in the deck (mad stream) keeps it, fed by a tap per
// kind (claude: a proxy in front of the API, see anthropic.go), and the
// desktop window asks it for each agent's latest message.
package live

import (
	"context"
	"sync"
)

// Block is one part of a message: text, thinking, or a tool call.
type Block struct {
	Kind string `json:"kind"` // text, thinking, tool; other kinds as the API names them
	// Text is what came so far: the text, the thinking, a call's input
	// (JSON, cut off where the stream is).
	Text string `json:"text,omitempty"`
	Tool string `json:"tool,omitempty"` // the tool a call is to
	ID   string `json:"id,omitempty"`   // a call's id
	Done bool   `json:"done,omitempty"`
}

// Message is what an agent's model is writing, or wrote last.
type Message struct {
	ID     string  `json:"id"` // the API's; the transcript has it too
	Blocks []Block `json:"blocks"`
	// Session is the agent's session (thread) as its calls name it: one
	// codex names itself is known here before the deck learns it.
	Session string `json:"session,omitempty"`
	Done    bool   `json:"done,omitempty"`
	// Version orders the changes: a look with an older one gets this.
	Version int64 `json:"version"`
}

// Hub keeps each agent's latest message and wakes those waiting for it.
type Hub struct {
	mu   sync.Mutex
	msgs map[string]*Message // by agent id
	ver  int64
	wake chan struct{} // closed, and replaced, on every change
}

func NewHub() *Hub {
	return &Hub{msgs: map[string]*Message{}, wake: make(chan struct{})}
}

// change runs fn on agent's message, if it is message id, and wakes
// whoever waits.
func (h *Hub) change(agent, id string, fn func(m *Message)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.msgs[agent]
	if m == nil || m.ID != id {
		return // another message started since
	}
	fn(m)
	h.ver++
	m.Version = h.ver
	close(h.wake)
	h.wake = make(chan struct{})
}

// Begin starts agent's message id in session, in place of the one before.
func (h *Hub) Begin(agent, id, session string) {
	h.mu.Lock()
	h.msgs[agent] = &Message{ID: id, Blocks: []Block{}, Session: session}
	h.mu.Unlock()
	h.change(agent, id, func(*Message) {})
}

// Block changes block i of agent's message id.
func (h *Hub) Block(agent, id string, i int, fn func(b *Block)) {
	if i < 0 || i > 1000 {
		return
	}
	h.change(agent, id, func(m *Message) {
		for len(m.Blocks) <= i {
			m.Blocks = append(m.Blocks, Block{})
		}
		fn(&m.Blocks[i])
	})
}

// End marks agent's message id written, every block of it.
func (h *Hub) End(agent, id string) {
	h.change(agent, id, func(m *Message) {
		m.Done = true
		for i := range m.Blocks {
			m.Blocks[i].Done = true
		}
	})
}

// Wait returns agent's message once it changed after version since, or
// as it is when ctx ends.
func (h *Hub) Wait(ctx context.Context, agent string, since int64) Message {
	for {
		h.mu.Lock()
		var out Message
		if m := h.msgs[agent]; m != nil {
			out = *m
			out.Blocks = append([]Block(nil), m.Blocks...)
		}
		wake := h.wake
		h.mu.Unlock()
		if out.Version > since {
			return out
		}
		select {
		case <-ctx.Done():
			return out
		case <-wake:
		}
	}
}

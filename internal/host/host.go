// Package host runs an agent headless for the desktop window to draw.
// `mad host <agent>` runs in the agent's pane in place of its TUI (window
// mode, deck.Windowed) and drives the agent through its structured
// protocol (claude: stream-json, as the Agent SDK does). It serves what
// the agent is doing, and what it asks the person, on a unix socket
// (paths.HostSocket): newline-delimited JSON, a State each time it
// changes; the window sends Commands back. Its pane shows the same,
// plainly, with a key to take the agent back to its TUI.
package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/live"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// What the agent is doing (State.Status).
const (
	Starting = "starting"
	Idle     = "idle"
	Running  = "running"
	Waiting  = "waiting" // a Request is open
	Exited   = "exited"
)

// State is what the window draws.
type State struct {
	Status string  `json:"status"`
	Model  string  `json:"model,omitempty"`
	Models []Model `json:"models,omitempty"`
	// Permission is the mode it asks in, one of Permissions.
	Permission  string  `json:"permission,omitempty"`
	Permissions []Model `json:"permissions,omitempty"`
	// Message is the one being written, or written last (as package live
	// has it from a tap).
	Message  *live.Message `json:"message,omitempty"`
	Requests []Request     `json:"requests"`
	Session  string        `json:"session,omitempty"`
	Error    string        `json:"error,omitempty"`
	Version  int64         `json:"version"`
}

// Model is one the agent can switch to.
type Model struct {
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Request is something the agent asks the person: whether it may use a
// tool, a question with options, or a plan to approve.
type Request struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // tool, question, plan
	Tool  string `json:"tool,omitempty"`
	Title string `json:"title,omitempty"`
	// Label and Input say what the call would do, as the conversation
	// shows calls (discover.ToolText); Args is its input, as the
	// conversation's entries have it (discover.ToolArgs).
	Label     string          `json:"label,omitempty"`
	Input     string          `json:"input,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	Questions []Question      `json:"questions,omitempty"`
	Plan      string          `json:"plan,omitempty"`
	// Always: it can be allowed from now on, not just this once.
	Always bool `json:"always,omitempty"`
}

type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header,omitempty"`
	Options  []Option `json:"options"`
	Multi    bool     `json:"multiSelect,omitempty"`
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Answer is the person's to Request ID.
type Answer struct {
	ID     string `json:"id"`
	Allow  bool   `json:"allow"`
	Always bool   `json:"always,omitempty"`
	// Message: why not, or what to do instead.
	Message string `json:"message,omitempty"`
	// Answers to a question's questions, by question: the label chosen,
	// labels joined by ", " for several, or what was written.
	Answers map[string]string `json:"answers,omitempty"`
}

// Image is a picture sent with a message.
type Image struct {
	Mime string `json:"mime"` // image/png, ...
	Data string `json:"data"` // base64, without a data: prefix
}

// The most pictures a message takes, and the biggest, decoded.
const (
	maxImages    = 8
	maxImageSize = 10 << 20
)

// checkImages says why images cannot be sent, if they cannot.
func checkImages(images []Image) error {
	if len(images) > maxImages {
		return fmt.Errorf("at most %d images in a message", maxImages)
	}
	for _, im := range images {
		if !strings.HasPrefix(im.Mime, "image/") {
			return fmt.Errorf("%q is not an image type", im.Mime)
		}
		// Decoded size, from the encoded: three bytes to four characters.
		if len(im.Data)/4*3 > maxImageSize {
			return errors.New("an image is over 10 MB")
		}
	}
	return nil
}

// Command is what the window asks the host to do.
type Command struct {
	Type   string  `json:"type"` // send, answer, model, permission, interrupt
	Text   string  `json:"text,omitempty"`
	Images []Image `json:"images,omitempty"` // with a send
	Answer *Answer `json:"answer,omitempty"`
	Model  string  `json:"model,omitempty"`
	Mode   string  `json:"mode,omitempty"` // a permission mode
}

// Line is one line the host sends: a state, or how a command went.
type Line struct {
	Type  string `json:"type"` // state, done, error
	State *State `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
}

// driver speaks one kind's protocol.
type driver interface {
	// run starts the agent and reads it until it ends.
	run(ctx context.Context) error
	send(text string, images []Image) error
	answer(a Answer) error
	setModel(model string) error
	setPermission(mode string) error
	interrupt() error
}

// Host keeps the agent's State and the windows looking at it.
type Host struct {
	agent string
	d     driver
	hub   *live.Hub // assembles the message being written

	mu      sync.Mutex
	st      State
	clients map[chan []byte]bool
	redraw  chan struct{}
	replied string // id of the message whose turn was last written
}

func newHost(id string) *Host {
	return &Host{
		agent:   id,
		hub:     live.NewHub(),
		st:      State{Status: Starting, Requests: []Request{}},
		clients: map[chan []byte]bool{},
		redraw:  make(chan struct{}, 1),
	}
}

// Run hosts agent id until it ends.
func Run(ctx context.Context, id string, kinds []agent.Kind) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	p, a := st.FindAgent(id)
	if a == nil {
		return fmt.Errorf("unknown agent %s", id)
	}
	if !deck.Headless[a.Kind] {
		return fmt.Errorf("%s agents do not run headless", a.Kind)
	}
	h := newHost(id)
	switch a.Kind {
	case "codex":
		h.d = newCodex(h, a, p.Dir(a))
	case "pi":
		h.d = newPi(h, deck.TUICommand(p, a, true, kinds), p.Dir(a))
	default:
		h.d = newClaude(h, deck.TUICommand(p, a, true, kinds), p.Dir(a))
	}

	sock := paths.HostSocket(id)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer os.Remove(sock)
	defer ln.Close()
	_ = os.Chmod(sock, 0o600)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go h.serve(ln)
	go h.heartbeat(ctx)
	go view(ctx, h, a, kinds)
	err = h.d.run(ctx)
	h.update(func(s *State) {
		s.Status = Exited
		if err != nil {
			s.Error = err.Error()
		}
	})
	return err
}

// update changes the state and tells everyone looking.
func (h *Host) update(fn func(s *State)) {
	h.mu.Lock()
	was := h.st.Status
	fn(&h.st)
	h.st.Version++
	now := h.st.Status
	data, _ := json.Marshal(Line{Type: "state", State: &h.st})
	for c := range h.clients {
		// Each holds the latest state only: one behind gets that.
		select {
		case <-c:
		default:
		}
		c <- data
	}
	h.mu.Unlock()
	if now != was {
		h.report(now)
	}
	select {
	case h.redraw <- struct{}{}:
	default:
	}
}

// showMessage puts the message being written, as the hub has it, in the
// state; one being written means a turn under way.
func (h *Host) showMessage() {
	msg := h.hub.Wait(context.Background(), h.agent, -1)
	if msg.ID == "" {
		return // none begun yet
	}
	h.update(func(s *State) {
		s.Message = &msg
		if !msg.Done && (s.Status == Idle || s.Status == Starting) {
			s.Status = Running
		}
	})
}

// State returns a copy of the state.
func (h *Host) State() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.st
	s.Requests = append([]Request(nil), h.st.Requests...)
	return s
}

// report tells the deck, through the agent's hook report, as its hooks
// would: the deck works out its status from that.
func (h *Host) report(s string) {
	hook := status.Idle
	switch s {
	case Running, Starting:
		hook = status.Running
	case Waiting:
		hook = status.Waiting
	}
	if hook == status.Idle {
		h.writeTurn() // before the Idle report: the deck reads the reply when it sees the turn end
	}
	_ = status.WriteHook(h.agent, &status.Hook{State: hook, Event: "host"}, time.Now())
}

// writeTurn records what the agent said last, as its hook would at the
// end of a turn, for the deck's notification.
func (h *Host) writeTurn() {
	h.mu.Lock()
	msg := h.st.Message
	if msg == nil || msg.ID == h.replied {
		h.mu.Unlock()
		return // no new message since the last turn
	}
	h.replied = msg.ID
	var parts []string
	for _, b := range msg.Blocks {
		if b.Kind == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	h.mu.Unlock()
	if len(parts) > 0 {
		_ = status.WriteTurn(h.agent, &status.Turn{Reply: strings.Join(parts, "\n\n")}, time.Now())
	}
}

// heartbeat keeps a running report fresh: the deck takes one that is not
// for a while, with a still screen, for a turn cut short.
func (h *Host) heartbeat(ctx context.Context) {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s := h.State().Status; s == Running {
				h.report(s)
			}
		}
	}
}

func (h *Host) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go h.client(conn)
	}
}

// client sends conn each state, and does what it asks.
func (h *Host) client(conn net.Conn) {
	defer conn.Close()
	out := make(chan []byte, 1)
	h.mu.Lock()
	data, _ := json.Marshal(Line{Type: "state", State: &h.st})
	out <- data
	h.clients[out] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, out)
		h.mu.Unlock()
	}()

	var wmu sync.Mutex
	write := func(b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_, err := conn.Write(append(b, '\n'))
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case b := <-out:
				if write(b) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	sc := bufio.NewScanner(conn)
	// Room for 8 images of 10 MB, base64.
	sc.Buffer(make([]byte, 64<<10), 128<<20)
	for sc.Scan() {
		var c Command
		err := json.Unmarshal(sc.Bytes(), &c)
		if err == nil {
			err = h.do(c)
		}
		reply := Line{Type: "done"}
		if err != nil {
			reply = Line{Type: "error", Error: err.Error()}
		}
		b, _ := json.Marshal(reply)
		_ = write(b)
	}
}

// do carries out c.
func (h *Host) do(c Command) error {
	switch c.Type {
	case "send":
		if strings.TrimSpace(c.Text) == "" && len(c.Images) == 0 {
			return errors.New("nothing to send")
		}
		if err := checkImages(c.Images); err != nil {
			return err
		}
		return h.d.send(c.Text, c.Images)
	case "answer":
		if c.Answer == nil {
			return errors.New("no answer")
		}
		return h.d.answer(*c.Answer)
	case "model":
		if c.Model == "" {
			return errors.New("no model")
		}
		return h.d.setModel(c.Model)
	case "permission":
		if c.Mode == "" {
			return errors.New("no mode")
		}
		return h.d.setPermission(c.Mode)
	case "interrupt":
		return h.d.interrupt()
	}
	return fmt.Errorf("unknown command %q", c.Type)
}

// agentCmd runs the agent, name with args, in dir: in a session of its
// own, so that it and whatever it starts are ended together when ctx is
// done, though a shell need not exec what it runs; killed if they
// have not ended a moment after. Without a terminal, a command of
// its that asks for one fails instead of stopping for a key; the host
// ends it on the signals that end the pane.
func agentCmd(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		group := -cmd.Process.Pid
		time.AfterFunc(3*time.Second, func() { _ = syscall.Kill(group, syscall.SIGKILL) })
		return syscall.Kill(group, syscall.SIGTERM)
	}
	return cmd
}

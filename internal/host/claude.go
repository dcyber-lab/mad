package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/live"
)

// claude runs a headless claude as the Agent SDK does: its own command
// line (the deck's, with mad's settings and the session to resume), in
// print mode, stream-json both ways. It asks before using a tool through
// a control request (--permission-prompt-tool stdio), questions
// (AskUserQuestion) and plans (ExitPlanMode) included, and streams what it
// writes as the Messages API's events (--include-partial-messages).
type claude struct {
	h   *Host
	cmd string // its TUI's command line
	dir string

	wmu     sync.Mutex    // one write at a time; the reader never waits on it
	started chan struct{} // closed once in is set
	mu      sync.Mutex
	in      io.Writer
	asks    map[string]claudeAsk // open permission requests, by request id
	seq     int
	stream  *live.Stream
}

// claudePermissions are the modes it can ask in.
var claudePermissions = []Model{
	{Value: "default", Name: "Ask before edits", Description: "Asks before it edits or runs anything"},
	{Value: "acceptEdits", Name: "Accept edits", Description: "Edits files without asking"},
	{Value: "plan", Name: "Plan only", Description: "Plans, and changes nothing until you approve"},
	{Value: "bypassPermissions", Name: "Don't ask", Description: "Does everything without asking"},
}

type claudeAsk struct {
	tool        string
	input       json.RawMessage
	suggestions json.RawMessage
}

// claudeHeadless turns claude's command line headless.
const claudeHeadless = " -p --verbose --input-format stream-json --output-format stream-json" +
	" --include-partial-messages --permission-prompt-tool stdio"

func newClaude(h *Host, cmd, dir string) *claude {
	return &claude{h: h, cmd: cmd, dir: dir, asks: map[string]claudeAsk{}, started: make(chan struct{})}
}

func (c *claude) run(ctx context.Context) error {
	cmd := agentCmd(ctx, c.dir, "sh", "-c", c.cmd+claudeHeadless)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errs tail
	cmd.Stderr = &errs
	if err := cmd.Start(); err != nil {
		return err
	}
	c.mu.Lock()
	c.in = in
	c.mu.Unlock()
	close(c.started)
	// Its models, among what it says to an SDK starting up.
	_ = c.write(map[string]any{"type": "control_request", "request_id": "init",
		"request": map[string]any{"subtype": "initialize"}})

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		c.line(sc.Bytes())
	}
	err = cmd.Wait()
	if err != nil && errs.String() != "" {
		err = fmt.Errorf("%w: %s", err, errs.String())
	}
	return err
}

// line takes one line claude printed.
func (c *claude) line(data []byte) {
	var m struct {
		Type      string          `json:"type"`
		Subtype   string          `json:"subtype"`
		SessionID string          `json:"session_id"`
		Model     string          `json:"model"`
		Mode      string          `json:"permissionMode"`
		Parent    *string         `json:"parent_tool_use_id"`
		Event     json.RawMessage `json:"event"`
		RequestID string          `json:"request_id"`
		Request   json.RawMessage `json:"request"`
		Response  struct {
			Subtype   string          `json:"subtype"`
			RequestID string          `json:"request_id"`
			Error     string          `json:"error"`
			Response  json.RawMessage `json:"response"`
		} `json:"response"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch m.Type {
	case "system":
		if m.Subtype == "init" {
			c.mu.Lock()
			c.stream = live.NewStream(c.h.hub, c.h.agent, m.SessionID)
			c.mu.Unlock()
			c.h.update(func(s *State) {
				s.Session, s.Model = m.SessionID, m.Model
				if m.Mode != "" {
					s.Permission = m.Mode
				}
				if s.Status == Starting {
					s.Status = Idle
				}
			})
		}
	case "stream_event":
		if m.Parent != nil && *m.Parent != "" {
			return // a subagent's
		}
		c.mu.Lock()
		stream := c.stream
		c.mu.Unlock()
		if stream == nil {
			return
		}
		stream.Event(m.Event)
		c.h.showMessage()
	case "control_request":
		c.ask(m.RequestID, m.Request)
	case "control_cancel_request":
		c.forget(m.RequestID)
	case "control_response":
		if m.Response.RequestID == "init" && m.Response.Subtype == "success" {
			var r struct {
				Models []struct {
					Value       string `json:"value"`
					DisplayName string `json:"displayName"`
					Description string `json:"description"`
				} `json:"models"`
				Mode string `json:"current_permission_mode"`
			}
			_ = json.Unmarshal(m.Response.Response, &r)
			var models []Model
			for _, x := range r.Models {
				models = append(models, Model{Value: x.Value, Name: x.DisplayName, Description: x.Description})
			}
			// Ready: it says who it is (system init) only once given a
			// message.
			c.h.update(func(s *State) {
				s.Models, s.Permissions = models, claudePermissions
				if r.Mode != "" {
					s.Permission = r.Mode
				}
				if s.Status == Starting {
					s.Status = Idle
				}
			})
		}
	case "result":
		c.mu.Lock()
		open := len(c.asks)
		c.mu.Unlock()
		c.h.update(func(s *State) {
			if s.Message != nil {
				s.Message.Done = true
			}
			if open == 0 {
				s.Status = Idle
			}
			s.Error = ""
			if m.IsError {
				s.Error = m.Result
			}
		})
	}
}

// ask turns a control request into a Request for the person, or answers
// it as unsupported.
func (c *claude) ask(id string, raw json.RawMessage) {
	var r struct {
		Subtype     string          `json:"subtype"`
		Tool        string          `json:"tool_name"`
		Input       json.RawMessage `json:"input"`
		Suggestions json.RawMessage `json:"permission_suggestions"`
		Title       string          `json:"title"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.Subtype != "can_use_tool" {
		_ = c.write(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": id, "error": "not supported by mad host: " + r.Subtype}})
		return
	}
	req := Request{ID: id, Kind: "tool", Tool: r.Tool, Title: r.Title}
	switch r.Tool {
	case "AskUserQuestion":
		var in struct {
			Questions []Question `json:"questions"`
		}
		_ = json.Unmarshal(r.Input, &in)
		req.Kind, req.Questions = "question", in.Questions
	case "ExitPlanMode":
		var in struct {
			Plan string `json:"plan"`
		}
		_ = json.Unmarshal(r.Input, &in)
		req.Kind, req.Plan = "plan", in.Plan
	default:
		req.Label, req.Input = discover.ToolText(r.Tool, r.Input)
		req.Args = discover.ToolArgs(r.Input)
		var sugg []json.RawMessage
		req.Always = json.Unmarshal(r.Suggestions, &sugg) == nil && len(sugg) > 0
	}
	c.mu.Lock()
	c.asks[id] = claudeAsk{tool: r.Tool, input: r.Input, suggestions: r.Suggestions}
	c.mu.Unlock()
	c.h.update(func(s *State) {
		s.Requests = append(s.Requests, req)
		s.Status = Waiting
	})
}

// forget drops request id, answered or withdrawn.
func (c *claude) forget(id string) {
	c.mu.Lock()
	delete(c.asks, id)
	c.mu.Unlock()
	c.unlist(id)
}

// unlist takes request id off the state, and the agent off waiting when it
// was the last.
func (c *claude) unlist(id string) {
	c.mu.Lock()
	open := len(c.asks)
	c.mu.Unlock()
	c.h.update(func(s *State) {
		kept := s.Requests[:0]
		for _, r := range s.Requests {
			if r.ID != id {
				kept = append(kept, r)
			}
		}
		s.Requests = kept
		if open == 0 && s.Status == Waiting {
			s.Status = Running
		}
	})
}

func (c *claude) answer(a Answer) error {
	c.mu.Lock()
	q, ok := c.asks[a.ID]
	// Answered from here on: claude may end the turn the moment it has the
	// answer, and that end must not find the request still open.
	delete(c.asks, a.ID)
	c.mu.Unlock()
	if !ok {
		return errors.New("that was answered already")
	}
	var resp map[string]any
	switch {
	case a.Allow && q.tool == "AskUserQuestion":
		var in map[string]any
		_ = json.Unmarshal(q.input, &in)
		if in == nil {
			in = map[string]any{}
		}
		in["answers"] = a.Answers
		resp = map[string]any{"behavior": "allow", "updatedInput": in}
	case a.Allow:
		resp = map[string]any{"behavior": "allow", "updatedInput": q.input}
		if a.Always && len(q.suggestions) > 0 && string(q.suggestions) != "null" {
			resp["updatedPermissions"] = q.suggestions
		}
	default:
		msg := a.Message
		if msg == "" {
			msg = "The user doesn't want to proceed with this tool use."
		}
		resp = map[string]any{"behavior": "deny", "message": msg}
	}
	err := c.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": a.ID, "response": resp}})
	if err != nil {
		c.mu.Lock()
		c.asks[a.ID] = q
		c.mu.Unlock()
		return err
	}
	c.unlist(a.ID)
	return nil
}

// send gives claude a message: its text, as a string, or with images as
// content blocks.
func (c *claude) send(text string, images []Image) error {
	var content any = text
	if len(images) > 0 {
		var blocks []map[string]any
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		for _, im := range images {
			blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": im.Mime, "data": im.Data}})
		}
		content = blocks
	}
	err := c.write(map[string]any{"type": "user", "parent_tool_use_id": nil,
		"message": map[string]any{"role": "user", "content": content}})
	if err == nil {
		c.h.update(func(s *State) {
			if s.Status != Waiting {
				s.Status = Running
			}
		})
	}
	return err
}

func (c *claude) setModel(model string) error {
	err := c.control(map[string]any{"subtype": "set_model", "model": model})
	if err == nil {
		c.h.update(func(s *State) { s.Model = model })
	}
	return err
}

func (c *claude) setPermission(mode string) error {
	err := c.control(map[string]any{"subtype": "set_permission_mode", "mode": mode})
	if err == nil {
		c.h.update(func(s *State) { s.Permission = mode })
	}
	return err
}

func (c *claude) interrupt() error {
	return c.control(map[string]any{"subtype": "interrupt"})
}

// control sends claude a control request; its answer is not waited for.
func (c *claude) control(req map[string]any) error {
	c.mu.Lock()
	c.seq++
	id := "mad-" + strconv.Itoa(c.seq)
	c.mu.Unlock()
	return c.write(map[string]any{"type": "control_request", "request_id": id, "request": req})
}

func (c *claude) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case <-c.started:
	case <-time.After(10 * time.Second):
		return errors.New("claude has not started")
	}
	c.mu.Lock()
	in := c.in
	c.mu.Unlock()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = in.Write(append(data, '\n'))
	return err
}

// tail keeps the end of what was written to it: the last thing a process
// said before it failed.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

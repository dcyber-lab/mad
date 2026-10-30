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

	"github.com/dcyber-lab/mad/internal/live"
)

// pi runs pi in RPC mode: its own command line (the session it keeps,
// mad's extension), JSON lines both ways. It streams what it writes as
// message updates; what its extensions ask the person (select, confirm,
// input) comes as UI requests to answer.
type pi struct {
	h   *Host
	cmd string
	dir string

	started chan struct{}
	mu      sync.Mutex
	in      io.Writer
	seq     int
	msg     string               // the message being written: its timestamp, as its session file has it
	asks    map[string]piAsk     // extension UI requests, by id
	models  map[string][2]string // provider and id, by model value
}

type piAsk struct{ method string }

func newPi(h *Host, cmd, dir string) *pi {
	return &pi{h: h, cmd: cmd, dir: dir, started: make(chan struct{}), asks: map[string]piAsk{}, models: map[string][2]string{}}
}

func (p *pi) run(ctx context.Context) error {
	cmd := agentCmd(ctx, p.dir, "sh", "-c", p.cmd+" --mode rpc")
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
	p.mu.Lock()
	p.in = in
	p.mu.Unlock()
	close(p.started)
	_ = p.write(map[string]any{"id": "state", "type": "get_state"})
	_ = p.write(map[string]any{"id": "models", "type": "get_available_models"})
	// LF only: a JSON string may hold other line separators.
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		p.line(sc.Bytes())
	}
	err = cmd.Wait()
	if err != nil && errs.String() != "" {
		err = fmt.Errorf("%w: %s", err, errs.String())
	}
	return err
}

type piModel struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

func (m piModel) value() string { return m.Provider + "/" + m.ID }

func (p *pi) line(data []byte) {
	var m struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		Command string          `json:"command"`
		Success bool            `json:"success"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
		// An agent message for message events, a string for an
		// extension's confirm request.
		Message json.RawMessage `json:"message"`
		Event   struct {
			Type     string `json:"type"`
			Index    int    `json:"contentIndex"`
			Delta    string `json:"delta"`
			ToolCall struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
				ID        string          `json:"id"`
			} `json:"toolCall"`
		} `json:"assistantMessageEvent"`
		Method      string   `json:"method"`
		Title       string   `json:"title"`
		Options     []string `json:"options"`
		Placeholder string   `json:"placeholder"`
		Prefill     string   `json:"prefill"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	var msg struct {
		Role      string `json:"role"`
		Timestamp int64  `json:"timestamp"`
	}
	_ = json.Unmarshal(m.Message, &msg)
	switch m.Type {
	case "response":
		p.response(m.ID, m.Command, m.Success, m.Error, m.Data)
	case "agent_start":
		p.h.update(func(s *State) {
			if s.Status != Waiting {
				s.Status = Running
			}
		})
	case "agent_settled":
		p.mu.Lock()
		open := len(p.asks)
		p.mu.Unlock()
		p.h.update(func(s *State) {
			if open == 0 {
				s.Status = Idle
			}
		})
	case "message_start":
		if msg.Role != "assistant" {
			return
		}
		id := strconv.FormatInt(msg.Timestamp, 10)
		p.mu.Lock()
		p.msg = id
		p.mu.Unlock()
		p.h.hub.Begin(p.h.agent, id, p.h.State().Session)
		p.h.showMessage()
	case "message_update":
		p.mu.Lock()
		id := p.msg
		p.mu.Unlock()
		if id == "" {
			return
		}
		e := m.Event
		switch e.Type {
		case "text_start", "thinking_start", "toolcall_start":
			kind := map[string]string{"text_start": "text", "thinking_start": "thinking", "toolcall_start": "tool"}[e.Type]
			p.h.hub.Block(p.h.agent, id, e.Index, func(b *live.Block) { *b = live.Block{Kind: kind} })
		case "text_delta", "thinking_delta", "toolcall_delta":
			p.h.hub.Block(p.h.agent, id, e.Index, func(b *live.Block) { b.Text += e.Delta })
		case "text_end", "thinking_end":
			p.h.hub.Block(p.h.agent, id, e.Index, func(b *live.Block) { b.Done = true })
		case "toolcall_end":
			p.h.hub.Block(p.h.agent, id, e.Index, func(b *live.Block) {
				b.Tool, b.ID, b.Done = e.ToolCall.Name, e.ToolCall.ID, true
				if len(e.ToolCall.Arguments) > 0 {
					b.Text = string(e.ToolCall.Arguments)
				}
			})
		case "done", "error":
			p.h.hub.End(p.h.agent, id)
		default:
			return
		}
		p.h.showMessage()
	case "message_end":
		p.mu.Lock()
		id := p.msg
		p.mu.Unlock()
		if msg.Role == "assistant" && id != "" {
			p.h.hub.End(p.h.agent, id)
			p.h.showMessage()
		}
	case "extension_ui_request":
		var text string
		_ = json.Unmarshal(m.Message, &text)
		p.ask(m.ID, m.Method, m.Title, text, m.Options, m.Placeholder+m.Prefill)
	}
}

func (p *pi) response(id, command string, ok bool, msg string, data json.RawMessage) {
	if !ok {
		if msg != "" {
			p.h.update(func(s *State) { s.Error = command + ": " + msg })
		}
		return
	}
	switch id {
	case "state":
		var st struct {
			Model     piModel `json:"model"`
			SessionID string  `json:"sessionId"`
		}
		_ = json.Unmarshal(data, &st)
		p.h.update(func(s *State) {
			s.Model, s.Session = st.Model.value(), st.SessionID
			if st.Model.ID == "" {
				s.Model = ""
			}
			if s.Status == Starting {
				s.Status = Idle
			}
		})
	case "models":
		var list []piModel
		if json.Unmarshal(data, &list) != nil {
			var wrapped struct {
				Models []piModel `json:"models"`
			}
			_ = json.Unmarshal(data, &wrapped)
			list = wrapped.Models
		}
		var models []Model
		p.mu.Lock()
		for _, m := range list {
			p.models[m.value()] = [2]string{m.Provider, m.ID}
			models = append(models, Model{Value: m.value(), Name: m.Name, Description: m.Provider})
		}
		p.mu.Unlock()
		p.h.update(func(s *State) { s.Models = models })
	}
}

// ask turns an extension's UI request into a Request; ones that ask
// nothing (notify, status, widgets) are left be.
func (p *pi) ask(id, method, title, text string, options []string, placeholder string) {
	req := Request{ID: id, Title: title}
	switch method {
	case "confirm":
		req.Kind, req.Tool, req.Input = "tool", "confirm", text
	case "select":
		var opts []Option
		for _, o := range options {
			opts = append(opts, Option{Label: o})
		}
		req.Kind, req.Questions = "question", []Question{{Question: title, Options: opts}}
	case "input", "editor":
		q := title
		if placeholder != "" {
			q += " (" + strings.TrimSpace(placeholder) + ")"
		}
		req.Kind, req.Questions = "question", []Question{{Question: q}}
	default:
		return
	}
	p.mu.Lock()
	p.asks[id] = piAsk{method: method}
	p.mu.Unlock()
	p.h.update(func(s *State) {
		s.Requests = append(s.Requests, req)
		s.Status = Waiting
	})
}

func (p *pi) answer(a Answer) error {
	p.mu.Lock()
	q, ok := p.asks[a.ID]
	delete(p.asks, a.ID)
	open := len(p.asks)
	p.mu.Unlock()
	if !ok {
		return errors.New("that was answered already")
	}
	resp := map[string]any{"type": "extension_ui_response", "id": a.ID}
	switch {
	case q.method == "confirm":
		resp["confirmed"] = a.Allow
	case !a.Allow:
		resp["cancelled"] = true
	default:
		for _, v := range a.Answers {
			resp["value"] = v
		}
	}
	if err := p.write(resp); err != nil {
		return err
	}
	p.h.update(func(s *State) {
		kept := s.Requests[:0]
		for _, r := range s.Requests {
			if r.ID != a.ID {
				kept = append(kept, r)
			}
		}
		s.Requests = kept
		if open == 0 && s.Status == Waiting {
			s.Status = Running
		}
	})
	return nil
}

func (p *pi) send(text string, images []Image) error {
	cmd := map[string]any{"type": "prompt", "message": text}
	if len(images) > 0 {
		var list []map[string]any
		for _, im := range images {
			list = append(list, map[string]any{"type": "image", "data": im.Data, "mimeType": im.Mime})
		}
		cmd["images"] = list
	}
	if p.h.State().Status == Running {
		cmd["streamingBehavior"] = "steer"
	}
	err := p.write(cmd)
	if err == nil {
		p.h.update(func(s *State) {
			if s.Status != Waiting {
				s.Status = Running
			}
		})
	}
	return err
}

func (p *pi) setModel(model string) error {
	p.mu.Lock()
	m, ok := p.models[model]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("no model %s", model)
	}
	err := p.write(map[string]any{"type": "set_model", "provider": m[0], "modelId": m[1]})
	if err == nil {
		p.h.update(func(s *State) { s.Model = model })
	}
	return err
}

func (p *pi) setPermission(string) error {
	return errors.New("pi has no permission modes")
}

func (p *pi) interrupt() error {
	return p.write(map[string]any{"type": "abort"})
}

func (p *pi) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case <-p.started:
	case <-time.After(10 * time.Second):
		return errors.New("pi has not started")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	_, err = p.in.Write(append(data, '\n'))
	return err
}

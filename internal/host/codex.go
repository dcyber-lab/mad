package host

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/live"
	"github.com/dcyber-lab/mad/internal/state"
)

// codex runs `codex app-server`, the JSON-RPC server codex's own desktop
// app and editor extensions drive: a thread (the agent's session, resumed
// when it has one), a turn per message, its items streamed as they come,
// and approvals and questions asked as requests to the client.
type codex struct {
	h     *Host
	agent string
	dir   string
	sid   string   // the thread to resume
	model string   // from the agent's arguments, or picked since
	mode  string   // the approval policy picked since, for its next turns
	files []string // images written for the person's messages

	wmu     sync.Mutex // one write at a time; the reader never waits on it
	started chan struct{}
	ready   chan struct{} // closed once its thread is open, or failed to
	mu      sync.Mutex
	in      io.Writer
	seq     int
	waiting map[string]chan codexReply // our requests, by id
	thread  string
	turn    string                // the one in progress
	items   map[string]int        // its items' blocks, by item id
	changes map[string]string     // file changes, by item id, for approving them
	asks    map[string]codexAsk   // the server's requests, by id (as JSON)
	quest   map[string]codexQuest // a question request's question ids, by text
}

type codexReply struct {
	result json.RawMessage
	err    error
}

type codexAsk struct {
	method string
	id     json.RawMessage
	params json.RawMessage
}

type codexQuest map[string]string // question → id

func newCodex(h *Host, a *state.Agent, dir string) *codex {
	c := &codex{h: h, agent: a.ID, dir: dir, sid: a.SessionID, started: make(chan struct{}), ready: make(chan struct{}),
		waiting: map[string]chan codexReply{}, items: map[string]int{}, changes: map[string]string{},
		asks: map[string]codexAsk{}, quest: map[string]codexQuest{}}
	for i, arg := range a.Args {
		if (arg == "-m" || arg == "--model") && i+1 < len(a.Args) {
			c.model = a.Args[i+1]
		}
	}
	return c
}

// codexPermissions are the approval policies it can run under.
var codexPermissions = []Model{
	{Value: "untrusted", Name: "Ask for everything", Description: "Asks before it runs anything but what it knows is safe"},
	{Value: "on-request", Name: "Ask when it needs to", Description: "Decides itself when to ask"},
	{Value: "never", Name: "Don't ask", Description: "Never asks, and carries on from failures"},
}

// codexBin is the codex executable; tests run a fake.
var codexBin = "codex"

func (c *codex) run(ctx context.Context) error {
	defer c.forgetImages()
	// With mad's MCP server, as its TUI starts with it (deck.MCPFlags).
	cmd := agentCmd(ctx, c.dir, codexBin, append([]string{"app-server"}, deck.CodexMCPArgs(c.agent)...)...)
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
	read := make(chan struct{})
	go func() {
		defer close(read)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 64<<20)
		for sc.Scan() {
			c.line(sc.Bytes())
		}
	}()
	if err := c.open(); err != nil {
		c.h.update(func(s *State) { s.Error = err.Error() })
	}
	close(c.ready)
	<-read
	err = cmd.Wait()
	if err != nil && errs.String() != "" {
		err = fmt.Errorf("%w: %s", err, errs.String())
	}
	return err
}

// open says hello, lists the models, and resumes the agent's thread, or
// starts one.
func (c *codex) open() error {
	if _, err := c.request("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "mad", "title": "mad", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return err
	}
	if err := c.write(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	if res, err := c.request("model/list", map[string]any{}); err == nil {
		var r struct {
			Data []struct {
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
				Hidden      bool   `json:"hidden"`
			} `json:"data"`
		}
		_ = json.Unmarshal(res, &r)
		var models []Model
		for _, m := range r.Data {
			if !m.Hidden {
				models = append(models, Model{Value: m.Model, Name: m.DisplayName, Description: m.Description})
			}
		}
		c.h.update(func(s *State) { s.Models = models })
	}
	params := map[string]any{"cwd": c.dir}
	if c.model != "" {
		params["model"] = c.model
	}
	var res json.RawMessage
	err := errors.New("no thread")
	if c.sid != "" {
		res, err = c.request("thread/resume", map[string]any{"threadId": c.sid, "cwd": c.dir, "model": nilIfEmpty(c.model)})
	}
	if err != nil {
		if res, err = c.request("thread/start", params); err != nil {
			return err
		}
	}
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model  string          `json:"model"`
		Policy json.RawMessage `json:"approvalPolicy"`
	}
	_ = json.Unmarshal(res, &r)
	var policy string // an object is not one of the modes
	_ = json.Unmarshal(r.Policy, &policy)
	c.mu.Lock()
	c.thread = r.Thread.ID
	c.mu.Unlock()
	keepSession(c.agent, r.Thread.ID)
	c.h.update(func(s *State) {
		s.Session, s.Model = r.Thread.ID, r.Model
		s.Permission, s.Permissions = policy, codexPermissions
		if s.Status == Starting {
			s.Status = Idle
		}
	})
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// keepSession records the session agent id runs as its own: the TUI
// resumes it.
func keepSession(id, sid string) {
	if sid == "" {
		return
	}
	_ = state.Update(func(st *state.State) error {
		if _, a := st.FindAgent(id); a != nil && a.SessionID != sid {
			a.SessionID = sid
		}
		return nil
	})
}

// line takes one JSON-RPC message from the server.
func (c *codex) line(data []byte) {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch {
	case m.Method == "" && len(m.ID) > 0:
		c.mu.Lock()
		w := c.waiting[string(m.ID)]
		delete(c.waiting, string(m.ID))
		c.mu.Unlock()
		if w != nil {
			r := codexReply{result: m.Result}
			if m.Error != nil {
				r.err = errors.New(m.Error.Message)
			}
			w <- r
		}
	case len(m.ID) > 0:
		c.ask(m.ID, m.Method, m.Params)
	default:
		c.notified(m.Method, m.Params)
	}
}

func (c *codex) notified(method string, params json.RawMessage) {
	var p struct {
		TurnID string `json:"turnId"`
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
		Turn   struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Item      json.RawMessage `json:"item"`
		RequestID json.RawMessage `json:"requestId"`
		Error     struct {
			Message string `json:"message"`
		} `json:"error"`
		WillRetry bool `json:"willRetry"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "turn/started":
		c.mu.Lock()
		c.turn, c.items = p.Turn.ID, map[string]int{}
		thread := c.thread
		c.mu.Unlock()
		c.h.hub.Begin(c.agent, p.Turn.ID, thread)
		c.h.update(func(s *State) {
			if s.Status != Waiting {
				s.Status = Running
			}
		})
		c.h.showMessage()
	case "item/started", "item/completed":
		c.item(p.Item, method == "item/completed")
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		c.mu.Lock()
		i, ok := c.items[p.ItemID]
		turn := c.turn
		c.mu.Unlock()
		if ok && p.Delta != "" {
			c.h.hub.Block(c.agent, turn, i, func(b *live.Block) { b.Text += p.Delta })
			c.h.showMessage()
		}
	case "turn/completed":
		c.mu.Lock()
		turn := c.turn
		c.turn = ""
		open := len(c.asks)
		c.mu.Unlock()
		c.h.hub.End(c.agent, turn)
		c.h.showMessage()
		c.h.update(func(s *State) {
			if open == 0 {
				s.Status = Idle
			}
			s.Error = ""
			if p.Turn.Error != nil {
				s.Error = p.Turn.Error.Message
			}
		})
	case "error":
		if !p.WillRetry {
			c.h.update(func(s *State) { s.Error = p.Error.Message })
		}
	case "serverRequest/resolved":
		c.forget(string(p.RequestID))
	}
}

// item shows a turn's item as a block of its message: what it says,
// thinks, runs or changes.
func (c *codex) item(raw json.RawMessage, done bool) {
	var it struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Tool    string `json:"tool"`
		Query   string `json:"query"`
		Changes []struct {
			Path string `json:"path"`
			Diff string `json:"diff"`
		} `json:"changes"`
		Actions []struct {
			Command string `json:"command"`
		} `json:"commandActions"`
	}
	if json.Unmarshal(raw, &it) != nil {
		return
	}
	b := live.Block{ID: it.ID, Done: done}
	switch it.Type {
	case "agentMessage":
		b.Kind, b.Text = "text", it.Text
	case "reasoning":
		b.Kind = "thinking"
	case "commandExecution":
		b.Kind, b.Tool, b.Text = "tool", "shell", shownCommand(it.Command, it.Actions)
	case "fileChange":
		var files, diff []string
		for _, ch := range it.Changes {
			files = append(files, ch.Path)
			diff = append(diff, ch.Diff)
		}
		b.Kind, b.Tool, b.Text = "tool", "apply_patch", strings.Join(files, "\n")
		c.mu.Lock()
		c.changes[it.ID] = strings.Join(diff, "\n")
		c.mu.Unlock()
	case "mcpToolCall", "dynamicToolCall":
		b.Kind, b.Tool = "tool", it.Tool
	case "webSearch":
		b.Kind, b.Tool, b.Text = "tool", "web_search", it.Query
	default:
		return // the person's own message, and the like
	}
	c.mu.Lock()
	turn := c.turn
	i, ok := c.items[it.ID]
	if !ok {
		i = len(c.items)
		c.items[it.ID] = i
	}
	c.mu.Unlock()
	if turn == "" {
		return
	}
	c.h.hub.Block(c.agent, turn, i, func(old *live.Block) {
		if b.Text == "" {
			b.Text = old.Text // deltas so far, of thinking
		}
		*old = b
	})
	c.h.showMessage()
}

// shownCommand is the command a shell call runs, without the shell it is
// wrapped in.
func shownCommand(cmd string, actions []struct {
	Command string `json:"command"`
}) string {
	if len(actions) == 1 && actions[0].Command != "" {
		return actions[0].Command
	}
	return cmd
}

// ask turns a server request into a Request for the person, or refuses
// one mad does not do.
func (c *codex) ask(id json.RawMessage, method string, params json.RawMessage) {
	var p struct {
		ItemID    string          `json:"itemId"`
		Command   string          `json:"command"`
		Cwd       string          `json:"cwd"`
		Reason    string          `json:"reason"`
		Perms     json.RawMessage `json:"permissions"`
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
		Actions []struct {
			Command string `json:"command"`
		} `json:"commandActions"`
	}
	_ = json.Unmarshal(params, &p)
	key := string(id)
	req := Request{ID: key, Kind: "tool", Always: true}
	switch method {
	case "item/commandExecution/requestApproval":
		cmd := shownCommand(p.Command, p.Actions)
		req.Tool, req.Label, req.Input = "shell", firstLine(cmd), cmd
		if p.Reason != "" {
			req.Title = p.Reason
		}
	case "item/fileChange/requestApproval":
		c.mu.Lock()
		diff := c.changes[p.ItemID]
		c.mu.Unlock()
		req.Tool, req.Input, req.Title = "apply_patch", diff, p.Reason
	case "item/permissions/requestApproval":
		req.Tool, req.Title, req.Input = "permissions", p.Reason, string(p.Perms)
		if req.Title == "" {
			req.Title = "Allow it more access?"
		}
	case "item/tool/requestUserInput":
		req.Kind, req.Always = "question", false
		ids := codexQuest{}
		for _, q := range p.Questions {
			var opts []Option
			for _, o := range q.Options {
				opts = append(opts, Option{Label: o.Label, Description: o.Description})
			}
			req.Questions = append(req.Questions, Question{Question: q.Question, Header: q.Header, Options: opts})
			ids[q.Question] = q.ID
		}
		c.mu.Lock()
		c.quest[key] = ids
		c.mu.Unlock()
	default:
		_ = c.write(map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": "not supported by mad host: " + method}})
		return
	}
	c.mu.Lock()
	c.asks[key] = codexAsk{method: method, id: id, params: params}
	c.mu.Unlock()
	c.h.update(func(s *State) {
		s.Requests = append(s.Requests, req)
		s.Status = Waiting
	})
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// forget drops request id, answered or settled otherwise.
func (c *codex) forget(id string) {
	c.mu.Lock()
	_, had := c.asks[id]
	delete(c.asks, id)
	delete(c.quest, id)
	c.mu.Unlock()
	if had {
		c.unlist(id)
	}
}

// unlist takes request id off the state, and the agent off waiting when it
// was the last.
func (c *codex) unlist(id string) {
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

func (c *codex) answer(a Answer) error {
	c.mu.Lock()
	q, ok := c.asks[a.ID]
	ids := c.quest[a.ID]
	// Answered from here on: the server may end the turn the moment it has
	// the answer, and that end must not find the request still open.
	delete(c.asks, a.ID)
	delete(c.quest, a.ID)
	c.mu.Unlock()
	if !ok {
		return errors.New("that was answered already")
	}
	var result any
	switch q.method {
	case "item/tool/requestUserInput":
		answers := map[string]any{}
		if a.Allow {
			for question, text := range a.Answers {
				if qid := ids[question]; qid != "" {
					answers[qid] = map[string]any{"answers": strings.Split(text, ", ")}
				}
			}
		}
		result = map[string]any{"answers": answers}
	case "item/permissions/requestApproval":
		var p struct {
			Perms json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(q.params, &p)
		scope := "turn"
		if a.Always {
			scope = "session"
		}
		granted := json.RawMessage(`{}`)
		if a.Allow {
			granted = p.Perms
		}
		result = map[string]any{"permissions": granted, "scope": scope}
	default:
		decision := "decline"
		switch {
		case a.Allow && a.Always:
			decision = "acceptForSession"
		case a.Allow:
			decision = "accept"
		}
		result = map[string]any{"decision": decision}
	}
	if err := c.write(map[string]any{"id": q.id, "result": result}); err != nil {
		c.mu.Lock()
		c.asks[a.ID], c.quest[a.ID] = q, ids
		c.mu.Unlock()
		return err
	}
	c.unlist(a.ID)
	// codex takes no word on a refusal: what to do instead is a message.
	if !a.Allow && a.Message != "" {
		return c.send(a.Message, nil)
	}
	return nil
}

// send gives codex a message: a turn, or a steer of the one under way.
// Images are written to files, which it takes by path.
func (c *codex) send(text string, images []Image) error {
	// A message that came as it started waits for its thread.
	select {
	case <-c.ready:
	case <-time.After(60 * time.Second):
	}
	c.mu.Lock()
	thread, turn, model, mode := c.thread, c.turn, c.model, c.mode
	c.mu.Unlock()
	if thread == "" {
		return errors.New("codex has no thread yet")
	}
	input := []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}
	for _, im := range images {
		path, err := c.keepImage(im)
		if err != nil {
			return err
		}
		input = append(input, map[string]any{"type": "localImage", "path": path})
	}
	var err error
	if turn != "" {
		_, err = c.request("turn/steer", map[string]any{"threadId": thread, "input": input, "expectedTurnId": turn})
	} else {
		params := map[string]any{"threadId": thread, "input": input}
		if model != "" {
			params["model"] = model
		}
		if mode != "" {
			params["approvalPolicy"] = mode
		}
		_, err = c.request("turn/start", params)
	}
	if err == nil {
		c.h.update(func(s *State) {
			if s.Status != Waiting {
				s.Status = Running
			}
		})
	}
	return err
}

// keepImage writes im to a file, removed when the host ends.
func (c *codex) keepImage(im Image) (string, error) {
	data, err := base64.StdEncoding.DecodeString(im.Data)
	if err != nil {
		return "", errors.New("an image is not base64")
	}
	ext := ".img"
	switch im.Mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png", "image/gif", "image/webp":
		ext = "." + strings.TrimPrefix(im.Mime, "image/")
	}
	f, err := os.CreateTemp("", "mad-image-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	c.mu.Lock()
	c.files = append(c.files, f.Name())
	c.mu.Unlock()
	return f.Name(), nil
}

// forgetImages removes the files written for images, as far as it can.
func (c *codex) forgetImages() {
	c.mu.Lock()
	files := c.files
	c.files = nil
	c.mu.Unlock()
	for _, f := range files {
		os.Remove(f)
	}
}

// setPermission takes effect from the next turn on.
func (c *codex) setPermission(mode string) error {
	c.mu.Lock()
	c.mode = mode
	c.mu.Unlock()
	c.h.update(func(s *State) { s.Permission = mode })
	return nil
}

// setModel takes effect from the next turn on.
func (c *codex) setModel(model string) error {
	c.mu.Lock()
	c.model = model
	c.mu.Unlock()
	c.h.update(func(s *State) { s.Model = model })
	return nil
}

func (c *codex) interrupt() error {
	c.mu.Lock()
	thread, turn := c.thread, c.turn
	c.mu.Unlock()
	if turn == "" {
		return nil
	}
	_, err := c.request("turn/interrupt", map[string]any{"threadId": thread, "turnId": turn})
	return err
}

// request calls method and waits for its result.
func (c *codex) request(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.seq++
	id := fmt.Sprintf(`"mad-%d"`, c.seq)
	w := make(chan codexReply, 1)
	c.waiting[id] = w
	c.mu.Unlock()
	if err := c.write(map[string]any{"id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case r := <-w:
		return r.result, r.err
	case <-time.After(60 * time.Second):
		return nil, fmt.Errorf("codex did not answer %s", method)
	}
}

func (c *codex) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case <-c.started:
	case <-time.After(10 * time.Second):
		return errors.New("codex has not started")
	}
	c.mu.Lock()
	in := c.in
	c.mu.Unlock()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = in.Write(append(data, '\n'))
	return err
}

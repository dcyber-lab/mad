package host

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
)

// fakeCodex speaks app-server's JSON-RPC for a test: a thread (resuming
// fails), and per turn "Hello", then a command to approve, or on the
// second turn a question. What it was answered goes to MAD_FAKE_OUT.
func fakeCodex() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	record, _ := os.OpenFile(os.Getenv("MAD_FAKE_OUT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer record.Close()
	note := func(method string, params map[string]any) {
		_ = out.Encode(map[string]any{"method": method, "params": params})
	}
	turns := 0
	for in.Scan() {
		var m map[string]any
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		reply := func(result any) { _ = out.Encode(map[string]any{"id": m["id"], "result": result}) }
		switch m["method"] {
		case "initialize":
			reply(map[string]any{})
		case "model/list":
			reply(map[string]any{"data": []map[string]any{
				{"model": "gpt-x", "displayName": "GPT X"}, {"model": "gpt-old", "displayName": "Old", "hidden": true}}})
		case "thread/resume":
			_ = out.Encode(map[string]any{"id": m["id"], "error": map[string]any{"code": 1, "message": "no such thread"}})
		case "thread/start":
			reply(map[string]any{"thread": map[string]any{"id": "t1"}, "model": "gpt-x", "approvalPolicy": "on-request"})
		case "turn/interrupt", "turn/steer":
			_, _ = record.Write(append(in.Bytes(), '\n'))
			reply(map[string]any{})
		case "turn/start":
			_, _ = record.Write(append(in.Bytes(), '\n'))
			turns++
			reply(map[string]any{"turn": map[string]any{"id": "u1"}})
			note("turn/started", map[string]any{"turn": map[string]any{"id": "u1"}})
			note("item/started", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg1", "text": ""}})
			note("item/agentMessage/delta", map[string]any{"itemId": "msg1", "delta": "Hel"})
			note("item/agentMessage/delta", map[string]any{"itemId": "msg1", "delta": "lo"})
			note("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg1", "text": "Hello"}})
			ask := map[string]any{"id": 7, "method": "item/commandExecution/requestApproval", "params": map[string]any{
				"itemId": "e1", "command": `/bin/zsh -lc "ls -la"`, "commandActions": []map[string]any{{"command": "ls -la"}}}}
			if turns > 1 {
				ask = map[string]any{"id": 8, "method": "item/tool/requestUserInput", "params": map[string]any{
					"questions": []map[string]any{{"id": "q1", "header": "Pick", "question": "Which?",
						"options": []map[string]any{{"label": "A", "description": ""}, {"label": "B", "description": ""}}}}}}
			}
			_ = out.Encode(ask)
			for in.Scan() {
				var a map[string]any
				if json.Unmarshal(in.Bytes(), &a) == nil && a["method"] == nil && a["result"] != nil {
					_, _ = record.Write(append(in.Bytes(), '\n'))
					break
				}
			}
			note("turn/completed", map[string]any{"turn": map[string]any{"id": "u1", "status": "completed"}})
		}
	}
}

// fakePi speaks pi's RPC for a test: its state and models, and per prompt
// "pong" written, then a select to answer.
func fakePi() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	record, _ := os.OpenFile(os.Getenv("MAD_FAKE_OUT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer record.Close()
	update := func(e map[string]any) {
		_ = out.Encode(map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant"}, "assistantMessageEvent": e})
	}
	for in.Scan() {
		var m map[string]any
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		respond := func(data any) {
			_ = out.Encode(map[string]any{"id": m["id"], "type": "response", "command": m["type"], "success": true, "data": data})
		}
		switch m["type"] {
		case "get_state":
			respond(map[string]any{"model": map[string]any{"provider": "p", "id": "m1", "name": "M1"}, "sessionId": "s1"})
		case "get_available_models":
			respond(map[string]any{"models": []map[string]any{{"provider": "p", "id": "m1", "name": "M1"}, {"provider": "p", "id": "m2", "name": "M2"}}})
		case "set_model", "abort", "set_thinking_level":
			_, _ = record.Write(append(in.Bytes(), '\n'))
			respond(nil)
		case "prompt":
			_, _ = record.Write(append(in.Bytes(), '\n'))
			respond(nil)
			_ = out.Encode(map[string]any{"type": "agent_start"})
			_ = out.Encode(map[string]any{"type": "message_start", "message": map[string]any{"role": "assistant", "timestamp": 123}})
			update(map[string]any{"type": "text_start", "contentIndex": 0})
			update(map[string]any{"type": "text_delta", "contentIndex": 0, "delta": "po"})
			update(map[string]any{"type": "text_delta", "contentIndex": 0, "delta": "ng"})
			update(map[string]any{"type": "text_end", "contentIndex": 0})
			_ = out.Encode(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "timestamp": 123}})
			_ = out.Encode(map[string]any{"type": "extension_ui_request", "id": "x1", "method": "select", "title": "Pick one", "options": []string{"A", "B"}})
			for in.Scan() {
				var a map[string]any
				if json.Unmarshal(in.Bytes(), &a) == nil && a["type"] == "extension_ui_response" {
					_, _ = record.Write(append(in.Bytes(), '\n'))
					break
				}
			}
			_ = out.Encode(map[string]any{"type": "agent_settled"})
		}
	}
}

func startFake(t *testing.T, h *Host) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = h.d.run(ctx)
		close(done)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return stop
}

func record(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("MAD_SOCKET", "")
	rec := filepath.Join(dir, "answers")
	t.Setenv("MAD_FAKE_OUT", rec)
	return rec
}

func TestCodex(t *testing.T) {
	rec := record(t)
	t.Setenv("MAD_FAKE_CODEX", "1")
	old := codexBin
	codexBin = os.Args[0]
	t.Cleanup(func() { codexBin = old })
	// An agent of its own, whose thread the host records.
	a := &state.Agent{ID: "cx-1", Kind: "codex", SessionID: "gone", Args: []string{"-m", "gpt-x"}}
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{a}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	h := newHost(a.ID)
	h.d = newCodex(h, a, t.TempDir())
	cancelCodex := startFake(t, h)

	s := waitState(t, h, "its thread", func(s State) bool { return s.Status == Idle && s.Session == "t1" })
	if len(s.Models) != 1 || s.Models[0].Name != "GPT X" || s.Model != "gpt-x" {
		t.Errorf("models %+v, model %q", s.Models, s.Model)
	}
	if st, _ := state.Load(); st.Projects[0].Agents[0].SessionID != "t1" {
		t.Error("the thread is not kept as its session")
	}
	if s.Permission != "on-request" || len(s.Permissions) != 3 {
		t.Errorf("permission %q of %+v", s.Permission, s.Permissions)
	}
	img := Image{Mime: "image/png", Data: "QUJD"}
	if err := h.d.send("hi", []Image{img}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "an approval", func(s State) bool { return len(s.Requests) == 1 })
	if r := s.Requests[0]; r.Tool != "shell" || r.Label != "ls -la" || !r.Always || s.Status != Waiting {
		t.Errorf("request %+v, status %s", r, s.Status)
	}
	if m := s.Message; m == nil || len(m.Blocks) != 1 || m.Blocks[0].Text != "Hello" {
		t.Errorf("message %+v", s.Message)
	}
	if err := h.d.answer(Answer{ID: s.Requests[0].ID, Allow: true, Always: true}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the turn's end", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })

	if err := h.d.setModel("gpt-y"); err != nil {
		t.Fatal(err)
	}
	if err := h.d.setPermission("never"); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Permission != "never" {
		t.Errorf("permission %q", s.Permission)
	}
	if err := h.d.send("again", nil); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "a question", func(s State) bool { return len(s.Requests) == 1 })
	q := s.Requests[0]
	if q.Kind != "question" || q.Questions[0].Header != "Pick" || len(q.Questions[0].Options) != 2 {
		t.Fatalf("question %+v", q)
	}
	if err := h.d.answer(Answer{ID: q.ID, Allow: true, Answers: map[string]string{"Which?": "B"}}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the second turn's end", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })

	got := strings.Join(lines(t, rec), "\n")
	var path string
	for _, l := range lines(t, rec) {
		var m struct {
			Params struct {
				Input []struct{ Type, Path string } `json:"input"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(l), &m) == nil {
			for _, it := range m.Params.Input {
				if it.Type == "localImage" {
					path = it.Path
				}
			}
		}
	}
	if b, err := os.ReadFile(path); path == "" || err != nil || string(b) != "ABC" {
		t.Errorf("image file %q: %q, %v", path, b, err)
	}
	cancelCodex()
	if _, err := os.Stat(path); err == nil {
		t.Error("the image file is kept after the host ends")
	}
	for _, want := range []string{`"type":"localImage"`, `"approvalPolicy":"never"`, `"q1":{"answers":["B"]}`, `"model":"gpt-y"`, `"model":"gpt-x"`} {
		if !strings.Contains(got, want) {
			t.Errorf("codex was not sent %s:\n%s", want, got)
		}
	}
}

func TestPi(t *testing.T) {
	rec := record(t)
	t.Setenv("MAD_FAKE_PI", "1")
	h := newHost("pi-1")
	h.d = newPi(h, os.Args[0], t.TempDir())
	startFake(t, h)

	s := waitState(t, h, "ready", func(s State) bool { return s.Status == Idle && len(s.Models) == 2 })
	if len(s.Permissions) != 0 {
		t.Errorf("permissions %+v", s.Permissions)
	}
	if err := h.d.setPermission("plan"); err == nil {
		t.Error("pi took a permission mode")
	}
	if s.Model != "p/m1" || s.Session != "s1" {
		t.Errorf("model %q session %q", s.Model, s.Session)
	}
	if err := h.d.send("ping", []Image{{Mime: "image/jpeg", Data: "QUJD"}}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "a select", func(s State) bool { return len(s.Requests) == 1 })
	r := s.Requests[0]
	if r.Kind != "question" || r.Questions[0].Question != "Pick one" || len(r.Questions[0].Options) != 2 {
		t.Errorf("request %+v", r)
	}
	if m := s.Message; m == nil || m.ID != "123" || len(m.Blocks) != 1 || m.Blocks[0].Text != "pong" || !m.Done {
		t.Errorf("message %+v", s.Message)
	}
	if err := h.d.answer(Answer{ID: r.ID, Allow: true, Answers: map[string]string{"Pick one": "B"}}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "settled", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })
	if err := h.d.setModel("p/m2"); err != nil {
		t.Fatal(err)
	}
	if err := h.d.setModel("nope"); err == nil {
		t.Error("an unknown model was taken")
	}
	if err := h.d.interrupt(); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the model picked", func(s State) bool { return s.Model == "p/m2" })
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		got := strings.Join(lines(t, rec), "\n")
		if strings.Contains(got, `"abort"`) {
			for _, want := range []string{`"value":"B"`, `"modelId":"m2"`,
				`"images":[{"data":"QUJD","mimeType":"image/jpeg","type":"image"}]`} {
				if !strings.Contains(got, want) {
					t.Errorf("pi was not sent %s:\n%s", want, got)
				}
			}
			return
		}
	}
	t.Error("no abort sent")
}

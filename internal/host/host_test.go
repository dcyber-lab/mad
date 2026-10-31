package host

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// The test binary stands in for claude when MAD_FAKE_CLAUDE is set: it
// answers initialize, and to each message writes "Hello" and asks before
// using a tool, Write for the first, AskUserQuestion for the next. What
// it was answered goes, a line each, to MAD_FAKE_OUT.
func TestMain(m *testing.M) {
	switch {
	case os.Getenv("MAD_FAKE_CLAUDE") == "1":
		fakeClaude()
		os.Exit(0)
	case os.Getenv("MAD_FAKE_CODEX") == "1":
		fakeCodex()
		os.Exit(0)
	case os.Getenv("MAD_FAKE_PI") == "1":
		fakePi()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeClaude() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	record, _ := os.OpenFile(os.Getenv("MAD_FAKE_OUT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer record.Close()
	ev := func(e map[string]any) { _ = out.Encode(map[string]any{"type": "stream_event", "event": e}) }
	turn := 0
	for in.Scan() {
		var m map[string]any
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		switch m["type"] {
		case "control_request":
			req, _ := m["request"].(map[string]any)
			resp := map[string]any{}
			if req["subtype"] == "initialize" {
				resp["models"] = []map[string]any{{"value": "haiku", "displayName": "Haiku"}, {"value": "opus", "displayName": "Opus"}}
			}
			_, _ = record.Write(append(in.Bytes(), '\n'))
			_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": m["request_id"], "response": resp}})
		case "user":
			turn++
			_, _ = record.Write(append(in.Bytes(), '\n'))
			_ = out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": "s1", "model": "claude-haiku-5-5",
				"permissionMode": "default"})
			ev(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg" + string(rune('0'+turn))}})
			ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "Hel"}})
			ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "lo"}})
			ev(map[string]any{"type": "content_block_stop", "index": 0})
			ask := map[string]any{"subtype": "can_use_tool", "tool_name": "Write",
				"input":                  map[string]any{"file_path": "/w/a.txt", "content": "hi"},
				"permission_suggestions": []map[string]any{{"type": "addRules", "rules": []string{"Write"}}}}
			if turn > 1 {
				ask = map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "input": map[string]any{
					"questions": []map[string]any{{"question": "Which?", "header": "Pick",
						"options": []map[string]any{{"label": "Red"}, {"label": "Blue"}}}}}}
			}
			_ = out.Encode(map[string]any{"type": "control_request", "request_id": "r" + string(rune('0'+turn)), "request": ask})
			// The answer, then the turn's end.
			for in.Scan() {
				var a map[string]any
				if json.Unmarshal(in.Bytes(), &a) == nil && a["type"] == "control_response" {
					_, _ = record.Write(append(in.Bytes(), '\n'))
					break
				} else if a["type"] == "control_request" {
					_, _ = record.Write(append(in.Bytes(), '\n')) // one sent while it waits
				}
			}
			ev(map[string]any{"type": "message_stop"})
			_ = out.Encode(map[string]any{"type": "result", "subtype": "success", "is_error": false})
		}
	}
}

// waitState waits for h's state to satisfy ok.
func waitState(t *testing.T, h *Host, what string, ok func(State) bool) State {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if s := h.State(); ok(s) {
			return s
		}
	}
	t.Fatalf("waiting for %s: state %+v", what, h.State())
	return State{}
}

func fakeHost(t *testing.T) (*Host, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir) // status reports
	t.Setenv("MAD_SOCKET", "")
	t.Setenv("MAD_FAKE_CLAUDE", "1")
	rec := filepath.Join(dir, "answers")
	t.Setenv("MAD_FAKE_OUT", rec)
	h := newHost("agent-1")
	h.d = newClaude(h, os.Args[0], dir)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = h.d.run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h, rec
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestClaudeAsksAndIsAnswered(t *testing.T) {
	h, rec := fakeHost(t)
	s := waitState(t, h, "ready", func(s State) bool { return s.Status == Idle && len(s.Models) == 2 })
	if s.Models[0].Name != "Haiku" {
		t.Errorf("models %+v", s.Models)
	}

	if err := h.d.send("hi", nil); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "a permission request", func(s State) bool { return s.Status == Waiting && len(s.Requests) == 1 })
	r := s.Requests[0]
	if r.Kind != "tool" || r.Tool != "Write" || r.Label != "a.txt" || !r.Always || !strings.Contains(r.Input, "hi") {
		t.Errorf("request %+v", r)
	}
	if s.Model != "claude-haiku-5-5" || s.Session != "s1" {
		t.Errorf("model %q session %q", s.Model, s.Session)
	}
	if m := s.Message; m == nil || len(m.Blocks) != 1 || m.Blocks[0].Text != "Hello" {
		t.Errorf("message %+v", s.Message)
	}
	if err := h.d.answer(Answer{ID: r.ID, Allow: true, Always: true}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "the turn's end", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })
	if !s.Message.Done {
		t.Error("message not done at the turn's end")
	}
	if err := h.d.answer(Answer{ID: r.ID, Allow: true}); err == nil {
		t.Error("answered twice")
	}

	// A question, answered with an option.
	if err := h.d.send("again", nil); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "a question", func(s State) bool { return len(s.Requests) == 1 })
	q := s.Requests[0]
	if q.Kind != "question" || len(q.Questions) != 1 || len(q.Questions[0].Options) != 2 || q.Questions[0].Header != "Pick" {
		t.Fatalf("question %+v", q)
	}
	if err := h.d.answer(Answer{ID: q.ID, Allow: true, Answers: map[string]string{"Which?": "Blue"}}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the second turn's end", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })

	got := lines(t, rec)
	var allow, question map[string]any
	for _, l := range got {
		var m map[string]any
		_ = json.Unmarshal([]byte(l), &m)
		resp, _ := m["response"].(map[string]any)
		switch resp["request_id"] {
		case "r1":
			allow, _ = resp["response"].(map[string]any)
		case "r2":
			question, _ = resp["response"].(map[string]any)
		}
	}
	if allow["behavior"] != "allow" || allow["updatedPermissions"] == nil {
		t.Errorf("allow always went as %v", allow)
	}
	in, _ := question["updatedInput"].(map[string]any)
	if answers, _ := in["answers"].(map[string]any); answers["Which?"] != "Blue" || in["questions"] == nil {
		t.Errorf("the question's answer went as %v", question)
	}
}

// Images go as content blocks, and a permission mode as a control request.
func TestClaudeImagesAndPermission(t *testing.T) {
	h, rec := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	if err := h.do(Command{Type: "send", Text: "look", Images: []Image{{Mime: "image/png", Data: "QUJD"}}}); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, h, "its init", func(s State) bool { return s.Permission == "default" && len(s.Requests) == 1 })
	if len(s.Permissions) != 4 || s.Permissions[3].Value != "bypassPermissions" {
		t.Errorf("permissions %+v", s.Permissions)
	}
	if err := h.d.answer(Answer{ID: s.Requests[0].ID, Allow: true}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the turn's end", func(s State) bool { return s.Status == Idle })
	if err := h.do(Command{Type: "permission", Mode: "plan"}); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Permission != "plan" {
		t.Errorf("permission %q", s.Permission)
	}
	if err := h.do(Command{Type: "permission"}); err == nil {
		t.Error("no mode was taken")
	}
	_ = h.d.interrupt() // a request after it, so the one before is surely read
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(rec); strings.Contains(string(b), `"interrupt"`) {
			break
		}
	}
	got := strings.Join(lines(t, rec), "\n")
	for _, want := range []string{
		`"content":[{"text":"look","type":"text"},{"source":{"data":"QUJD","media_type":"image/png","type":"base64"},"type":"image"}]`,
		`"subtype":"set_permission_mode"`, `"mode":"plan"`} {
		if !strings.Contains(got, want) {
			t.Errorf("claude was not sent %s:\n%s", want, got)
		}
	}
}

// The host is the one writer of <id>.asks.json: what it holds open is
// there while it does, and gone once answered.
func TestHostWritesAsks(t *testing.T) {
	h, _ := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	if status.ReadAsks("agent-1") != nil {
		t.Fatal("asks before any request")
	}
	before := time.Now()
	if err := h.d.send("hi", nil); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, h, "a permission request", func(s State) bool { return len(s.Requests) == 1 })
	asks := status.ReadAsks("agent-1")
	if len(asks) != 1 {
		t.Fatalf("asks = %+v", asks)
	}
	if a := asks[0]; a.ID != s.Requests[0].ID || a.Kind != status.AskPermission || a.Tool != "Write" || a.Target != "a.txt" || !a.Always || a.At.Before(before) {
		t.Errorf("ask = %+v", a)
	}
	at := asks[0].At
	h.update(func(*State) {}) // another change keeps the file and the time
	if a := status.ReadAsks("agent-1"); len(a) != 1 || !a[0].At.Equal(at) {
		t.Errorf("after a change: %+v", a)
	}
	if err := h.d.answer(Answer{ID: s.Requests[0].ID, Allow: true}); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "the turn's end", func(s State) bool { return len(s.Requests) == 0 })
	if a := status.ReadAsks("agent-1"); a != nil {
		t.Errorf("answered, still asks %+v", a)
	}

	if err := h.d.send("again", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, h, "a question", func(s State) bool { return len(s.Requests) == 1 })
	if a := status.ReadAsks("agent-1"); len(a) != 1 || a[0].Kind != status.AskQuestion || a[0].Question != "Which?" ||
		a[0].Header != "Pick" || len(a[0].Options) != 2 || !a[0].OneTap {
		t.Errorf("question asks %+v", a)
	}
}

func TestAsksOf(t *testing.T) {
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	got := asksOf([]Request{
		{ID: "1", Kind: "tool", Tool: "Bash", Label: "Run the tests", Input: "$ go test ./...\n(more)", Title: "Claude wants to run"},
		{ID: "2", Kind: "tool", Tool: "shell", Label: "make build", Input: "make build"},
		{ID: "3", Kind: "plan", Plan: "Do it\nthen more"},
		{ID: "4", Kind: "question", Questions: []Question{{Question: "A?", Multi: true}, {Question: "B?"}}},
	}, map[string]time.Time{"1": at}, "h1")
	if a := got[0]; a.Command != "go test ./..." || a.Target != "Run the tests" || a.Title != "Claude wants to run" || !a.At.Equal(at) {
		t.Errorf("bash: %+v", a)
	}
	if a := got[1]; a.Command != "make build" || a.Tool != "shell" {
		t.Errorf("shell: %+v", a)
	}
	if a := got[2]; a.Kind != status.AskPlan || a.Plan != "Do it" {
		t.Errorf("plan: %+v", a)
	}
	if a := got[3]; a.Kind != status.AskQuestion || a.Question != "A?" || a.OneTap {
		t.Errorf("questions: %+v", a)
	}
}

// Too many images, too big or not images are refused.
func TestImageLimits(t *testing.T) {
	h := newHost("agent-1")
	var many []Image
	for range maxImages + 1 {
		many = append(many, Image{Mime: "image/png", Data: "QUJD"})
	}
	big := Image{Mime: "image/png", Data: strings.Repeat("QUJD", maxImageSize/3+1)}
	for what, c := range map[string]Command{
		"many":    {Type: "send", Text: "x", Images: many},
		"big":     {Type: "send", Text: "x", Images: []Image{big}},
		"not one": {Type: "send", Text: "x", Images: []Image{{Mime: "text/plain", Data: "QUJD"}}},
	} {
		if err := h.do(c); err == nil {
			t.Errorf("%s: sent", what)
		}
	}
	if err := h.do(Command{Type: "send"}); err == nil {
		t.Error("nothing was sent")
	}
}

func TestClaudeModelAndStop(t *testing.T) {
	h, rec := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	if err := h.d.setModel("opus"); err != nil {
		t.Fatal(err)
	}
	if err := h.d.interrupt(); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Model != "opus" {
		t.Errorf("model %q", s.Model)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(rec); strings.Contains(string(b), `"interrupt"`) {
			if !strings.Contains(string(b), `"set_model"`) || !strings.Contains(string(b), `"opus"`) {
				t.Errorf("sent %s", b)
			}
			return
		}
	}
	t.Error("no interrupt sent")
}

// The socket: a state on connecting and on each change; commands done,
// or why not.
func TestServe(t *testing.T) {
	h, _ := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	dir, err := os.MkdirTemp("", "mh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go h.serve(ln)
	conn, err := net.Dial("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	next := func() Line {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if !sc.Scan() {
			t.Fatalf("nothing from the host: %v", sc.Err())
		}
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	if l := next(); l.Type != "state" || l.State.Status != Idle {
		t.Fatalf("first line %+v", l)
	}
	_, _ = conn.Write([]byte(`{"type":"send","text":"  "}` + "\n"))
	if l := next(); l.Type != "error" {
		t.Errorf("an empty message: %+v", l)
	}
	_, _ = conn.Write([]byte(`{"type":"send","text":"hi"}` + "\n"))
	sawDone, sawAsk := false, false
	for !sawDone || !sawAsk {
		l := next()
		sawDone = sawDone || l.Type == "done"
		sawAsk = sawAsk || l.Type == "state" && len(l.State.Requests) == 1
	}
}

func TestRender(t *testing.T) {
	s := State{Status: Waiting, Model: "opus", Requests: []Request{{Kind: "tool", Tool: "Bash", Label: "ls"}}}
	out := render(s, "✻ claude", "", 60, 12)
	for _, want := range []string{"in the mad window", "needs you", "May it use Bash? ls", "Answer in the mad window", "t  back to its TUI"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\r\n") + 1; n != 12 {
		t.Errorf("%d lines for a pane of 12", n)
	}
}

// A mode set before the first message stands once claude's init, which
// reports the mode it started in, comes back; claude is told again.
func TestClaudePermissionSurvivesInit(t *testing.T) {
	h, rec := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	if err := h.do(Command{Type: "permission", Mode: "bypassPermissions"}); err != nil {
		t.Fatal(err)
	}
	if err := h.d.send("hi", nil); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, h, "its init", func(s State) bool { return s.Session == "s1" && len(s.Requests) == 1 })
	if s.Permission != "bypassPermissions" {
		t.Errorf("permission %q after init", s.Permission)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(rec); strings.Count(string(b), `"subtype":"set_permission_mode"`) >= 2 {
			return
		}
	}
	t.Errorf("the mode was not sent again:\n%s", strings.Join(lines(t, rec), "\n"))
}

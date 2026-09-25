package status

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestParseClaude(t *testing.T) {
	cases := []struct {
		in    string
		state string // "" = ignored
	}{
		{`{"hook_event_name":"SessionStart","session_id":"s1"}`, Idle},
		{`{"hook_event_name":"UserPromptSubmit"}`, Running},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, Running},
		{`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion"}`, Waiting},
		{`{"hook_event_name":"PreToolUse","tool_name":"ExitPlanMode"}`, Waiting},
		{`{"hook_event_name":"PostToolUse"}`, Running},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, Waiting},
		{`{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, Waiting},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, Idle},
		{`{"hook_event_name":"Notification","message":"Claude is waiting for your input"}`, Idle},
		{`{"hook_event_name":"Notification","message":"something else"}`, ""},
		{`{"hook_event_name":"Stop"}`, Idle},
		{`{"hook_event_name":"SubagentStop"}`, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		h := ParseClaude(strings.NewReader(c.in))
		switch {
		case c.state == "" && h != nil:
			t.Errorf("%s: want ignored, got %+v", c.in, h)
		case c.state != "" && (h == nil || h.State != c.state):
			t.Errorf("%s: got %+v, want state %s", c.in, h, c.state)
		}
	}
	if h := ParseClaude(strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"s1"}`)); h.SessionID != "s1" {
		t.Errorf("session id not kept: %+v", h)
	}
}

func TestParseCodex(t *testing.T) {
	h := ParseCodex(`{"type":"agent-turn-complete","thread-id":"t-1","last-assistant-message":"done"}`)
	if h == nil || h.State != Idle || h.SessionID != "t-1" {
		t.Errorf("got %+v", h)
	}
	for _, in := range []string{`{"type":"other"}`, `nope`} {
		if h := ParseCodex(in); h != nil {
			t.Errorf("%s: want nil, got %+v", in, h)
		}
	}
}

func TestHookFiles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if ReadHook("a") != nil {
		t.Fatal("missing file should read as nil")
	}
	if err := WriteHook("a", &Hook{State: Idle, SessionID: "s1"}, now); err != nil {
		t.Fatal(err)
	}
	// A later report without a session id keeps the known one.
	if err := WriteHook("a", &Hook{State: Running}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	h := ReadHook("a")
	if h == nil || h.State != Running || h.SessionID != "s1" || !h.At.Equal(now.Add(time.Second)) {
		t.Errorf("got %+v", h)
	}
	RemoveHook("a")
	if ReadHook("a") != nil {
		t.Error("RemoveHook left the file")
	}
}

var (
	t0     = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	claude = agent.ByName(agent.Builtin(), "claude")
	codex  = agent.ByName(agent.Builtin(), "codex")
	alive  = tmux.Pane{ID: "%1"}
)

func TestObservePaneState(t *testing.T) {
	var tr Tracker
	if s := tr.Observe(codex, tmux.Pane{}, false, nil, "", false, t0); s != Stopped {
		t.Errorf("no pane = %s", s)
	}
	if s := tr.Observe(codex, tmux.Pane{Dead: true}, true, nil, "", false, t0); s != Exited {
		t.Errorf("dead pane = %s", s)
	}
}

func TestObserveScreenHeuristics(t *testing.T) {
	var tr Tracker
	// First sight of a screen is not "activity".
	if s := tr.Observe(codex, alive, true, nil, "prompt", false, t0); s != Idle {
		t.Errorf("first observation = %s", s)
	}
	if s := tr.Observe(codex, alive, true, nil, "Working (1s)", false, t0.Add(time.Second)); s != Running {
		t.Errorf("changing screen = %s", s)
	}
	if s := tr.Observe(codex, alive, true, nil, "Working (1s)", false, t0.Add(time.Second+ActiveWindow)); s != Idle {
		t.Errorf("screen frozen past ActiveWindow = %s", s)
	}
	if !tr.Attention {
		t.Error("running → idle off stage should ask for attention")
	}
	// A static approval prompt reads as waiting.
	prompt := strings.Repeat("output\n", 40) + "Would you like to run the following command?\n"
	tr.Observe(codex, alive, true, nil, prompt, false, t0.Add(10*time.Second))
	if s := tr.Observe(codex, alive, true, nil, prompt, false, t0.Add(20*time.Second)); s != Waiting {
		t.Errorf("approval prompt = %s", s)
	}
	// Only the bottom of the screen counts.
	old := "Would you like to run the following command?\n" + strings.Repeat("later output\n", 40)
	tr.Observe(codex, alive, true, nil, old, false, t0.Add(30*time.Second))
	if s := tr.Observe(codex, alive, true, nil, old, false, t0.Add(40*time.Second)); s != Idle {
		t.Errorf("prompt scrolled off = %s", s)
	}
}

func TestObserveHooks(t *testing.T) {
	var tr Tracker
	running := &Hook{State: Running, At: t0}
	if s := tr.Observe(claude, alive, true, running, "x", false, t0); s != Running {
		t.Errorf("hook running = %s", s)
	}
	// Screen frozen and no newer hook: the turn was interrupted.
	if s := tr.Observe(claude, alive, true, running, "x", false, t0.Add(StaleRunning+time.Second)); s != Idle {
		t.Errorf("stale running = %s", s)
	}
	// Screen still moving keeps it running even with an old hook.
	tr = Tracker{}
	tr.Observe(claude, alive, true, running, "a", false, t0)
	tr.Observe(claude, alive, true, running, "b", false, t0.Add(StaleRunning))
	if s := tr.Observe(claude, alive, true, running, "b", false, t0.Add(StaleRunning+time.Second)); s != Running {
		t.Errorf("active screen with old hook = %s", s)
	}

	if s := tr.Observe(claude, alive, true, &Hook{State: Waiting, At: t0}, "b", false, t0.Add(time.Hour)); s != Waiting {
		t.Errorf("hook waiting = %s", s)
	}
	// Idle from hooks wins over typing on screen.
	if s := tr.Observe(claude, alive, true, &Hook{State: Idle, At: t0}, "typing…", false, t0.Add(2*time.Hour)); s != Idle {
		t.Errorf("hook idle = %s", s)
	}
	// Hook-less claude (hooks not delivered yet) falls back to the screen.
	tr = Tracker{}
	tr.Observe(claude, alive, true, nil, "Yes, I trust this folder", false, t0)
	if s := tr.Observe(claude, alive, true, nil, "Yes, I trust this folder", false, t0.Add(time.Minute)); s != Waiting {
		t.Errorf("trust dialog without hooks = %s", s)
	}
}

func TestAttention(t *testing.T) {
	var tr Tracker
	run := &Hook{State: Running, At: t0}
	idle := &Hook{State: Idle, At: t0}

	tr.Observe(claude, alive, true, run, "x", true, t0)
	tr.Observe(claude, alive, true, idle, "x", true, t0)
	if tr.Attention {
		t.Error("finishing on stage needs no attention")
	}

	tr.Observe(claude, alive, true, run, "x", false, t0)
	tr.Observe(claude, alive, true, idle, "x", false, t0)
	if !tr.Attention {
		t.Error("finishing off stage needs attention")
	}
	tr.Observe(claude, alive, true, idle, "x", false, t0)
	if !tr.Attention {
		t.Error("attention should stick until viewed")
	}
	tr.Observe(claude, alive, true, idle, "x", true, t0)
	if tr.Attention {
		t.Error("viewing clears attention")
	}
}

func TestQuota(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, ok := ParseStatusLine(strings.NewReader(`{"model":{"display_name":"Opus"}}`)); ok {
		t.Error("no rate_limits should not parse")
	}
	if _, ok := ParseStatusLine(strings.NewReader(`not json`)); ok {
		t.Error("garbage parsed")
	}
	q, ok := ParseStatusLine(strings.NewReader(`{"rate_limits":{"five_hour":{"used_percentage":62,"resets_at":1738425600},"seven_day":{"used_percentage":31,"resets_at":1738857600}}}`))
	if !ok || q.FiveHour.Used != 62 || q.FiveHour.ResetAt.Unix() != 1738425600 || q.SevenDay.Used != 31 {
		t.Fatalf("parsed %+v %v", q, ok)
	}
	// A window Claude dropped (its reset passed) reads as unknown.
	q2, ok := ParseStatusLine(strings.NewReader(`{"rate_limits":{"seven_day":{"used_percentage":5,"resets_at":1738857600}}}`))
	if !ok || q2.FiveHour.Known() || q2.SevenDay.Used != 5 {
		t.Errorf("partial: %+v", q2)
	}

	now := time.Unix(1738400000, 0)
	if _, ok := ReadQuota("claude"); ok {
		t.Error("read before write")
	}
	if err := WriteQuota("claude", q, now); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadQuota("claude")
	if !ok || !got.At.Equal(now) || got.FiveHour.Used != 62 || !got.SevenDay.ResetAt.Equal(q.SevenDay.ResetAt) {
		t.Errorf("read back %+v %v", got, ok)
	}
	// Past its reset the five-hour window is spent from zero; the weekly
	// one is untouched.
	e := got.Expire(time.Unix(1738425600, 0))
	if e.FiveHour.Known() || e.SevenDay.Used != 31 {
		t.Errorf("expired: %+v", e)
	}
	if e := got.Expire(now); e.FiveHour.Used != 62 {
		t.Errorf("not yet expired: %+v", e)
	}
}

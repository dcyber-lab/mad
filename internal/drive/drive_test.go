package drive

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestMain(m *testing.M) {
	if os.Getenv("MAD_FAKE_AGENT") != "" {
		fakeAgent()
		return
	}
	os.Exit(m.Run())
}

// fakeAgent stands in for claude: it asks for bracketed paste, reports
// through the status files as `mad hook claude` would, and answers each
// message with "echo: " and the message. Some messages do more:
//
//	slow  works (its screen moving) for a while first
//	linger  keeps busy for a while after reporting the turn done, as
//	      claude does running the user's own Stop hooks
//	ask   asks for permission, and goes on after a line is typed
//	drop  ends the turn without a report, as claude when interrupted
//	quit  exits
func fakeAgent() {
	id := os.Getenv("MAD_AGENT_ID")
	report := func(state, event, msg string) {
		_ = status.WriteHook(id, &status.Hook{State: state, Event: event, Message: msg}, time.Now())
	}
	fmt.Print("\x1b[?2004h")
	fmt.Println("fake agent ready")
	report(status.Idle, "SessionStart", "")
	in := bufio.NewReader(os.Stdin)
	for {
		msg, ok := readMessage(in)
		if !ok {
			return
		}
		report(status.Running, "UserPromptSubmit", "")
		switch msg {
		case "slow":
			for i := 0; i < 30; i++ {
				fmt.Print(".")
				time.Sleep(100 * time.Millisecond)
			}
			fmt.Println()
		case "ask":
			report(status.Waiting, "Notification", "needs permission")
			fmt.Println("Do you want to proceed?\n❯ 1. Yes")
			if _, ok := readMessage(in); !ok {
				return
			}
			fmt.Print("\x1b[2J\x1b[H") // the prompt goes, as claude's does
			report(status.Running, "PostToolUse", "")
		case "drop":
			fmt.Println("Interrupted")
			continue
		case "quit":
			return
		}
		fmt.Println("replied")
		now := time.Now()
		_ = status.WriteTurn(id, &status.Turn{Reply: "echo: " + msg}, now)
		_ = status.WriteHook(id, &status.Hook{State: status.Idle, Event: "Stop"}, now)
		if msg == "linger" {
			for i := 0; i < 10; i++ {
				fmt.Print("+")
				time.Sleep(100 * time.Millisecond)
			}
			fmt.Println()
		}
	}
}

// readMessage reads a bracketed paste and the enter after it, or a line.
func readMessage(in *bufio.Reader) (string, bool) {
	var b strings.Builder
	pasting := false
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return "", false
		}
		if i := strings.Index(line, "\x1b[200~"); i >= 0 {
			line, pasting = line[i+len("\x1b[200~"):], true
		}
		if i := strings.Index(line, "\x1b[201~"); i >= 0 {
			b.WriteString(line[:i])
			return b.String(), true
		}
		if !pasting {
			return strings.TrimRight(line, "\r\n"), true
		}
		b.WriteString(line)
	}
}

// useDeck isolates state and config, puts mad's tmux server on a socket
// of its own with the deck's own panes running sleep, and speeds drive up.
func useDeck(t *testing.T) (dir string, kinds []agent.Kind) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	t.Setenv("MAD_AGENT_ID", "")

	oldSocket, oldSelf, oldSettle := tmux.Socket, deck.SelfCommand, tmux.PasteSettle
	oldTick, oldLook, oldQuiet, oldStart, oldStale := tick, look, quietFor, startFor, stale
	tmux.Socket = fmt.Sprintf("mad-drive-test-%d", time.Now().UnixNano())
	deck.SelfCommand = func(sub string) string { return "sleep 600 # mad " + sub }
	tmux.PasteSettle, tick, look, quietFor, startFor, stale = 50*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond, 400*time.Millisecond, 20*time.Second, time.Second
	t.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand, tmux.PasteSettle = oldSocket, oldSelf, oldSettle
		tick, look, quietFor, startFor, stale = oldTick, oldLook, oldQuiet, oldStart, oldStale
	})

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Named claude so it is waited for like one: its provider says agents
	// of the kind report when they are done.
	kinds = []agent.Kind{{Name: "claude", Start: "MAD_FAKE_AGENT=1 exec " + paths.ShellQuote(self), Hooks: true}}
	return dir, kinds
}

func TestSendAndWait(t *testing.T) {
	dir, kinds := useDeck(t)
	kinds = withWaiting(t, kinds)

	a, err := Spawn(Spec{Kind: "claude", Name: "fake", Dir: dir, Model: "m1"}, kinds)
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, got, err := Find(st, "fake", dir); err != nil || got.ID != a.ID || strings.Join(got.Args, " ") != "--model m1" {
		t.Fatalf("spawned agent = %+v, %v", got, err)
	}
	if _, err := Spawn(Spec{Kind: "claude", Name: "fake", Dir: dir}, kinds); err == nil {
		t.Error("a second agent of the same name in the project")
	}
	if _, err := Spawn(Spec{Kind: "nope", Dir: dir}, kinds); err == nil {
		t.Error("spawned an unknown kind")
	}

	reply := func(want string) {
		t.Helper()
		turn, err := Wait(a, kinds, WaitOptions{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatalf("wait for %q: %v", want, err)
		}
		if turn == nil || turn.Reply != want {
			t.Fatalf("turn = %+v, want reply %q", turn, want)
		}
	}

	// A message of several lines arrives as one.
	if err := Send(st, a, "hello\nworld\n", kinds); err != nil {
		t.Fatal(err)
	}
	reply("echo: hello\nworld")
	// Nothing pending: the last turn, at once.
	reply("echo: hello\nworld")

	// One at a time.
	if err := Send(st, a, "slow", kinds); err != nil {
		t.Fatal(err)
	}
	if err := Send(st, a, "too soon", kinds); !errors.Is(err, ErrBusy) {
		t.Errorf("send while working: %v", err)
	}
	if _, err := Wait(a, kinds, WaitOptions{Timeout: 200 * time.Millisecond}); !errors.Is(err, ErrTimeout) {
		t.Errorf("short wait: %v", err)
	}
	reply("echo: slow")

	// Done, but still busy a moment: the next message waits for it.
	if err := Send(st, a, "linger", kinds); err != nil {
		t.Fatal(err)
	}
	reply("echo: linger")
	if err := Send(st, a, "next", kinds); err != nil {
		t.Fatalf("send after a turn reported done: %v", err)
	}
	reply("echo: next")

	// Waiting for an answer is said once, and waited through.
	if err := Send(st, a, "ask", kinds); err != nil {
		t.Fatal(err)
	}
	notes := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		turn, err := Wait(a, kinds, WaitOptions{Timeout: 10 * time.Second, Note: func(asking bool, q string) {
			if asking {
				notes <- Label(a) + " is waiting for you" + describe(q)
			}
		}})
		if err == nil && (turn == nil || turn.Reply != "echo: ask") {
			err = fmt.Errorf("turn = %+v", turn)
		}
		done <- err
	}()
	select {
	case n := <-notes:
		if !strings.Contains(n, "fake is waiting for you (needs permission)") {
			t.Errorf("note = %q", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never told it was waiting")
	}
	if err := Send(st, a, "answer", kinds); err == nil || !strings.Contains(err.Error(), "waiting for you") {
		t.Errorf("send while it waits: %v", err)
	}
	pane, _ := tmux.FindPane(panes(t), a.ID)
	if err := tmux.Run("send-keys", "-t", pane.ID, "Enter"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("told again: %q", <-notes)
	}

	// A turn reported done through an older mad, which keeps no reply.
	if err := Send(st, a, "drop", kinds); err != nil {
		t.Fatal(err)
	}
	status.WriteHook(a.ID, &status.Hook{State: status.Idle, Event: "Stop"}, time.Now())
	if _, err := Wait(a, kinds, WaitOptions{Timeout: 10 * time.Second}); err == nil || !strings.Contains(err.Error(), "keeps no replies") {
		t.Errorf("done without a reply kept: %v", err)
	}

	// A turn that ends without a word.
	if err := Send(st, a, "drop", kinds); err != nil {
		t.Fatal(err)
	}
	if _, err := Wait(a, kinds, WaitOptions{Timeout: 10 * time.Second}); !errors.Is(err, ErrNoReply) {
		t.Errorf("dropped turn: %v", err)
	}

	// An agent that exited is resumed by the next message. Its screen
	// stays still while it exits, which takes a while on a slow machine:
	// that must not pass for a turn that ended without a word.
	stale = 10 * time.Second
	if err := Send(st, a, "quit", kinds); err != nil {
		t.Fatal(err)
	}
	if _, err := Wait(a, kinds, WaitOptions{Timeout: 10 * time.Second}); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Errorf("wait on an agent that quit: %v", err)
	}
	if err := Send(st, a, "again", kinds); err != nil {
		t.Fatal(err)
	}
	reply("echo: again")
	if s := Statuses(st, kinds)[a.ID]; s != status.Idle {
		t.Errorf("status = %s", s)
	}

	t.Setenv("MAD_AGENT_ID", a.ID)
	if err := Send(st, a, "me", kinds); err == nil {
		t.Error("an agent sent to itself")
	}
}

// withWaiting gives kinds compiled waiting patterns, through agents.json
// as users would.
func withWaiting(t *testing.T, kinds []agent.Kind) []agent.Kind {
	t.Helper()
	data := fmt.Sprintf(`[{"name":"claude","start":%q,"resume":"","fork":"","hooks":true,"waiting":["Do you want to"]}]`, kinds[0].Start)
	if err := paths.WriteFileAtomic(paths.AgentsConfig(), []byte(data)); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func panes(t *testing.T) []tmux.Pane {
	t.Helper()
	ps, err := tmux.ListPanes()
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestFind(t *testing.T) {
	st := &state.State{}
	p1, _ := st.AddProject("/one")
	p2, _ := st.AddProject("/two")
	a := &state.Agent{ID: "aaaa1111-0000", Name: "impl"}
	b := &state.Agent{ID: "aaaa2222-0000", Name: "impl"}
	c := &state.Agent{ID: "cccc3333-0000", Name: "review"}
	p1.Agents = []*state.Agent{a, c}
	p2.Agents = []*state.Agent{b}

	cases := []struct {
		ref, dir string
		want     *state.Agent // nil: an error
	}{
		{"aaaa2222-0000", "/", b},
		{"cccc", "/", c},
		{"aaaa", "/", nil}, // two ids start so
		{"aaa", "/", nil},  // too short to be a prefix
		{"review", "/", c},
		{"impl", "/two", b}, // the project the command runs in wins
		{"impl", "/", nil},
		{"nobody", "/", nil},
		{"", "/", nil},
	}
	for _, c := range cases {
		_, got, err := Find(st, c.ref, c.dir)
		if c.want == nil && err == nil || c.want != nil && got != c.want {
			t.Errorf("Find(%q, %q) = %+v, %v", c.ref, c.dir, got, err)
		}
	}
}

func TestPending(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	turn := &status.Turn{At: at(10)}
	cases := []struct {
		name string
		turn *status.Turn
		hook *status.Hook
		sent time.Time
		want bool
	}{
		{"nothing ever", nil, nil, time.Time{}, false},
		{"sent, no turn yet", nil, nil, at(1), true},
		{"sent after the last turn", turn, &status.Hook{State: status.Idle, At: at(10)}, at(11), true},
		{"answered", turn, &status.Hook{State: status.Idle, At: at(10)}, at(9), false},
		{"typed in the deck", turn, &status.Hook{State: status.Running, At: at(12)}, at(9), true},
		{"asking after a typed message", turn, &status.Hook{State: status.Waiting, At: at(12)}, at(9), true},
		{"idle again later", turn, &status.Hook{State: status.Idle, At: at(12)}, at(9), false},
	}
	for _, c := range cases {
		if got := pending(c.turn, c.hook, c.sent); got != c.want {
			t.Errorf("%s: pending = %v", c.name, got)
		}
	}
}

func TestDraft(t *testing.T) {
	for screen, want := range map[string]string{
		// claude: empty with its placeholder, typed, empty after a reply
		"\x1b[39m❯ \x1b[2mTry \"fix typecheck errors\"\x1b[0m": "",
		"\x1b[39m❯ my draft": "my draft",
		"\x1b[38;5;239m\x1b[48;5;237m❯ \x1b[38;5;231mReply ok\x1b[39m\n\x1b[38;5;246m❯ \x1b[39m": "",
		// codex: its placeholder, typed; a message sent above the prompt
		"\x1b[1m›\x1b[0m \x1b[2mAsk Codex to do anything\x1b[0m":  "",
		"› Round 1: reply\n…\n\x1b[1m›\x1b[0m half a thought":     "half a thought",
		"› sent earlier\n\x1b[1m›\x1b[0m \x1b[2mAsk Codex\x1b[0m": "",
		"no prompt here": "",
	} {
		if got := Draft(screen); got != want {
			t.Errorf("Draft(%q) = %q, want %q", screen, got, want)
		}
	}
}

func TestTook(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	kinds := []agent.Kind{{Name: "claude", Hooks: true}, {Name: "codex"}}
	claude, codex := &state.Agent{ID: "a", Kind: "claude"}, &state.Agent{ID: "b", Kind: "codex"}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	step := Digest("[mad run x · design] write it")
	if Digest("  [mad run x · design] write it\n") != step || Digest("other") == step {
		t.Fatal("a digest should stand for the trimmed text alone")
	}
	mark := func(a *state.Agent, at time.Time, d string) {
		if err := status.MarkSent(a.ID, at, d); err != nil {
			t.Fatal(err)
		}
	}
	hook := func(at time.Time, st, event string) {
		if err := status.WriteHook(claude.ID, &status.Hook{State: st, Event: event}, at); err != nil {
			t.Fatal(err)
		}
	}

	if Took(claude, kinds, step, t0) || Took(claude, kinds, "", t0) {
		t.Error("took with nothing sent")
	}
	mark(claude, t0.Add(time.Second), step)
	if Took(claude, kinds, step, t0) {
		t.Error("took before it said it did")
	}
	hook(t0.Add(2*time.Second), status.Idle, "SessionStart") // restarted under the paste
	if Took(claude, kinds, step, t0) {
		t.Error("an agent that only started took the message")
	}
	hook(t0.Add(3*time.Second), status.Running, "UserPromptSubmit")
	if !Took(claude, kinds, step, t0) {
		t.Error("an agent at work on it did not take the message")
	}
	if Took(claude, kinds, step, t0.Add(2*time.Second)) {
		t.Error("a message sent before since was taken")
	}
	if Took(claude, kinds, Digest("other"), t0) {
		t.Error("took a message that was not the last one")
	}
	hook(t0.Add(4*time.Second), status.Idle, "Stop")
	if err := status.WriteTurn(claude.ID, &status.Turn{Reply: "done"}, t0.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !Took(claude, kinds, step, t0) {
		t.Error("an agent done with it did not take the message")
	}
	mark(claude, t0.Add(5*time.Second), Digest("/compact")) // something sent since
	if Took(claude, kinds, step, t0) {
		t.Error("took a message followed by another")
	}

	// Nothing tells more of an agent without hooks than the paste.
	mark(codex, t0.Add(time.Second), step)
	if !Took(codex, kinds, step, t0) {
		t.Error("codex did not take the message typed in")
	}
}

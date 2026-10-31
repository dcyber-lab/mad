package attention

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// The fixtures in testdata are panes of real agents captured as they
// asked (tmux capture-pane -p), claude Code 2.1 and codex 0.157.

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func labels(p prompt) []string {
	var out []string
	for _, o := range p.Options {
		out = append(out, o.Label)
	}
	return out
}

func TestParsePrompt(t *testing.T) {
	for _, c := range []struct {
		file, question, rest string
		options              []string
		numbers              []int
	}{
		// All it says, the command it asks about with it; claude's dashed
		// lines around the command are left out.
		{file: "claude-bash.txt", question: "Do you want to proceed?",
			rest: "Bash command\nTip: auto mode handles these prompts for you — choose \"switch to auto mode\" below\nWrite hello-b3k to out1.txt and print it\necho hello-b3k > out1.txt && cat out1.txt",
			options: []string{"Yes", "Yes, and always allow access to /private/tmp/b3k/repo from this project",
				"Yes, and switch to auto mode · auto mode handles these prompts for you", "No"},
			numbers: []int{1, 2, 3, 4}},
		{file: "claude-write.txt", question: "Do you want to create notes.md?", rest: "Create file\nnotes.md\n1 hi",
			options: []string{"Yes", "Yes, and switch to accept edits (auto-approve file edits and common file commands) for this session", "No"},
			numbers: []int{1, 2, 3}},
		// "4. Type something." opens a field to type in: not offered.
		{file: "claude-ask.txt", question: "Which color do you prefer?", rest: "☐ Color",
			options: []string{"Red", "Green", "Blue", "Chat about this"},
			numbers: []int{1, 2, 3, 5}},
		{file: "codex-approval.txt", question: "Would you like to run the following command?",
			rest:    "Environment: local\nReason: Do you want to allow creating codex-file2.txt in the workspace?\n$ touch codex-file2.txt",
			options: []string{"Yes, proceed", "Yes, and don't ask again for commands that start with `touch codex-file2.txt`", "No, and tell Codex what to do differently"},
			numbers: []int{1, 2, 3}},
		// At the top of the screen, under a blank line: it begins there.
		{file: "codex-trust.txt", question: "Trust this folder? Codex can read, edit, and run files here, subject to your permission settings. Folder settings can run code automatically, even without a model request. Continue only if you",
			rest:    "Folder access\n/private/tmp/b3k/repo\ntrust these files. Your trust decision will be saved.",
			options: []string{"Trust and continue", "Quit"}, numbers: []int{1, 2}},
		{file: "codex-update.txt", question: "Update available · 0.157.1 → 0.166.0",
			rest:    "Release notes: https://github.com/openai/codex/releases/latest",
			options: []string{"Update now (runs `npm install -g @openai/codex`)", "Skip", "Skip until next version"}, numbers: []int{1, 2, 3}},
	} {
		p, ok := parsePrompt(fixture(t, c.file))
		if !ok {
			t.Errorf("%s: not read as a prompt", c.file)
			continue
		}
		var ns []int
		for _, o := range p.Options {
			ns = append(ns, o.N)
		}
		if p.Question != c.question || strings.Join(p.Rest, "\n") != c.rest || !reflect.DeepEqual(labels(p), c.options) || !reflect.DeepEqual(ns, c.numbers) || p.Cursor != 1 || p.ID == "" {
			t.Errorf("%s: %q %q %q %v cursor %d id %q", c.file, p.Question, p.Rest, labels(p), ns, p.Cursor, p.ID)
		}
	}
}

// What is not one numbered choice is not read: the item offers Open.
func TestParsePromptRefuses(t *testing.T) {
	for _, file := range []string{
		"claude-ask-two.txt",   // two questions in tabs, then Submit
		"claude-ask-multi.txt", // checkboxes, then Submit
		"codex-working.txt",    // working: "esc to interrupt", no options
		"claude-idle.txt",      // at its prompt, a survey line with y/n
	} {
		if p, ok := parsePrompt(fixture(t, file)); ok {
			t.Errorf("%s: read as %+v", file, p)
		}
	}
	if _, ok := parsePrompt("\nProceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"); !ok {
		t.Fatal("the least prompt is not read")
	}
	for name, screen := range map[string]string{
		"empty":        "",
		"no footer":    "\nProceed?\n❯ 1. Yes\n  2. No\n\nsome other line\nmore\nand more",
		"no cursor":    "\nProceed?\n  1. Yes\n  2. No\nEsc to cancel",
		"two cursors":  "\nProceed?\n❯ 1. Yes\n❯ 2. No\nEsc to cancel",
		"a gap":        "\nProceed?\n❯ 1. Yes\n  3. No\nEsc to cancel",
		"not from one": "\nProceed?\n❯ 2. Yes\n  3. No\nEsc to cancel",
		"one option":   "\nProceed?\n❯ 1. Yes\nEsc to cancel",
		"same words":   "\nProceed?\n❯ 1. Yes\n  2. Yes\nEsc to cancel",
		"nothing over": "\n❯ 1. Yes\n  2. No\nEsc to cancel",
		"text between": "\nProceed?\n❯ 1. Yes\nsomething else\n  2. No\nEsc to cancel",
		// Its top may be cut: what it lets through cannot be shown whole.
		"off the top": "rm -rf build &&\nProceed?\n❯ 1. Yes\n  2. No\nEsc to cancel",
		"too long":    "\n" + strings.Repeat("cat <<EOF\n", promptLines) + "Proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel",
	} {
		if p, ok := parsePrompt(screen); ok {
			t.Errorf("%s: read as %+v", name, p)
		}
	}
}

// The cursor moved in the pane is the same prompt; other words are not.
func TestPromptID(t *testing.T) {
	screen := fixture(t, "claude-bash.txt")
	p, _ := parsePrompt(screen)
	moved := strings.Replace(strings.Replace(screen, " ❯ 1. Yes", "   1. Yes", 1), "   2. Yes, and", " ❯ 2. Yes, and", 1)
	q, ok := parsePrompt(moved)
	if !ok || q.ID != p.ID || q.Cursor != 2 {
		t.Errorf("cursor moved: %v %q (was %q) cursor %d", ok, q.ID, p.ID, q.Cursor)
	}
	other, ok := parsePrompt(strings.Replace(screen, "\n echo hello-b3k", "\n rm -rf out1.txt; echo", 1))
	if !ok || other.ID == p.ID {
		t.Errorf("another command, same id %q", other.ID)
	}
	// What is under an option is its too: the rest of a wrapped label.
	wrapped := "\nRun it?\n› 1. Yes\n  2. Yes, and don't ask again for\n     `touch a`\n  3. No\nesc to cancel"
	a, _ := parsePrompt(wrapped)
	b, ok := parsePrompt(strings.Replace(wrapped, "`touch a`", "`rm -rf ~`", 1))
	if !ok || a.ID == b.ID || b.Options[1].Under != "`rm -rf ~`" {
		t.Errorf("another wrapped option, same id %q: %+v", a.ID, b.Options)
	}
}

// keyed is a desk whose one agent waits with screen on its pane, and what
// the route captures and sends.
type keyed struct {
	d      *Desk
	screen string // what capture returns now
	sent   []string
}

func newKeyed(t *testing.T, screen string, info transcript.Info) *keyed {
	t.Helper()
	k := &keyed{d: NewDesk(), screen: screen}
	oldCap, oldSend, oldSettle := capture, sendKey, keySettle
	t.Cleanup(func() { capture, sendKey, keySettle = oldCap, oldSend, oldSettle })
	keySettle = 0
	capture = func(pane string) string {
		if pane != "%7" {
			t.Errorf("captured %s", pane)
		}
		return k.screen
	}
	sendKey = func(pane, key string) error {
		k.sent = append(k.sent, pane+" "+key)
		return nil
	}
	k.look(info)
	return k
}

func (k *keyed) look(info transcript.Info) View {
	at := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	a := Agent{ID: "tk", Label: "tk", PaneID: "%7", Status: status.Waiting, Shown: status.Waiting, Since: at, Info: info,
		Report: status.Report{Hook: &status.Hook{State: status.Waiting, At: at}}, Screen: k.screen}
	return k.d.Look(Facts{Agents: []Agent{a}, Now: at.Add(time.Minute)})
}

func (k *keyed) item(t *testing.T) Item {
	t.Helper()
	v := k.d.View()
	if len(v.Inbox.Items) != 1 {
		t.Fatalf("items %+v", v.Inbox.Items)
	}
	return v.Inbox.Items[0]
}

func actList(it Item) []string {
	var got []string
	for _, a := range it.Acts {
		s := string(a.Verb) + ":" + a.Label
		if a.Option != "" && a.Option != a.Label {
			s += " =" + a.Option
		}
		if a.Row {
			s += " row"
		}
		if a.Primary {
			s += " primary"
		}
		got = append(got, s)
	}
	return got
}

func TestKeysRouteActs(t *testing.T) {
	// The transcript's line for a command is claude's description of it,
	// cut: the item says what the screen does, the command whole.
	k := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{Tool: "Bash · Write hello-b3k to out1.txt and print it"})
	it := k.item(t)
	if _, ok := it.route.(keysRoute); !ok {
		t.Fatalf("route %T", it.route)
	}
	// What lets it through is Allow, which a row offers only as Review.
	want := []string{"allow:Yes row primary", "allow:Yes, and always allow access to /private/tmp/b3k/repo from this project",
		"allow:Yes, and switch to auto mode · auto mode handles these prompts for you", "answer:No row", "deny:Cancel (Esc)"}
	if got := actList(it); !reflect.DeepEqual(got, want) {
		t.Errorf("acts\n%q\nwant\n%q", got, want)
	}
	body := "Bash command\nTip: auto mode handles these prompts for you — choose \"switch to auto mode\" below\nWrite hello-b3k to out1.txt and print it\necho hello-b3k > out1.txt && cat out1.txt"
	if it.Kind != KindPermission || it.Context != "Bash" || it.Text != "Do you want to proceed?" || it.Body != body || !it.Full || it.Mono ||
		!strings.HasSuffix(string(it.Key), "/"+it.route.(keysRoute).p.ID) {
		t.Errorf("item %q %q %q %q full %v mono %v", it.Context, it.Text, it.Body, it.Key, it.Full, it.Mono)
	}

	// codex's: its "No, and tell Codex…" says no; an option the pane
	// wrapped is offered with all of it.
	c := newKeyed(t, strings.Replace(fixture(t, "codex-approval.txt"), "start with `touch codex-file2.txt` (p)", "start with\n     `touch codex-file2.txt` (p)", 1), transcript.Info{})
	want = []string{"allow:Yes, proceed row primary", "allow:Yes, and don't ask again for commands that start with `touch codex-file2.txt` =Yes, and don't ask again for commands that start with",
		"answer:No, and tell Codex what to do differently", "deny:Cancel (Esc)"}
	if got := actList(c.item(t)); !reflect.DeepEqual(got, want) {
		t.Errorf("codex acts\n%q\nwant\n%q", got, want)
	}

	// A question: its words from the screen, its options answers.
	q := newKeyed(t, fixture(t, "claude-ask.txt"), transcript.Info{Tool: "AskUserQuestion"})
	it = q.item(t)
	want = []string{"answer:Red row primary", "answer:Green row", "answer:Blue", "answer:Chat about this", "deny:Cancel (Esc)"}
	if got := actList(it); it.Kind != KindQuestion || it.Text != "Which color do you prefer?" || !reflect.DeepEqual(got, want) {
		t.Errorf("question %q %q %q", it.Kind, it.Text, got)
	}
}

func TestKeysRouteAnswers(t *testing.T) {
	k := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{})
	it := k.item(t)
	if err := k.d.Act(it.Key, Answer{Verb: VerbAnswer, Option: "No"}); err != nil {
		t.Fatal(err)
	}
	// The same prompt shows still, its cursor not on No: no Enter, which
	// would pick what the cursor is on.
	if !reflect.DeepEqual(k.sent, []string{"%7 4"}) {
		t.Errorf("sent %q", k.sent)
	}
	// Once per key: the second tap sends nothing.
	k.sent = nil
	if err := k.d.Act(it.Key, Answer{Verb: VerbAnswer, Option: "No"}); !errors.Is(err, ErrGone) || k.sent != nil {
		t.Errorf("second act: %v, sent %q", err, k.sent)
	}

	// The option under the cursor: Enter, one key, never a digit and an
	// Enter that could reach what follows.
	y := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{})
	if err := y.d.Act(y.item(t).Key, Answer{Verb: VerbAllow, Option: "Yes"}); err != nil || !reflect.DeepEqual(y.sent, []string{"%7 Enter"}) {
		t.Errorf("yes: %v, sent %q", err, y.sent)
	}

	// The digit is seen to have only moved the cursor to it (codex's trust
	// screen): Enter picks it.
	tr := newKeyed(t, fixture(t, "codex-trust.txt"), transcript.Info{})
	sendKey = func(pane, key string) error {
		tr.sent = append(tr.sent, pane+" "+key)
		if key == "2" {
			tr.screen = strings.Replace(strings.Replace(tr.screen, "› 1. Trust", "  1. Trust", 1), "  2. Quit", "› 2. Quit", 1)
		}
		return nil
	}
	if err := tr.d.Act(tr.item(t).Key, Answer{Verb: VerbAllow, Option: "Quit"}); err != nil || !reflect.DeepEqual(tr.sent, []string{"%7 2", "%7 Enter"}) {
		t.Errorf("trust: %v, sent %q", err, tr.sent)
	}

	// Esc.
	e := newKeyed(t, fixture(t, "codex-approval.txt"), transcript.Info{})
	if err := e.d.Act(e.item(t).Key, Answer{Verb: VerbDeny}); err != nil || !reflect.DeepEqual(e.sent, []string{"%7 Escape"}) {
		t.Errorf("esc: %v, sent %q", err, e.sent)
	}
}

// The digit took: the prompt is gone from the screen, and no Enter goes
// after it to whatever comes next.
func TestKeysRouteNoEnterOnceTaken(t *testing.T) {
	k := newKeyed(t, fixture(t, "codex-approval.txt"), transcript.Info{})
	it := k.item(t)
	sendKey = func(pane, key string) error {
		k.sent = append(k.sent, pane+" "+key)
		k.screen = fixture(t, "codex-working.txt")
		return nil
	}
	if err := k.d.Act(it.Key, Answer{Verb: VerbAnswer, Option: "No, and tell Codex what to do differently"}); err != nil || !reflect.DeepEqual(k.sent, []string{"%7 3"}) {
		t.Errorf("%v, sent %q", err, k.sent)
	}
}

// The prompt changed between the look and the act (answered in the pane,
// or another one): ErrGone, nothing sent.
func TestKeysRouteStale(t *testing.T) {
	for name, now := range map[string]string{
		"answered":      "",
		"another":       strings.Replace(fixture(t, "claude-bash.txt"), "\n echo hello-b3k", "\n rm -rf out1.txt; echo", 1),
		"another kind":  fixture(t, "claude-write.txt"),
		"not a prompt":  fixture(t, "claude-ask-two.txt"),
		"working again": fixture(t, "codex-working.txt"),
	} {
		k := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{})
		it := k.item(t)
		k.screen = now
		if err := k.d.Act(it.Key, Answer{Verb: VerbAllow, Option: "Yes"}); !errors.Is(err, ErrGone) || k.sent != nil {
			t.Errorf("%s: %v, sent %q", name, err, k.sent)
		}
		// Gone stays hidden while listed; the next look with the changed
		// screen is a new item, not the old one with new options.
		v := k.look(transcript.Info{})
		for _, x := range v.Inbox.Items {
			if x.Key == it.Key {
				t.Errorf("%s: the stale item shows again", name)
			}
		}
	}
	// Likewise an act on the old prompt's key after a look saw the new one.
	k := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{})
	old := k.item(t)
	k.screen = fixture(t, "claude-write.txt")
	k.look(transcript.Info{})
	if err := k.d.Act(old.Key, Answer{Verb: VerbAllow, Option: "Yes"}); !errors.Is(err, ErrGone) || k.sent != nil {
		t.Errorf("old key: %v, sent %q", err, k.sent)
	}
}

// Only an offered option is sent, and only a key on the allow-list.
func TestKeysRouteAllowList(t *testing.T) {
	k := newKeyed(t, fixture(t, "claude-bash.txt"), transcript.Info{})
	it := k.item(t)
	for _, a := range []Answer{
		{Verb: VerbAnswer, Option: "Maybe"},
		{Verb: VerbAnswer, Option: "1"},
		{Verb: VerbAnswer, Text: "rm -rf /"},
		{Verb: VerbAllow},
		{Verb: VerbAlways},
		{Verb: VerbDeny, Text: "no"},
		{Verb: VerbReply, Text: "hi"},
		{Verb: VerbOpen},
	} {
		if err := k.d.Act(it.Key, a); err == nil || k.sent != nil {
			t.Errorf("%+v: %v, sent %q", a, err, k.sent)
		}
	}
	// A crafted option on the screen itself is still only its digit.
	crafted := "\nRun it?\n❯ 1. Enter\n  2. C-c\n  3. ; rm -rf ~\nEsc to cancel"
	c := newKeyed(t, crafted, transcript.Info{})
	if err := c.d.Act(c.item(t).Key, Answer{Verb: VerbAllow, Option: "; rm -rf ~"}); err != nil || !reflect.DeepEqual(c.sent, []string{"%7 3"}) {
		t.Errorf("crafted: %v, sent %q", err, c.sent)
	}
	// press refuses anything else, whoever calls it.
	var sent []string
	sendKey = func(pane, key string) error { sent = append(sent, key); return nil }
	for _, x := range [][2]string{{"%7", "0"}, {"%7", "10"}, {"%7", "y"}, {"%7", "C-c"}, {"%7", "Enter Enter"}, {"%7", "rm -rf ~"}, {"main", "1"}, {"", "1"}, {"=mad:0", "Escape"}} {
		if err := press(x[0], x[1]); err == nil {
			t.Errorf("press %q %q went", x[0], x[1])
		}
	}
	if sent != nil {
		t.Errorf("sent %q", sent)
	}
}

// A screen that cannot be read keeps Open only.
func TestKeysRouteUnreadable(t *testing.T) {
	k := newKeyed(t, fixture(t, "claude-ask-two.txt"), transcript.Info{Tool: "AskUserQuestion"})
	it := k.item(t)
	if _, ok := it.route.(openRoute); !ok || len(it.Acts) != 1 || it.Acts[0].Verb != VerbOpen {
		t.Errorf("route %T acts %+v", it.route, it.Acts)
	}
	if strings.Count(string(it.Key), "/") != 1 {
		t.Errorf("key %q", it.Key)
	}
	// No pane: nothing to send keys to.
	a := Agent{ID: "x", Label: "x", Status: status.Waiting, Screen: fixture(t, "claude-bash.txt")}
	v := NewDesk().Look(Facts{Agents: []Agent{a}, Now: time.Now()})
	if _, ok := v.Inbox.Items[0].route.(openRoute); !ok {
		t.Errorf("no pane: %T", v.Inbox.Items[0].route)
	}
}

// Esc goes to a TUI only while it works: its screen says Esc interrupts
// it, or its tracker says it runs; never onto a prompt.
func TestInterruptPane(t *testing.T) {
	k := newKeyed(t, "", transcript.Info{})
	for _, c := range []struct {
		name, screen string
		working      bool
		sent         []string
	}{
		{"codex says so", fixture(t, "codex-working.txt"), false, []string{"%7 Escape"}},
		// claude 2.1.312 no longer says "esc to interrupt" while it works
		{"claude runs", fixture(t, "claude-working.txt"), true, []string{"%7 Escape"}},
		{"claude, not running", fixture(t, "claude-working.txt"), false, nil},
		{"idle", fixture(t, "claude-idle.txt"), false, nil},
		{"asking", fixture(t, "claude-bash.txt"), true, nil},
		{"said long ago", "press esc to interrupt it\n" + strings.Repeat("a line\n", statusLines) + "❯ ", false, nil},
	} {
		k.screen, k.sent = c.screen, nil
		err := InterruptPane("%7", c.working)
		if !reflect.DeepEqual(k.sent, c.sent) || (c.sent == nil) != errors.Is(err, ErrGone) {
			t.Errorf("%s: %v, sent %q", c.name, err, k.sent)
		}
	}
}

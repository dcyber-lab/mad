package attention

import (
	"errors"
	"testing"

	"github.com/dcyber-lab/mad/internal/transcript"
)

// A live claude 2.1.312 made a Bash call and an AskUserQuestion in one
// message: its screen asks leave for the command, its transcript's last
// tool call is the question. The screen says what it asks: Allow, beside
// all of the prompt, never an answer one tap away on a row.
func TestKeysRouteKindFromScreen(t *testing.T) {
	k := newKeyed(t, fixture(t, "claude-bash-parallel.txt"), transcript.Info{Tool: "AskUserQuestion"})
	it := k.item(t)
	yes := false
	for _, a := range it.Acts {
		if a.Label == "Yes" {
			yes = true
			if a.Verb != VerbAllow || a.Row {
				t.Errorf("the command's Yes is %s on a row %v, not Allow beside the whole prompt", a.Verb, a.Row)
			}
		}
	}
	if !yes || it.Kind != KindPermission || it.Context != "needs you" || !it.Full {
		t.Errorf("kind %s context %q full %v acts %q", it.Kind, it.Context, it.Full, actList(it))
	}
	if err := k.d.Act(it.Key, Answer{Verb: VerbAnswer, Option: "Yes"}); !errors.Is(err, ErrNotOffered) || k.sent != nil {
		t.Errorf("answer Yes: %v, sent %q", err, k.sent)
	}

	// The question, once on the screen, is a question.
	q := newKeyed(t, fixture(t, "claude-ask.txt"), transcript.Info{Tool: "AskUserQuestion"})
	if it := q.item(t); it.Kind != KindQuestion || it.Acts[0].Verb != VerbAnswer || !it.Acts[0].Row {
		t.Errorf("question: kind %s acts %q", it.Kind, actList(it))
	}
	// One its transcript does not tell of (not yet read, or several tools
	// at once) is offered as leave: Review first, never a guess.
	for _, info := range []transcript.Info{{}, {Tool: "Bash · echo parallel > par.txt"}} {
		n := newKeyed(t, fixture(t, "claude-ask.txt"), info)
		it := n.item(t)
		for _, a := range it.Acts[:len(it.Acts)-1] {
			if a.Verb != VerbAllow || a.Row {
				t.Errorf("transcript %q: kind %s acts %q", info.Tool, it.Kind, actList(it))
				break
			}
		}
	}
}

// What tells a question is under the prompt and over it, where a command
// cannot reach: its footer, and its header's box.
func TestPromptQuestion(t *testing.T) {
	for file, want := range map[string]bool{
		"claude-ask.txt": true, "claude-bash.txt": false, "claude-bash-parallel.txt": false, "claude-write.txt": false,
		"codex-approval.txt": false, "codex-trust.txt": false,
	} {
		p, ok := parsePrompt(fixture(t, file))
		if !ok || p.question() != want {
			t.Errorf("%s: read %v, question %v", file, ok, p.question())
		}
	}
	// A box in the command it asks leave for is not a question's.
	crafted := "\n──────────────\nBash command\n☐ Color\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel · Tab to amend"
	if p, ok := parsePrompt(crafted); !ok || p.question() {
		t.Errorf("crafted: read %v, question %v", ok, p.question())
	}
}

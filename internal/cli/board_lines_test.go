package cli

import (
	"strings"
	"testing"
)

// A task note with several lines is one line under "Waiting
// for you": the list's rows are items, not text.
func TestBoardWaitingLinesAreItems(t *testing.T) {
	arch, _ := boardDeck(t)
	mcpCall(t, arch, "task_set", `{"title":"pick a name","owner":"you","note":"short list:\n- alpha\n- beta"}`)
	code, out, errs := run(t, "", "board")
	if code != 0 {
		t.Fatalf("board = %d %q", code, errs)
	}
	var waiting []string
	in := false
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(l, " ") {
			in = l == "Waiting for you"
			continue
		}
		if in {
			waiting = append(waiting, l)
		}
	}
	if len(waiting) != 1 {
		t.Errorf("one item, one line %q\n%s", waiting, out)
	}
	if strings.Contains(out, "- alpha") {
		t.Errorf("the note's list leaked into the rows:\n%s", out)
	}
}

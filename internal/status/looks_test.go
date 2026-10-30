package status

import "testing"

func TestShown(t *testing.T) {
	for _, c := range []struct {
		raw  string
		done bool
		want string
	}{
		{Running, false, Running},
		{Running, true, Running},
		{Waiting, true, Waiting},
		{Idle, false, Idle},
		{Idle, true, Finished},
		{Asleep, true, Finished},
		{Asleep, false, Asleep},
		{"off", false, Asleep},
		{Exited, false, Stopped},
		{Exited, true, Stopped},
		{Stopped, false, Stopped},
		{Starting, false, Starting},
		{"no such", false, Stopped},
	} {
		if got := Shown(c.raw, c.done); got != c.want {
			t.Errorf("Shown(%q, %v) = %q, want %q", c.raw, c.done, got, c.want)
		}
	}
}

func TestLooksAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range Looks {
		if l.Word == "" || l.Meaning == "" || l.Glyph == "" || l.Tone == "" {
			t.Errorf("%s: incomplete look %+v", l.State, l)
		}
		if seen[l.Word] {
			t.Errorf("word %q used twice", l.Word)
		}
		seen[l.Word] = true
		if LookOf(l.State).State != l.State {
			t.Errorf("LookOf(%q) is not itself", l.State)
		}
	}
	if w := LookOf(Finished).Word; w != "Finished" {
		t.Errorf("finished reads %q", w)
	}
}

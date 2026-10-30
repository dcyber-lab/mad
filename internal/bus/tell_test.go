package bus

import (
	"strings"
	"testing"
)

// told is what tell was given, in the tests.
var told []string

// The person is told what is for them: a DM to them, an @you, a channel
// that waits for them; not their own posts, nor what is for agents.
func TestTell(t *testing.T) {
	setup(t)
	if _, err := Create("ops", "", []string{idArch, idBuild}, me); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", me, "@architect please look at the deploy", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", arch, "@builder the deploy failed on step 3", 0); err != nil {
		t.Fatal(err)
	}
	if len(told) != 0 {
		t.Fatalf("told %q", told)
	}
	if _, err := Post("ops", arch, "@you the deploy needs your approval", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DM(build, You, "the tests pass now, merging when you agree"); err != nil {
		t.Fatal(err)
	}
	if len(told) != 2 || !strings.HasPrefix(told[0], "ops architect: @you") || !strings.Contains(told[1], "builder: the tests pass") {
		t.Errorf("told %q", told)
	}
	// Agents going on alone until the channel pauses.
	told = nil
	for i := 0; i < 40 && len(told) == 0; i++ {
		from := arch
		if i%3 == 1 {
			from = build
		}
		_, _ = Post("ops", from, "a long enough line number "+strings.Repeat("x", i%5)+" for the channel", 0)
	}
	if len(told) != 1 || !strings.Contains(told[0], "#ops is paused") {
		t.Errorf("paused: told %q", told)
	}
}

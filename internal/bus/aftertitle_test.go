package bus

import (
	"strings"
	"testing"
)

// An ask card's body is what its title does not already say.
func TestAfterTitle(t *testing.T) {
	long := strings.Repeat("word ", 30) + "end"
	title := firstLine(long, titleRunes)
	if !strings.HasSuffix(title, "…") {
		t.Fatalf("title %q was not cut", title)
	}
	for _, c := range []struct{ text, want string }{
		{"@you may I drop it?", ""},
		{"@you which one?\nA: postgres\nB: sqlite", "A: postgres\nB: sqlite"},
		{long, strings.TrimSpace(strings.TrimPrefix(long, strings.TrimSuffix(title, "…")))},
	} {
		got := afterTitle(c.text, firstLine(c.text, titleRunes))
		if got != c.want {
			t.Errorf("afterTitle(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// An asker's own trailing ellipsis is not a cut: the body is what follows
// the line, not a stray "…".
func TestAfterTitleOwnEllipsis(t *testing.T) {
	for text, want := range map[string]string{
		"@you should I wait…\nA: yes\nB: no": "A: yes\nB: no",
		"@you which one…":                    "",
	} {
		if got := afterTitle(text, firstLine(text, titleRunes)); got != want {
			t.Errorf("afterTitle(%q) = %q, want %q", text, got, want)
		}
	}
}

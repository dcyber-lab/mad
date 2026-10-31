package desktop

import (
	"regexp"
	"testing"
)

// An id is one element's: a second with the same id makes the page's
// lookups by id reach the wrong button.
func TestIndexIDsUnique(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllSubmatch(b, -1) {
		if id := string(m[1]); seen[id] {
			t.Errorf("id %q appears twice in index.html", id)
		} else {
			seen[id] = true
		}
	}
	if len(seen) < 10 {
		t.Errorf("found only %d ids: the pattern is off", len(seen))
	}
}

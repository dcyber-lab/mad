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

// The apps open the page on a view they have no native screen for with #open=<view> (and #agent=<id>):
// every view the apps ask for must be one the page knows.
func TestPageOpensTheViewsTheAppsAskFor(t *testing.T) {
	b, err := webFS.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"usage", "board", "skills", "phone", "notify", "addproject", "theme", "newchat", "newchannel", "newteam"} {
		if !regexp.MustCompile(`\b` + view + `: \(\) =>`).Match(b) {
			t.Errorf("app.js does not open #open=%s", view)
		}
	}
	// #open=__proto__, valueOf and hasOwnProperty are names an object has, not views
	if !regexp.MustCompile(`Object\.hasOwn\(views, appAsks\.view\)`).Match(b) {
		t.Error("app.js looks #open= up in views without checking it is an own key")
	}
}

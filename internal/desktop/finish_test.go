package desktop

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
)

// The window's finish menu is the sidebar's (deck.FinishActions) for the
// agent's checkout: listed with a kind the page names in its own words, run
// by its place in the list, refused when the list moved since the page saw it.
func TestFinishAPI(t *testing.T) {
	isolate(t)
	repo := gitRepo(t, map[string]string{"a.txt": "one\n"})
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v %s", err, out)
	}
	st := &state.State{Projects: []*state.Project{{Name: "r", Path: repo, Agents: []*state.Agent{{ID: "a1", Kind: "claude"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	var seen deck.Checkout
	oldActs, oldRun := finishActions, finishRun
	t.Cleanup(func() { finishActions, finishRun = oldActs, oldRun })
	finishActions = func(c deck.Checkout) []deck.FinishAction {
		seen = c
		return []deck.FinishAction{{Name: "push & open PR", Command: "pr"}, {Name: "rebase onto main", Command: "rebase"}, {Name: "push", Command: "push"}, {Name: "deploy", Command: "mine"}}
	}
	var ran string
	finishRun = func(_ context.Context, dir, command string) (string, error) {
		ran = dir + " " + command
		return "a lot\nof lines\nhttps://example.com/pr/1\n", nil
	}
	s := New(manageKinds)

	w := call(t, s, "GET", "/api/finish?id=a1", "")
	var list struct {
		Branch, Base string
		Actions      []finishAct
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if list.Branch != "feature" || seen.Branch != "feature" || seen.Dir != repo || seen.Repo != repo {
		t.Errorf("the checkout: %+v, the list: %+v", seen, list)
	}
	kinds := []string{}
	for _, a := range list.Actions {
		kinds = append(kinds, a.Kind)
	}
	if want := []string{"pr", "rebase", "push", ""}; len(kinds) != 4 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] || kinds[3] != want[3] {
		t.Errorf("kinds %q, want %q", kinds, want)
	}

	w = call(t, s, "POST", "/api/finish?id=a1&i=2&name=push", "")
	var res struct {
		OK     bool
		Output string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || w.Code != 200 || !res.OK {
		t.Fatalf("run: %d %s", w.Code, w.Body)
	}
	if ran != repo+" push" {
		t.Errorf("ran %q", ran)
	}
	if w := call(t, s, "POST", "/api/finish?id=a1&i=1&name=push", ""); w.Code != 409 {
		t.Errorf("a name that is not the action's: %d, want 409", w.Code)
	}
	if w := call(t, s, "POST", "/api/finish?id=a1&i=9", ""); w.Code != 400 {
		t.Errorf("no such action: %d, want 400", w.Code)
	}
	if w := call(t, s, "GET", "/api/finish?id=nope", ""); w.Code != 404 {
		t.Errorf("no such agent: %d, want 404", w.Code)
	}
}

func TestArgModel(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--model", "opus"}, "opus"},
		{[]string{"--permission-mode", "auto", "--model=sonnet"}, "sonnet"},
		{[]string{"-m", "gpt-5"}, "gpt-5"},
		{[]string{"--model"}, ""},
	} {
		if got := argModel(c.args); got != c.want {
			t.Errorf("argModel(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestTail(t *testing.T) {
	if got := tail("short", 10); got != "short" {
		t.Errorf("tail of a short text: %q", got)
	}
	if got := tail("first line\nsecond\nthird", 12); got != "third" {
		t.Errorf("tail cut at a line: %q", got)
	}
}

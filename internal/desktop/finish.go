package desktop

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/desktop/api"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/state"
)

// Finishing a branch from the window: the same menu as the sidebar's f
// (deck.FinishActions: push, push and open a pull request, rebase onto or
// merge into the default branch, or the user's own commands). GET lists
// what applies to an agent's checkout; POST runs one and answers what it
// printed, so the page can say how it went (a PR's link, a conflict).

// finishTimeout bounds one finish command: a push or `gh pr create` that
// takes longer than this is waiting on something the window cannot answer.
const finishTimeout = 3 * time.Minute

// finishCheckout is the checkout agent id works in, as a finish action
// sees it.
func finishCheckout(ctx context.Context, id string) (deck.Checkout, error) {
	st, err := state.Load()
	if err != nil {
		return deck.Checkout{}, err
	}
	p, a := st.FindAgent(id)
	if a == nil {
		return deck.Checkout{}, errNoAgent
	}
	c := deck.Checkout{Dir: p.Dir(a), Repo: p.Path}
	branch := func(dir string) string {
		out, err := gitOut(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return ""
		}
		b := strings.TrimSpace(string(out))
		if b == "HEAD" { // detached
			return ""
		}
		return b
	}
	if c.Branch = branch(c.Dir); c.Branch == "" {
		return c, errors.New("not on a branch")
	}
	c.RepoBranch = branch(c.Repo)
	c.Base = git.DefaultBranch(c.Repo)
	return c, nil
}

var errNoAgent = errors.New("no such agent")

// finishActions is replaceable in tests.
var finishActions = func(c deck.Checkout) []deck.FinishAction {
	cfg, _ := deck.LoadConfig()
	return deck.FinishActions(cfg.Finish, c)
}

// finishRun runs command through sh in dir; replaceable in tests.
var finishRun = func(ctx context.Context, dir, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	// Nothing can be typed in: a prompt (an editor, a password) fails
	// rather than waits.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GH_PROMPT_DISABLED=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type finishAct = api.FinishAction

// finishKind tells which built-in action name is.
func finishKind(name string) string {
	switch {
	case name == "push":
		return "push"
	case name == "push & open PR":
		return "pr"
	case strings.HasPrefix(name, "rebase onto "):
		return "rebase"
	case strings.HasPrefix(name, "merge into "):
		return "merge"
	}
	return ""
}

func (s *Server) finish(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), gitTimeout)
	defer cancel()
	id := r.URL.Query().Get("id")
	c, err := finishCheckout(ctx, id)
	if errors.Is(err, errNoAgent) {
		fail(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		reply(w, api.FinishFailed{Actions: []finishAct{}, Error: err.Error()})
		return
	}
	acts := finishActions(c)
	if r.Method == http.MethodGet {
		list := make([]finishAct, len(acts))
		for i, a := range acts {
			list[i] = finishAct{Name: a.Name, Kind: finishKind(a.Name)}
		}
		reply(w, api.FinishMenu{Actions: list, Base: c.Base, Branch: c.Branch})
		return
	}
	i, err := strconv.Atoi(r.URL.Query().Get("i"))
	if err != nil || i < 0 || i >= len(acts) {
		fail(w, http.StatusBadRequest, errors.New("no such action"))
		return
	}
	// The name must still be the one the page showed: the menu is built
	// again here, and a branch that changed since may offer other entries.
	if name := r.URL.Query().Get("name"); name != "" && name != acts[i].Name {
		fail(w, http.StatusConflict, errors.New("the actions changed: look again"))
		return
	}
	rctx, rcancel := context.WithTimeout(context.Background(), finishTimeout)
	defer rcancel()
	out, err := finishRun(rctx, c.Dir, acts[i].Command)
	res := api.FinishResult{OK: err == nil, Output: tail(out, 4000)}
	if err != nil {
		res.Error = err.Error()
	}
	reply(w, res)
}

// tail is the last n bytes of s, cut at a line.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

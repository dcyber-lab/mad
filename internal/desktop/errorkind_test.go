package desktop

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/desktop/api"
	"github.com/dcyber-lab/mad/internal/state"
)

// A 403 says which kind of refusal it is: a token mad does not know, which
// pairing again puts right, or a thing a good token may not do, which it
// does not. A client clears what it kept of the Mac on the first, so the
// two must not be taken for each other.
func TestARefusalSaysItsKind(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "alice"}, {ID: a2, Kind: "codex", Name: "bob"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(bus.From{ID: a1, Label: "alice", Kind: "claude"}, a2, "between the two of us, bob"); err != nil {
		t.Fatal(err)
	}
	s := New(agent.Builtin())
	h := s.Handler()
	const devTok = "00112233445566778899aabbccddeeff"
	pairAs(t, "mine", devTok)

	for _, c := range []struct {
		what, method, url, token, body string
		header                         [2]string // one more header, when set
		kind                           string
	}{
		{"no token", "GET", "/api/deck", "", "", [2]string{}, api.ErrToken},
		{"a token mad does not know", "GET", "/api/deck", strings.Repeat("9", 32), "", [2]string{}, api.ErrToken},
		{"the master token through a tunnel", "GET", "/api/deck", s.Token, "", [2]string{"Cf-Ray", "x"}, api.ErrToken},
		{"a token in the URL through a tunnel", "GET", "/api/deck?token=" + devTok, "", "", [2]string{"Cf-Ray", "x"}, api.ErrToken},
		{"a window code asked for by a device", "POST", "/api/window/code", devTok, "", [2]string{}, api.ErrDenied},
		{"a window code that is not one", "POST", "/api/window/token", "", `{"code":"nope"}`, [2]string{}, api.ErrDenied},
		{"a pairing code that is not one", "POST", "/api/pair", "", `{"code":"` + strings.Repeat("0", 32) + `","name":"p","token":"` + strings.Repeat("1", 32) + `"}`, [2]string{}, api.ErrDenied},
		{"a post in a DM between two agents", "POST", "/api/bus/post", s.Token, `{"channel":"` + bus.DMChannel(a1, a2) + `","text":"may I join in here?"}`, [2]string{}, api.ErrDenied},
	} {
		r := httptest.NewRequest(c.method, c.url, strings.NewReader(c.body))
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		if c.header[0] != "" {
			r.Header.Set(c.header[0], c.header[1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Header().Get(api.ErrorHeader); w.Code != 403 || got != c.kind {
			t.Errorf("%s: %d, %s %q, want 403 and %q (%s)", c.what, w.Code, api.ErrorHeader, got, c.kind, strings.TrimSpace(w.Body.String()))
		}
	}
	// An answer that is none says nothing of the kind.
	if w := do(h, "GET", "/api/deck", s.Token, ""); w.Code != 200 || w.Header().Get(api.ErrorHeader) != "" {
		t.Errorf("the deck: %d, %s %q", w.Code, api.ErrorHeader, w.Header().Get(api.ErrorHeader))
	}
}

// Every 403 of the server is answered by refuse, or by fail, which hands
// it to refuse: one written out by hand would say nothing of its kind,
// and a client would take it for what an older mad means by that.
func TestEvery403GoesThroughRefuse(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, "StatusForbidden") {
				continue
			}
			switch l := strings.TrimSpace(line); {
			case strings.HasPrefix(l, "fail(w, http.StatusForbidden, "):
			case f == "manage.go" && (l == "if code == http.StatusForbidden {" || l == "http.Error(w, text, http.StatusForbidden)"):
			default:
				t.Errorf("%s:%d answers 403 by hand: use refuse, which says the kind\n\t%s", f, i+1, l)
			}
		}
	}
}

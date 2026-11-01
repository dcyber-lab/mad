package desktop

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// The server names its build in X-Mad-Build on /api/deck and in the page's meta tag; with no build
// it says nothing, and the rest of the page is served as it is.
func TestBuildIsServed(t *testing.T) {
	isolate(t)
	s := New(nil)
	s.Build = `v1.2.3-"x"`
	call := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if got := call("/api/deck?token=" + s.Token).Header().Get(BuildHeader); got != s.Build {
		t.Errorf("%s = %q, want %q", BuildHeader, got, s.Build)
	}
	for _, path := range []string{"/", "/index.html"} {
		w := call(path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<meta name="mad-build" content="v1.2.3-&#34;x&#34;">`) {
			t.Errorf("%s: %d, no build meta in the page", path, w.Code)
		}
	}
	if w := call("/app.js"); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "mad-build\" content=") {
		t.Errorf("/app.js: %d", w.Code)
	}

	s.Build = ""
	if got := call("/api/deck?token=" + s.Token).Header().Get(BuildHeader); got != "" {
		t.Errorf("no build, but %s = %q", BuildHeader, got)
	}
	if !strings.Contains(call("/").Body.String(), `<meta name="mad-build" content="">`) {
		t.Error("no build: the page's meta should stay empty")
	}
}

// The page's reload decision (web/stale.js) is checked by node. Where there is no node, it is skipped.
func TestStaleAction(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: web/stale.js is not checked")
	}
	out, err := exec.Command(node, "testdata/stale_check.js").CombinedOutput()
	if err != nil {
		t.Fatalf("stale_check.js: %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "ok "); n < 5 {
		t.Errorf("only %d of the 5 checks ran:\n%s", n, out)
	}
}

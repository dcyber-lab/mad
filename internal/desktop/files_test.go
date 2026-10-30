package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

func TestFilesAPI(t *testing.T) {
	isolate(t)
	repo := gitRepo(t, map[string]string{"README.md": "x", ".gitignore": "build/\n"})
	// Not committed, and not ignored but build/'s: listed all the same.
	for name, text := range map[string]string{
		"internal/desktop/files.go": "x", "internal/deck/deck.go": "x", "new.go": "x", "build/out.bin": "x",
	} {
		p := filepath.Join(repo, name)
		mustMkdir(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plain := t.TempDir()
	for _, name := range []string{"notes.txt", "sub/deep.txt", ".hidden/x.txt", "node_modules/m/i.js"} {
		p := filepath.Join(plain, name)
		mustMkdir(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := &state.State{Projects: []*state.Project{
		{Name: "r", Path: repo, Agents: []*state.Agent{{ID: "a1", Kind: "claude"}}},
		{Name: "p", Path: plain},
	}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	get := func(url string) string {
		t.Helper()
		w := call(t, s, "GET", url, "")
		var out []fileEntry
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s: %d %s", url, w.Code, w.Body)
		}
		var paths []string
		for _, e := range out {
			paths = append(paths, e.Path)
		}
		return strings.Join(paths, " ")
	}
	for _, c := range []struct{ url, want string }{
		// Nothing typed: the top of the tree. Ignored files are not there.
		{"/api/files?id=a1", "internal/ .gitignore new.go README.md"},
		// The name first, then a path with it, then its letters in order.
		{"/api/files?id=a1&q=deck", "internal/deck/ internal/deck/deck.go"},
		{"/api/files?id=a1&q=files", "internal/desktop/files.go"},
		{"/api/files?id=a1&q=intdesk", "internal/desktop/ internal/desktop/files.go"},
		{"/api/files?id=a1&q=README", "README.md"},
		// Not a repository: what is in it, but hidden and node_modules.
		{"/api/files?project=" + plain, "sub/ notes.txt"},
		{"/api/files?project=" + plain + "&q=deep", "sub/deep.txt"},
	} {
		if got := get(c.url); got != c.want {
			t.Errorf("%s = %q, want %q", c.url, got, c.want)
		}
	}
	if w := call(t, s, "GET", "/api/files?id=nope", ""); w.Code != 404 {
		t.Errorf("no such agent: %d", w.Code)
	}
}

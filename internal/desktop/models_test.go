package desktop

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
)

// A new chat's models: its kind's, asked in its project, else at home.
func TestModelsAPI(t *testing.T) {
	isolate(t)
	proj := t.TempDir()
	if err := (&state.State{Projects: []*state.Project{{Name: "p", Path: proj}}}).Save(); err != nil {
		t.Fatal(err)
	}
	asked := map[string]int{}
	findModels = func(_ context.Context, kind, dir string) ([]discover.Model, error) {
		asked[kind+" "+dir]++
		if kind == "shell" {
			return nil, nil
		}
		return []discover.Model{{Value: kind + "-1", Name: dir}}, nil
	}
	t.Cleanup(func() { findModels = discover.Models })
	s := New(manageKinds)
	get := func(url string) []discover.Model {
		t.Helper()
		w := call(t, s, "GET", url, "")
		var out []discover.Model
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s: %d %s", url, w.Code, w.Body)
		}
		return out
	}
	home, _ := os.UserHomeDir()
	if got := get("/api/models?kind=codex&project=" + proj); len(got) != 1 || got[0].Value != "codex-1" || got[0].Name != proj {
		t.Errorf("codex in its project: %+v", got)
	}
	if got := get("/api/models?kind=pi&project=/no/such"); len(got) != 1 || got[0].Name != home {
		t.Errorf("pi, no project: %+v", got)
	}
	// Kept: asked once.
	get("/api/models?kind=codex&project=" + proj)
	if n := asked["codex "+proj]; n != 1 {
		t.Errorf("codex asked %d times", n)
	}
	if got := get("/api/models?kind=shell"); got == nil || len(got) != 0 {
		t.Errorf("shell: %#v, want []", got)
	}
}

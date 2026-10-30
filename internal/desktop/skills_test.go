package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
)

func TestSkillsAPI(t *testing.T) {
	dir := isolate(t)
	project := mustMkdir(t, filepath.Join(dir, "work", "demo"))
	st := &state.State{Projects: []*state.Project{{Name: "demo", Path: project}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Save("alice", project, "project", "deploy", "When deploying", "ship it", false); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Save("alice", project, "user", "tidy", "When tidying", "sweep", false); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	type listing struct {
		Pending []struct {
			Name, Text, ProjectName string
		}
		Approved []struct {
			Name, Scope string
		}
	}
	get := func(url string) listing {
		t.Helper()
		w := call(t, s, "GET", url, "")
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", url, w.Code, w.Body)
		}
		var l listing
		if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := get("/api/skills")
	if len(l.Pending) != 2 || len(l.Approved) != 0 || !strings.Contains(l.Pending[0].Text, "name: ") {
		t.Fatalf("listing %+v", l)
	}
	if w := call(t, s, "POST", "/api/skills/approve", `{"name":"nope"}`); w.Code != 400 {
		t.Errorf("approve unknown: %d", w.Code)
	}
	if w := call(t, s, "POST", "/api/skills/approve", `{"name":"deploy"}`); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	edited, _ := json.Marshal(map[string]string{"name": "tidy", "body": "---\nname: tidy\ndescription: Tidy, edited\n---\n\nsweep twice\n"})
	if w := call(t, s, "POST", "/api/skills/approve", string(edited)); w.Code != 200 {
		t.Fatalf("approve edited: %d %s", w.Code, w.Body)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "tidy", "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "sweep twice") {
		t.Errorf("edited copy: %v %s", err, b)
	}
	l = get("/api/skills")
	if len(l.Pending) != 0 || len(l.Approved) != 2 {
		t.Fatalf("after approving %+v", l)
	}
	if l = get("/api/skills?project=demo"); len(l.Approved) != 2 {
		t.Errorf("by project %+v", l)
	}
	if l = get("/api/skills?project=" + dir); len(l.Approved) != 1 || l.Approved[0].Scope != "user" {
		t.Errorf("other project %+v", l)
	}
	if _, err := skill.Save("alice", project, "project", "junk", "d", "b", false); err != nil {
		t.Fatal(err)
	}
	if w := call(t, s, "POST", "/api/skills/discard", `{"name":"junk"}`); w.Code != 200 {
		t.Fatalf("discard: %d", w.Code)
	}
	if w := call(t, s, "POST", "/api/skills/discard", `{"name":"junk"}`); w.Code != 400 {
		t.Errorf("discard twice: %d", w.Code)
	}
}

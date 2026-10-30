package desktop

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
)

const dotPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// The images of a conversation: what the user sent and a call gave back,
// fetched by the URL /api/chat gives them; and image files a reply names.
func TestImagesAPI(t *testing.T) {
	dir := isolate(t)
	const sid = "c0ffee00-0000-4000-8000-000000000002"
	if err := (&state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{
		{ID: "a1", Kind: "claude", SessionID: sid},
	}}}}).Save(); err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(dir, ".claude", "projects", regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-"))
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + dotPNG + `"}}`
	lines := `{"type":"user","message":{"content":[{"type":"text","text":"what is this?"},` + img + `]}}` + "\n" +
		`{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x.png"}}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[` + img + `]}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(tdir, sid+".jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(agent.Builtin())
	w := call(t, s, "GET", "/api/chat?id=a1", "")
	var c Chat
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil || len(c.Entries) != 3 {
		t.Fatalf("chat: %d %s", w.Code, w.Body)
	}
	want, _ := base64.StdEncoding.DecodeString(dotPNG)
	for _, e := range []int{0, 2} {
		en := c.Entries[e]
		if len(en.Images) != 1 || en.Images[0].Mime != "image/png" || en.Images[0].URL == "" {
			t.Fatalf("entry %d: %+v", e, en)
		}
		if en.Kind == "result" && en.Text != "" {
			t.Errorf("the result's text %q: the image stands for itself", en.Text)
		}
		got := call(t, s, "GET", en.Images[0].URL, "")
		if got.Code != 200 || got.Header().Get("Content-Type") != "image/png" || got.Body.String() != string(want) {
			t.Errorf("image of entry %d: %d %s", e, got.Code, got.Header().Get("Content-Type"))
		}
	}
	for url, code := range map[string]int{
		"/api/image?id=a1&file=nope&at=0&e=0&n=0":           404,
		"/api/image?id=a1&file=" + c.File + "&at=0&e=0&n=1": 404,
		"/api/image?id=a1&file=" + c.File + "&at=x&e=0&n=0": 400,
	} {
		if got := call(t, s, "GET", url, ""); got.Code != code {
			t.Errorf("%s: %d, want %d", url, got.Code, code)
		}
	}

	// Files: from its directory, home, or whole; images only.
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, code := range map[string]int{
		"shot.png": 200, "~/shot.png": 200, filepath.Join(dir, "shot.png"): 200,
		"notes.txt": 415, "gone.png": 404, ".": 415,
	} {
		got := call(t, s, "GET", "/api/file?id=a1&path="+path, "")
		if got.Code != code || code == 200 && got.Body.String() != string(want) {
			t.Errorf("file %s: %d, want %d", path, got.Code, code)
		}
	}
}

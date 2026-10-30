package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// pi's extension posts the message as it grows; a snapshot late, out of
// order or after the message ended is dropped.
func TestPiSnapshots(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(h.pi("tok"))
	defer srv.Close()
	post := func(agent, token, body string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/p/"+agent, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post("a1", "nope", `{"id":"1","seq":1}`); code != http.StatusForbidden {
		t.Errorf("wrong token: %d", code)
	}
	post("a1", "tok", `{"id":"100","seq":1,"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Hel"}]}`)
	post("a1", "tok", `{"id":"100","seq":3,"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Hello"},{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"ls"}}]}`)
	post("a1", "tok", `{"id":"100","seq":2,"content":[{"type":"text","text":"He"}]}`) // late
	m := h.Wait(context.Background(), "a1", 0)
	if m.ID != "100" || m.Done || len(m.Blocks) != 3 || m.Blocks[1].Text != "Hello" ||
		m.Blocks[2].Kind != "tool" || m.Blocks[2].Tool != "bash" || m.Blocks[2].ID != "c1" || m.Blocks[2].Text != `{"command":"ls"}` {
		t.Fatalf("message = %+v", m)
	}
	post("a1", "tok", `{"id":"100","seq":4,"done":true,"content":[{"type":"text","text":"Hello!"}]}`)
	post("a1", "tok", `{"id":"100","seq":5,"content":[{"type":"text","text":"after"}]}`) // after its end
	if m := h.Wait(context.Background(), "a1", 0); !m.Done || len(m.Blocks) != 1 || m.Blocks[0].Text != "Hello!" || !m.Blocks[0].Done {
		t.Errorf("ended: %+v", m)
	}
	// The next message starts over.
	post("a1", "tok", `{"id":"200","seq":1,"content":[{"type":"text","text":"Next"}]}`)
	if m := h.Wait(context.Background(), "a1", 0); m.ID != "200" || m.Done || m.Blocks[0].Text != "Next" {
		t.Errorf("next: %+v", m)
	}
}

// A pi agent loads mad's extension and posts to the hub while it runs.
func TestPiVars(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	if v := Vars("pi", "a1"); v["{pi_live}"] != "" {
		t.Errorf("no hub: %v", v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Serve(ctx) }()
	defer func() { cancel(); <-done }()
	var in info
	for i := 0; i < 100; i++ {
		if in, _ = readInfo(); in.Addr != "" && alive(in.Addr) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v := Vars("pi", "a1"); v["{pi_live}"] != "-e '"+piExtensionFile()+"'" || v["{codex_live}"] != "" {
		t.Errorf("vars = %v", v)
	}
	if data, err := os.ReadFile(piExtensionFile()); err != nil || !strings.Contains(string(data), `pi.on("message_update"`) {
		t.Errorf("extension: %v", err)
	}
	env := Env("pi", "a1")
	if len(env) != 2 || env[0] != "MAD_LIVE_URL=http://"+in.Addr+"/p/a1" || env[1] != "MAD_LIVE_TOKEN="+in.Token {
		t.Errorf("env = %v", env)
	}
}

package live

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHub(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if m := h.Wait(ctx, "a", 0); m.Version != 0 || m.ID != "" {
		t.Errorf("nothing yet: %+v", m)
	}

	got := make(chan Message)
	go func() { got <- h.Wait(context.Background(), "a", 0) }()
	h.Begin("a", "m1", "s1")
	if m := <-got; m.ID != "m1" || m.Session != "s1" || m.Version == 0 {
		t.Errorf("woken with %+v", m)
	}
	h.Block("a", "m1", 1, func(b *Block) { b.Kind, b.Text = "text", "hel" })
	h.Block("a", "m1", 1, func(b *Block) { b.Text += "lo" })
	h.Block("a", "old", 1, func(b *Block) { b.Text += "!" }) // a message replaced
	m := h.Wait(context.Background(), "a", 0)
	if len(m.Blocks) != 2 || m.Blocks[1].Text != "hello" || m.Done {
		t.Errorf("message = %+v", m)
	}
	h.End("a", "m1")
	if m2 := h.Wait(context.Background(), "a", m.Version); !m2.Done || !m2.Blocks[1].Done || m2.Version <= m.Version {
		t.Errorf("ended: %+v", m2)
	}
	// What a look got is a copy.
	m.Blocks[1].Text = "changed"
	if h.Wait(context.Background(), "a", 0).Blocks[1].Text != "hello" {
		t.Error("a look's copy changed the hub")
	}
}

const sse = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hm\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"x\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: content_block_start\r\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\r\n\r\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\", 世界\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t1\",\"name\":\"Bash\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\": \\\"ls\"}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

// The proxy passes calls on as they came, under the upstream's own path,
// and reads the agent's own turns, not a subagent's.
func TestAnthropicProxy(t *testing.T) {
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, fmt.Sprintf("%s %s auth=%s gzip=%v body=%d", r.Method, r.URL.Path,
			r.Header.Get("Authorization"), strings.Contains(r.Header.Get("Accept-Encoding"), "br"), len(body)))
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range strings.SplitAfter(sse, "\n\n") {
			_, _ = io.WriteString(w, ev)
			w.(http.Flusher).Flush()
		}
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL + "/gw")
	h := NewHub()
	proxy := httptest.NewServer(h.anthropic(u))
	defer proxy.Close()

	call := func(agent, path, body string) string {
		t.Helper()
		req, _ := http.NewRequest("POST", proxy.URL+"/a/"+agent+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Accept-Encoding", "gzip, br")
		req.Header.Set("X-Claude-Code-Session-Id", "s-"+agent)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return string(out)
	}
	const turn = `{"stream":true,"tools":[{"name":"Bash"}],"system":[{"type":"text","text":"cc_entrypoint=cli;"}],"messages":[]}`
	if out := call("a1", "/v1/messages", turn); out != strings.ReplaceAll(sse, "\r", "") && out != sse {
		t.Errorf("the reply changed on its way: %q", out)
	}
	m := h.Wait(context.Background(), "a1", 0)
	if m.ID != "msg_1" || m.Session != "s-a1" || !m.Done || len(m.Blocks) != 3 {
		t.Fatalf("message = %+v", m)
	}
	if b := m.Blocks; b[0].Kind != "thinking" || b[0].Text != "hm" || b[1].Kind != "text" || b[1].Text != "Hello, 世界" ||
		b[2].Kind != "tool" || b[2].Tool != "Bash" || b[2].ID != "t1" || b[2].Text != `{"command": "ls` {
		t.Errorf("blocks = %+v", b)
	}

	const sub = `{"stream":true,"tools":[{}],"system":[{"type":"text","text":"cc_is_subagent=true;"}]}`
	const title = `{"stream":true,"messages":[]}` // no tools: a title, say
	call("a2", "/v1/messages", sub)
	call("a3", "/v1/messages", title)
	call("a4", "/api/hello", "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	for _, a := range []string{"a2", "a3", "a4"} {
		if m := h.Wait(ctx, a, 0); m.ID != "" {
			t.Errorf("%s tapped: %+v", a, m)
		}
	}
	want := []string{
		fmt.Sprintf("POST /gw/v1/messages auth=Bearer secret gzip=false body=%d", len(turn)),
		fmt.Sprintf("POST /gw/v1/messages auth=Bearer secret gzip=false body=%d", len(sub)),
		fmt.Sprintf("POST /gw/v1/messages auth=Bearer secret gzip=false body=%d", len(title)),
		"POST /gw/api/hello auth=Bearer secret gzip=false body=0",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("upstream saw:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
	if res, _ := http.Get(proxy.URL + "/a/../etc/v1/messages"); res.StatusCode != http.StatusNotFound {
		t.Errorf("a bad agent id: %d", res.StatusCode)
	}
}

// An agent is pointed at the hub only while it answers.
func TestEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	if env := Env("claude", "a1"); env != nil {
		t.Errorf("no hub: %v", env)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Serve(ctx) }()
	var in info
	for i := 0; i < 100; i++ {
		if in, _ = readInfo(); in.Addr != "" && alive(in.Addr) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if env := Env("claude", "a1"); len(env) != 1 || env[0] != "ANTHROPIC_BASE_URL=http://"+in.Addr+"/a/a1" {
		t.Errorf("env = %v", env)
	}
	for _, c := range [][2]string{{"codex", "a1"}, {"claude", "../x"}} {
		if env := Env(c[0], c[1]); env != nil {
			t.Errorf("%v: %v", c, env)
		}
	}
	if _, err := Fetch(context.Background(), "a1", -1); err != nil {
		t.Errorf("fetch: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if env := Env("claude", "a1"); env != nil {
		t.Errorf("hub gone: %v", env)
	}

	// Started again, it is where agents already point, with the token
	// pi agents already post with.
	ctx, cancel = context.WithCancel(context.Background())
	go func() { done <- Serve(ctx) }()
	for i := 0; i < 100 && !alive(in.Addr); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if again, _ := readInfo(); again != in {
		t.Errorf("restarted as %+v, want %+v", again, in)
	}
	cancel()
	<-done
}

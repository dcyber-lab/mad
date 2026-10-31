package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// get answers a GET to the fixture of scenario name.
func get(t *testing.T, name, path string) (*http.Response, []byte) {
	t.Helper()
	h, err := newHandler(name, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	r, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r, b
}

// Every scenario serves a deck the page can read, and the page itself.
func TestScenariosServeADeck(t *testing.T) {
	if len(names()) < 6 {
		t.Fatalf("scenarios: %v", names())
	}
	for _, name := range names() {
		t.Run(name, func(t *testing.T) {
			r, b := get(t, name, "/api/deck")
			if r.StatusCode != 200 {
				t.Fatalf("/api/deck: %d", r.StatusCode)
			}
			var d struct {
				Projects []any    `json:"projects"`
				Bus      []any    `json:"bus"`
				States   []any    `json:"states"`
				Working  []string `json:"working"`
				Machine  string   `json:"machine"`
			}
			if err := json.Unmarshal(b, &d); err != nil {
				t.Fatal(err)
			}
			if d.Projects == nil || d.Bus == nil || len(d.States) == 0 || d.Working == nil || d.Machine == "" {
				t.Fatalf("a part of the deck is missing: %s", b)
			}
			r, b = get(t, name, "/")
			if r.StatusCode != 200 || !strings.Contains(string(b), `id="embed-menu"`) {
				t.Fatalf("the page: %d", r.StatusCode)
			}
			if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'self'") {
				t.Fatal("the page is not held to the policy")
			}
		})
	}
}

func TestEdgeHome(t *testing.T) {
	_, b := get(t, "edge-home", "/api/deck")
	var d struct {
		Machine string `json:"machine"`
		Working []string
		Inbox   struct{ Needs int }
		Bus     []struct {
			ID     string
			LastAt string `json:"last_at"`
			Team   any
		}
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(d.Machine)); n != 60 {
		t.Errorf("the Mac's name is %d characters", n)
	}
	if len(d.Working) != 2 || d.Inbox.Needs != 1 {
		t.Errorf("working %v, needs %d", d.Working, d.Inbox.Needs)
	}
	for _, c := range d.Bus {
		if strings.HasPrefix(c.LastAt, "-") || c.LastAt == "" {
			t.Errorf("%s: last_at %q is not a time", c.ID, c.LastAt)
		}
	}
}

func TestMessagesAndImages(t *testing.T) {
	r, b := get(t, "channel-fold", "/api/bus/messages?channel=fold&since=5")
	if r.StatusCode != 200 {
		t.Fatalf("%d", r.StatusCode)
	}
	var m struct {
		Messages []struct{ Seq int }
		Last     int
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Messages) != 8 || m.Last != 13 {
		t.Errorf("after 5: %d messages, last %d", len(m.Messages), m.Last)
	}
	r, b = get(t, "channel-fold", "/api/bus/image?id=fx-before")
	if r.Header.Get("Content-Type") != "image/png" || len(b) < 50 {
		t.Errorf("image: %s, %d bytes", r.Header.Get("Content-Type"), len(b))
	}
}

// The channel-fold scenario's done message says "PR" right before the link to the pull
// request, which the page's folded line must not turn into "PR PR #88" (ChannelFoldTests).
func TestFoldDoneMessageHasPRBeforeTheLink(t *testing.T) {
	_, b := get(t, "channel-fold", "/api/bus/messages?channel=fold&since=0")
	var m struct {
		Messages []struct{ Kind, Text string }
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, x := range m.Messages {
		if x.Kind == "done" {
			if !strings.Contains(x.Text, "PR https://github.com/dcyber-lab/mad/pull/88 ") {
				t.Errorf("the done message no longer has PR before a pull request link: %q", x.Text)
			}
			return
		}
	}
	t.Error("no done message in channel-fold")
}

func TestPairing(t *testing.T) {
	h, err := newHandler("pairing", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	post := func(code, token string) int {
		body := `{"code":"` + code + `","name":"t","token":"` + token + `"}`
		r, err := http.Post(srv.URL+"/api/pair", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	tok := strings.Repeat("1", 32)
	for _, c := range []struct {
		code, token string
		want        int
	}{
		{strings.Repeat("a", 32), tok, 200},
		{strings.Repeat("b", 32), tok, 403},
		{strings.Repeat("a", 32), "nothex", 400},
		{strings.Repeat("c", 32), tok, 503},
		{strings.Repeat("c", 32), tok, 200},
	} {
		if got := post(c.code, c.token); got != c.want {
			t.Errorf("code %.4s…, token %.4s…: %d, want %d", c.code, c.token, got, c.want)
		}
	}
}

func TestListenRefusals(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.5:9000", "127.0.0.1:7484", "127.0.0.1:07484", "127.0.0.1:+7484",
		"127.0.0.1:007484", "[::1]:7484", "127.0.0.1:http", "localhost-ish"} {
		if ln, err := listen(addr); err == nil {
			ln.Close()
			t.Errorf("%s: listened", addr)
		}
	}
	ln, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestUnknownScenario(t *testing.T) {
	if _, err := newHandler("nope", ""); err == nil || !strings.Contains(err.Error(), "edge-home") {
		t.Errorf("error: %v", err)
	}
	if _, err := newHandler("../x", ""); err == nil {
		t.Error("a path was taken for a name")
	}
}

// A conversation is read once in full; asked again from its end it has nothing new, so a poll does not repeat it.
func TestChatIsReadOnce(t *testing.T) {
	var first, again struct {
		File    string
		Offset  int64
		Reset   bool
		Entries []struct{ Kind string }
	}
	_, b := get(t, "edge-home", "/api/chat?id=need0001-bbbb&file=&from=0")
	if err := json.Unmarshal(b, &first); err != nil {
		t.Fatal(err)
	}
	if !first.Reset || len(first.Entries) < 5 || first.File == "" {
		t.Fatalf("first look: %+v", first)
	}
	_, b = get(t, "edge-home", fmt.Sprintf("/api/chat?id=need0001-bbbb&file=%s&from=%d", first.File, first.Offset))
	if err := json.Unmarshal(b, &again); err != nil {
		t.Fatal(err)
	}
	if len(again.Entries) != 0 || again.Reset {
		t.Errorf("second look: %+v", again)
	}
}

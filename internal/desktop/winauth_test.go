package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/phone"
)

// do sends method url to h with token as a Bearer ("" sends none).
func do(h http.Handler, method, url, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHello(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	nonce := strings.Repeat("ab", 16)
	w := do(h, "GET", "/api/hello?nonce="+nonce, "", "")
	var out map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("hello: %d %s", w.Code, w.Body)
	}
	if len(out) != 1 || out["proof"] != helloProof(s.Token, nonce) {
		t.Errorf("hello answered %v, want the proof alone", out)
	}
	for _, n := range []string{"", "abc", strings.Repeat("a", 31), strings.Repeat("zz", 16), strings.Repeat("a", 200)} {
		if w := do(h, "GET", "/api/hello?nonce="+n, "", ""); w.Code != 400 {
			t.Errorf("nonce %q: %d, want 400", n, w.Code)
		}
	}
}

// squatter is a server on mad's port that is not mad: it records every
// request and answers /api/hello with a proof keyed by another token,
// and anything else as a mad would.
type squatter struct {
	mu   sync.Mutex
	seen []string // each request's URL, headers and body
}

func (q *squatter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	q.mu.Lock()
	q.seen = append(q.seen, r.URL.String()+" "+strings.Join(r.Header.Values("Authorization"), ",")+" "+string(b))
	q.mu.Unlock()
	switch r.URL.Path {
	case "/api/hello":
		reply(w, map[string]string{"proof": helloProof(strings.Repeat("f", 32), r.URL.Query().Get("nonce"))})
	case "/api/window/code":
		reply(w, map[string]string{"code": "c"})
	default:
		reply(w, map[string]any{"projects": []any{}, "open": true})
	}
}

// A process squatting on the port, with a proof by the wrong key, is
// never sent the token: not by Reusable, OpenOnServer or WindowCodeFrom.
func TestSquatterNeverSeesTheToken(t *testing.T) {
	isolate(t)
	token := strings.Repeat("a", 32)
	q := &squatter{}
	srv := httptest.NewServer(q)
	defer srv.Close()
	ctx := context.Background()
	if Reusable(ctx, srv.Client(), srv.URL, token) {
		t.Error("a squatter with a wrong proof is reusable")
	}
	if OpenOnServer(ctx, srv.Client(), srv.URL, token, "w1") {
		t.Error("a squatter's window reads as open")
	}
	if _, err := WindowCodeFrom(ctx, srv.Client(), srv.URL, token); err == nil {
		t.Error("a squatter gave a window code")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.seen) == 0 {
		t.Fatal("the squatter saw nothing: the test proves nothing")
	}
	for _, s := range q.seen {
		if strings.Contains(s, token) {
			t.Errorf("the squatter saw the token: %s", s)
		}
		if !strings.HasPrefix(s, "/api/hello?") {
			t.Errorf("a request past the hello: %s", s)
		}
	}
}

// Against mad itself the token goes in a header only, after the proof.
func TestReusableSendsBearerOnly(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	s.Token = strings.Repeat("a", 32)
	var mu sync.Mutex
	var urls []string
	inner := s.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.URL.String())
		mu.Unlock()
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	if !Reusable(ctx, srv.Client(), srv.URL, s.Token) {
		t.Fatal("mad is not reusable")
	}
	code, err := WindowCodeFrom(ctx, srv.Client(), srv.URL, s.Token)
	if err != nil || code == "" {
		t.Fatalf("window code: %q, %v", code, err)
	}
	if OpenOnServer(ctx, srv.Client(), srv.URL, s.Token, "nope") {
		t.Error("a window never opened reads as open")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, u := range urls {
		if strings.Contains(u, s.Token) {
			t.Errorf("the token in a URL: %s", u)
		}
	}
}

func TestWindowCode(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	now := time.Now()
	s.winCodes.now = func() time.Time { return now }
	mint := func() string {
		t.Helper()
		w := do(h, "POST", "/api/window/code", s.Token, "")
		var v struct{ Code string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Code) != 32 {
			t.Fatalf("window code: %d %s", w.Code, w.Body)
		}
		return v.Code
	}
	trade := func(code string, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/window/token", strings.NewReader(`{"code":"`+code+`"}`))
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do(h, "POST", "/api/window/code", "", ""); w.Code != 403 {
		t.Errorf("a window code without a token: %d", w.Code)
	}
	code := mint()
	w := trade(code)
	var v struct{ Token string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Token != s.Token {
		t.Fatalf("trade: %d %s", w.Code, w.Body)
	}
	if w := trade(code); w.Code != 403 {
		t.Errorf("a code traded twice: %d", w.Code)
	}
	if w := trade(""); w.Code != 403 {
		t.Errorf("no code: %d", w.Code)
	}
	if w := trade("nope"); w.Code != 403 {
		t.Errorf("a wrong code: %d", w.Code)
	}
	late := mint()
	now = now.Add(windowCodeTTL)
	if w := trade(late); w.Code != 403 {
		t.Errorf("an expired code: %d", w.Code)
	}
	// Through a tunnel the master token is refused, so it is not given.
	if w := trade(mint(), "Cf-Ray", "x"); w.Code != 403 {
		t.Errorf("a trade through cloudflare: %d", w.Code)
	}
	if w := do(h, "POST", "/api/window/token", "", "{"); w.Code != 400 {
		t.Errorf("a bad body: %d", w.Code)
	}
}

func pairAs(t *testing.T, name, tok string) string {
	t.Helper()
	code, err := phone.PairCode()
	if err != nil {
		t.Fatal(err)
	}
	id, err := phone.Pair(code, name, tok)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// What only the master token may do is refused a paired device, and what
// it may still do is not.
func TestMasterOnly(t *testing.T) {
	isolate(t)
	st := phone.Status{On: true, Host: "box.ts.net", URL: "https://box.ts.net:7443/#pair=c"}
	phoneGet = func(context.Context) phone.Status { return st }
	phonePeek = func(context.Context) phone.Status { return phone.Status{On: st.On, Host: st.Host} }
	phoneOn = func(context.Context) (phone.Status, error) { return st, nil }
	phoneOff = func(context.Context) error { return nil }
	t.Cleanup(func() { phoneGet, phonePeek, phoneOn, phoneOff = phone.Get, phone.Peek, phone.On, phone.Off })
	s := New(agent.Builtin())
	h := s.Handler()
	const devTok, otherTok = "00112233445566778899aabbccddeeff", "ffeeddccbbaa99887766554433221100"
	dev := pairAs(t, "mine", devTok)
	other := pairAs(t, "other", otherTok)

	for _, c := range []struct{ method, url string }{
		{"POST", "/api/phone"},
		{"DELETE", "/api/phone"},
		{"GET", "/api/phone/qr"},
		{"POST", "/api/window/code"},
		{"DELETE", "/api/phone/devices?id=" + other},
	} {
		if w := do(h, c.method, c.url, devTok, ""); w.Code != 403 {
			t.Errorf("%s %s by a device: %d, want 403", c.method, c.url, w.Code)
		}
		if w := do(h, c.method, c.url, s.Token, ""); w.Code != 200 {
			t.Errorf("%s %s by the master token: %d %s", c.method, c.url, w.Code, w.Body)
		}
	}
	// A device still sees how it stands, without a pairing code.
	var got phone.Status
	if w := do(h, "GET", "/api/phone", devTok, ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !got.On || got.URL != "" {
		t.Errorf("GET /api/phone by a device: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/phone", s.Token, ""); !strings.Contains(w.Body.String(), "#pair=") {
		t.Errorf("GET /api/phone by the master token: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/phone/devices", devTok, ""); w.Code != 200 || !strings.Contains(w.Body.String(), dev) {
		t.Errorf("devices listed to a device: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/deck", devTok, ""); w.Code != 200 {
		t.Errorf("the deck to a device: %d", w.Code)
	}
	// It may remove itself.
	if w := do(h, "DELETE", "/api/phone/devices?id="+dev, devTok, ""); w.Code != 200 {
		t.Errorf("a device removing itself: %d %s", w.Code, w.Body)
	}
	if _, ok := phone.DeviceFor(devTok); ok {
		t.Error("the device is still paired after removing itself")
	}
}

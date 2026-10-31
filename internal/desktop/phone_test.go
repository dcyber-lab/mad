package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/phone"
	"github.com/dcyber-lab/mad/internal/push"
)

func TestPhoneAPI(t *testing.T) {
	isolate(t)
	st := phone.Status{Host: "box.ts.net"}
	did := ""
	phoneGet = func(context.Context) phone.Status { return st }
	phoneOn = func(context.Context) (phone.Status, error) {
		did += "on "
		st = phone.Status{On: true, Host: "box.ts.net", URL: "https://box.ts.net:7443/#token=t"}
		return st, nil
	}
	phoneOff = func(context.Context) error {
		did += "off "
		st = phone.Status{Host: "box.ts.net"}
		return nil
	}
	t.Cleanup(func() { phoneGet, phoneOn, phoneOff = phone.Get, phone.On, phone.Off })
	s := New(manageKinds)
	look := func(method string) phone.Status {
		t.Helper()
		w := call(t, s, method, "/api/phone", "")
		var out phone.Status
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s /api/phone: %d %s", method, w.Code, w.Body)
		}
		return out
	}
	if got := look("GET"); got.On || got.Host != "box.ts.net" {
		t.Errorf("off: %+v", got)
	}
	if w := call(t, s, "GET", "/api/phone/qr", ""); w.Code != 404 {
		t.Errorf("a QR code while off: %d", w.Code)
	}
	if got := look("POST"); !got.On || got.URL == "" {
		t.Errorf("on: %+v", got)
	}
	if w := call(t, s, "GET", "/api/phone/qr", ""); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("QR code: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if got := look("DELETE"); got.On || did != "on off " {
		t.Errorf("off again: %+v, did %q", got, did)
	}
}

// The phone's page comes from tailscale serve's origin: its websockets
// are taken from there, and from nowhere else.
func TestOrigins(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	s.Origins = []string{"box.ts.net:7443"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/host?id=nope&token=" + s.Token
	for origin, ok := range map[string]bool{"https://box.ts.net:7443": true, "https://evil.example": false} {
		c, resp, err := websocket.Dial(context.Background(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
		if c != nil {
			c.CloseNow()
		}
		if got := err == nil; got != ok {
			code := 0
			if resp != nil {
				code = resp.StatusCode
			}
			t.Errorf("origin %s: accepted %v (%d, %v), want %v", origin, got, code, err, ok)
		}
	}
}

func pairedDevice(t *testing.T) string {
	t.Helper()
	code, err := phone.PairCode()
	if err != nil {
		t.Fatal(err)
	}
	tok := "00112233445566778899aabbccddeeff"
	if _, err := phone.Pair(code, "test phone", tok); err != nil {
		t.Fatal(err)
	}
	return tok
}

// Devices get in with their own token anywhere; the master token only off
// a Cloudflare Tunnel.
func TestDeviceAuth(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	dev := pairedDevice(t)
	get := func(token string, hdr map[string]string) int {
		r := httptest.NewRequest("GET", "/api/deck?token="+token, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	cf := map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}
	for _, c := range []struct {
		name  string
		token string
		hdr   map[string]string
		want  int
	}{
		{"master, local", s.Token, nil, 200},
		{"master through cloudflare", s.Token, cf, 403},
		{"device, local", dev, nil, 200},
		{"device through cloudflare", dev, cf, 200},
		{"unknown", "nope", cf, 403},
	} {
		if got := get(c.token, c.hdr); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	r := httptest.NewRequest("GET", "/api/deck", nil)
	r.Header.Set("Authorization", "Bearer "+dev)
	r.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("device Bearer through cloudflare: %d", w.Code)
	}
}

func TestPairEndpoint(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/pair", strings.NewReader(body)))
		return w
	}
	const tok = "0123456789abcdef0123456789abcdef"
	code, _ := phone.PairCode()
	w := post(`{"code":"` + code + `","name":"iPhone Safari","token":"` + tok + `"}`)
	var out struct{ Token, ID string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.ID == "" || out.Token != "" {
		t.Fatalf("pair: %d %s", w.Code, w.Body)
	}
	// The answer lost: the same pair again gets the same device.
	if w := post(`{"code":"` + code + `","name":"iPhone Safari","token":"` + tok + `"}`); w.Code != 200 || !strings.Contains(w.Body.String(), out.ID) {
		t.Errorf("a replay: %d %s", w.Code, w.Body)
	}
	if w := post(`{"code":"` + code + `","name":"x","token":"ffffffffffffffffffffffffffffffff"}`); w.Code != 403 {
		t.Errorf("a used code, another token: %d", w.Code)
	}
	if w := post(`{"code":"nope","token":"` + tok + `"}`); w.Code != 403 {
		t.Errorf("a wrong code: %d", w.Code)
	}
	if w := post(`{"code":"` + code + `","token":"short"}`); w.Code != 400 {
		t.Errorf("a bad token: %d", w.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/deck?token="+tok, nil))
	if rec.Code != 200 {
		t.Errorf("the device's own token: %d", rec.Code)
	}
}

// A device removed by another process (the CLI) loses its websocket within
// a few ticks.
func TestRevokeClosesWebsocket(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	s.revokeTick = 50 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	dev := pairedDevice(t)
	ws := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/window?id=w1&token=" + dev
	c, _, err := websocket.Dial(context.Background(), ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	// The CLI, as another process: it only writes the file.
	if err := os.WriteFile(paths.PhoneDevices(), []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil || ctx.Err() != nil {
		t.Errorf("the websocket stayed open after the revoke: %v", err)
	}
	if _, _, err := websocket.Dial(context.Background(), ws, nil); err == nil {
		t.Error("a revoked device opened a websocket")
	}
}

// A push subscription is bound to the device that registered it, the
// master token's to none, whatever the body says.
func TestPushBoundToDevice(t *testing.T) {
	isolate(t)
	s := New(agent.Builtin())
	h := s.Handler()
	dev := pairedDevice(t)
	devs, _ := phone.Devices()
	body := func(ep string) string {
		k := base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
		a := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
		return `{"endpoint":"` + ep + `","keys":{"p256dh":"` + k + `","auth":"` + a + `"},"device":"forged"}`
	}
	for ep, token := range map[string]string{"https://push.example/dev": dev, "https://push.example/master": s.Token} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/push?token="+token, strings.NewReader(body(ep))))
		if w.Code != 200 {
			t.Fatalf("subscribe: %d %s", w.Code, w.Body)
		}
	}
	subs, _ := push.List()
	for _, sub := range subs {
		want := ""
		if strings.HasSuffix(sub.Endpoint, "/dev") {
			want = devs[0].ID
		}
		if sub.Device != want {
			t.Errorf("%s bound to %q, want %q", sub.Endpoint, sub.Device, want)
		}
	}
}

// The Mac's friendly name is in the deck's snapshot and in the pair reply,
// looked up once.
func TestMachineName(t *testing.T) {
	isolate(t)
	oldLookup := lookupMachine
	calls := 0
	lookupMachine = func() string { calls++; return "Ann's MacBook Pro" }
	machineOnce, machineName = sync.Once{}, ""
	t.Cleanup(func() {
		lookupMachine = oldLookup
		machineOnce, machineName = sync.Once{}, ""
	})
	s := New(agent.Builtin())
	h := s.Handler()
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/deck?token="+s.Token, nil))
		var snap struct{ Machine string }
		if json.Unmarshal(w.Body.Bytes(), &snap) != nil || snap.Machine != "Ann's MacBook Pro" {
			t.Fatalf("deck machine: %s", w.Body)
		}
	}
	code, _ := phone.PairCode()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/pair", strings.NewReader(`{"code":"`+code+`","name":"x","token":"0123456789abcdef0123456789abcdef"}`)))
	var out struct{ ID, Machine string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Machine != "Ann's MacBook Pro" || out.ID == "" {
		t.Errorf("pair reply: %d %s", w.Code, w.Body)
	}
	if calls != 1 {
		t.Errorf("looked up %d times", calls)
	}
}

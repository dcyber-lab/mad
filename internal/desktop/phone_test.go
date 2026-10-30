package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/dcyber-lab/mad/internal/phone"
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

package desktop

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
)

func TestPushAPI(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	state := func(endpoint string) (key string, on bool) {
		t.Helper()
		w := call(t, s, "GET", "/api/push?endpoint="+url.QueryEscape(endpoint), "")
		var out struct {
			Key string
			On  bool
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("GET /api/push: %d %s", w.Code, w.Body)
		}
		return out.Key, out.On
	}
	key, on := state("https://push.example/a")
	if k, _ := base64.RawURLEncoding.DecodeString(key); len(k) != 65 || on {
		t.Fatalf("key %q (%d bytes), on %v", key, len(k), on)
	}
	if again, _ := state(""); again != key {
		t.Error("mad's key changed")
	}
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	sub, _ := json.Marshal(map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}})
	if w := call(t, s, "POST", "/api/push", string(sub)); w.Code != 200 {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	if _, on := state("https://push.example/a"); !on {
		t.Error("not subscribed")
	}
	if w := call(t, s, "POST", "/api/push", `{"endpoint":"https://push.example/b","keys":{"p256dh":"x","auth":"y"}}`); w.Code != 400 {
		t.Errorf("bad keys taken: %d", w.Code)
	}
	if w := call(t, s, "DELETE", "/api/push?endpoint="+url.QueryEscape("https://push.example/a"), ""); w.Code != 200 {
		t.Fatalf("unsubscribe: %d", w.Code)
	}
	if _, on := state("https://push.example/a"); on {
		t.Error("still subscribed")
	}
}

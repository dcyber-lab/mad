package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291, Appendix A: the same keys and salt make the same message.
func TestEncryptRFC8291(t *testing.T) {
	as, err := ecdh.P256().NewPrivateKey(mustDecode(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got := b64.EncodeToString(as.PublicKey().Bytes()); got != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatalf("as_public %s", got)
	}
	got, err := encrypt([]byte("When I grow up, I want to be a watermelon"),
		mustDecode(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		mustDecode(t, "BTBZMqHH6r4Tts7J_aSIgg"), mustDecode(t, "DGv6ra1nlYgDCS1FRnbzlw"), as)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Errorf("message\n%s\nwant\n%s", b64.EncodeToString(got), want)
	}
}

// decrypt is a browser's side of RFC 8291, to read what was sent.
func decrypt(t *testing.T, body []byte, ua *ecdh.PrivateKey, auth []byte) []byte {
	t.Helper()
	salt, idlen := body[:16], int(body[20])
	asPub := body[21 : 21+idlen]
	asKey, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := ua.ECDH(asKey)
	mac := func(key []byte, parts ...[]byte) []byte {
		h := hmac.New(sha256.New, key)
		for _, p := range parts {
			h.Write(p)
		}
		return h.Sum(nil)
	}
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPub...)
	prk := mac(salt, mac(mac(auth, secret), info, []byte{1}))
	block, _ := aes.NewCipher(mac(prk, []byte("Content-Encoding: aes128gcm\x00"), []byte{1})[:16])
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, mac(prk, []byte("Content-Encoding: nonce\x00"), []byte{1})[:12], body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(plain, []byte{2})
}

// A push service: what it was sent decrypts, signed by mad; one gone is
// forgotten.
func TestSend(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := []byte("0123456789abcdef")
	var got Message
	var gone bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			gone = true
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
			t.Errorf("headers %v", r.Header)
		}
		// vapid t=JWT, k=key: a signature by the key mad publishes.
		a := r.Header.Get("Authorization")
		tok := strings.TrimSuffix(strings.SplitN(strings.TrimPrefix(a, "vapid t="), ", k=", 2)[0], ",")
		k := strings.SplitN(a, ", k=", 2)[1]
		pub, _ := PublicKey()
		if k != pub {
			t.Errorf("k %s, want %s", k, pub)
		}
		parts := strings.Split(tok, ".")
		var claims struct{ Aud, Sub string }
		_ = json.Unmarshal(mustDecode(t, parts[1]), &claims)
		if !strings.HasPrefix(claims.Aud, "https://127.0.0.1:") || claims.Sub == "" {
			t.Errorf("claims %+v", claims)
		}
		p := mustDecode(t, k)
		key := ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(p[1:33]), Y: new(big.Int).SetBytes(p[33:])}
		sig := mustDecode(t, parts[2])
		sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if !ecdsa.Verify(&key, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Error("the JWT's signature does not verify")
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(decrypt(t, body, ua, auth), &got)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	old := client
	client = srv.Client()
	t.Cleanup(func() { client = old })

	sub := func(path string) Subscription {
		var s Subscription
		s.Endpoint = srv.URL + path
		s.Keys.P256dh = b64.EncodeToString(ua.PublicKey().Bytes())
		s.Keys.Auth = b64.EncodeToString(auth)
		return s
	}
	for _, p := range []string{"/a", "/gone", "/a"} {
		if err := Subscribe(sub(p)); err != nil {
			t.Fatal(err)
		}
	}
	if subs, _ := List(); len(subs) != 2 {
		t.Fatalf("subscriptions %+v: the same endpoint twice", subs)
	}
	if err := Send(context.Background(), Message{Title: "mad · p", Body: "claude#1 needs you", Agent: "a1"}, ""); err != nil {
		t.Fatal(err)
	}
	if got.Body != "claude#1 needs you" || got.Agent != "a1" || !gone {
		t.Errorf("got %+v, gone %v", got, gone)
	}
	if subs, _ := List(); len(subs) != 1 || subs[0].Endpoint != srv.URL+"/a" {
		t.Errorf("after a gone one: %+v", subs)
	}
	if err := Unsubscribe(srv.URL + "/a"); err != nil {
		t.Fatal(err)
	}
	if subs, _ := List(); len(subs) != 0 {
		t.Errorf("unsubscribed: %+v", subs)
	}
	var bad Subscription
	bad.Endpoint = "http://x"
	if Subscribe(bad) == nil {
		t.Error("a plain http endpoint was taken")
	}
}

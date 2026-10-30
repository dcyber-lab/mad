// Package push tells the user's phone what the deck's notifications say,
// as Web Push (RFC 8030): the page subscribes, its browser's push service
// gives it an endpoint, and mad sends each notification there, encrypted
// for that browser alone (RFC 8291) and signed as mad (VAPID, RFC 8292).
// Only the standard library: P-256, HKDF and AES-GCM.
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
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Subscription is a browser's: where to send, and its keys, as the page's
// PushSubscription.toJSON() gives them.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Added time.Time `json:"added,omitempty"`
}

// Message is what a notification says; Agent is the id of the agent to
// open when it is clicked.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Agent string `json:"agent,omitempty"`
	// Channel is the bus channel to open, for a message on the bus.
	Channel string `json:"channel,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

var b64 = base64.RawURLEncoding

// decode reads base64url with or without padding, as browsers give it.
func decode(s string) ([]byte, error) {
	return b64.DecodeString(strings.TrimRight(s, "="))
}

// ---- mad's keys, as an application server (VAPID) ----

type keyFile struct {
	Private string `json:"private"` // the P-256 scalar
	Public  string `json:"public"`  // the point, uncompressed
}

var keyMu sync.Mutex

// keys are mad's VAPID keys, made the first time.
func keys() (*ecdsa.PrivateKey, []byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	path := filepath.Join(paths.StateDir(), "push_vapid.json")
	var kf keyFile
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &kf) == nil {
		d, err1 := decode(kf.Private)
		pub, err2 := decode(kf.Public)
		if err1 == nil && err2 == nil && len(pub) == 65 {
			x, y := new(big.Int).SetBytes(pub[1:33]), new(big.Int).SetBytes(pub[33:])
			return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: new(big.Int).SetBytes(d)}, pub, nil
		}
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pk, err := k.PublicKey.ECDH()
	if err != nil {
		return nil, nil, err
	}
	pub := pk.Bytes()
	d := make([]byte, 32)
	k.D.FillBytes(d)
	b, _ := json.Marshal(keyFile{Private: b64.EncodeToString(d), Public: b64.EncodeToString(pub)})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	return k, pub, os.WriteFile(path, b, 0o600)
}

// PublicKey is mad's, base64url: what the page subscribes with.
func PublicKey() (string, error) {
	_, pub, err := keys()
	return b64.EncodeToString(pub), err
}

// jwt signs mad's claim to send to the push service at aud.
func jwt(k *ecdsa.PrivateKey, aud string, now time.Time) (string, error) {
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": aud, "exp": now.Add(12 * time.Hour).Unix(), "sub": "https://github.com/dcyber-lab/mad",
	})
	signed := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, k, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signed + "." + b64.EncodeToString(sig), nil
}

// ---- the subscriptions ----

var subsMu sync.Mutex

func subsFile() string { return filepath.Join(paths.StateDir(), "push_subscriptions.json") }

// List is the browsers subscribed.
func List() ([]Subscription, error) {
	b, err := os.ReadFile(subsFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var subs []Subscription
	return subs, json.Unmarshal(b, &subs)
}

func change(f func([]Subscription) []Subscription) error {
	subsMu.Lock()
	defer subsMu.Unlock()
	subs, err := List()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(f(subs), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	tmp := subsFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, subsFile())
}

// Subscribe keeps s, in place of one of the same endpoint.
func Subscribe(s Subscription) error {
	if !strings.HasPrefix(s.Endpoint, "https://") {
		return errors.New("a push endpoint is an https URL")
	}
	if p, err := decode(s.Keys.P256dh); err != nil || len(p) != 65 {
		return errors.New("a subscription's p256dh is a P-256 point")
	}
	if a, err := decode(s.Keys.Auth); err != nil || len(a) != 16 {
		return errors.New("a subscription's auth is 16 bytes")
	}
	if s.Added.IsZero() {
		s.Added = time.Now().UTC()
	}
	return change(func(subs []Subscription) []Subscription {
		return append(without(subs, s.Endpoint), s)
	})
}

// Unsubscribe forgets endpoint's subscription.
func Unsubscribe(endpoint string) error {
	return change(func(subs []Subscription) []Subscription { return without(subs, endpoint) })
}

func without(subs []Subscription, endpoint string) []Subscription {
	out := []Subscription{}
	for _, s := range subs {
		if s.Endpoint != endpoint {
			out = append(out, s)
		}
	}
	return out
}

// ---- sending ----

// client sends to push services; tests replace it.
var client = &http.Client{Timeout: 20 * time.Second}

// Send sends m to every browser subscribed, or to the one of endpoint
// alone when it is given. One its push service says is gone is forgotten.
func Send(ctx context.Context, m Message, endpoint string) error {
	subs, err := List()
	if err != nil || len(subs) == 0 {
		return err
	}
	payload, _ := json.Marshal(m)
	var errs []error
	for _, s := range subs {
		if endpoint != "" && s.Endpoint != endpoint {
			continue
		}
		code, err := send(ctx, s, payload)
		if code == http.StatusNotFound || code == http.StatusGone {
			_ = Unsubscribe(s.Endpoint)
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func send(ctx context.Context, s Subscription, payload []byte) (int, error) {
	k, pub, err := keys()
	if err != nil {
		return 0, err
	}
	ua, err1 := decode(s.Keys.P256dh)
	auth, err2 := decode(s.Keys.Auth)
	if err := errors.Join(err1, err2); err != nil {
		return 0, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return 0, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return 0, err
	}
	body, err := encrypt(payload, ua, auth, salt, as)
	if err != nil {
		return 0, err
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return 0, err
	}
	token, err := jwt(k, u.Scheme+"://"+u.Host, time.Now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+token+", k="+b64.EncodeToString(pub))
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("push to %s: %s: %s", u.Host, resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp.StatusCode, nil
}

// encrypt is RFC 8291: plaintext for the browser of public key ua and
// secret auth, with the server's one-off key as and salt, as one
// aes128gcm record (RFC 8188) behind its header.
func encrypt(plaintext, ua, auth, salt []byte, as *ecdh.PrivateKey) ([]byte, error) {
	uaKey, err := ecdh.P256().NewPublicKey(ua)
	if err != nil {
		return nil, err
	}
	secret, err := as.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	mac := func(key []byte, parts ...[]byte) []byte {
		h := hmac.New(sha256.New, key)
		for _, p := range parts {
			h.Write(p)
		}
		return h.Sum(nil)
	}
	// IKM = HKDF(auth, ECDH secret, "WebPush: info" || ua || as), 32 bytes.
	info := append(append([]byte("WebPush: info\x00"), ua...), asPub...)
	ikm := mac(mac(auth, secret), info, []byte{1})
	prk := mac(salt, ikm)
	cek := mac(prk, []byte("Content-Encoding: aes128gcm\x00"), []byte{1})[:16]
	nonce := mac(prk, []byte("Content-Encoding: nonce\x00"), []byte{1})[:12]
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	const recordSize = 4096
	if len(plaintext)+1+16 > recordSize {
		return nil, errors.New("too long to push")
	}
	out := append([]byte{}, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	// The last record ends with 2.
	return gcm.Seal(out, nonce, append(append([]byte{}, plaintext...), 2), nil), nil
}

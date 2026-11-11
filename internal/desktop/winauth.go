package desktop

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/desktop/api"
)

// The master token stays off the wire until the server has shown it is
// mad (hello), and off a browser's command line, where any local user
// reads it with ps (window codes). A window code is the master token's
// to ask for (masterOnly).

// helloLabel is what the proof signs besides the nonce, so the proof is
// of nothing but this.
const helloLabel = "mad-hello:"

// helloProof is the proof a server holding token gives for nonce.
func helloProof(token, nonce string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte(helloLabel + nonce))
	return hex.EncodeToString(m.Sum(nil))
}

// validNonce: 32 to 128 hex digits. Short ones would let a squatter
// replay a proof it saw; long ones are only work for the server.
func validNonce(n string) bool {
	if len(n) < 32 || len(n) > 128 {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

// hello proves this server holds the token without a token to ask for:
// it answers a nonce, and nothing else.
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	n := r.URL.Query().Get("nonce")
	if !validNonce(n) {
		http.Error(w, "bad nonce", http.StatusBadRequest)
		return
	}
	reply(w, api.Hello{Proof: helloProof(s.Token, n)})
}

// windowCodeTTL is how long a window code lasts: the browser only has to
// start and load the page.
const windowCodeTTL = 60 * time.Second

// windowCodes are the codes not yet traded, by their hash, with when
// each expires. They are in memory only: a server restarted has none,
// as a window opened on it has its token already.
type windowCodes struct {
	mu    sync.Mutex
	codes map[string]time.Time
	now   func() time.Time // time.Now; tests move it
}

func (c *windowCodes) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *windowCodes) mint() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.codes == nil {
		c.codes = map[string]time.Time{}
	}
	for h, exp := range c.codes {
		if !now.Before(exp) {
			delete(c.codes, h)
		}
	}
	c.codes[codeHash(code)] = now.Add(windowCodeTTL)
	return code, nil
}

// take uses code up and says whether it was good. It is looked up by
// its hash, so how long the lookup takes says nothing of the code.
func (c *windowCodes) take(code string) bool {
	if code == "" {
		return false
	}
	h := codeHash(code)
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.codes[h]
	if !ok {
		return false
	}
	delete(c.codes, h)
	return c.clock().Before(exp)
}

func codeHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// WindowCode mints a code for a window of this server's own, which its
// page trades for the token.
func (s *Server) WindowCode() (string, error) { return s.winCodes.mint() }

// windowCode mints a code for a window that mad opens on this server
// (one the phone's started).
func (s *Server) windowCode(w http.ResponseWriter, r *http.Request) {
	code, err := s.WindowCode()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, api.WindowCode{Code: code})
}

// windowToken trades a window code for the token, once. It has no token
// to ask for: the code, 128 random bits good once for a minute, is the
// credential. Not through a Cloudflare Tunnel, where the master token is
// refused anyway.
func (s *Server) windowToken(w http.ResponseWriter, r *http.Request) {
	if viaCloudflare(r) {
		refuse(w, api.ErrDenied, "forbidden")
		return
	}
	var in struct{ Code string }
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.winCodes.take(in.Code) {
		refuse(w, api.ErrDenied, "forbidden")
		return
	}
	reply(w, api.WindowToken{Token: s.Token})
}

// masterOnly refuses a paired device what only the master token may ask
// for: a window code, which is traded for the master token itself. A
// device has a token of its own and is not to come by that one.
func masterOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deviceOf(r) != "" {
			refuse(w, api.ErrDenied, "forbidden")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ---- the client side: a mad that opens its window on another's server ----

// ErrNotMad is a server that could not prove it holds the token.
var ErrNotMad = errors.New("the server there did not prove it is mad")

// proven says whether the server at base proves it holds token, for a
// fresh nonce; only then may the token be sent to it.
func proven(ctx context.Context, c *http.Client, base, token string) bool {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return false
	}
	nonce := hex.EncodeToString(b)
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/hello?nonce="+nonce, nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v api.Hello
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<10)).Decode(&v) != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(v.Proof), []byte(helloProof(token, nonce))) == 1
}

// bearer makes a request carrying token in its header, never its URL.
func bearer(ctx context.Context, method, url, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

// WindowCodeFrom asks the server at base, once it proved it is mad, for
// a code a window opened on it trades for token.
func WindowCodeFrom(ctx context.Context, c *http.Client, base, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !proven(ctx, c, base, token) {
		return "", ErrNotMad
	}
	req, err := bearer(ctx, "POST", base+"/api/window/code", token)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v api.WindowCode
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&v) != nil || v.Code == "" {
		return "", errors.New("the server gave no window code: " + resp.Status)
	}
	return v.Code, nil
}

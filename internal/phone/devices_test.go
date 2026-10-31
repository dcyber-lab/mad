package phone

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/push"
)

func TestPairExchange(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, err := PairCode()
	if err != nil || len(code) != 32 {
		t.Fatalf("code %q, %v", code, err)
	}
	// The QR shown again is the same code, not one a call.
	if again, _ := PairCode(); again != code {
		t.Errorf("a second code %q while %q is unused", again, code)
	}
	if _, err := Pair("0123456789abcdef0123456789abcdef", "x", newTok()); err != ErrBadCode {
		t.Errorf("a wrong code: %v", err)
	}
	tok := newTok()
	id, err := Pair(code, "Ann's iPhone", tok)
	if err != nil || len(tok) != 32 || id == "" {
		t.Fatalf("pair: %q %q %v", tok, id, err)
	}
	if _, err := Pair(code, "again", newTok()); err != ErrBadCode {
		t.Errorf("a used code: %v", err)
	}
	// A new code once the last was used.
	if next, _ := PairCode(); next == code {
		t.Error("a used code was shown again")
	}
	d, ok := DeviceFor(tok)
	if !ok || d.ID != id || d.Name != "Ann's iPhone" {
		t.Errorf("device %+v, %v", d, ok)
	}
	if _, ok := DeviceFor(code); ok {
		t.Error("a pairing code works as a token")
	}
	// Kept hashed, 0600.
	b, _ := os.ReadFile(paths.PhoneDevices())
	if strings.Contains(string(b), tok) || !strings.Contains(string(b), sum(tok)) {
		t.Errorf("devices file holds the token itself:\n%s", b)
	}
	pb, _ := os.ReadFile(paths.PhonePairs())
	if strings.Contains(string(pb), code) {
		t.Errorf("pairs file holds the code itself:\n%s", pb)
	}
	if fi, err := os.Stat(paths.PhoneDevices()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("devices mode %v, %v", fi.Mode().Perm(), err)
	}
}

func TestPairExpired(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, _ := PairCode()
	old := []pairEntry{{Hash: sum(code), Expires: time.Now().Add(-time.Second)}}
	if err := writeJSON(paths.PhonePairs(), old); err != nil {
		t.Fatal(err)
	}
	if _, err := Pair(code, "x", newTok()); err != ErrBadCode {
		t.Errorf("an expired code: %v", err)
	}
	if again, _ := PairCode(); again == code {
		t.Error("an expired code was shown again")
	}
}

// Two phones scanning the same code at once: one gets in.
func TestPairSingleUse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, _ := PairCode()
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Pair(code, "p", newTok()); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 || len(mustDevices(t)) != 1 {
		t.Errorf("%d pairings, %d devices from one code", won, len(mustDevices(t)))
	}
}

func TestRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	pairOne := func(name string) (string, string) {
		c, _ := PairCode()
		tok := newTok()
		id, err := Pair(c, name, tok)
		if err != nil {
			t.Fatal(err)
		}
		return tok, id
	}
	t1, id1 := pairOne("iPhone")
	t2, _ := pairOne("iPad")
	pairOne("iPad")
	if _, err := Revoke("iPad"); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Errorf("an ambiguous name: %v", err)
	}
	if _, err := Revoke("nobody"); err == nil {
		t.Error("revoked a device that is not there")
	}
	if d, err := Revoke("iPhone"); err != nil || d.ID != id1 {
		t.Fatalf("by name: %+v, %v", d, err)
	}
	if Holds(t1) || !Holds(t2) {
		t.Errorf("after revoking: first %v, second %v", Holds(t1), Holds(t2))
	}
	// Another process writes the file: this one sees it.
	var devs []Device
	b, _ := os.ReadFile(paths.PhoneDevices())
	json.Unmarshal(b, &devs)
	if err := writeJSON(paths.PhoneDevices(), devs[:0]); err != nil {
		t.Fatal(err)
	}
	if Holds(t2) {
		t.Error("a device removed by hand still holds")
	}
}

func mustDevices(t *testing.T) []Device {
	t.Helper()
	d, err := Devices()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func pairDevice(t *testing.T, name string) (token, id string) {
	t.Helper()
	c, err := PairCode()
	if err != nil {
		t.Fatal(err)
	}
	token = newTok()
	if id, err = Pair(c, name, token); err != nil {
		t.Fatal(err)
	}
	return token, id
}

// A file replaced by one of the same size in the same timestamp tick is
// seen: it is another file.
func TestRevokeEqualSizeSameTick(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tok, _ := pairDevice(t, "iPhone")
	if !Holds(tok) {
		t.Fatal("not held")
	}
	fi, _ := os.Stat(paths.PhoneDevices())
	// Another device in its place: the same size, the same mtime.
	devs := mustDevices(t)
	devs[0].TokenHash = strings.Repeat("0", len(devs[0].TokenHash))
	if err := writeJSON(paths.PhoneDevices(), devs); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(paths.PhoneDevices(), fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.Stat(paths.PhoneDevices()); now.Size() != fi.Size() || !now.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("the test's files differ: %d/%v and %d/%v", fi.Size(), fi.ModTime(), now.Size(), now.ModTime())
	}
	if Holds(tok) {
		t.Error("a revoked token is still accepted")
	}
}

// The code is used up before the device is made: when making it fails,
// the code does not work a second time.
func TestPairFailsClosed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, _ := PairCode()
	// Making the device fails, after the code was used up.
	old := saveDevices
	saveDevices = func([]Device) error { return errors.New("disk full") }
	t.Cleanup(func() { saveDevices = old })
	tok := newTok()
	if _, err := Pair(code, "x", tok); err == nil {
		t.Fatal("paired with no place to keep the device")
	}
	saveDevices = old
	if _, err := Pair(code, "x", tok); err != ErrBadCode {
		t.Errorf("the code worked again after a failure: %v", err)
	}
	if _, err := os.Stat(paths.PhonePairShown()); err == nil {
		t.Error("the used code is still kept for showing")
	}
}

// Expired codes leave the files whenever they are read, and a file that
// can't be read is an error, not an empty list.
func TestPairFilesCleanedAndErrors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, _ := PairCode()
	past := []pairEntry{{Hash: sum(code), Expires: time.Now().Add(-time.Minute)}, {Hash: "x", Expires: time.Now().Add(-time.Hour)}}
	if err := writeJSON(paths.PhonePairs(), past); err != nil {
		t.Fatal(err)
	}
	if _, err := Pair("nope", "x", newTok()); err != ErrBadCode {
		t.Fatal(err)
	}
	var left []pairEntry
	readJSON(paths.PhonePairs(), &left)
	if len(left) != 0 {
		t.Errorf("expired hashes kept: %+v", left)
	}
	if _, err := os.Stat(paths.PhonePairShown()); err == nil {
		t.Error("an expired code is still kept for showing")
	}

	if err := os.WriteFile(paths.PhoneDevices(), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Devices(); err == nil {
		t.Error("a corrupt devices file read as no devices")
	}
	c, _ := PairCode()
	if _, err := Pair(c, "x", newTok()); err == nil || err == ErrBadCode {
		t.Errorf("pairing over a corrupt devices file: %v", err)
	}
	if Holds("anything") {
		t.Error("a corrupt devices file holds someone")
	}
}

// Revoking a device ends the Web Push subscriptions it registered, and
// only those.
func TestRevokeEndsPush(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, id := pairDevice(t, "iPhone")
	_, other := pairDevice(t, "iPad")
	for ep, dev := range map[string]string{"https://push.example/a": id, "https://push.example/b": other, "https://push.example/c": ""} {
		var s push.Subscription
		s.Endpoint = ep
		s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
		s.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
		s.Device = dev
		if err := push.Subscribe(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Revoke(id); err != nil {
		t.Fatal(err)
	}
	subs, _ := push.List()
	var got []string
	for _, s := range subs {
		got = append(got, s.Endpoint)
	}
	if len(got) != 2 || !strings.Contains(strings.Join(got, " "), "/b") || !strings.Contains(strings.Join(got, " "), "/c") {
		t.Errorf("subscriptions left: %v", got)
	}
}

func newTok() string {
	t, _ := randHex()
	return t
}

// A pair whose answer was lost is sent again, the same: it gets the same
// device. The same code with another token gets nothing.
func TestPairReplay(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, _ := PairCode()
	tok := newTok()
	id, err := Pair(code, "iPhone", tok)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := Pair(code, "iPhone", tok); err != nil || again != id {
		t.Errorf("replay: %q, %v; want %q", again, err, id)
	}
	if len(mustDevices(t)) != 1 {
		t.Errorf("a replay made a device: %d", len(mustDevices(t)))
	}
	if _, err := Pair(code, "thief", newTok()); err != ErrBadCode {
		t.Errorf("a consumed code with another token: %v", err)
	}
	if _, err := Pair(code, "x", "NOT-HEX"); err != ErrBadToken {
		t.Errorf("a bad token: %v", err)
	}
	// Another code with a token already taken is no pair.
	c2, _ := PairCode()
	if _, err := Pair(c2, "other", tok); err != ErrBadCode {
		t.Errorf("a token of another device: %v", err)
	}
	// Revoked: the replay finds nothing.
	if _, err := Revoke(id); err != nil {
		t.Fatal(err)
	}
	if _, err := Pair(code, "iPhone", tok); err != ErrBadCode {
		t.Errorf("replay after revoke: %v", err)
	}
	// Only hashes are kept.
	b, _ := os.ReadFile(paths.PhoneDevices())
	if strings.Contains(string(b), tok) {
		t.Error("the token is kept")
	}
}

// Revoke's push cleanup fails: the device stays, and revoking again works.
func TestRevokeRetriedAfterPushFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tok, id := pairDevice(t, "iPhone")
	if err := os.MkdirAll(filepath.Join(paths.StateDir(), "push_subscriptions.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Revoke(id); err == nil {
		t.Fatal("revoked with the push file unreadable")
	}
	if !Holds(tok) || len(mustDevices(t)) != 1 {
		t.Error("the device went with a failed cleanup")
	}
	os.Remove(filepath.Join(paths.StateDir(), "push_subscriptions.json"))
	if _, err := Revoke(id); err != nil || Holds(tok) {
		t.Errorf("retry: %v, held %v", err, Holds(tok))
	}
}

func testSub(ep, dev string) push.Subscription {
	var s push.Subscription
	s.Endpoint = ep
	s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	s.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	s.Device = dev
	return s
}

// A subscribe is before a revoke (and cleaned by it) or after it (and
// refused): none lands after the cleanup.
func TestSubscribeVsRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, id := pairDevice(t, "iPhone")
	if err := SubscribeDevice(testSub("https://push.example/before", id)); err != nil {
		t.Fatal(err)
	}
	if err := SubscribeDevice(testSub("https://push.example/master", "")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = SubscribeDevice(testSub(fmt.Sprintf("https://push.example/race%d", i), id))
		}(i)
	}
	if _, err := Revoke(id); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if err := SubscribeDevice(testSub("https://push.example/after", id)); err != ErrDeviceGone {
		t.Errorf("subscribe after revoke: %v", err)
	}
	subs, _ := push.List()
	for _, s := range subs {
		if s.Device == id {
			t.Errorf("a subscription of the revoked device is left: %s", s.Endpoint)
		}
	}
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/master" {
		t.Errorf("subscriptions: %+v", subs)
	}
}

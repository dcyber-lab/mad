package phone

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
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
	if _, _, err := Pair("0123456789abcdef0123456789abcdef", "x"); err != ErrBadCode {
		t.Errorf("a wrong code: %v", err)
	}
	tok, id, err := Pair(code, "Ann's iPhone")
	if err != nil || len(tok) != 32 || id == "" {
		t.Fatalf("pair: %q %q %v", tok, id, err)
	}
	if _, _, err := Pair(code, "again"); err != ErrBadCode {
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
	if _, _, err := Pair(code, "x"); err != ErrBadCode {
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
			if _, _, err := Pair(code, "p"); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 || len(Devices()) != 1 {
		t.Errorf("%d pairings, %d devices from one code", won, len(Devices()))
	}
}

func TestRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	pairOne := func(name string) (string, string) {
		c, _ := PairCode()
		tok, id, err := Pair(c, name)
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

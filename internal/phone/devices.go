package phone

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// A phone gets in with a pairing code, shown once in the QR and good for
// one use within PairTTL, and is handed a token of its own for it: the
// QR photographed, or the phone lost, is no permanent key, and a device
// can be cut off alone. `mad phone` (one process) mints the codes and
// `mad desktop -serve` (another) takes them, so all of it lives in files
// of the state directory, changed under a lock.

// PairTTL is how long a pairing code lasts.
const PairTTL = 10 * time.Minute

// seenEvery is how seldom a device's last_seen is written.
const seenEvery = time.Minute

// Device is one paired phone. Only the hash of its token is kept.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"last_seen"`
}

type pairEntry struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

// ErrBadCode is a pairing code unknown, used, or out of time.
var ErrBadCode = errors.New("that pairing code is not valid: it was used or has expired")

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// locked runs f holding the lock on the files, across processes.
func locked(f func() error) error {
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(paths.PhoneLock(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return f()
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

// writeJSON replaces path whole, so a reader never sees half of it.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".phone-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func loadDevices() []Device {
	var d []Device
	readJSON(paths.PhoneDevices(), &d)
	return d
}

// PairCode is a code to put in the QR: the last one if it is still good
// and unused, else a new one. Its plaintext is kept for showing the QR
// again until it is used or expires; the check only knows its hash.
func PairCode() (string, error) {
	var code string
	err := locked(func() error {
		now := time.Now()
		var pairs, live []pairEntry
		readJSON(paths.PhonePairs(), &pairs)
		for _, p := range pairs {
			if p.Expires.After(now) {
				live = append(live, p)
			}
		}
		var shown struct {
			Code string `json:"code"`
		}
		readJSON(paths.PhonePairShown(), &shown)
		if shown.Code != "" {
			h := sum(shown.Code)
			for _, p := range live {
				if p.Hash == h {
					code = shown.Code
					return nil
				}
			}
		}
		c, err := randHex()
		if err != nil {
			return err
		}
		live = append(live, pairEntry{Hash: sum(c), Expires: now.Add(PairTTL)})
		if err := writeJSON(paths.PhonePairs(), live); err != nil {
			return err
		}
		if err := writeJSON(paths.PhonePairShown(), map[string]string{"code": c}); err != nil {
			return err
		}
		code = c
		return nil
	})
	return code, err
}

// Pair takes a pairing code, once, and gives the device its token.
func Pair(code, name string) (token, id string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "phone"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	err = locked(func() error {
		now := time.Now()
		var pairs, rest []pairEntry
		readJSON(paths.PhonePairs(), &pairs)
		h := sum(code)
		found := false
		for _, p := range pairs {
			switch {
			case p.Hash == h && p.Expires.After(now) && !found:
				found = true
			case p.Expires.After(now):
				rest = append(rest, p)
			}
		}
		if !found {
			return ErrBadCode
		}
		if token, err = randHex(); err != nil {
			return err
		}
		if id, err = randHex(); err != nil {
			return err
		}
		id = id[:8]
		devs := append(loadDevices(), Device{ID: id, Name: name, TokenHash: sum(token), Created: now, LastSeen: now})
		if err := writeJSON(paths.PhoneDevices(), devs); err != nil {
			return err
		}
		if err := writeJSON(paths.PhonePairs(), rest); err != nil {
			return err
		}
		var shown struct {
			Code string `json:"code"`
		}
		readJSON(paths.PhonePairShown(), &shown)
		if shown.Code == code {
			_ = os.Remove(paths.PhonePairShown())
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return token, id, nil
}

// Devices are the paired phones, oldest first.
func Devices() []Device { return loadDevices() }

// Revoke removes the device with this id, or this name when it is only
// one's, and says which it was.
func Revoke(idOrName string) (Device, error) {
	var gone Device
	err := locked(func() error {
		devs := loadDevices()
		at := -1
		for i, d := range devs {
			if d.ID == idOrName {
				at = i
				break
			}
		}
		if at < 0 {
			for i, d := range devs {
				if d.Name == idOrName {
					if at >= 0 {
						return fmt.Errorf("more than one device is named %q: use its id (mad phone devices)", idOrName)
					}
					at = i
				}
			}
		}
		if at < 0 {
			return fmt.Errorf("no paired device %q (mad phone devices)", idOrName)
		}
		gone = devs[at]
		return writeJSON(paths.PhoneDevices(), append(devs[:at:at], devs[at+1:]...))
	})
	return gone, err
}

// The devices as the server last read them: the file is read again when
// its mtime or size moves, which is how another process's revoke is seen.
var (
	cacheMu   sync.Mutex
	cacheMod  time.Time
	cacheSize int64             = -1
	cacheDevs map[string]Device // by token hash
)

func byHash() map[string]Device {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	fi, err := os.Stat(paths.PhoneDevices())
	if err != nil {
		cacheMod, cacheSize, cacheDevs = time.Time{}, -1, nil
		return nil
	}
	if !fi.ModTime().Equal(cacheMod) || fi.Size() != cacheSize || cacheDevs == nil {
		m := map[string]Device{}
		for _, d := range loadDevices() {
			m[d.TokenHash] = d
		}
		cacheMod, cacheSize, cacheDevs = fi.ModTime(), fi.Size(), m
	}
	return cacheDevs
}

// DeviceFor is the paired device holding token, if any, and notes that it
// was seen (at most once a minute).
func DeviceFor(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	h := sum(token)
	d, ok := byHash()[h]
	if ok && time.Since(d.LastSeen) >= seenEvery {
		_ = locked(func() error {
			devs := loadDevices()
			for i := range devs {
				if devs[i].TokenHash == h {
					devs[i].LastSeen = time.Now()
					return writeJSON(paths.PhoneDevices(), devs)
				}
			}
			return nil
		})
	}
	return d, ok
}

// Holds says whether the device with this token is still paired.
func Holds(token string) bool {
	_, ok := byHash()[sum(token)]
	return ok
}

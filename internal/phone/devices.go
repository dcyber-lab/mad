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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/push"
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
	CodeHash  string    `json:"code_hash,omitempty"` // of the pairing code it came in with
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

// readJSON loads path into v; a file not there is empty, anything else
// that goes wrong is an error, and the caller fails closed.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	return nil
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

func loadDevices() ([]Device, error) {
	var d []Device
	return d, readJSON(paths.PhoneDevices(), &d)
}

// livePairs are the codes not yet expired, and the code shown for the
// QR if it is one of them. An expired hash, or a shown code that is no
// longer good, is removed here, whenever the files are read; the lock is held.
func livePairs(now time.Time) (live []pairEntry, shown string, err error) {
	var pairs []pairEntry
	if err := readJSON(paths.PhonePairs(), &pairs); err != nil {
		return nil, "", err
	}
	for _, p := range pairs {
		if p.Expires.After(now) {
			live = append(live, p)
		}
	}
	if len(live) != len(pairs) {
		if err := writeJSON(paths.PhonePairs(), live); err != nil {
			return nil, "", err
		}
	}
	var sh struct {
		Code string `json:"code"`
	}
	if err := readJSON(paths.PhonePairShown(), &sh); err != nil {
		return nil, "", err
	}
	for _, p := range live {
		if sh.Code != "" && p.Hash == sum(sh.Code) {
			return live, sh.Code, nil
		}
	}
	if err := os.Remove(paths.PhonePairShown()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	return live, "", nil
}

// PairCode is a code to put in the QR: the last one if it is still good
// and unused, else a new one. Its plaintext is kept for showing the QR
// again until it is used or expires; the check only knows its hash.
func PairCode() (string, error) {
	var code string
	err := locked(func() error {
		now := time.Now()
		live, shown, err := livePairs(now)
		if err != nil {
			return err
		}
		if shown != "" {
			code = shown
			return nil
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

// saveDevices writes the devices file; tests replace it to fail.
var saveDevices = func(devs []Device) error { return writeJSON(paths.PhoneDevices(), devs) }

// ErrBadToken is a token that is not 32 lowercase hex digits.
var ErrBadToken = errors.New("a device's token is 32 lowercase hex digits, made on the device")

var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Pair takes a pairing code, once, for the device whose own token this is
// (made on the device, so it is the same in a retry); only its hash is
// kept. The code is used up before the device is made: a failure between
// the two costs a new scan, never a code that works for another. A retry
// of a pair whose answer was lost, with the same code and the same token,
// finds the device and gets its id again; the same code with another
// token is refused.
func Pair(code, name, token string) (id string, err error) {
	if !tokenRe.MatchString(token) {
		return "", ErrBadToken
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "phone"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	err = locked(func() error {
		now := time.Now()
		live, shown, err := livePairs(now)
		if err != nil {
			return err
		}
		devs, err := loadDevices()
		if err != nil {
			return err
		}
		h, th := sum(code), sum(token)
		var rest []pairEntry
		found := false
		for _, p := range live {
			if p.Hash == h && !found {
				found = true
			} else {
				rest = append(rest, p)
			}
		}
		if !found {
			for _, d := range devs {
				if d.CodeHash == h && d.TokenHash == th {
					id = d.ID
					return nil
				}
			}
			return ErrBadCode
		}
		for _, d := range devs {
			if d.TokenHash == th {
				return ErrBadCode // that token is another device's
			}
		}
		if err := writeJSON(paths.PhonePairs(), rest); err != nil {
			return err
		}
		if shown == code {
			if err := os.Remove(paths.PhonePairShown()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if id, err = randHex(); err != nil {
			return err
		}
		id = id[:8]
		return saveDevices(append(devs, Device{ID: id, Name: name, TokenHash: th, CodeHash: h, Created: now, LastSeen: now}))
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ErrDeviceGone is a subscribe from a device that is no longer paired.
var ErrDeviceGone = errors.New("that device is no longer paired")

// SubscribeDevice keeps a Web Push subscription for the device sub.Device
// ("" is the master token's, which is always there), unless it was revoked
// meanwhile: under the lock Revoke holds, so a subscribe can't land after
// its cleanup.
func SubscribeDevice(sub push.Subscription) error {
	return locked(func() error {
		if sub.Device != "" {
			devs, err := loadDevices()
			if err != nil {
				return err
			}
			ok := false
			for _, d := range devs {
				ok = ok || d.ID == sub.Device
			}
			if !ok {
				return ErrDeviceGone
			}
		}
		return push.Subscribe(sub)
	})
}

// Devices are the paired phones, oldest first.
func Devices() ([]Device, error) { return loadDevices() }

// Revoke removes the device with this id, or this name when it is only
// one's, and says which it was. Its Web Push subscriptions go with it.
func Revoke(idOrName string) (Device, error) {
	var gone Device
	err := locked(func() error {
		devs, err := loadDevices()
		if err != nil {
			return err
		}
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
		// Notifications carry what agents wrote: not to a device cut off.
		// Its push goes first, so a failure leaves the device to revoke
		// again.
		if err := push.UnsubscribeDevice(gone.ID); err != nil {
			return err
		}
		return writeJSON(paths.PhoneDevices(), append(devs[:at:at], devs[at+1:]...))
	})
	return gone, err
}

// The devices as the server last read them: the file is read again when
// its mtime or size moves, which is how another process's revoke is seen.
var (
	cacheMu   sync.Mutex
	cacheInfo os.FileInfo
	cacheDevs map[string]Device // by token hash
)

// byHash is the devices by token hash. The file is read again when it is
// another file (devices are written by rename, so that is a new inode),
// or its mtime or size moved: an equal-size replacement in the same
// timestamp tick is still seen. A file that can't be read holds nobody.
func byHash() map[string]Device {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	fi, err := os.Stat(paths.PhoneDevices())
	if err != nil {
		cacheInfo, cacheDevs = nil, nil
		return nil
	}
	if cacheDevs == nil || cacheInfo == nil || !os.SameFile(cacheInfo, fi) ||
		!fi.ModTime().Equal(cacheInfo.ModTime()) || fi.Size() != cacheInfo.Size() {
		m := map[string]Device{}
		devs, err := loadDevices()
		if err != nil {
			cacheInfo, cacheDevs = nil, nil
			return nil
		}
		for _, d := range devs {
			m[d.TokenHash] = d
		}
		cacheInfo, cacheDevs = fi, m
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
			devs, err := loadDevices()
			if err != nil {
				return err
			}
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

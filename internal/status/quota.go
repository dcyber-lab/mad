package status

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Quota is how much of a subscription's allowance is used: the rolling
// five-hour window and the weekly one. It belongs to the account, not to
// an agent, so it is kept per agent kind. Claude reports it to the status
// line command mad injects; codex writes it into its rollout after every
// response.
type Quota struct {
	FiveHour Window    `json:"five_hour"`
	SevenDay Window    `json:"seven_day"`
	At       time.Time `json:"at"` // when it was reported
}

// Window is one metered window.
type Window struct {
	Used    float64   `json:"used"`               // percent, 0 to 100 (or above, over a spend limit)
	ResetAt time.Time `json:"reset_at,omitempty"` // zero when unknown
}

// Known reports whether the window was ever reported.
func (w Window) Known() bool { return w.Used > 0 || !w.ResetAt.IsZero() }

// Expire zeroes a window whose reset time has passed: nothing of the new
// window is used yet, and the next report says when it resets.
func (q Quota) Expire(now time.Time) Quota {
	for _, w := range []*Window{&q.FiveHour, &q.SevenDay} {
		if !w.ResetAt.IsZero() && !now.Before(w.ResetAt) {
			*w = Window{}
		}
	}
	return q
}

// ParseStatusLine reads the JSON Claude Code gives its status line
// command and returns the rate limits in it, if any: they are present for
// claude.ai subscribers after the first response, and absent on an API
// key. A window whose reset has passed is dropped by Claude itself.
func ParseStatusLine(r io.Reader) (Quota, bool) {
	var ev struct {
		RateLimits *struct {
			FiveHour *statusWindow `json:"five_hour"`
			SevenDay *statusWindow `json:"seven_day"`
		} `json:"rate_limits"`
	}
	data, _ := io.ReadAll(r)
	if json.Unmarshal(data, &ev) != nil || ev.RateLimits == nil {
		return Quota{}, false
	}
	q := Quota{FiveHour: ev.RateLimits.FiveHour.window(), SevenDay: ev.RateLimits.SevenDay.window()}
	return q, q.FiveHour.Known() || q.SevenDay.Known()
}

type statusWindow struct {
	Used    float64 `json:"used_percentage"`
	ResetAt int64   `json:"resets_at"` // unix seconds
}

func (w *statusWindow) window() Window {
	if w == nil {
		return Window{}
	}
	out := Window{Used: w.Used}
	if w.ResetAt > 0 {
		out.ResetAt = time.Unix(w.ResetAt, 0)
	}
	return out
}

func QuotaPath(kind string) string { return filepath.Join(paths.StatusDir(), "quota-"+kind+".json") }

// WriteQuota records the latest report for kind.
func WriteQuota(kind string, q Quota, now time.Time) error {
	q.At = now
	data, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(QuotaPath(kind), data)
}

// ReadQuota returns the last report for kind, if there is one.
func ReadQuota(kind string) (Quota, bool) {
	data, err := os.ReadFile(QuotaPath(kind))
	if err != nil {
		return Quota{}, false
	}
	var q Quota
	if json.Unmarshal(data, &q) != nil || q.At.IsZero() {
		return Quota{}, false
	}
	return q, true
}

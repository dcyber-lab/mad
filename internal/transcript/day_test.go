package transcript

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
)

// priced is a response of claude-haiku-4-5 ($5 per million output tokens)
// costing usd, logged at when.
func priced(id string, usd float64, when time.Time) map[string]any {
	return map[string]any{
		"type": "assistant", "timestamp": when.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"id": id, "model": "claude-haiku-4-5-20251001", "content": []any{},
			"usage": map[string]any{"output_tokens": int64(usd * 200000)},
		},
	}
}

func TestDay(t *testing.T) {
	dir := t.TempDir()
	zone := time.FixedZone("GMT+8", 8*3600)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, zone)
	yesterday := time.Date(2026, 9, 29, 23, 30, 0, 0, zone) // 15:30 UTC: still the 29th here
	early := time.Date(2026, 9, 30, 0, 30, 0, 0, zone)      // 16:30 UTC on the 29th: the 30th here

	session := filepath.Join(dir, "p", "s1.jsonl")
	fork := filepath.Join(dir, "p", "s2.jsonl")
	sub := filepath.Join(dir, "p", "s1", "subagents", "a.jsonl")
	later := filepath.Join(dir, "p", "s3.jsonl")
	files := []string{session, fork, sub}
	writeLines(t, session,
		map[string]any{"type": "user", "timestamp": early.Format(time.RFC3339), "message": map[string]any{"content": "hi"}},
		priced("old", 7, yesterday),
		priced("m1", 1, early),
		priced("m1", 1, early), // the same response, one line per content block
		priced("m2", 2, now.Add(-time.Hour)))
	writeLines(t, fork, priced("m1", 1, early), priced("m3", 4, now.Add(-time.Minute))) // m1 copied over
	writeLines(t, sub, priced("s1", 0.5, now.Add(-time.Minute)))

	d := NewDay()
	d.written = func(p discover.Provider, since time.Time) []string {
		if want := time.Date(2026, 9, 30, 0, 0, 0, 0, zone); !since.Equal(want) {
			t.Errorf("since = %v, want local midnight", since)
		}
		if p.Kind() != "claude" {
			return nil
		}
		return files
	}
	check := func(at time.Time, want float64, what string) {
		t.Helper()
		if got := d.Read(at); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: $%.4f, want $%.4f", what, got, want)
		}
	}
	check(now, 7.5, "first read") // m1 1 + m2 2 + m3 4 + s1 0.5

	appendLines(t, session, 0, priced("m4", 3, now))
	check(now.Add(time.Second), 10.5, "appended")

	// A new session is found at the next listing.
	writeLines(t, later, priced("m5", 1, now))
	files = append(files, later)
	check(now.Add(2*time.Second), 10.5, "before relisting")
	check(now.Add(relistEvery), 11.5, "relisted")

	// Rewritten shorter: read again, nothing counted twice.
	writeLines(t, session, priced("m1", 1, early))
	check(now.Add(relistEvery+time.Second), 11.5, "rewritten")

	// A new day starts from nothing.
	os.Remove(later)
	next := time.Date(2026, 10, 1, 9, 0, 0, 0, zone)
	d.written = func(p discover.Provider, since time.Time) []string {
		if p.Kind() != "claude" {
			return nil
		}
		return files
	}
	appendLines(t, fork, 0, priced("m6", 2, next))
	check(next, 2, "next day")
}

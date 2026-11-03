package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A thread whose rollout is not written yet is looked for under today's
// day on every call, and under every day only every codexMissEvery.
func TestCodexTranscriptsMissed(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)
	old := codexNow
	t.Cleanup(func() { codexNow = old })
	codexNow = func() time.Time { return now }
	write := func(day, sid string) string {
		t.Helper()
		dir := filepath.Join(codexSessionsDir(), day)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "rollout-2026-01-05T10-00-00-"+sid+".jsonl")
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	c := codex{}
	adopted := write("2026/01/05", "th-adopted")
	if got := c.Transcripts("", "th-adopted"); len(got) != 1 || got[0] != adopted {
		t.Fatalf("an older session at its first look: %v", got)
	}

	if got := c.Transcripts("", "th-new"); got != nil {
		t.Fatalf("no rollout yet: %v", got)
	}
	late := write("2026/01/06", "th-new") // written where only the full search looks
	if got := c.Transcripts("", "th-new"); got != nil {
		t.Fatalf("every day looked through again within %v: %v", codexMissEvery, got)
	}
	now = now.Add(codexMissEvery)
	if got := c.Transcripts("", "th-new"); len(got) != 1 || got[0] != late {
		t.Fatalf("after %v: %v", codexMissEvery, got)
	}

	if got := c.Transcripts("", "th-today"); got != nil {
		t.Fatalf("no rollout yet: %v", got)
	}
	today := write(now.Format("2006/01/02"), "th-today")
	if got := c.Transcripts("", "th-today"); len(got) != 1 || got[0] != today {
		t.Fatalf("today's rollout at once: %v", got)
	}
}

// A claude session in no project is looked for in every project again only
// every claudeMissEvery; one under the agent's own project at once.
func TestClaudeTranscriptsMissed(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	old := claudeNow
	t.Cleanup(func() { claudeNow = old })
	claudeNow = func() time.Time { return now }
	app, other := f.project("app"), f.project("other")
	c := claude{}
	if got := c.Transcripts(app, "s9"); got != nil {
		t.Fatalf("no session yet: %v", got)
	}
	elsewhere := filepath.Join(claudeProjectDir(other), "s9.jsonl")
	f.write(elsewhere, []any{map[string]any{"type": "user"}}, now)
	if got := c.Transcripts(app, "s9"); got != nil {
		t.Fatalf("every project looked through again within %v: %v", claudeMissEvery, got)
	}
	now = now.Add(claudeMissEvery)
	if got := c.Transcripts(app, "s9"); len(got) != 1 || got[0] != elsewhere {
		t.Fatalf("after %v: %v", claudeMissEvery, got)
	}

	if got := c.Transcripts(app, "s10"); got != nil {
		t.Fatalf("no session yet: %v", got)
	}
	own := filepath.Join(claudeProjectDir(app), "s10.jsonl")
	f.write(own, []any{map[string]any{"type": "user"}}, now)
	if got := c.Transcripts(app, "s10"); len(got) != 1 || got[0] != own {
		t.Fatalf("under its own project at once: %v", got)
	}
}

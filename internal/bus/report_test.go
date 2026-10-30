package bus

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

var idRe = regexp.MustCompile(`\b[th]-[0-9a-f]{8}\b`)

// reportDeck is a channel with five open tasks in it.
func reportDeck(t *testing.T) (map[string]string, *state.State) {
	t.Helper()
	followSetup(t)
	mustCreate(t, "api-redesign", idArch, idBuild, idCodex)
	readHook = func(id string) *status.Hook {
		if id == idBuild {
			return &status.Hook{State: status.Waiting, Message: "Bash go test\nmore", At: now()}
		}
		return nil
	}
	mk := func(owner, title, st string, extra ...func(*TaskChange)) Task {
		c := TaskChange{Title: sp(title), Owner: sp(owner), Status: sp(st), Channel: sp("api-redesign")}
		for _, f := range extra {
			f(&c)
		}
		tk, err := SetTask(me, c)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	base := mk(idCodex, "write the handler", "doing")
	mk(idBuild, "run the tests", "doing")
	mk(idArch, "pick the database", "doing")
	mk(idCodex, "wire the route", "todo", func(c *TaskChange) { c.WaitsOn = &[]string{base.ID} })
	mk(idCodex, "check the docs", "review")
	setNow(time.Minute)
	mustPost(t, "api-redesign", arch, "@you which database do we use?")
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{idArch: status.Idle, idBuild: status.Waiting, idCodex: status.Idle}, st
}

func TestReport(t *testing.T) {
	statuses, st := reportDeck(t)
	got := idRe.ReplaceAllString(Report(st, statuses, now().Add(time.Minute)), "X-id")
	want := "5 open · 2 waiting for you · 1 doing · 1 blocked · 1 review\n" +
		"api-redesign: 1 doing (codex 1a2b3c4d), 1 blocked (X-id waits), 1 review, " +
		"2 waiting for you (permission: builder · Bash go test; @you from architect)"
	if got != want {
		t.Errorf("Report:\n%s\nwant:\n%s", got, want)
	}
	ch := idRe.ReplaceAllString(ReportChannel(st, statuses, now().Add(time.Minute), "api-redesign"), "X-id")
	if !strings.HasPrefix(want, "5 open") || ch != strings.SplitN(want, "\n", 2)[1] {
		t.Errorf("ReportChannel = %q", ch)
	}
	if ReportChannel(st, statuses, now(), "elsewhere") != "" {
		t.Error("a channel with nothing open has a report")
	}
	lines := ReportLines(st, statuses, now().Add(time.Minute))
	if len(lines) != 1 || lines["api-redesign"] != ch && idRe.ReplaceAllString(lines["api-redesign"], "X-id") != ch {
		t.Errorf("ReportLines = %v", lines)
	}
}

func TestReportEmpty(t *testing.T) {
	followSetup(t)
	st, _ := state.Load()
	if r := Report(st, idleBoth, now()); r != "" {
		t.Errorf("empty deck: %q", r)
	}
	// Done work is not open.
	if _, err := SetTask(me, TaskChange{Title: sp("done one"), Owner: sp(idBuild), Status: sp("done")}); err != nil {
		t.Fatal(err)
	}
	if r := Report(st, idleBoth, now()); r != "" {
		t.Errorf("only done: %q", r)
	}
}

func TestReportDMAndNoChannel(t *testing.T) {
	followSetup(t)
	mustHand(t, arch, idBuild, "build the thing", "")
	if _, err := SetTask(me, TaskChange{Title: sp("loose end"), Owner: sp(idCodex)}); err != nil {
		t.Fatal(err)
	}
	st, _ := state.Load()
	got := idRe.ReplaceAllString(Report(st, idleBoth, now()), "X-id")
	want := "2 open · 2 todo\nDMs: 1 todo\nno channel: 1 todo"
	if got != want {
		t.Errorf("Report:\n%s\nwant:\n%s", got, want)
	}
}

func TestCheckReport(t *testing.T) {
	statuses, st := reportDeck(t)
	// The first look only learns what waits.
	if err := CheckReport(st, statuses, now(), ""); err != nil {
		t.Fatal(err)
	}
	if len(personTold) != 0 {
		t.Fatalf("told on first look: %q", personTold)
	}
	if _, err := os.Stat(reportFile()); err != nil {
		t.Fatalf("no report.json: %v", err)
	}
	// Nothing new: nothing sent.
	if err := CheckReport(st, statuses, now().Add(time.Hour), ""); err != nil || len(personTold) != 0 {
		t.Fatalf("told %q %v", personTold, err)
	}
	// Something new waits for the person.
	mk := func(title string) {
		if _, err := SetTask(me, TaskChange{Title: sp(title), Owner: sp(You), Channel: sp("api-redesign")}); err != nil {
			t.Fatal(err)
		}
	}
	setNow(2 * time.Hour)
	mk("sign off the design")
	if err := CheckReport(st, statuses, now(), ""); err != nil {
		t.Fatal(err)
	}
	if len(personTold) != 1 || !strings.HasPrefix(personTold[0], "report: 6 open") || !strings.Contains(personTold[0], "3 waiting for you") {
		t.Fatalf("told %q", personTold)
	}
	// Another within 10 minutes waits; it goes once the 10 are over, once.
	setNow(2*time.Hour + 5*time.Minute)
	mk("and the schema")
	_ = CheckReport(st, statuses, now(), "")
	if len(personTold) != 1 {
		t.Fatalf("told within 10 minutes: %q", personTold)
	}
	setNow(2*time.Hour + 11*time.Minute)
	_ = CheckReport(st, statuses, now(), "")
	_ = CheckReport(st, statuses, now().Add(time.Minute), "")
	if len(personTold) != 2 || !strings.Contains(personTold[1], "4 waiting for you") {
		t.Fatalf("told %q", personTold)
	}
}

func TestCheckReportDaily(t *testing.T) {
	statuses, st := reportDeck(t)
	_ = CheckReport(st, statuses, now(), "14:30") // learns
	at := func(h, m int) time.Time { return time.Date(2026, 10, 30, h, m, 0, 0, time.UTC) }
	_ = CheckReport(st, statuses, at(14, 29), "14:30")
	if len(personTold) != 0 {
		t.Fatalf("early: %q", personTold)
	}
	_ = CheckReport(st, statuses, at(14, 30), "14:30")
	_ = CheckReport(st, statuses, at(15, 0), "14:30")
	if len(personTold) != 1 || !strings.HasPrefix(personTold[0], "report: 5 open") {
		t.Fatalf("daily: %q", personTold)
	}
	// Tomorrow at the time again; but not at night, long after it.
	_ = CheckReport(st, statuses, at(23, 0).Add(24*time.Hour), "14:30")
	if len(personTold) != 1 {
		t.Fatalf("late: %q", personTold)
	}
	_ = CheckReport(st, statuses, at(14, 31).Add(24*time.Hour), "14:30")
	if len(personTold) != 2 {
		t.Fatalf("next day: %q", personTold)
	}
	// "" is never.
	_ = CheckReport(st, statuses, at(14, 31).Add(48*time.Hour), "")
	if len(personTold) != 2 {
		t.Fatalf("off: %q", personTold)
	}
}

// A report that comes with a followup notification is folded into it.
func TestCheckReportCoalescesWithFollowup(t *testing.T) {
	statuses, st := reportDeck(t)
	_ = CheckReport(st, statuses, now(), "")
	setNow(30 * time.Minute)
	if _, err := SetTask(me, TaskChange{Title: sp("sign off"), Owner: sp(You)}); err != nil {
		t.Fatal(err)
	}
	_ = changeNudges(func(ns *nudgeStore) error {
		ns.Sent = map[string]time.Time{"followup": now().Add(-10 * time.Second)}
		return nil
	})
	_ = CheckReport(st, statuses, now(), "")
	if len(personTold) != 0 {
		t.Fatalf("not folded: %q", personTold)
	}
	// ...and what it would have said is not said again.
	_ = CheckReport(st, statuses, now().Add(time.Hour), "")
	if len(personTold) != 0 {
		t.Fatalf("said later: %q", personTold)
	}
}

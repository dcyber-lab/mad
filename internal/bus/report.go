package bus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// Report timing, replaceable in tests.
var (
	// reportMin is the least time between two report notifications.
	reportMin = 10 * time.Minute
	// reportWindow: the daily report goes out within this long after its
	// time of day, not at night because work appeared late.
	reportWindow = time.Hour
	// coalesce: a report this soon after a followup notification is
	// folded into it.
	coalesce = 15 * time.Second
)

// Report is the board in words, from Board: a first line with the totals,
// then a line for each channel with the work open in it:
//
//	3 open · 2 waiting for you · 1 doing
//	api-redesign: 1 doing (builder), 2 waiting for you (permission: builder · Bash go test; @you from architect)
//
// It is "" when nothing is open. Done and dropped work is not open.
func Report(st *state.State, statuses map[string]string, at time.Time) string {
	cards, err := Board(st, statuses, at)
	if err != nil {
		return ""
	}
	return reportOf(cards, "", true)
}

// ReportChannel is Report's line for channel alone (no totals), or "".
func ReportChannel(st *state.State, statuses map[string]string, at time.Time, channel string) string {
	cards, err := Board(st, statuses, at)
	if err != nil {
		return ""
	}
	return reportOf(cards, channel, false)
}

// ReportLines is Report's channel lines by channel id; work in DMs or in
// no channel is under "".
func ReportLines(st *state.State, statuses map[string]string, at time.Time) map[string]string {
	cards, err := Board(st, statuses, at)
	out := map[string]string{}
	if err != nil {
		return out
	}
	for _, k := range reportChannels(cards) {
		id := k
		if k == noChannel || k == dmsName {
			id = ""
		}
		if line := reportOf(cards, k, false); line != "" && id != "" {
			out[id] = line
		}
	}
	return out
}

const (
	noChannel = "no channel"
	dmsName   = "DMs"
)

// groupOf is the name of the group a card is reported under.
func groupOf(c Card) string {
	switch {
	case c.Channel == "":
		return noChannel
	case isDM(c.Channel):
		return dmsName
	}
	return c.Channel
}

// reportChannels are the groups with open work, channels by name, then the
// DMs, then work in no channel.
func reportChannels(cards []Card) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cards {
		if c.Column == ColDone {
			continue
		}
		if g := groupOf(c); !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	rank := func(g string) int {
		switch g {
		case dmsName:
			return 1
		case noChannel:
			return 2
		}
		return 0
	}
	sort.Slice(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// reportOf writes the report of cards: for one group when only is not "",
// else for all, with the totals line when totals.
func reportOf(cards []Card, only string, totals bool) string {
	var lines []string
	count := map[string]int{}
	for _, g := range reportChannels(cards) {
		if only != "" && g != only {
			continue
		}
		var by = map[string][]Card{}
		for _, c := range cards {
			if c.Column != ColDone && groupOf(c) == g {
				by[c.Column] = append(by[c.Column], c)
				count[c.Column]++
			}
		}
		var parts []string
		for _, col := range []string{ColDoing, ColBlocked, ColReview, ColTodo, ColWaitingYou} {
			cs := by[col]
			if len(cs) == 0 {
				continue
			}
			part := fmt.Sprintf("%d %s", len(cs), columnWord(col))
			if d := detail(col, cs); d != "" {
				part += " (" + d + ")"
			}
			parts = append(parts, part)
		}
		lines = append(lines, g+": "+strings.Join(parts, ", "))
	}
	if len(lines) == 0 {
		return ""
	}
	if !totals {
		return strings.Join(lines, "\n")
	}
	n := 0
	var parts []string
	for _, col := range []string{ColWaitingYou, ColDoing, ColBlocked, ColReview, ColTodo} {
		if count[col] > 0 {
			n += count[col]
			parts = append(parts, fmt.Sprintf("%d %s", count[col], columnWord(col)))
		}
	}
	return strings.Join(append([]string{fmt.Sprintf("%d open · %s", n, strings.Join(parts, " · "))}, lines...), "\n")
}

func columnWord(col string) string {
	if col == ColWaitingYou {
		return "waiting for you"
	}
	return col
}

// detail says, for the cards of a column in a group, who has them or why
// they wait.
func detail(col string, cs []Card) string {
	var out []string
	add := func(s string) {
		for _, x := range out {
			if x == s {
				return
			}
		}
		out = append(out, s)
	}
	for _, c := range cs {
		owner := c.OwnerLabel
		if owner == "" {
			owner = "unowned"
		}
		id := ""
		if c.Task != nil {
			id = c.Task.ID
		}
		switch col {
		case ColDoing:
			add(owner)
		case ColBlocked:
			ref := id
			if c.Why == WhyLapsed && c.Task != nil && c.Task.Handoff != "" {
				ref = c.Task.Handoff
			}
			add(ref + " " + c.Why)
		case ColWaitingYou:
			switch {
			case c.Why == WhyPermission:
				s := "permission: " + owner
				if hk := readHook(c.Owner); hk != nil && hk.State == status.Waiting && strings.TrimSpace(hk.Message) != "" {
					s += " · " + firstLine(hk.Message, 60)
				}
				add(s)
			case c.Why == WhyPaused:
				add("paused")
			case c.Why == WhyAsked:
				add("@you from " + owner)
			case c.Why == WhyUnowned:
				add("unowned " + id)
			case c.Owner == You && c.Task != nil:
				add("yours: " + firstLine(c.Title, 40))
			default:
				add(c.Title)
			}
		}
	}
	return strings.Join(out, "; ")
}

// reportState is bus/report.json: what the last report notification was
// about.
type reportState struct {
	// Day is the date of the last daily report.
	Day string `json:"day,omitempty"`
	// At is when a report notification was last sent.
	At time.Time `json:"at,omitzero"`
	// Waiting are the keys of the cards that waited for the person then.
	Waiting []string `json:"waiting,omitempty"`
}

func reportFile() string { return filepath.Join(dir(), "report.json") }

// waitingKeys names the cards that wait for the person, to tell new ones.
func waitingKeys(cards []Card) []string {
	var out []string
	for _, c := range cards {
		if c.Column != ColWaitingYou {
			continue
		}
		k := c.Why + ":" + c.Channel
		switch {
		case c.Task != nil:
			k = c.Task.ID + ":" + c.Why
		case c.Message != nil:
			k += "#" + strconv.Itoa(c.Message.Seq)
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CheckReport sends the report as a notification of kind "report" (the
// sidebar runs it every minute, after CheckFollowUps): once a day within an
// hour after clock ("15:04", local; "" for never) while anything is open,
// and when a card new to the person's list waits for them. Never twice
// within ten minutes, and not when a followup notification has just gone
// out (within 15 seconds): that one stands for both. The first call on a
// deck only learns what waits already. State is kept in bus/report.json.
func CheckReport(st *state.State, statuses map[string]string, at time.Time, clock string) error {
	cards, err := Board(st, statuses, at)
	if err != nil {
		return err
	}
	text := reportOf(cards, "", true)
	waiting := waitingKeys(cards)
	var rs reportState
	data, rerr := os.ReadFile(reportFile())
	first := rerr != nil
	if !first {
		_ = json.Unmarshal(data, &rs)
	}
	grew := false
	for _, k := range waiting {
		if !has(rs.Waiting, k) {
			grew = true
		}
	}
	daily := false
	if t, err := time.Parse("15:04", clock); err == nil && clock != "" {
		start := time.Date(at.Year(), at.Month(), at.Day(), t.Hour(), t.Minute(), 0, 0, at.Location())
		daily = !at.Before(start) && at.Sub(start) < reportWindow && rs.Day != at.Format("2006-01-02")
	}
	send := text != "" && !first && (daily || grew) && at.Sub(rs.At) >= reportMin
	folded := send && at.Sub(lastFollowupNotice()) <= coalesce && at.Sub(lastFollowupNotice()) >= 0
	switch {
	case send:
		rs.At, rs.Waiting = at, waiting
		if daily {
			rs.Day = at.Format("2006-01-02")
		}
		if !folded {
			tellPerson(notify.Report, text)
		}
	case first || !grew:
		rs.Waiting = waiting // learned, or some were answered
	}
	if first && daily && text != "" {
		// The deck starts at its report time: a first report is welcome.
		rs.At, rs.Day = at, at.Format("2006-01-02")
		tellPerson(notify.Report, text)
	}
	out, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	if string(out) == string(data) {
		return nil
	}
	return locked(func() error { return writeAtomic(reportFile(), out) })
}

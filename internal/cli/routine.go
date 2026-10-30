package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
)

const routineUsage = `usage:
  mad routine add SCHEDULE CHANNEL [TEXT...|--report]
                                      post TEXT in CHANNEL on a schedule (an @agent in
                                      it is woken), or the channel's progress report
  mad routine ls                      the routines: next time, last, misses
  mad routine rm|pause|resume ID
  mad routine fire ID                 go off now
  mad routine checkins                the check-ins agents booked for themselves

SCHEDULE, quoted: daily 09:00, weekdays 09:00, weekends 10:00, mon,wed 09:00,
every 30m, every 2h 08:00-18:00 (at least every 15m).
Inside an agent (MAD_AGENT_ID set) routines are the person's: only rm of
the agent's own check-ins works.
`

// routineCmd is `mad routine`: the person's scheduled posts, and a look at
// the agents' check-ins.
func routineCmd(args []string, stdio IO) error {
	if len(args) == 0 {
		fmt.Fprint(stdio.Err, routineUsage)
		return errUsage
	}
	sub, args := args[0], args[1:]
	agentID := os.Getenv("MAD_AGENT_ID")
	if agentID != "" && sub != "rm" && sub != "ls" && sub != "checkins" {
		return errors.New("routines are the person's to set: ask them with @you; book a check-in for yourself with bus_checkin")
	}
	switch sub {
	case "ls":
		return routineList(stdio.Out)
	case "checkins":
		return routineCheckins(stdio.Out)
	case "add":
		return routineAdd(args, stdio)
	case "rm", "pause", "resume", "fire":
		if len(args) != 1 {
			fmt.Fprint(stdio.Err, routineUsage)
			return errUsage
		}
		id := strings.TrimSpace(args[0])
		switch sub {
		case "rm":
			by := bus.You
			if agentID != "" {
				by = agentID
			}
			return bus.RemoveTimer(id, by)
		case "pause", "resume":
			return bus.PauseTimer(id, sub == "pause", time.Now())
		}
		st, err := state.Load()
		if err != nil {
			return err
		}
		if err := bus.FireTimer(id, time.Now(), drive.Statuses(st, kinds())); err != nil {
			return err
		}
		fmt.Fprintln(stdio.Out, "fired", id)
		return nil
	}
	fmt.Fprint(stdio.Err, routineUsage)
	return errUsage
}

func routineAdd(args []string, stdio IO) error {
	var words []string
	report := false
	for _, a := range args {
		if a == "--report" || a == "-report" {
			report = true
		} else {
			words = append(words, a)
		}
	}
	if len(words) < 2 || report && len(words) != 2 {
		fmt.Fprint(stdio.Err, routineUsage)
		return errUsage
	}
	t, err := bus.AddRoutine(bus.RoutineSpec{Every: words[0], Channel: words[1], Text: strings.Join(words[2:], " "), Report: report}, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdio.Out, "%s  %s  next %s\n", t.ID, t.Every, routineWhen(t.NextAt, time.Now()))
	return nil
}

// routineWhen writes a time as 15:04 today, Mon 15:04 within a week, or
// the date.
func routineWhen(t, at time.Time) string {
	switch {
	case t.IsZero():
		return "-"
	case t.Format("2006-01-02") == at.Format("2006-01-02"):
		return t.Format("15:04")
	case t.Sub(at) < 6*24*time.Hour && t.After(at):
		return t.Format("Mon 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

func routineList(out io.Writer) error {
	ts, err := bus.Timers()
	if err != nil {
		return err
	}
	at := time.Now()
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	n := 0
	fmt.Fprintln(tw, "ID\tSCHEDULE\tCHANNEL\tTEXT\tNEXT\tLAST\tFIRED\tMISSED\tSTATE")
	for _, t := range ts {
		if t.Kind != bus.TimerRoutine {
			continue
		}
		n++
		text, state := firstLineOf(t.Text, 40), t.Why
		if t.Action == bus.ActionReport {
			text = "(progress report)"
		}
		if t.Paused {
			state = "paused " + state
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n", t.ID, t.Every, channelLabel(t.Channel), text,
			routineWhen(t.NextAt, at), routineWhen(t.LastFired, at), t.Fired, t.Missed, strings.TrimSpace(state))
	}
	if n == 0 {
		fmt.Fprintln(out, "No routines. mad routine add \"weekdays 09:00\" CHANNEL TEXT...")
		return nil
	}
	return tw.Flush()
}

func channelLabel(ch string) string {
	if strings.HasPrefix(ch, "dm.") {
		return "DM"
	}
	return "#" + ch
}

func firstLineOf(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

func routineCheckins(out io.Writer) error {
	cs, err := bus.Checkins("")
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		fmt.Fprintln(out, "No check-ins booked.")
		return nil
	}
	st, _ := state.Load()
	at := time.Now()
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAGENT\tAT\tCHANNEL\tNOTE\tSTATE")
	for _, t := range cs {
		who := drive.ShortID(t.By)
		if _, a := st.FindAgent(t.By); a != nil {
			who = drive.Label(a)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, who, routineWhen(t.NextAt, at), channelLabel(t.Channel), firstLineOf(t.Note, 50), t.Why)
	}
	return tw.Flush()
}

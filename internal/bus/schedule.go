package bus

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Limits of the schedules, replaceable in tests.
var (
	// minAhead and maxAhead bound how far ahead "in" may look; a check-in
	// is held to them too.
	minAhead = 5 * time.Minute
	maxAhead = 24 * time.Hour
)

// Schedule is when a timer fires: once, or on a rhythm. ParseSchedule
// reads these forms (no cron):
//
//	in 20m, in 2h               once, that long from now (5m to 24h)
//	at 15:00, at 2026-11-02 09:00
//	                            once, by the wall clock; a time already past
//	                            today is tomorrow's
//	daily 09:00, weekdays 09:00, weekends 10:00
//	mon,wed,fri 09:00, mon 09:00
//	                            each such day at that time
//	every 30m, every 2h 08:00-18:00 [weekdays]
//	                            at that interval, inside a window of the
//	                            day if one is given, on those days
type Schedule struct {
	Once bool
	At   time.Time // once: when

	Days  [7]bool       // recurring: the weekdays, by time.Weekday
	Clock int           // minutes after midnight; -1 for an interval
	Every time.Duration // the interval
	// From and To are the window of an interval, in minutes after
	// midnight; -1 when it has none.
	From, To int
}

// dayNames are the weekdays by the first three letters of their name.
var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// parseDays reads daily, weekdays, weekends or a list such as mon,wed.
func parseDays(s string) ([7]bool, bool) {
	var d [7]bool
	switch s {
	case "daily", "everyday":
		for i := range d {
			d[i] = true
		}
		return d, true
	case "weekdays":
		for i := time.Monday; i <= time.Friday; i++ {
			d[i] = true
		}
		return d, true
	case "weekends":
		d[time.Saturday], d[time.Sunday] = true, true
		return d, true
	}
	for _, name := range strings.Split(s, ",") {
		if len(name) < 3 {
			return d, false
		}
		wd, ok := dayNames[name[:3]]
		if !ok {
			return d, false
		}
		d[wd] = true
	}
	return d, true
}

// parseClock reads 9:05 or 15:00 as minutes after midnight.
func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// ParseSchedule reads s as a schedule, as of time now (whose zone the wall
// clocks are in).
func ParseSchedule(s string, now time.Time) (Schedule, error) {
	f := strings.Fields(strings.ToLower(s))
	bad := func(why string) (Schedule, error) {
		return Schedule{}, fmt.Errorf("schedule %q: %s (in 20m, at 15:00, daily 09:00, weekdays 09:00, mon,wed 09:00, every 30m [08:00-18:00])", strings.TrimSpace(s), why)
	}
	if len(f) < 2 {
		return bad("what time?")
	}
	sc := Schedule{Clock: -1, From: -1, To: -1}
	switch f[0] {
	case "in":
		d, err := time.ParseDuration(f[1])
		if len(f) != 2 || err != nil {
			return bad("in takes a length such as 20m or 2h")
		}
		if d < minAhead || d > maxAhead {
			return bad(fmt.Sprintf("in must be between %s and %s", dur(minAhead), dur(maxAhead)))
		}
		sc.Once, sc.At = true, now.Add(d)
	case "at":
		sc.Once = true
		switch len(f) {
		case 2:
			c, ok := parseClock(f[1])
			if !ok {
				return bad("at takes a time such as 15:00")
			}
			sc.At = time.Date(now.Year(), now.Month(), now.Day(), c/60, c%60, 0, 0, now.Location())
			if !sc.At.After(now) {
				sc.At = sc.At.AddDate(0, 0, 1)
			}
		case 3:
			t, err := time.ParseInLocation("2006-01-02 15:04", f[1]+" "+f[2], now.Location())
			if err != nil {
				return bad("at takes a date and time such as 2026-11-02 09:00")
			}
			if !t.After(now) {
				return bad("that time has passed")
			}
			sc.At = t
		default:
			return bad("at takes a time")
		}
	case "every":
		d, err := time.ParseDuration(f[1])
		if err != nil || d < time.Minute || d%time.Minute != 0 {
			return bad("every takes a length in whole minutes, such as 30m or 2h")
		}
		sc.Every = d
		sc.Days, _ = parseDays("daily")
		for _, x := range f[2:] {
			if a, b, ok := strings.Cut(x, "-"); ok {
				from, ok1 := parseClock(a)
				to, ok2 := parseClock(b)
				if !ok1 || !ok2 || to <= from || sc.From >= 0 {
					return bad("the window is two times such as 08:00-18:00")
				}
				sc.From, sc.To = from, to
			} else if days, ok := parseDays(x); ok {
				sc.Days = days
			} else {
				return bad(fmt.Sprintf("%q is neither a window nor days", x))
			}
		}
	default:
		days, ok := parseDays(f[0])
		if !ok {
			return bad(fmt.Sprintf("%q is not daily, weekdays, weekends, days such as mon,wed, in, at or every", f[0]))
		}
		c, ok2 := parseClock(f[1])
		if !ok2 || len(f) != 2 {
			return bad("give one time such as 09:00")
		}
		sc.Days, sc.Clock = days, c
	}
	return sc, nil
}

// Next is the first time after the given one that the schedule fires, or
// the zero time if it never does (a once that has passed). The wall clock
// is read in the zone of after, so a change of daylight time keeps 09:00
// at 09:00.
func (sc Schedule) Next(after time.Time) time.Time {
	if sc.Once {
		if sc.At.After(after) {
			return sc.At
		}
		return time.Time{}
	}
	at := func(d time.Time, min int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), min/60, min%60, 0, 0, after.Location())
	}
	allDays := true
	for _, on := range sc.Days {
		allDays = allDays && on
	}
	switch {
	case sc.Clock >= 0:
		for i := 0; i <= 8; i++ {
			d := after.AddDate(0, 0, i)
			if t := at(d, sc.Clock); sc.Days[d.Weekday()] && t.After(after) {
				return t
			}
		}
	case sc.From < 0 && allDays:
		return after.Add(sc.Every)
	default:
		from, to := sc.From, sc.To
		if from < 0 {
			from, to = 0, 24*60-1
		}
		step := int(sc.Every / time.Minute)
		for i := 0; i <= 8; i++ {
			d := after.AddDate(0, 0, i)
			if !sc.Days[d.Weekday()] {
				continue
			}
			for m := from; m <= to; m += step {
				if t := at(d, m); t.After(after) {
					return t
				}
			}
		}
	}
	return time.Time{}
}

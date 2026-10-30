package deck

import (
	"fmt"
	"time"

	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/paths"
)

// Config is ~/.config/mad/config.json; every section is optional:
//
//	{"notify": {...}, "diff": {...}, "finish": [...], "quota": false,
//	 "sleep": {"after": "60m"}}
type Config struct {
	Notify notify.Config
	Diff   DiffConfig
	// Finish replaces the built-in finish menu when set.
	Finish []FinishAction
	// Quota is whether mad follows the plans' usage limits (claude through
	// its status line, codex through its rollout) and shows them under the
	// sidebar's header. On unless turned off.
	Quota bool
	// SleepAfter is how long an agent that can resume may sit idle off
	// stage before mad ends its process to free the memory it holds;
	// opening it resumes the session. An hour unless set; 0 never.
	SleepAfter time.Duration
	// Stream is whether what agents write is followed as they write it,
	// for the desktop window (package live): claude's and codex's API
	// calls go through mad stream, pi loads mad's extension. On unless
	// turned off; agents started since follow it.
	Stream bool
	// Bus is the "bus" object: follow-ups and the progress report.
	Bus BusConfig
}

// BusConfig is the "bus" object of config.json:
//
//	{"bus": {"follow_up": "20m", "report": "09:00",
//	         "budget": {"usd": 20, "turns": 150}}}
//
// follow_up is how long work may stand without a word before mad nudges
// the agents (and the board calls it stalled); "0" turns the nudges off.
// report is the local time of day of the daily progress report
// notification; "" turns it off. budget bounds, per local day, what mad
// starts by itself (turns for agents that mention each other, nudges,
// relays; never the person's own work): usd of claude's cost, turns of
// every kind. 0 turns that bound off.
type BusConfig struct {
	FollowUp    time.Duration
	Report      string
	BudgetUSD   float64
	BudgetTurns int
}

// DefaultConfig is what an empty or missing config.json means.
func DefaultConfig() Config {
	return Config{Notify: notify.Default(), Quota: true, SleepAfter: time.Hour, Stream: true, Bus: BusConfig{FollowUp: 20 * time.Minute, Report: "09:00", BudgetUSD: 20, BudgetTurns: 150}}
}

// LoadConfig reads config.json. A malformed file gives DefaultConfig and
// an error naming the line, for the caller to show.
func LoadConfig() (Config, error) {
	var file struct {
		Notify *notify.Config `json:"notify"`
		Diff   *DiffConfig    `json:"diff"`
		Finish []FinishAction `json:"finish"`
		Quota  *bool          `json:"quota"`
		Stream *bool          `json:"stream"`
		Bus    *struct {
			FollowUp *string `json:"follow_up"`
			Report   *string `json:"report"`
			Budget   *struct {
				USD   *float64 `json:"usd"`
				Turns *int     `json:"turns"`
			} `json:"budget"`
		} `json:"bus"`
		Sleep *struct {
			After string `json:"after"`
		} `json:"sleep"`
	}
	c := DefaultConfig()
	if err := paths.ReadJSON(paths.ConfigFile(), &file); err != nil {
		return c, err
	}
	if file.Sleep != nil && file.Sleep.After != "" {
		d, err := time.ParseDuration(file.Sleep.After)
		if err != nil || d < 0 {
			return c, fmt.Errorf("config.json: sleep.after %q is not a duration such as \"60m\" or \"2h\"", file.Sleep.After)
		}
		c.SleepAfter = d
	}
	if b := file.Bus; b != nil {
		if b.FollowUp != nil {
			d, err := time.ParseDuration(*b.FollowUp)
			if err != nil || d < 0 {
				return c, fmt.Errorf("config.json: bus.follow_up %q is not a duration such as \"20m\" (\"0\" turns nudges off)", *b.FollowUp)
			}
			c.Bus.FollowUp = d
		}
		if b.Report != nil {
			if _, err := time.Parse("15:04", *b.Report); err != nil && *b.Report != "" {
				return c, fmt.Errorf("config.json: bus.report %q is not a time of day such as \"09:00\" (\"\" turns it off)", *b.Report)
			}
			c.Bus.Report = *b.Report
		}
		if bg := b.Budget; bg != nil {
			if bg.USD != nil {
				if *bg.USD < 0 {
					return c, fmt.Errorf("config.json: bus.budget.usd %v is negative (0 turns the dollar bound off)", *bg.USD)
				}
				c.Bus.BudgetUSD = *bg.USD
			}
			if bg.Turns != nil {
				if *bg.Turns < 0 {
					return c, fmt.Errorf("config.json: bus.budget.turns %d is negative (0 turns the turn bound off)", *bg.Turns)
				}
				c.Bus.BudgetTurns = *bg.Turns
			}
		}
	}
	c.Notify = notify.FromFile(file.Notify)
	if file.Diff != nil {
		c.Diff = *file.Diff
	}
	c.Finish = file.Finish
	if file.Quota != nil {
		c.Quota = *file.Quota
	}
	if file.Stream != nil {
		c.Stream = *file.Stream
	}
	return c, nil
}

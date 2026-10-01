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
}

// DefaultConfig is what an empty or missing config.json means.
func DefaultConfig() Config {
	return Config{Notify: notify.Default(), Quota: true, SleepAfter: time.Hour}
}

// LoadConfig reads config.json. A malformed file gives DefaultConfig and
// an error naming the line, for the caller to show.
func LoadConfig() (Config, error) {
	var file struct {
		Notify *notify.Config `json:"notify"`
		Diff   *DiffConfig    `json:"diff"`
		Finish []FinishAction `json:"finish"`
		Quota  *bool          `json:"quota"`
		Sleep  *struct {
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
	c.Notify = notify.FromFile(file.Notify)
	if file.Diff != nil {
		c.Diff = *file.Diff
	}
	c.Finish = file.Finish
	if file.Quota != nil {
		c.Quota = *file.Quota
	}
	return c, nil
}

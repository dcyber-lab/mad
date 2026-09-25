package deck

import (
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/paths"
)

// Config is ~/.config/mad/config.json; every section is optional:
//
//	{"notify": {...}, "diff": {...}, "finish": [...], "quota": false}
type Config struct {
	Notify notify.Config
	Diff   DiffConfig
	// Finish replaces the built-in finish menu when set.
	Finish []FinishAction
	// Quota is whether mad follows the plans' usage limits (claude through
	// its status line, codex through its rollout) and shows them under the
	// sidebar's header. On unless turned off.
	Quota bool
}

// DefaultConfig is what an empty or missing config.json means.
func DefaultConfig() Config { return Config{Notify: notify.Default(), Quota: true} }

// LoadConfig reads config.json. A malformed file gives DefaultConfig and
// an error naming the line, for the caller to show.
func LoadConfig() (Config, error) {
	var file struct {
		Notify *notify.Config `json:"notify"`
		Diff   *DiffConfig    `json:"diff"`
		Finish []FinishAction `json:"finish"`
		Quota  *bool          `json:"quota"`
	}
	c := DefaultConfig()
	if err := paths.ReadJSON(paths.ConfigFile(), &file); err != nil {
		return c, err
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

package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/notify"
)

func TestLoadConfigBus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write := func(body string) {
		os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
		os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(body), 0o644)
	}
	if c := DefaultConfig(); c.Bus.FollowUp != 20*time.Minute || c.Bus.Report != "09:00" || !c.Notify.Wants(notify.FollowUp) || !c.Notify.Wants(notify.Report) {
		t.Errorf("defaults: %+v", c)
	}
	write(`{"bus": {"follow_up": "45m", "report": "07:30"}}`)
	if c, err := LoadConfig(); err != nil || c.Bus.FollowUp != 45*time.Minute || c.Bus.Report != "07:30" {
		t.Errorf("set: %+v %v", c, err)
	}
	write(`{"bus": {"follow_up": "0", "report": ""}}`)
	if c, err := LoadConfig(); err != nil || c.Bus.FollowUp != 0 || c.Bus.Report != "" {
		t.Errorf("off: %+v %v", c, err)
	}
	write(`{"bus": {"report": "8am"}}`)
	if c, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "bus.report") || c.Bus.FollowUp != 20*time.Minute {
		t.Errorf("bad report: %+v %v", c, err)
	}
	for _, bad := range []string{`"soon"`, `"-5m"`} {
		write(`{"bus": {"follow_up": ` + bad + `}}`)
		if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "bus.follow_up") {
			t.Errorf("follow_up %s: %v", bad, err)
		}
	}
	write(`{"notify": {"on": ["done"]}}`)
	if c, _ := LoadConfig(); c.Bus.FollowUp != 20*time.Minute {
		t.Errorf("no bus object: %+v", c.Bus)
	}
}

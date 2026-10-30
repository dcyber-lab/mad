package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/notify"
)

func TestLoadConfigBusBudget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write := func(body string) {
		os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
		os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(body), 0o644)
	}
	if c := DefaultConfig(); c.Bus.BudgetUSD != 20 || c.Bus.BudgetTurns != 150 || !c.Notify.Wants(notify.Budget) {
		t.Errorf("defaults: %+v", c.Bus)
	}
	write(`{"bus": {"budget": {"usd": 7.5, "turns": 40}}}`)
	if c, err := LoadConfig(); err != nil || c.Bus.BudgetUSD != 7.5 || c.Bus.BudgetTurns != 40 {
		t.Errorf("set: %+v %v", c.Bus, err)
	}
	write(`{"bus": {"budget": {"usd": 0}}}`) // dollars off, the turn bound stays
	if c, err := LoadConfig(); err != nil || c.Bus.BudgetUSD != 0 || c.Bus.BudgetTurns != 150 {
		t.Errorf("usd 0: %+v %v", c.Bus, err)
	}
	write(`{"bus": {"budget": {"turns": -1}}}`)
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "bus.budget.turns") {
		t.Errorf("negative turns: %v", err)
	}
	write(`{"bus": {"budget": {"usd": -3}}}`)
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "bus.budget.usd") {
		t.Errorf("negative usd: %v", err)
	}
	write(`{"bus": {"follow_up": "30m"}}`)
	if c, _ := LoadConfig(); c.Bus.BudgetUSD != 20 || c.Bus.BudgetTurns != 150 {
		t.Errorf("no budget object: %+v", c.Bus)
	}
}

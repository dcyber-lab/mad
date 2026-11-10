package deck

import (
	"os"
	"path/filepath"
	"testing"
)

// agents_create_without_asking is off unless the config turns it on.
func TestConfigCreateWithoutAsking(t *testing.T) {
	if DefaultConfig().CreateWithoutAsking {
		t.Fatal("on by default")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
	for body, want := range map[string]bool{`{}`: false, `{"agents_create_without_asking": true}`: true, `{"agents_create_without_asking": false}`: false} {
		if err := os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if c, err := LoadConfig(); err != nil || c.CreateWithoutAsking != want {
			t.Errorf("%s: %v %v", body, c.CreateWithoutAsking, err)
		}
	}
}

package host

import (
	"reflect"
	"testing"
)

func TestCodexArgs(t *testing.T) {
	model, config := codexArgs([]string{"--model", "gpt-x", "-c", `mcp_servers.seatalk.url="http://x/mcp"`,
		"--config=a=1", "-cb=2", "-s", "read-only", "-a", "never"})
	if model != "gpt-x" {
		t.Errorf("model = %q", model)
	}
	want := []string{"-c", `mcp_servers.seatalk.url="http://x/mcp"`, "-c", "a=1", "-c", "b=2"}
	if !reflect.DeepEqual(config, want) {
		t.Errorf("config = %q, want %q (the TUI's -s and -a are its own)", config, want)
	}
	if m, c := codexArgs([]string{"-c"}); m != "" || c != nil {
		t.Errorf("a -c with nothing after it: %q %q", m, c)
	}
}

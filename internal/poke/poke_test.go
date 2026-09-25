package poke

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/tmux"
)

// shortState keeps the socket path under the ~104 byte limit that
// t.TempDir() can exceed on macOS.
func shortState(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "mad")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("XDG_STATE_HOME", dir)
	old := tmux.Socket
	tmux.Socket = "t"
	t.Cleanup(func() { tmux.Socket = old })
}

func TestSendAndListen(t *testing.T) {
	shortState(t)
	if err := Send(Poll); err != ErrNoSidebar {
		t.Fatalf("no sidebar: err = %v", err)
	}

	got := make(chan string, 4)
	l, err := Listen(func(cmd string) { got <- cmd })
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{Jump, Poll} {
		if err := Send(cmd); err != nil {
			t.Fatal(err)
		}
		select {
		case c := <-got:
			if c != cmd {
				t.Errorf("got %q, want %q", c, cmd)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s never arrived", cmd)
		}
	}

	if _, err := Listen(func(string) {}); err == nil {
		t.Error("a second sidebar must not steal the socket")
	}
	l.Close()
	if err := Send(Poll); err != ErrNoSidebar {
		t.Errorf("after close: err = %v", err)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	shortState(t)
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), nil, 0o600); err != nil { // left by a crash
		t.Fatal(err)
	}
	l, err := Listen(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := Send(Poll); err != nil {
		t.Error(err)
	}
}

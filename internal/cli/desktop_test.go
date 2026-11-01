package cli

import (
	"bufio"
	"io"
	"testing"
	"time"
)

// With -exit-on-stdin-eof the server ends when its stdin does: a parent that
// holds the write end and dies takes the server with it.
func TestDesktopExitsWhenStdinReachesEOF(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	in, parent := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- desktopCmd([]string{"-no-window", "-addr", "127.0.0.1:0", "-exit-on-stdin-eof"}, IO{In: in, Out: outW, Err: io.Discard})
	}()
	if _, err := bufio.NewReader(outR).ReadString('\n'); err != nil { // it listens: the URL line
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before stdin closed: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	parent.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still serving after stdin reached EOF")
	}
}

// Without the flag, EOF on stdin changes nothing.
func TestDesktopKeepsServingWithoutTheFlag(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	in, parent := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- desktopCmd([]string{"-no-window", "-addr", "127.0.0.1:0"}, IO{In: in, Out: outW, Err: io.Discard})
	}()
	if _, err := bufio.NewReader(outR).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	parent.Close()
	select {
	case <-done:
		t.Fatal("ended on stdin EOF without the flag")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNativeAppPath(t *testing.T) {
	has := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range paths {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		name, goos string
		exists     func(string) bool
		want       string
	}{
		{"installed", "darwin", has("/Applications/madmac.app"), "/Applications/madmac.app"},
		{"in the home's Applications", "darwin", has("/Users/me/Applications/madmac.app"), "/Users/me/Applications/madmac.app"},
		{"/Applications wins", "darwin", has("/Applications/madmac.app", "/Users/me/Applications/madmac.app"), "/Applications/madmac.app"},
		{"not installed", "darwin", has(), ""},
		{"not on macOS", "linux", has("/Applications/madmac.app"), ""},
	} {
		if got := nativeAppPath(c.goos, "/Users/me", c.exists); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

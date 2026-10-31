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

package drive

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"

	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// fakeHost serves agent id's host socket: each connection gets a state of
// status, and each command it sends goes to got and is done.
func fakeHost(t *testing.T, id, status string) <-chan host.Command {
	t.Helper()
	ln, err := net.Listen("unix", paths.HostSocket(id))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan host.Command, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, _ := json.Marshal(host.Line{Type: "state", State: &host.State{Status: status, Requests: []host.Request{}}})
				_, _ = conn.Write(append(line, '\n'))
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					var c host.Command
					if json.Unmarshal(sc.Bytes(), &c) == nil {
						got <- c
						_, _ = conn.Write([]byte(`{"type":"done"}` + "\n"))
					}
				}
			}()
		}
	}()
	return got
}

func TestSendToWindowMode(t *testing.T) {
	dir, kinds := useDeck(t)
	// A socket's path is short: a state directory to match.
	short, err := os.MkdirTemp("/tmp", "md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	t.Setenv("XDG_STATE_HOME", short)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &state.Agent{ID: "a7c1e0f2-0000-4000-8000-000000000001", Kind: "claude", Mode: state.ModeWindow}
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{a}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	got := fakeHost(t, a.ID, host.Idle)
	if err := Send(st, a, "hello there", kinds); err != nil {
		t.Fatal(err)
	}
	if c := <-got; c.Type != "send" || c.Text != "hello there" {
		t.Errorf("host got %+v", c)
	}
}

func TestSendToWindowModeBusy(t *testing.T) {
	dir, kinds := useDeck(t)
	short, err := os.MkdirTemp("/tmp", "md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	t.Setenv("XDG_STATE_HOME", short)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &state.Agent{ID: "a7c1e0f2-0000-4000-8000-000000000002", Kind: "claude", Mode: state.ModeWindow}
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{a}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	fakeHost(t, a.ID, host.Running)
	if err := Send(st, a, "hi", kinds); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
}

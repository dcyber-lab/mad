// Package poke is a small control channel into the running sidebar, over a
// Unix socket in the state directory. Other mad processes use it to have
// the sidebar re-poll right away (after `mad switch` swapped the stage
// behind its back) or jump to the next agent that needs you.
package poke

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

const (
	Poll = "poll" // the stage changed: refresh now
	Jump = "jump" // open the next agent that is waiting or done
)

// Path is the sidebar's socket; one per tmux server, like the sidebar.
func Path() string {
	return filepath.Join(paths.StateDir(), "sidebar-"+tmux.Socket+".sock")
}

// ErrNoSidebar: nothing is listening, i.e. no deck is running.
var ErrNoSidebar = errors.New("the sidebar is not running")

// Send delivers cmd to the sidebar.
func Send(cmd string) error {
	conn, err := net.DialTimeout("unix", Path(), 200*time.Millisecond)
	if err != nil {
		return ErrNoSidebar
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
	_, err = io.WriteString(conn, cmd+"\n")
	return err
}

// Listen serves the socket until closed, calling handle for each command.
// A stale socket left by a crashed sidebar is replaced.
func Listen(handle func(cmd string)) (io.Closer, error) {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if Send(Poll) == nil {
		return nil, errors.New("another sidebar is already listening")
	}
	_ = os.Remove(p)
	l, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return // closed
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					if cmd := strings.TrimSpace(sc.Text()); cmd != "" {
						handle(cmd)
					}
				}
			}()
		}
	}()
	return l, nil
}

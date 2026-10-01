package deck

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// StopCommand is the shell command that stops this deck from outside it.
func StopCommand() string {
	cmd := SelfCommand("kill-server")
	if tmux.Socket != "mad" {
		cmd = "env MAD_SOCKET=" + paths.ShellQuote(tmux.Socket) + " " + cmd
	}
	return cmd
}

// stopGrace is how long the processes left get to end after each signal.
var stopGrace = 2 * time.Second

// Stop ends the deck: its tmux server, and what the agents in it started.
// tmux hangs up each pane's process group, which leaves running whatever
// an agent put in a group of its own (claude runs each command in one) or
// told to ignore the hangup (nohup). Those are hung up, then terminated,
// then killed, stopGrace apart, and Stop names them ("node 4242").
func Stop() ([]string, error) {
	panes, err := tmux.ListPanes()
	if err != nil {
		return nil, tmux.Run("kill-server") // no deck: tmux says so
	}
	var tree []proc
	if out, err := processes(); err == nil {
		t, self := procTable(string(out)), os.Getpid()
		for _, p := range panes {
			// A dead pane's pid may belong to another process by now.
			if p.Dead {
				continue
			}
			if pr, ok := t[p.PID]; ok {
				tree = append(tree, pr)
			}
			for _, pr := range below(t, p.PID) {
				if pr.pid != self {
					tree = append(tree, pr)
				}
			}
		}
	}
	// Run in the deck (an agent's command), mad outlives its pane.
	signal.Ignore(syscall.SIGHUP)
	if err := tmux.Run("kill-server"); err != nil {
		return nil, err
	}
	left := waitGone(tree, stopGrace)
	var names []string
	for _, p := range left {
		names = append(names, fmt.Sprintf("%s %d", p.name(), p.pid))
	}
	own := syscall.Getpgrp()
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGKILL} {
		if len(left) == 0 {
			break
		}
		for _, p := range left {
			// The leader's whole group: what it started since it was
			// listed. Never mad's own group.
			if p.pgid == p.pid && p.pgid != own {
				_ = syscall.Kill(-p.pgid, sig)
			}
			_ = syscall.Kill(p.pid, sig)
		}
		left = waitGone(left, stopGrace)
	}
	return names, nil
}

// waitGone waits up to d for the processes to end, and returns those
// still running: ps lists their pid with the same command.
func waitGone(ps []proc, d time.Duration) []proc {
	deadline := time.Now().Add(d)
	for {
		left := running(ps)
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func running(ps []proc) []proc {
	out, err := processes()
	if err != nil {
		return nil
	}
	t := procTable(string(out))
	var left []proc
	for _, p := range ps {
		if q, ok := t[p.pid]; ok && q.comm == p.comm {
			left = append(left, p)
		}
	}
	return left
}

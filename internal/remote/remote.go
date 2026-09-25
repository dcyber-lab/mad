// Package remote runs bash scripts on a host over the system ssh binary.
//
// Going through ssh(1) instead of a Go SSH library means ~/.ssh/config,
// ProxyJump, agents, certificates and known_hosts all behave exactly as they
// do when the user types `ssh host`. One master connection per host is opened
// up front (so a password or 2FA prompt happens once, on the terminal) and
// every later command is multiplexed over it.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Host is one ssh target, e.g. "web1" or "root@10.0.0.5".
type Host struct {
	Target  string
	extra   []string
	ctlPath string
}

// New returns a Host that will share a control socket under ctlDir.
// extra is passed to every ssh invocation (e.g. "-p", "2222").
func New(target, ctlDir string, extra []string) *Host {
	return &Host{Target: target, extra: extra, ctlPath: ctlDir + "/%C"}
}

func (h *Host) baseArgs() []string {
	args := []string{
		"-o", "ControlPath=" + h.ctlPath,
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=3",
	}
	return append(args, h.extra...)
}

// Open starts the master connection. It is attached to the terminal so the
// user can answer password / host-key / 2FA prompts.
func (h *Host) Open(ctx context.Context) error {
	args := append(h.baseArgs(),
		"-o", "ControlMaster=yes",
		"-o", "ControlPersist=300",
		"-f", "-N", h.Target)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh %s: %w", h.Target, err)
	}
	return nil
}

// Close tears the master connection down.
func (h *Host) Close() {
	args := append(h.baseArgs(), "-O", "exit", h.Target)
	cmd := exec.Command("ssh", args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	_ = cmd.Run()
}

// Resolve asks ssh what HostName a target maps to, so "web1" from
// ~/.ssh/config becomes the address the other server should dial.
func Resolve(target string, extra []string) (string, error) {
	out, err := exec.Command("ssh", append(append([]string{}, extra...), "-G", target)...).Output()
	if err != nil {
		return "", fmt.Errorf("ssh -G %s: %w", target, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok && k == "hostname" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("ssh -G %s: no hostname", target)
}

// safeArg is what we allow on the remote command line. The remote login
// shell (bash, zsh, fish...) parses it, so nothing that needs quoting.
var safeArg = regexp.MustCompile(`^[A-Za-z0-9._:%/-]+$`)

func (h *Host) command(ctx context.Context, script string, args []string) (*exec.Cmd, error) {
	for _, a := range args {
		if !safeArg.MatchString(a) {
			return nil, fmt.Errorf("refusing unsafe remote argument %q", a)
		}
	}
	sshArgs := append(h.baseArgs(), "-o", "BatchMode=yes", "-T", h.Target, "bash", "-s", "--")
	sshArgs = append(sshArgs, args...)
	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = strings.NewReader(script)
	return cmd, nil
}

// Result of a finished remote script.
type Result struct {
	Stdout string
	Stderr string
	Code   int // exit code of the script; 255 usually means ssh itself failed
}

// Run executes script remotely with args and waits for it.
func (h *Host) Run(ctx context.Context, timeout time.Duration, script string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := h.command(ctx, script, args)
	if err != nil {
		return Result{}, err
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return res, fmt.Errorf("%s: timed out after %s", h.Target, timeout)
	case errors.As(err, &ee):
		res.Code = ee.ExitCode()
		if res.Code == 255 {
			return res, fmt.Errorf("%s: ssh failed: %s", h.Target, strings.TrimSpace(res.Stderr))
		}
	default:
		return res, err
	}
	return res, nil
}

// Proc is a remote script still running, read line by line.
type Proc struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	lines  chan string
}

// Start launches script remotely and streams its stdout.
func (h *Host) Start(ctx context.Context, script string, args ...string) (*Proc, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd, err := h.command(ctx, script, args)
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	p := &Proc{cmd: cmd, cancel: cancel, lines: make(chan string, 16)}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
	}()
	return p, nil
}

// Next returns the next stdout line, or ok=false on EOF or timeout.
func (p *Proc) Next(timeout time.Duration) (string, bool) {
	select {
	case l, ok := <-p.lines:
		return l, ok
	case <-time.After(timeout):
		return "", false
	}
}

// Stop kills the local ssh client and reaps it. Remote scripts are always
// wrapped in their own timeout, so nothing is left behind for long.
func (p *Proc) Stop() {
	p.cancel()
	_ = p.cmd.Wait()
}

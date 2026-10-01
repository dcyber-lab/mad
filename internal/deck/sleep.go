package deck

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/dcyber-lab/mad/internal/tmux"
)

// processes lists every process as pid, parent pid, process group and
// command name; replaceable in tests, as is hangUp.
var (
	processes = func() ([]byte, error) { return exec.Command("ps", "-axo", "pid=,ppid=,pgid=,comm=").Output() }
	hangUp    = hangUpGroup
)

// BusyError keeps an agent awake: a command runs under it in a shell, or
// a background task, that ending the agent would cut short.
type BusyError struct{ Command string }

func (e *BusyError) Error() string { return e.Command + " runs under it" }

// SleepAgent ends agent id's process to free the memory it holds, and
// marks its dead pane asleep; opening the agent resumes the session
// (OpenAgent). It refuses with a *BusyError while a shell runs under the
// agent. With away it also leaves an agent on stage alone (it was opened
// since it was picked); otherwise the placeholder takes its place there.
// It reports whether the agent was put to sleep.
func SleepAgent(id string, away bool) (bool, error) {
	panes, err := tmux.ListPanes()
	if err != nil {
		return false, err
	}
	pane, ok := tmux.FindPane(panes, id)
	if !ok || pane.Dead {
		return false, nil
	}
	onStage := tmux.OnStage(pane)
	if onStage && away {
		return false, nil
	}
	out, err := processes()
	if err != nil {
		return false, err
	}
	if c := workUnder(procTable(string(out)), pane.PID); c != "" {
		return false, &BusyError{Command: c}
	}
	// Marked first, so the poll that pane-died sets off finds it asleep.
	if err := tmux.Run("set-option", "-p", "-t", pane.ID, "@mad_asleep", "1"); err != nil {
		return false, err
	}
	if onStage {
		if err := CloseView(id); err != nil {
			return false, err
		}
	}
	return true, hangUp(pane.PID)
}

// hangUpGroup ends pid's process group the way closing its terminal
// would: tmux starts a pane's process as the leader of a group of its
// own, which the agent's helpers (MCP servers) share.
func hangUpGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGHUP); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGHUP)
}

// Busy names what keeps each agent awake, by its pane's pid: see
// workUnder. Agents with nothing under them are left out.
func Busy(pids ...int) (map[int]string, error) {
	out, err := processes()
	if err != nil {
		return nil, err
	}
	t, busy := procTable(string(out)), map[int]string{}
	for _, pid := range pids {
		if c := workUnder(t, pid); c != "" {
			busy[pid] = c
		}
	}
	return busy, nil
}

// shells run the commands agents start (claude's Bash tool, codex's
// commands); an agent's long-lived helpers are started without one.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "tcsh": true, "csh": true}

// proc is a process as ps lists it.
type proc struct {
	pid, ppid, pgid int
	comm            string
}

// name is the command's name: zsh for /bin/zsh, and for -zsh, a login
// shell.
func (p proc) name() string { return strings.TrimPrefix(filepath.Base(p.comm), "-") }

// procTable reads `ps -axo pid=,ppid=,pgid=,comm=`, by pid.
func procTable(ps string) map[int]proc {
	t := map[int]proc{}
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		pgid, err3 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		t[pid] = proc{pid: pid, ppid: ppid, pgid: pgid, comm: strings.Join(f[3:], " ")}
	}
	return t
}

// below lists the processes under pid at any depth, by pid.
func below(t map[int]proc, pid int) []proc {
	var out []proc
	for p, pr := range t {
		if p == pid {
			continue
		}
		// Up the tree to pid; the bound guards against a cycle in a
		// listing taken while pids were reused.
		for q, n := pr.ppid, 0; q > 1 && n < 64; q, n = t[q].ppid, n+1 {
			if q == pid {
				out = append(out, pr)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

// workUnder names what a shell below pid runs (go, node), or the shell
// when it runs nothing else; "" when no shell runs below pid.
func workUnder(t map[int]proc, pid int) string {
	for _, sh := range below(t, pid) {
		if !shells[sh.name()] {
			continue
		}
		for _, p := range below(t, sh.pid) {
			if !shells[p.name()] {
				return p.name()
			}
		}
		return sh.name()
	}
	return ""
}

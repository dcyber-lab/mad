package deck

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dcyber-lab/mad/internal/tmux"
)

// processes lists every process as pid, parent pid and command name;
// replaceable in tests, as is hangUp.
var (
	processes = func() ([]byte, error) { return exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output() }
	hangUp    = hangUpGroup
)

// BusyError keeps an agent awake: a shell runs under it, a command or
// background task that ending the agent would cut short.
type BusyError struct{ Shell string }

func (e *BusyError) Error() string { return e.Shell + " runs under it" }

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
	stage, onStage := tmux.Stage(panes)
	onStage = onStage && stage.ID == pane.ID
	if onStage && away {
		return false, nil
	}
	out, err := processes()
	if err != nil {
		return false, err
	}
	if sh := shellUnder(string(out), pane.PID); sh != "" {
		return false, &BusyError{Shell: sh}
	}
	// Marked first, so the poll that pane-died sets off finds it asleep.
	if err := tmux.Run("set-option", "-p", "-t", pane.ID, "@mad_asleep", "1"); err != nil {
		return false, err
	}
	if onStage {
		if err := ShowPane(tmux.IDPlaceholder, false); err != nil {
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

// shells run the commands agents start (claude's Bash tool, codex's
// commands); an agent's long-lived helpers are started without one.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "tcsh": true, "csh": true}

// shellUnder names a shell running anywhere below pid in ps output, or
// returns "" when there is none.
func shellUnder(ps string, pid int) string {
	parent, name := map[int]int{}, map[int]string{}
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		p, err1 := strconv.Atoi(f[0])
		pp, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		parent[p], name[p] = pp, strings.Join(f[2:], " ")
	}
	for p, comm := range name {
		sh := strings.TrimPrefix(filepath.Base(comm), "-") // -zsh: a login shell
		if p == pid || !shells[sh] {
			continue
		}
		// Up the tree to pid; the bound guards against a cycle in a
		// listing taken while pids were reused.
		for q, n := parent[p], 0; q > 1 && n < 64; q, n = parent[q], n+1 {
			if q == pid {
				return sh
			}
		}
	}
	return ""
}

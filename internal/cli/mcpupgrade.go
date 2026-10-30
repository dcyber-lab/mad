package cli

import (
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/mcp"
	"github.com/dcyber-lab/mad/internal/paths"
)

// mcpUpgrade lets srv go on as the mad that is installed now: when the
// binary it started from is replaced (a rebuild), it execs the new one in
// place, on the same stdio, so the agent gets new tools without a restart.
// args are those of this run, to start the new one with. It returns the
// reader to serve from: stdin, made so an idle server can look up from it.
func mcpUpgrade(srv *mcp.Server, args []string, in io.Reader) io.Reader {
	self := paths.Self() // now: once replaced, Linux says "(deleted)"
	first, err := os.Stat(self)
	if err != nil {
		return in
	}
	srv.Stale = func() bool {
		cur, err := os.Stat(self)
		if err != nil || sameBinary(first, cur) {
			return false
		}
		// Replaced. Wait out a copy still being written.
		time.Sleep(time.Second)
		again, err := os.Stat(self)
		return err == nil && sameBinary(cur, again) && again.Mode().IsRegular() &&
			again.Mode().Perm()&0o111 != 0 && again.Size() > 0
	}

	// A non-blocking pipe lets os.File read deadlines work. The flag is on
	// the pipe itself, so it is put back before the new mad takes it over.
	nonblock := false
	if f, ok := in.(*os.File); ok && f == os.Stdin {
		if syscall.SetNonblock(0, true) == nil {
			nonblock = true
			in = os.NewFile(0, "stdin")
		}
	}
	srv.Reexec = func(protocol string) error {
		argv := []string{self, "mcp"}
		for i := 0; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--resumed" || a == "-resumed":
				i++ // with its value: this run's, not the next's
			case strings.HasPrefix(a, "--resumed=") || strings.HasPrefix(a, "-resumed="):
			default:
				argv = append(argv, a)
			}
		}
		if protocol != "" {
			argv = append(argv, "--resumed", protocol)
		}
		if nonblock {
			_ = syscall.SetNonblock(0, false)
		}
		err := syscall.Exec(self, argv, os.Environ())
		if nonblock {
			_ = syscall.SetNonblock(0, true)
		}
		return err
	}
	return in
}

func sameBinary(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

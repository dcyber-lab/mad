package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
)

// Outside says why a tool call of a run's role reaches out of dir, the
// run's worktree; "" when it stays in. root is the project's main
// checkout. A file may be written in dir, or in a temporary directory out
// of the project. A shell command may not name the main checkout or
// anything in it but dir: where it writes can't be told before it runs,
// but naming the checkout is how an agent strays into it.
func Outside(dir, root string, t *discover.Tool) string {
	if t.Path != "" && !within(t.Path, dir) && (!temporary(t.Path) || within(t.Path, root)) {
		return fmt.Sprintf("mad: this run works in its worktree %s alone, and %s is outside it. Make the change in the worktree.", dir, t.Path)
	}
	if t.Command != "" && dir != root {
		if named := namesOutside(t.Command, dir, root); named != "" {
			return fmt.Sprintf("mad: this run works in its worktree %s alone, and the command names %s, the project's main checkout. The worktree holds the same files: use them.", dir, named)
		}
	}
	return ""
}

// within reports whether path is dir or in it, symbolic links resolved.
func within(path, dir string) bool {
	rel, err := filepath.Rel(resolve(dir), resolve(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func temporary(path string) bool {
	for _, d := range []string{os.TempDir(), "/tmp"} {
		if within(path, d) {
			return true
		}
	}
	return false
}

// resolve resolves the symbolic links of path's longest part that exists
// (on macOS /tmp is /private/tmp), and keeps the rest as it is.
func resolve(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for p := path; ; p = filepath.Dir(p) {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		if filepath.Dir(p) == p {
			return path
		}
		rest = append([]string{filepath.Base(p)}, rest...)
	}
}

// namesOutside returns how command names root other than as dir or a
// path in dir, as written or with ~ for the home directory; "" when it
// doesn't.
func namesOutside(command, dir, root string) string {
	inner, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(inner, "..") {
		return ""
	}
	inner = "/" + filepath.ToSlash(inner)
	for _, r := range []string{root, resolve(root), paths.Short(root)} {
		for i := 0; ; {
			j := strings.Index(command[i:], r)
			if j < 0 {
				break
			}
			i += j + len(r)
			rest := command[i:]
			switch {
			case rest != "" && nameByte(rest[0]):
				continue // another name that starts the same: /src/mad-pre
			case strings.HasPrefix(rest, inner) && (len(rest) == len(inner) || !nameByte(rest[len(inner)])):
				continue // dir, or a path in it
			}
			return r
		}
	}
	return ""
}

// nameByte reports whether b continues a file name.
func nameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("-_.@+~", b) >= 0
}

// checkout is the state of the project's main checkout, for telling
// whether a step changed it: what git status says, its commit included.
// "" for a run without a worktree, or a checkout git can't read.
func (x *runner) checkout() string {
	if x.run.Dir == x.proj {
		return ""
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", x.proj, "status", "--porcelain=v2", "--branch").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// changes lists what differs between two states of a checkout, a few
// paths at most: those whose status changed, and "HEAD" when it moved.
func changes(before, after string) []string {
	if before == "" || after == "" || before == after {
		return nil
	}
	was := map[string]bool{}
	for _, l := range strings.Split(before, "\n") {
		was[l] = true
	}
	is := map[string]bool{}
	for _, l := range strings.Split(after, "\n") {
		is[l] = true
	}
	var out []string
	seen := map[string]bool{}
	add := func(lines string, other map[string]bool) {
		for _, l := range strings.Split(lines, "\n") {
			if l == "" || other[l] {
				continue
			}
			what := statusPath(l)
			if what != "" && !seen[what] {
				seen[what] = true
				out = append(out, what)
			}
		}
	}
	add(after, was)
	add(before, is)
	if len(out) > 3 {
		out = append(out[:3], fmt.Sprintf("%d more", len(out)-3))
	}
	return out
}

// statusPath is what a line of git status --porcelain=v2 is about: its
// path, or HEAD for the commit; "" for a line about nothing that changes.
func statusPath(line string) string {
	f := strings.Fields(line)
	switch {
	case len(f) == 0:
		return ""
	case f[0] == "#":
		if len(f) > 1 && f[1] == "branch.oid" {
			return "HEAD"
		}
		if len(f) > 1 && f[1] == "branch.head" {
			return "the branch"
		}
		return ""
	case f[0] == "?" || f[0] == "!":
		return strings.TrimPrefix(line, f[0]+" ")
	case f[0] == "1" && len(f) >= 9:
		return strings.Join(f[8:], " ")
	case f[0] == "2" && len(f) >= 10:
		path, _, _ := strings.Cut(strings.Join(f[9:], " "), "\t")
		return path
	case f[0] == "u" && len(f) >= 11:
		return strings.Join(f[10:], " ")
	}
	return ""
}

// Refused says why tool call t of role, in the run whose worktree is dir
// and whose own directory is runDir, is refused, or "": it reaches out of
// the worktree (root is the project's main checkout); it pushes or opens
// a pull request, which a run leaves to the person; or the role edits
// nothing, and t writes a file in the worktree. A file in a checkout
// nested in the worktree (a subagent's worktree) is that checkout's.
func Refused(dir, root, runDir string, role Role, t *discover.Tool) string {
	if why := Outside(dir, root, t); why != "" {
		return why
	}
	if t.Command != "" {
		if what := publishes(t.Command); what != "" {
			return fmt.Sprintf("mad: a run's roles do not %s: the person does that from the sidebar once the run is done. Commit, and say what is ready.", what)
		}
	}
	if role.NoEdits {
		return NoEdits(dir, runDir, role.Label, "the run's worktree", t)
	}
	return ""
}

// NoEdits says why tool call t of a role that edits nothing in dir is
// refused, or "": it writes a file there. label names the role and where
// the place, for the agent to read. A file in own (a run's directory;
// "" for none) or in a checkout nested in dir is not dir's.
func NoEdits(dir, own, label, where string, t *discover.Tool) string {
	if t.Path == "" || !within(t.Path, dir) || own != "" && within(t.Path, own) || nested(t.Path, dir) {
		return ""
	}
	return fmt.Sprintf("mad: the %s edits no file in %s. Put scratch files in %s, and say in your reply what should change.", label, where, os.TempDir())
}

// nested reports whether path is in a git checkout of its own below dir.
func nested(path, dir string) bool {
	top := resolve(dir)
	for d := filepath.Dir(resolve(path)); d != top && within(d, top); d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return true
		}
	}
	return false
}

// publishes says what command line does that a run leaves to the person
// (pushing, a pull request), or "". Only what runs counts: a git push
// quoted to grep or echo is no push.
func publishes(line string) string {
	for _, words := range simpleCommands(line) {
		words = unwrap(words)
		if len(words) == 0 {
			continue
		}
		switch filepath.Base(words[0]) {
		case "git":
			if gitSubcommand(words[1:]) == "push" {
				return "push"
			}
		case "gh":
			if len(words) > 2 && words[1] == "pr" && (words[2] == "create" || words[2] == "merge") {
				return "open or merge pull requests"
			}
		case "bash", "sh", "zsh":
			for i := 1; i+1 < len(words); i++ {
				if strings.HasPrefix(words[i], "-") && !strings.HasPrefix(words[i], "--") && strings.Contains(words[i], "c") {
					if what := publishes(words[i+1]); what != "" {
						return what
					}
					break
				}
			}
		}
	}
	return ""
}

// unwrap drops what runs a command rather than being it: variable
// assignments, and env, sudo, xargs and their like with their options.
func unwrap(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case strings.Contains(w, "=") && !strings.HasPrefix(w, "-") && !strings.ContainsAny(w[:strings.Index(w, "=")], "/."):
			words = words[1:]
		case w == "env" || w == "sudo" || w == "command" || w == "exec" || w == "nohup" || w == "time" || w == "nice" || w == "xargs":
			words = words[1:]
			for len(words) > 0 && strings.HasPrefix(words[0], "-") {
				words = words[1:]
			}
		default:
			return words
		}
	}
	return words
}

// gitSubcommand is the subcommand in git's arguments, past its options.
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" || a == "--config-env":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return ""
}

// simpleCommands splits a shell command line into the words of its simple
// commands, quotes taken off: enough to tell what a line runs, not to run
// it. A quoted string stays one word.
func simpleCommands(line string) [][]string {
	var out [][]string
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			out = append(out, words)
			words = nil
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'':
			inWord = true
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				j = len(line) - i - 1
			}
			word.WriteString(line[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			for i++; i < len(line) && line[i] != '"'; i++ {
				if line[i] == '\\' && i+1 < len(line) {
					i++
				}
				word.WriteByte(line[i])
			}
		case c == '\\' && i+1 < len(line):
			inWord = true
			i++
			word.WriteByte(line[i])
		case c == ' ' || c == '\t':
			endWord()
		case strings.IndexByte(";&|\n()`", c) >= 0:
			endCommand()
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			endCommand()
		default:
			inWord = true
			word.WriteByte(c)
		}
	}
	endCommand()
	return out
}

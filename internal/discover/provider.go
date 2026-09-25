package discover

import (
	"io"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// Provider is what mad knows about one kind of agent beyond its launch
// commands (see agent.Kind): how mad wires itself into the agent, where
// the agent keeps its sessions, what its transcripts say and how to tell
// it running outside the deck. Kinds without a provider (pi, shell, the
// ones added in agents.json) still run and show waiting from their
// screen, but have no history, session picker, transcript or sync.
// Adding one is a file here and a line in providers.
type Provider interface {
	// Kind is the agent kind (agent.Kind.Name), which is also the name of
	// its executable.
	Kind() string

	// Setup writes whatever the agent reads to report to mad, on every
	// deck start; quota is whether to follow the account's usage limits.
	Setup(quota bool) error
	// Placeholders are the command placeholders the agent's wiring fills,
	// "{name}" → shell text. Any kind's commands may use them.
	Placeholders() map[string]string
	// Hook takes what the agent passes to `mad hook <kind> args...`, the
	// payload on stdin or in args; what it prints goes to stdout.
	Hook(args []string, stdin io.Reader, stdout io.Writer, now time.Time) Report

	// History calls add for every session used since cutoff: where it
	// ran, when it was last written and whether a desktop app ran it.
	History(cutoff time.Time, add func(cwd string, t time.Time, desktop bool))
	// Sessions lists the sessions a person had under project root, its
	// agent worktrees included, in any order. Updated is filled in.
	Sessions(root string) []Session

	// Transcripts returns the files of session sid run in dir: the
	// session's own first, then any whose tokens count towards it
	// (subagents). None until the session was written.
	Transcripts(dir, sid string) []string
	// Parse reads one transcript line; ok is false for lines that say
	// nothing mad shows.
	Parse(line []byte) (e Event, ok bool)
	// Titles are session names kept outside the transcripts, by session
	// id; nil when the agent keeps none. Read once per poll.
	Titles() map[string]string

	// Launched reports whether a command line carries mad's wiring: the
	// process was started by a deck, maybe one on another socket.
	Launched(cmdline string) bool
	// Desktop recognizes, by its command line, a process a desktop app
	// runs for one conversation (it has no terminal) and returns its
	// arguments.
	Desktop(cmdline string) (args []string, ok bool)
	// ProcessSession is the session a process outside the deck has open,
	// from its arguments or the files it holds; "" when it can't tell.
	ProcessSession(pid int, args []string) string
	// RecentSessions lists the sessions run in cwd, newest first. They are
	// handed out as guesses to processes whose session is unknown.
	RecentSessions(cwd string) []string
	// HumanSession reports whether session id is a conversation with a
	// person, not one a program drove. Desktop apps run both.
	HumanSession(id string) bool
}

// Report is what an agent told `mad hook`.
type Report struct {
	Hook  *status.Hook  // a change of status; nil when none
	Quota *status.Quota // the account's usage limits; nil when not given
}

// providers are the kinds mad follows, in the order they are scanned.
var providers = []Provider{claude{}, codex{}}

// Providers returns every kind mad follows.
func Providers() []Provider { return providers }

// Setup runs every provider's Setup.
func Setup(quota bool) error {
	for _, p := range providers {
		if err := p.Setup(quota); err != nil {
			return err
		}
	}
	return nil
}

// Placeholders merges every provider's placeholders.
func Placeholders() map[string]string {
	vars := map[string]string{}
	for _, p := range providers {
		for k, v := range p.Placeholders() {
			vars[k] = v
		}
	}
	return vars
}

// Lookup returns the provider of kind, or nil.
func Lookup(kind string) Provider {
	for _, p := range providers {
		if p.Kind() == kind {
			return p
		}
	}
	return nil
}

// Tokens are counted as the model reports them.
type Tokens struct {
	Input      int64 // uncached prompt tokens
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

// Title sources, in the order a title from one beats the next.
const (
	TitleCustom = "custom" // the user named the session
	TitleAI     = "ai"     // the agent named it
)

// Event is what one transcript line says about its session. Fields left
// zero say nothing.
type Event struct {
	// Usage is what one model response consumed. Lines with the same
	// Message report the same response again, and the last one counts.
	Usage   *Tokens
	Message string
	// Total replaces everything counted so far, for agents that log a
	// running total.
	Total *Tokens

	// Title names the session; TitleSource is TitleCustom or TitleAI and
	// must be set for Title to count (an empty Title then clears it).
	Title       string
	TitleSource string
	// Prompt is something the user asked, first line only.
	Prompt string
	// Tool labels a tool call ("Bash · go test ./...").
	Tool string

	// Quota is the account's usage limits as of this line.
	Quota *status.Quota
}

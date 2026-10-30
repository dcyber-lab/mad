package status

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Report is everything agent id reported, as of one read. Each part is a
// file with one writer, merged here where it is read:
//
//	<id>.json       the hook (the agent's hooks; in window mode its host too)
//	<id>.turn.json  how the last turn ended (the Stop hook)
//	<id>.asks.json  what its host waits on the person for (the host only)
//
// The asks are not a field of the hook: in window mode claude runs with
// mad's hooks, and its next tool call would write over them.
type Report struct {
	Hook *Hook // nil before its first report
	Turn *Turn // nil before its first turn ended
	// Asks are its host's open requests, oldest first; nil for an agent
	// not in window mode, or with nothing open.
	Asks []Ask
}

// ReadReport reads agent id's hook, turn and asks. A file that is missing
// or half-written reads as nothing: the next poll gets it.
func ReadReport(id string) Report {
	return Report{Hook: ReadHook(id), Turn: ReadTurn(id), Asks: ReadAsks(id)}
}

// Kinds of Ask.
const (
	AskPermission = "permission" // may it use a tool
	AskQuestion   = "question"   // a question, maybe with options
	AskPlan       = "plan"       // a plan to approve
)

// Ask is one request an agent's host has open, in the deck's words: what
// the inbox shows and what an answer names. The host turns its own
// Request into this; nothing outside package host sees a Request.
type Ask struct {
	ID string `json:"id"` // the host's request id; an answer names it
	// Host is the instance of the host that wrote it (NewHostInstance).
	// Request ids are only unique within one host (codex's restart at 0
	// in each app-server), so an answer names both.
	Host string `json:"host"`
	Kind string `json:"kind"`
	// Tool and Target say what a permission is for: "Bash", "run the
	// tests"; Command is the shell command it would run, if it is one.
	Tool    string `json:"tool,omitempty"`
	Target  string `json:"target,omitempty"`
	Command string `json:"command,omitempty"`
	// Title is the agent's own line about it, when it gave one.
	Title string `json:"title,omitempty"`
	// Always: it can be allowed from now on, not just this once.
	Always bool `json:"always,omitempty"`
	// Question is a question's first (often only) question, Header its
	// short name and Options its option labels. OneTap: it is the only
	// question and takes one answer, so an option, or a line of one's
	// own, answers it whole.
	Question string   `json:"question,omitempty"`
	Header   string   `json:"header,omitempty"`
	Options  []string `json:"options,omitempty"`
	OneTap   bool     `json:"one_tap,omitempty"`
	// Plan is a plan's first line.
	Plan string    `json:"plan,omitempty"`
	At   time.Time `json:"at"` // when it was asked
}

func asksPath(id string) string { return filepath.Join(paths.StatusDir(), id+".asks.json") }

// WriteAsks stores what agent id's host has open; none removes the file.
// Only package host writes it.
func WriteAsks(id string, asks []Ask) error {
	if len(asks) == 0 {
		if err := os.Remove(asksPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(asks)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(asksPath(id), data)
}

// NewHostInstance names this process as a host: its pid, to tell it
// lives, and its start time, to tell it from the next with the same pid.
func NewHostInstance() string {
	return strconv.Itoa(os.Getpid()) + "." + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// hostAlive tells whether the host instance's process still runs: a host
// killed hard leaves its file behind. Not knowing is not alive.
func hostAlive(instance string) bool {
	pid, _, _ := strings.Cut(instance, ".")
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 0 {
		return false
	}
	err = syscall.Kill(n, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ReadAsks is what agent id's host has open, or nil. A file whose host is
// gone (or that names none) is no one's request: nothing waits on it.
func ReadAsks(id string) []Ask {
	data, err := os.ReadFile(asksPath(id))
	if err != nil {
		return nil
	}
	var asks []Ask
	if json.Unmarshal(data, &asks) != nil || len(asks) == 0 || !hostAlive(asks[0].Host) {
		return nil
	}
	return asks
}

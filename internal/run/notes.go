package run

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
)

// Note is something you said in a run: typed into one of its agents, or
// left for all of them (m in the sidebar, mad run note). Every role hears
// of it from its next step on, but the one it was typed into.
type Note struct {
	At   time.Time `json:"at"`
	Role string    `json:"role,omitempty"` // the role it was typed into; "" left for every role
	Step string    `json:"step,omitempty"` // the step under way then
	Text string    `json:"text"`
}

func inboxPath(dir string) string { return filepath.Join(dir, "inbox.jsonl") }

// AddNote leaves a note for run r's runner: told to role (typed into its
// agent), or to every role when role is "".
func AddNote(r *state.Run, role, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	data, err := json.Marshal(Note{At: time.Now(), Role: role, Text: text})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(inboxPath(Dir(r)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// FromRunner reports whether a message to an agent is one a runner sent:
// they all start with a [mad run …] header, and commands are no message.
func FromRunner(msg string) bool {
	msg = strings.TrimSpace(msg)
	return strings.HasPrefix(msg, "[mad run ") || strings.HasPrefix(msg, "/")
}

// takeNotes moves the notes left since the last call from the inbox into
// the run's progress, marked with the step last under way, and reports
// whether there were any.
func (x *runner) takeNotes() bool {
	f, err := os.Open(inboxPath(x.dir))
	if err != nil {
		return false
	}
	defer f.Close()
	x.mu.Lock()
	pr := &x.f.Progress
	if _, err := f.Seek(pr.NotesRead, io.SeekStart); err != nil {
		x.mu.Unlock()
		return false
	}
	var added []Note
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			break // a partial last line waits for its newline
		}
		pr.NotesRead += int64(len(line))
		var n Note
		if json.Unmarshal(bytes.TrimSpace(line), &n) != nil || n.Text == "" {
			continue
		}
		// The step last under way: the one a gate holds after, too.
		if k := len(pr.Entries); k > 0 {
			n.Step = pr.Entries[k-1].Step
		}
		pr.Notes = append(pr.Notes, n)
		added = append(added, n)
	}
	notes := append([]Note(nil), pr.Notes...)
	x.mu.Unlock()
	if len(added) == 0 {
		return false
	}
	for _, n := range added {
		if n.Role != "" {
			x.logf("you told the %s: %s", x.flow.RoleLabel(n.Role), firstLine(n.Text))
		} else {
			x.logf("your note: %s", firstLine(n.Text))
		}
	}
	_ = os.WriteFile(filepath.Join(x.dir, "notes.md"), []byte(x.notesText(notes, "")), 0o644)
	x.save()
	return true
}

// notesText lists notes, those typed into role left out.
func (x *runner) notesText(notes []Note, role string) string {
	var b strings.Builder
	for _, n := range notes {
		if role != "" && n.Role == role {
			continue
		}
		where := ""
		if n.Step != "" {
			where = ", at the " + n.Step
		}
		switch {
		case n.Role != "":
			fmt.Fprintf(&b, "- (told the %s%s) %s\n", x.flow.RoleLabel(n.Role), where, n.Text)
		default:
			fmt.Fprintf(&b, "- %s\n", n.Text)
		}
	}
	return b.String()
}

// newNotes is what role hasn't heard of yet from the user, as a block to
// put before its next message; it counts as heard.
func (x *runner) newNotes(role string) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	pr := &x.f.Progress
	if pr.Heard == nil {
		pr.Heard = map[string]int{}
	}
	from := pr.Heard[role]
	pr.Heard[role] = len(pr.Notes)
	if from >= len(pr.Notes) {
		return ""
	}
	text := x.notesText(pr.Notes[from:], role)
	if text == "" {
		return ""
	}
	return "From the user, for this run (it holds for every role, and over the task where they differ):\n" + text + "\n"
}

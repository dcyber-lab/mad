package bus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
)

// Stored statuses of a task.
const (
	TaskTodo    = "todo"
	TaskDoing   = "doing"
	TaskReview  = "review"
	TaskDone    = "done"
	TaskDropped = "dropped"
)

// taskKeep is how long a finished task stays in tasks.jsonl before it moves
// to the archive.
const taskKeep = 7 * 24 * time.Hour

// titleRunes is the longest title a handoff gives its task.
const titleRunes = 80

// Link points from a task to a pull request, a file or a URL.
type Link struct {
	Kind string `json:"kind"` // pr, file or url
	Ref  string `json:"ref"`
}

// Nudge is a reminder mad sent about a task.
type Nudge struct {
	At  time.Time `json:"at"`
	To  string    `json:"to"`
	Why string    `json:"why,omitempty"`
}

// Task is a piece of work on the deck's board: who owns it and how it
// stands. A handoff makes one by itself; agents and the person keep the
// rest true with SetTask.
type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Owner is an agent id, "you" or "".
	Owner string `json:"owner"`
	// Status is todo, doing, review, done or dropped.
	Status  string   `json:"status"`
	Channel string   `json:"channel,omitempty"`
	Handoff string   `json:"handoff,omitempty"`
	Parent  string   `json:"parent,omitempty"`
	WaitsOn []string `json:"waits_on,omitempty"`
	Links   []Link   `json:"links,omitempty"`
	Note    string   `json:"note,omitempty"`
	// By is who made it; Project the directory it belongs to.
	By        string    `json:"by,omitempty"`
	Project   string    `json:"project,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Nudges    []Nudge   `json:"nudges,omitempty"`
}

// open tells whether the task is not finished.
func (t Task) open() bool { return t.Status != TaskDone && t.Status != TaskDropped }

func taskFile() string { return filepath.Join(dir(), "tasks.jsonl") }

func taskArchive() string { return filepath.Join(dir(), "archive", "tasks.jsonl") }

func newTaskID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "t-" + hex.EncodeToString(b)
}

// loadTasks reads the tasks, oldest first; a line that does not parse is
// skipped.
func loadTasks() ([]Task, error) {
	data, err := os.ReadFile(taskFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, line := range strings.Split(string(data), "\n") {
		var t Task
		if line != "" && json.Unmarshal([]byte(line), &t) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func taskLines(ts []Task) ([]byte, error) {
	var b strings.Builder
	for _, t := range ts {
		line, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// saveTasks rewrites the file; tasks done or dropped more than a week ago
// move to the archive. The bus lock is held by the caller.
func saveTasks(ts []Task) error {
	var kept, old []Task
	for _, t := range ts {
		if !t.open() && now().Sub(t.UpdatedAt) > taskKeep {
			old = append(old, t)
		} else {
			kept = append(kept, t)
		}
	}
	if len(old) > 0 {
		data, err := taskLines(old)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(taskArchive()), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(taskArchive(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	data, err := taskLines(kept)
	if err != nil {
		return err
	}
	return writeAtomic(taskFile(), data)
}

// Tasks lists the tasks on the board, oldest first.
func Tasks() ([]Task, error) { return loadTasks() }

// FindTask returns task id.
func FindTask(id string) (Task, bool, error) {
	ts, err := loadTasks()
	if err != nil {
		return Task{}, false, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, true, nil
		}
	}
	return Task{}, false, nil
}

func findTask(ts []Task, id string) *Task {
	for i := range ts {
		if ts[i].ID == id {
			return &ts[i]
		}
	}
	return nil
}

// taskOf is the task handoff id belongs to, if any.
func taskOf(ts []Task, handoff string) *Task {
	if handoff == "" {
		return nil
	}
	for i := range ts {
		if ts[i].Handoff == handoff {
			return &ts[i]
		}
	}
	return nil
}

// changeTasks runs fn on the tasks under the lock and saves them.
func changeTasks(fn func(*[]Task) error) error {
	return locked(func() error {
		ts, err := loadTasks()
		if err != nil {
			return err
		}
		if err := fn(&ts); err != nil {
			return err
		}
		return saveTasks(ts)
	})
}

// firstLine is the first line of s, cut to n runes.
func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

// projectOf is the directory agent id runs in, or "".
func projectOf(st *state.State, id string) string {
	if p, a := st.FindAgent(id); a != nil {
		return p.Dir(a)
	}
	return ""
}

// TaskChange is what SetTask changes: fields left nil stay as they are. With
// no ID it makes a task, and Title is required.
type TaskChange struct {
	ID      string
	Title   *string
	Owner   *string // an agent id, "you" or ""
	Status  *string // todo, doing, review, done or dropped
	Channel *string
	Note    *string
	Links   *[]Link
	WaitsOn *[]string
	Parent  *string
}

func validTaskStatus(s string) bool {
	switch s {
	case TaskTodo, TaskDoing, TaskReview, TaskDone, TaskDropped:
		return true
	}
	return false
}

// SetTask makes a task (no ID in c) or changes one, as by. A task with an
// open handoff cannot be closed this way: its receiver finishes it with
// bus_done (the person may). A task made by an agent belongs to the
// directory the agent runs in.
func SetTask(by From, c TaskChange) (Task, error) {
	st, err := state.Load()
	if err != nil {
		return Task{}, err
	}
	if c.Status != nil && !validTaskStatus(*c.Status) {
		return Task{}, fmt.Errorf("status is todo, doing, review, done or dropped, not %q", *c.Status)
	}
	if c.Owner != nil && *c.Owner != "" && *c.Owner != You && findIn(st, *c.Owner) == nil {
		return Task{}, fmt.Errorf("no agent %q", *c.Owner)
	}
	if c.Channel != nil && *c.Channel != "" && !validChannel(*c.Channel) {
		return Task{}, fmt.Errorf("%q is no channel", *c.Channel)
	}
	if c.Links != nil {
		for _, l := range *c.Links {
			if (l.Kind != "pr" && l.Kind != "file" && l.Kind != "url") || strings.TrimSpace(l.Ref) == "" {
				return Task{}, fmt.Errorf("a link is pr, file or url with a ref, not %+v", l)
			}
		}
	}
	var out Task
	err = changeTasks(func(tsp *[]Task) error {
		ts := *tsp
		if c.Parent != nil && *c.Parent != "" && findTask(ts, *c.Parent) == nil {
			return fmt.Errorf("no task %s", *c.Parent)
		}
		if c.WaitsOn != nil {
			for _, w := range *c.WaitsOn {
				if findTask(ts, w) == nil {
					return fmt.Errorf("no task %s", w)
				}
				if w == c.ID {
					return errors.New("a task cannot wait on itself")
				}
			}
		}
		at := now()
		var t *Task
		if c.ID == "" {
			if c.Title == nil || strings.TrimSpace(*c.Title) == "" {
				return errors.New("a new task needs a title")
			}
			ts = append(ts, Task{ID: newTaskID(), Status: TaskTodo, By: by.ID, Project: projectOf(st, by.ID), CreatedAt: at})
			t = &ts[len(ts)-1]
			// An agent's own task is its own, unless it names another owner.
			if by.ID != You {
				t.Owner = by.ID
			}
		} else if t = findTask(ts, c.ID); t == nil {
			return fmt.Errorf("no task %s", c.ID)
		}
		if c.Status != nil && (*c.Status == TaskDone || *c.Status == TaskDropped) && by.ID != You && t.Handoff != "" {
			hs, err := loadHandoffs()
			if err != nil {
				return err
			}
			if h, ok := findHandoff(hs, t.Handoff); ok && h.Status == "open" {
				return fmt.Errorf("%s is relayed work in handoff %s: finish it with bus_done", t.ID, h.ID)
			}
		}
		if c.Title != nil {
			t.Title = strings.TrimSpace(*c.Title)
		}
		if c.Owner != nil {
			t.Owner = *c.Owner
		}
		if c.Status != nil {
			t.Status = *c.Status
		}
		if c.Channel != nil {
			t.Channel = *c.Channel
		}
		if c.Note != nil {
			t.Note = strings.TrimSpace(*c.Note)
		}
		if c.Links != nil {
			t.Links = *c.Links
		}
		if c.WaitsOn != nil {
			t.WaitsOn = *c.WaitsOn
		}
		if c.Parent != nil {
			t.Parent = *c.Parent
		}
		t.UpdatedAt = at
		out = *t
		*tsp = ts
		return nil
	})
	if err == nil {
		poked()
	}
	return out, err
}

// attachTask gives handoff h its task, in ts: the task taskID, moved to the
// receiver, or a new one. The bus lock is held by the caller. prev is the
// task as it was, nil when new.
func attachTask(ts []Task, h Handoff, taskID, project string) (out []Task, prev *Task, err error) {
	at := now()
	if taskID == "" {
		parent := ""
		if p := taskOf(ts, h.Parent); p != nil {
			parent = p.ID
		}
		t := Task{ID: newTaskID(), Title: firstLine(h.Task, titleRunes), Owner: h.To, Status: TaskTodo,
			Channel: h.Channel, Handoff: h.ID, Parent: parent, By: h.From, Project: project, CreatedAt: at, UpdatedAt: at}
		return append(ts, t), nil, nil
	}
	t := findTask(ts, taskID)
	if t == nil {
		return ts, nil, fmt.Errorf("no task %s", taskID)
	}
	if !t.open() {
		return ts, nil, fmt.Errorf("task %s is %s", t.ID, t.Status)
	}
	cp := *t
	prev = &cp
	t.Owner, t.Handoff, t.Status, t.UpdatedAt = h.To, h.ID, TaskTodo, at
	if t.Channel == "" {
		t.Channel = h.Channel
	}
	return ts, prev, nil
}

// undoTask takes handoff id's task back out after the handoff failed: a
// task made for it goes, one it was attached to is restored.
func undoTask(handoff string, prev *Task) {
	_ = changeTasks(func(tsp *[]Task) error {
		var kept []Task
		for _, t := range *tsp {
			switch {
			case t.Handoff != handoff:
				kept = append(kept, t)
			case prev != nil:
				kept = append(kept, *prev)
			}
		}
		*tsp = kept
		return nil
	})
}

// finishTask records on the task of handoff h how the handoff ended.
func finishTask(h Handoff) {
	_ = changeTasks(func(tsp *[]Task) error {
		t := taskOf(*tsp, h.ID)
		if t == nil || !t.open() {
			return nil
		}
		switch h.Status {
		case "done":
			t.Status, t.Note = TaskDone, firstLine(h.Result, 200)
		case "declined":
			t.Owner, t.Status, t.Note = h.From, TaskTodo, "declined: "+firstLine(h.Result, 200)
		default:
			return nil
		}
		t.UpdatedAt = now()
		return nil
	})
}

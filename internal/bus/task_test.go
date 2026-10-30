package bus

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

func sp(s string) *string { return &s }

func mustTask(t *testing.T, id string) Task {
	t.Helper()
	task, ok, err := FindTask(id)
	if err != nil || !ok {
		t.Fatalf("FindTask(%s) = %v %v", id, ok, err)
	}
	return task
}

func taskOfH(t *testing.T, h Handoff) Task {
	t.Helper()
	ts, _ := Tasks()
	for _, x := range ts {
		if x.Handoff == h.ID {
			return x
		}
	}
	t.Fatalf("no task for %s in %+v", h.ID, ts)
	return Task{}
}

func TestHandoffMakesTask(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "add the 404 case\nsecond line", "")
	task := taskOfH(t, h)
	if !strings.HasPrefix(task.ID, "t-") || len(task.ID) != 10 || task.Title != "add the 404 case" || task.Owner != idBuild ||
		task.Status != TaskTodo || task.Channel != h.Channel || task.By != idArch || task.Project != "/code/a" || task.Parent != "" {
		t.Fatalf("task = %+v", task)
	}

	// The card names it.
	text := deliver(t, idBuild)
	if !strings.Contains(text, "Task: "+task.ID+" (board)\n") {
		t.Errorf("card lacks the task:\n%s", text)
	}
	// Delivery starts it.
	if got := mustTask(t, task.ID); got.Status != TaskDoing {
		t.Errorf("after delivery: %+v", got)
	}

	// A handoff on from it is a part of it.
	h2 := mustHand(t, build, idCodex, "write the changelog", "")
	if got := taskOfH(t, h2); got.Parent != task.ID || got.Owner != idCodex {
		t.Errorf("child = %+v", got)
	}

	// bus_done closes it with the head of the result.
	if _, _, err := Done(build, h.ID, "added the case\nand tests", "done"); err != nil {
		t.Fatal(err)
	}
	if got := mustTask(t, task.ID); got.Status != TaskDone || got.Note != "added the case" {
		t.Errorf("after done: %+v", got)
	}

	// A title is cut to 80 runes.
	h3 := mustHand(t, arch, idBuild, strings.Repeat("é", 100), "")
	if got := []rune(taskOfH(t, h3).Title); len(got) != 80 {
		t.Errorf("title is %d runes", len(got))
	}
}

func TestDeclinedTaskGoesBack(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "port it to windows", "")
	deliver(t, idBuild)
	if _, _, err := Done(build, h.ID, "no windows box here\nsorry", "declined"); err != nil {
		t.Fatal(err)
	}
	got := taskOfH(t, h)
	if got.Owner != idArch || got.Status != TaskTodo || got.Note != "declined: no windows box here" {
		t.Errorf("declined = %+v", got)
	}
}

func TestLapseLeavesTaskDoing(t *testing.T) {
	setup(t)
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	h := mustHand(t, arch, idBuild, "x", "")
	deliver(t, idBuild)
	turns[idBuild] = &status.Turn{At: now().Add(time.Minute)}
	if err := CheckLapses(now().Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := taskOfH(t, h); got.Status != TaskDoing {
		t.Errorf("lapsed task = %+v", got)
	}
}

func TestSetTask(t *testing.T) {
	setup(t)
	task, err := SetTask(arch, TaskChange{Title: sp(" review the api "), Owner: sp(idBuild)})
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "review the api" || task.Status != TaskTodo || task.Owner != idBuild || task.Project != "/code/a" || task.By != idArch {
		t.Fatalf("created = %+v", task)
	}
	for _, c := range []TaskChange{
		{},                                       // no title
		{Title: sp("  ")},                        // blank
		{Title: sp("x"), Status: sp("finished")}, // bad status
		{Title: sp("x"), Owner: sp("nobody")},    // bad owner
		{ID: "t-nope", Note: sp("x")},            // unknown
		{ID: task.ID, WaitsOn: &[]string{"t-no"}},
		{ID: task.ID, Links: &[]Link{{Kind: "ftp", Ref: "x"}}},
	} {
		if _, err := SetTask(arch, c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	got, err := SetTask(build, TaskChange{ID: task.ID, Status: sp("review"), Owner: sp(You), Note: sp("PR open"),
		Links: &[]Link{{Kind: "pr", Ref: "#12"}}})
	if err != nil || got.Status != TaskReview || got.Owner != You || got.Note != "PR open" || len(got.Links) != 1 || got.Title != "review the api" {
		t.Fatalf("changed = %+v %v", got, err)
	}
	if len(told) != 0 {
		t.Errorf("told the person: %v", told)
	}
}

func TestSetTaskRefusesRelayedWork(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "add the 404 case", "")
	task := taskOfH(t, h)
	for _, st := range []string{"done", "dropped"} {
		_, err := SetTask(build, TaskChange{ID: task.ID, Status: sp(st)})
		if err == nil || !strings.Contains(err.Error(), "bus_done") {
			t.Errorf("closed relayed work as %s: %v", st, err)
		}
	}
	if _, err := SetTask(build, TaskChange{ID: task.ID, Status: sp("review"), Note: sp("PR up")}); err != nil {
		t.Errorf("review refused: %v", err)
	}
	// Once the handoff is finished it can be closed (dropped, here).
	if _, _, err := Done(build, h.ID, "done", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetTask(build, TaskChange{ID: task.ID, Status: sp("dropped")}); err != nil {
		t.Errorf("dropped after done: %v", err)
	}
}

func TestHandoffAttachesToTask(t *testing.T) {
	setup(t)
	task, err := SetTask(arch, TaskChange{Title: sp("ship v2"), Owner: sp(idArch), Status: sp("doing"), Channel: sp("dev")})
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := HandOffTask(arch, idBuild, "do the migration part", "", nil, "", "", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := mustTask(t, task.ID)
	if got.Owner != idBuild || got.Handoff != h.ID || got.Status != TaskTodo || got.Title != "ship v2" || got.Channel != "dev" {
		t.Errorf("attached = %+v", got)
	}
	if ts, _ := Tasks(); len(ts) != 1 {
		t.Errorf("%d tasks, want the one", len(ts))
	}
	// Not twice at once, not a task that is gone, not one finished.
	if _, _, err := HandOffTask(arch, idCodex, "again", "", nil, "", "", "t-nope"); err == nil {
		t.Error("attached to a task that is not there")
	}
	if hs, _ := Handoffs(); len(hs) != 1 {
		t.Errorf("a failed attach left a handoff: %d", len(hs))
	}
	if _, err := SetTask(arch, TaskChange{ID: task.ID, Status: sp("dropped")}); err == nil {
		t.Fatal("dropped relayed work") // open handoff
	}
	if _, _, err := Done(build, h.ID, "migrated", "done"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := HandOffTask(arch, idCodex, "again", "", nil, "", "", task.ID); err == nil {
		t.Error("attached to a finished task")
	}
}

func TestTaskRetention(t *testing.T) {
	setup(t)
	base := now()
	old, _ := SetTask(arch, TaskChange{Title: sp("old"), Status: sp("done")})
	fresh, _ := SetTask(arch, TaskChange{Title: sp("fresh"), Status: sp("done")})
	open, _ := SetTask(arch, TaskChange{Title: sp("open")})
	now = func() time.Time { return base.Add(6 * 24 * time.Hour) }
	if _, err := SetTask(arch, TaskChange{ID: fresh.ID, Note: sp("touched")}); err != nil {
		t.Fatal(err)
	}
	if ts, _ := Tasks(); len(ts) != 3 {
		t.Fatalf("moved too soon: %d tasks", len(ts))
	}
	now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	if _, err := SetTask(arch, TaskChange{ID: open.ID, Note: sp("still here")}); err != nil {
		t.Fatal(err)
	}
	ts, _ := Tasks()
	if len(ts) != 2 || findTask(ts, old.ID) != nil || findTask(ts, open.ID) == nil || findTask(ts, fresh.ID) == nil {
		t.Errorf("after a week: %+v", ts)
	}
	data, err := os.ReadFile(taskArchive())
	if err != nil || !strings.Contains(string(data), old.ID) || strings.Contains(string(data), open.ID) {
		t.Errorf("archive = %q %v", data, err)
	}
}

// A task an agent makes is its own unless it names an owner; one the
// person makes waits for an owner.
func TestTaskOwnerDefault(t *testing.T) {
	setup(t)
	title := "write the parser"
	a, err := SetTask(arch, TaskChange{Title: &title})
	if err != nil || a.Owner != idArch {
		t.Fatalf("agent's task: %+v %v", a, err)
	}
	b, err := SetTask(me, TaskChange{Title: &title})
	if err != nil || b.Owner != "" {
		t.Fatalf("person's task: %+v %v", b, err)
	}
}

package create

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestRequests(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if len(List()) != 0 {
		t.Fatal("a request from nowhere")
	}
	a, err := Add("gus", "id-gus", "", Spec{Type: TypeAgent, Name: "scout", Project: "/p", Kind: "claude", Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Add("gus", "id-gus", "", Spec{Type: TypeTeam, Name: "t", Project: "/p", Goal: "g"})
	if l := List(); len(l) != 2 || l[0].ID != a.ID || l[0].Spec.Name != "scout" || l[0].By != "gus" || l[1].ID != b.ID {
		t.Fatalf("list %+v", l)
	}
	if err := Answer(a.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(a.ID); got.Status != Allowed {
		t.Errorf("status %q", got.Status)
	}
	if err := Answer(a.ID, false, ""); !errors.Is(err, ErrGone) { // once
		t.Errorf("second answer: %v", err)
	}
	if got, _ := Get(a.ID); got.Status != Allowed {
		t.Errorf("status after second answer %q", got.Status)
	}
	if l := List(); len(l) != 1 || l[0].ID != b.ID {
		t.Errorf("answered ones are not listed: %+v", l)
	}
	Remove(a.ID)
	if _, err := Get(a.ID); !errors.Is(err, ErrGone) {
		t.Errorf("removed: %v", err)
	}
	// Ids make no path: nothing outside the directory is read or removed.
	for _, id := range []string{"../x", "c-zz", "", "c-0000000a/../../x"} {
		if _, err := Get(id); !errors.Is(err, ErrGone) || Answer(id, true, "") == nil {
			t.Errorf("id %q", id)
		}
		Remove(id)
	}
	// One that waited longer than Expiry was left by an agent that died: it
	// ends as expired, and is kept to say so.
	old := Request{ID: "c-00000001", At: time.Now().Add(-Expiry - time.Minute), Status: Waiting}
	if err := write(old); err != nil {
		t.Fatal(err)
	}
	if l := List(); len(l) != 1 || l[0].ID != b.ID {
		t.Errorf("list with an old one: %+v", l)
	}
	if got, _ := Get(old.ID); got.Status != Expired || got.DoneAt.IsZero() {
		t.Errorf("old one: %+v", got)
	}
	// Ended ones are kept for Keep, then dropped.
	gone := Request{ID: "c-00000002", At: time.Now().Add(-Keep - time.Hour), Status: Declined, DoneAt: time.Now().Add(-Keep - time.Minute)}
	if err := write(gone); err != nil {
		t.Fatal(err)
	}
	all := All()
	if _, err := os.Stat(path(gone.ID)); !os.IsNotExist(err) || len(all) != 2 {
		t.Errorf("all %+v, old declined: %v", all, err)
	}
}

// How a request ends: words with a deny, created or failed after allow, and
// nothing changes once it ended.
func TestOutcomes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spec := Spec{Type: TypeAgent, Name: "scout", Kind: "claude", Message: "hi"}
	r, _ := Add("gus", "id-gus", "api", spec)
	if r.From != "api" {
		t.Errorf("from %q", r.From)
	}
	if err := Answer(r.ID, false, "  use sonnet, not opus "); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(r.ID)
	if o := got.Outcome(); got.Status != Changes || o.Status != Changes || o.Reply != "use sonnet, not opus" || o.At.IsZero() {
		t.Errorf("changes: %+v %+v", got, o)
	}
	if err := Finish(r.ID, Expired, Result{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(r.ID); got.Status != Changes {
		t.Errorf("an ended one changed to %q", got.Status)
	}

	r, _ = Add("gus", "id-gus", "", spec)
	_ = Answer(r.ID, true, "")
	if got, _ := Get(r.ID); got.Outcome().Status != "creating" {
		t.Errorf("allowed: %+v", got.Outcome())
	}
	if err := Finish(r.ID, Expired, Result{}); err != nil { // too late to expire
		t.Fatal(err)
	}
	if got, _ := Get(r.ID); got.Status != Allowed {
		t.Errorf("an allowed one expired: %q", got.Status)
	}
	_ = Finish(r.ID, Created, Result{Agent: "id-scout", Channel: "dm.x"})
	if o, _ := Get(r.ID); o.Outcome().Status != Created || o.Outcome().Agent != "id-scout" || o.Outcome().Channel != "dm.x" {
		t.Errorf("created: %+v", o)
	}
	r, _ = Add("gus", "id-gus", "", spec)
	_ = Answer(r.ID, true, "")
	_ = Finish(r.ID, Failed, Result{Reason: "not a directory"})
	if o, _ := Get(r.ID); o.Outcome().Status != Failed || o.Outcome().Reason != "not a directory" {
		t.Errorf("failed: %+v", o)
	}
	if l := List(); len(l) != 0 {
		t.Errorf("listed as waiting: %+v", l)
	}
	if len(All()) != 3 {
		t.Errorf("all %+v", All())
	}
}

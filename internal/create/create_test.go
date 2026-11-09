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
	time.Sleep(2 * time.Millisecond)
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
	// One that waited too long is listed as it is: ending it is creator.Sweep's.
	old := Request{ID: "c-00000001", At: time.Now().Add(-Expiry - time.Minute), Status: Waiting}
	if err := Put(old); err != nil {
		t.Fatal(err)
	}
	if l := List(); len(l) != 2 {
		t.Errorf("list with an old one: %+v", l)
	}
	// Ended ones are kept for Keep, then dropped.
	gone := Request{ID: "c-00000002", At: time.Now().Add(-Keep - time.Hour), Status: Declined, DoneAt: time.Now().Add(-Keep - time.Minute)}
	if err := Put(gone); err != nil {
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
	if ok, err := Finish(r.ID, Waiting, Expired, Result{}); ok || err != nil { // it is not waiting
		t.Fatalf("finished an ended one: %v %v", ok, err)
	}
	if got, _ := Get(r.ID); got.Status != Changes {
		t.Errorf("an ended one changed to %q", got.Status)
	}

	r, _ = Add("gus", "id-gus", "", spec)
	_ = Answer(r.ID, true, "")
	if got, _ := Get(r.ID); got.Outcome().Status != "creating" {
		t.Errorf("allowed: %+v", got.Outcome())
	}
	if _, ok := Claim(r.ID); !ok {
		t.Fatal("no claim on an allowed one")
	}
	if _, ok := Claim(r.ID); ok {
		t.Error("claimed twice")
	}
	if got, _ := Get(r.ID); got.Status != Creating || got.Outcome().Status != Creating {
		t.Errorf("claimed: %+v", got)
	}
	_, _ = Finish(r.ID, Creating, Created, Result{Agent: "id-scout", Channel: "dm.x"})
	if o, _ := Get(r.ID); o.Outcome().Status != Created || o.Outcome().Agent != "id-scout" || o.Outcome().Channel != "dm.x" {
		t.Errorf("created: %+v", o)
	}
	r, _ = Add("gus", "id-gus", "", spec)
	_ = Answer(r.ID, true, "")
	_, _ = Finish(r.ID, Allowed, Failed, Result{Reason: "not a directory"})
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

// The order that raced: the person's Allow lands while a sweep, which listed
// the request as waiting, goes to expire it. The Allow stands, the expiry
// does not happen, and the claim is the starter's.
func TestExpiryAfterAllowDoesNotHappen(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	r, _ := Add("gus", "id-gus", "", Spec{Type: TypeAgent, Name: "scout", Kind: "claude", Message: "hi"})
	r.At = time.Now().Add(-Expiry - time.Minute)
	_ = Put(r)
	listed := All()[0] // the sweep's view: waiting, and due
	if listed.Status != Waiting {
		t.Fatalf("listed %+v", listed)
	}
	if err := Answer(r.ID, true, ""); err != nil { // the Allow lands
		t.Fatal(err)
	}
	if ok, _ := Finish(r.ID, listed.Status, Expired, Result{}); ok { // the sweep's step
		t.Fatal("expired an allowed request")
	}
	if got, _ := Get(r.ID); got.Status != Allowed {
		t.Fatalf("status %q", got.Status)
	}
	if _, ok := Claim(r.ID); !ok {
		t.Fatal("not claimed")
	}
	// Once claimed, an Allow-time view of it ends nothing either.
	if ok, _ := Finish(r.ID, Allowed, Failed, Result{}); ok {
		t.Error("ended a claimed request from allowed")
	}
	if ok, _ := Finish(r.ID, Creating, Created, Result{Agent: "a"}); !ok {
		t.Error("its starter could not end it")
	}
	// And the other order: expired first, an Allow after finds it gone.
	r2, _ := Add("gus", "id-gus", "", Spec{Type: TypeAgent, Name: "s2", Kind: "claude", Message: "hi"})
	if ok, _ := Finish(r2.ID, Waiting, Expired, Result{}); !ok {
		t.Fatal("did not expire")
	}
	if err := Answer(r2.ID, true, ""); !errors.Is(err, ErrGone) {
		t.Errorf("allowed an expired one: %v", err)
	}
}

// Answers at once: one wins, the other is gone; claims at once: one starts it.
func TestConcurrentAnswersAndClaims(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	r, _ := Add("gus", "id-gus", "", Spec{Type: TypeAgent, Name: "scout", Kind: "claude", Message: "hi"})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) { errs <- Answer(r.ID, i%2 == 0, "") }(i)
	}
	won := 0
	for i := 0; i < 8; i++ {
		if <-errs == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d answers took", won)
	}
	if got, _ := Get(r.ID); got.Status == Allowed {
		claims := make(chan bool, 8)
		for i := 0; i < 8; i++ {
			go func() { _, ok := Claim(r.ID); claims <- ok }()
		}
		n := 0
		for i := 0; i < 8; i++ {
			if <-claims {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d claims took", n)
		}
	}
}

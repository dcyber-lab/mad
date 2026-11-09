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
	a, err := Add("gus", "id-gus", Spec{Type: TypeAgent, Name: "scout", Project: "/p", Kind: "claude", Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Add("gus", "id-gus", Spec{Type: TypeTeam, Name: "t", Project: "/p", Goal: "g"})
	if l := List(); len(l) != 2 || l[0].ID != a.ID || l[0].Spec.Name != "scout" || l[0].By != "gus" || l[1].ID != b.ID {
		t.Fatalf("list %+v", l)
	}
	if err := Answer(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(a.ID); got.Status != Allowed {
		t.Errorf("status %q", got.Status)
	}
	if err := Answer(a.ID, false); !errors.Is(err, ErrGone) { // once
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
		if _, err := Get(id); !errors.Is(err, ErrGone) || Answer(id, true) == nil {
			t.Errorf("id %q", id)
		}
		Remove(id)
	}
	// One older than Expiry was left by an agent that died.
	old := Request{ID: "c-00000001", At: time.Now().Add(-Expiry - time.Minute), Status: Waiting}
	if err := write(old); err != nil {
		t.Fatal(err)
	}
	if l := List(); len(l) != 1 || l[0].ID != b.ID {
		t.Errorf("list with an old one: %+v", l)
	}
	if _, err := os.Stat(path(old.ID)); !os.IsNotExist(err) {
		t.Errorf("old one stays: %v", err)
	}
}

package queue

import (
	"os"
	"testing"
	"time"
)

func TestQueue(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Now()
	if _, err := Push("a", "  ", now); err == nil {
		t.Error("an empty message was queued")
	}
	first, _ := Push("a", "first", now)
	second, _ := Push("a", "second\nline", now)
	if _, err := Push("b", "other", now); err != nil {
		t.Fatal(err)
	}
	if p := Pending(); p["a"] != 2 || p["b"] != 1 {
		t.Errorf("Pending = %v", p)
	}

	// Paused: nothing to take, nothing pending.
	_ = SetPaused("a", true)
	if _, ok, _ := Take("a"); ok {
		t.Error("took from a paused queue")
	}
	if Pending()["a"] != 0 {
		t.Error("a paused queue is pending")
	}
	_ = SetPaused("a", false)

	it, ok, err := Take("a")
	if err != nil || !ok || it.ID != first.ID {
		t.Fatalf("Take = %+v %v %v", it, ok, err)
	}
	// Refused: back at the front, with why.
	_ = PutBack("a", it, "is working")
	q, _ := Load("a")
	if len(q.Items) != 2 || q.Items[0].ID != first.ID || q.Error != "is working" {
		t.Errorf("after PutBack: %+v", q)
	}
	_ = Remove("a", first.ID)
	_ = Sent("a")
	q, _ = Load("a")
	if len(q.Items) != 1 || q.Items[0].ID != second.ID || q.Error != "" {
		t.Errorf("after Remove and Sent: %+v", q)
	}

	// Emptied: no file left.
	if _, _, err := Take("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path("a")); !os.IsNotExist(err) {
		t.Errorf("empty queue left its file: %v", err)
	}
	_ = Clear("b")
	Forget("b")
	if all, _ := All(); len(all) != 0 {
		t.Errorf("All = %v", all)
	}
}

func TestPushBus(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Now()
	for i := 0; i < 3; i++ {
		added, err := PushBus("a", "[mad bus] messages waiting", now)
		if err != nil || added != (i == 0) {
			t.Fatalf("PushBus #%d = %v %v", i, added, err)
		}
	}
	if _, err := Push("a", "plain", now); err != nil {
		t.Fatal(err)
	}
	it, _, _ := Take("a")
	if !it.Bus {
		t.Errorf("first = %+v", it)
	}
	if added, _ := PushBus("a", "again", now); !added { // taken, so none pending
		t.Error("not added after the first was taken")
	}
}

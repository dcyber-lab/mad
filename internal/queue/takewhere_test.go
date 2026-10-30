package queue

import (
	"testing"
	"time"
)

func TestTakeWhereLeavesTheOthers(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Now()
	if _, err := PushBus("a1", "bus", now); err != nil {
		t.Fatal(err)
	}
	if _, err := Push("a1", "plain", now); err != nil {
		t.Fatal(err)
	}
	it, ok, err := TakeWhere("a1", func(x Item) bool { return !x.Bus })
	if err != nil || !ok || it.Text != "plain" {
		t.Fatalf("TakeWhere = %+v %v %v", it, ok, err)
	}
	if q, _ := Load("a1"); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("queue = %+v", q)
	}
	if _, ok, _ := TakeWhere("a1", func(x Item) bool { return !x.Bus }); ok {
		t.Error("took a bus item")
	}
}

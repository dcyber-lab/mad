package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRebuilt(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "mad")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatal(err)
	}
	b, ok := stampOf(bin)
	if !ok {
		t.Fatal("no stamp")
	}
	now := time.Now()
	if b.rebuilt(now) {
		t.Fatal("unchanged binary counted as rebuilt")
	}
	if (binStamp{}).rebuilt(now) {
		t.Fatal("an unknown binary counted as rebuilt")
	}

	if err := os.WriteFile(bin, []byte("v2, longer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if b.rebuilt(time.Now()) {
		t.Fatal("a binary still being written counted as rebuilt")
	}
	if !b.rebuilt(time.Now().Add(binSettle)) {
		t.Fatal("a settled new build not seen")
	}

	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if b.rebuilt(time.Now().Add(time.Minute)) {
		t.Fatal("a missing binary counted as rebuilt")
	}
}

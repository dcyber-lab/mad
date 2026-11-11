package phone

import (
	"os"
	"testing"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Rotate writes a different master token, as Token writes one, and has
// a running server take it.
func TestRotate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	orig := refreshRunning
	refreshRunning = func() (bool, error) { refreshed++; return true, nil }
	t.Cleanup(func() { refreshRunning = orig })
	restarted, err := Rotate()
	if err != nil || !restarted || refreshed != 1 {
		t.Fatalf("Rotate = %v, %v; refreshed %d times", restarted, err, refreshed)
	}
	got, ok := ExistingToken()
	if !ok || got == old || len(got) != 32 {
		t.Errorf("token after Rotate %q (ok %v), before %q", got, ok, old)
	}
	if again, _ := Token(); again != got {
		t.Errorf("Token after Rotate = %q, want the rotated %q", again, got)
	}
	fi, err := os.Stat(paths.PhoneToken())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, %v", fi.Mode().Perm(), err)
	}
}

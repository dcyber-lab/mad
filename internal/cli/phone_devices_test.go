package cli

import (
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/phone"
)

func TestPhoneDevicesAndRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if code, out, _ := run(t, "", "phone", "devices"); code != 0 || !strings.Contains(out, "no phone is paired") {
		t.Errorf("none: %d %q", code, out)
	}
	c, _ := phone.PairCode()
	_, id, err := phone.Pair(c, "Ann's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "", "phone", "devices")
	if code != 0 || !strings.Contains(out, id) || !strings.Contains(out, "Ann's iPhone") || strings.Contains(out, "token") && !strings.Contains(out, "NAME") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, _, errOut := run(t, "", "phone", "revoke", "nobody"); code == 0 || !strings.Contains(errOut, "no paired device") {
		t.Errorf("revoke unknown: %d %q", code, errOut)
	}
	if code, out, _ := run(t, "", "phone", "revoke", "Ann's iPhone"); code != 0 || !strings.Contains(out, "no longer paired") {
		t.Errorf("revoke: %d %q", code, out)
	}
	if len(phone.Devices()) != 0 {
		t.Error("still paired")
	}
}

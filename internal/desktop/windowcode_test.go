package desktop

import (
	"os/exec"
	"strings"
	"testing"
)

// The page's side of a window code (web/pair.js) is checked by node: traded
// once for the token, which then reads as #token= did. Where there is no
// node, it is skipped.
func TestPageWindowCode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: web/pair.js's window code is not checked")
	}
	out, err := exec.Command(node, "testdata/windowcode_check.js").CombinedOutput()
	if err != nil {
		t.Fatalf("windowcode_check.js: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok ") {
		t.Errorf("the check did not run:\n%s", out)
	}
}

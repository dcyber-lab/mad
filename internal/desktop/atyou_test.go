package desktop

import (
	"os/exec"
	"strings"
	"testing"
)

// The page's @you rule (YOU_MENTION in web/bus.js) is checked by node against the table the server's rule is
// held to (internal/bus/testdata/at-you.json). Where there is no node, it is skipped.
func TestPageAtYouRule(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: the page's @you rule is not checked")
	}
	out, err := exec.Command(node, "testdata/atyou_check.js").CombinedOutput()
	if err != nil {
		t.Fatalf("atyou_check.js: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok ") {
		t.Errorf("the check did not run:\n%s", out)
	}
}

package desktop

import (
	"os/exec"
	"strings"
	"testing"
)

// The words of a receipt (web/receipts.js) are checked by node: every state, several targets, no targets,
// a paused channel. Where there is no node, it is skipped.
func TestReceiptWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: web/receipts.js is not checked")
	}
	out, err := exec.Command(node, "testdata/receipts_check.js").CombinedOutput()
	if err != nil {
		t.Fatalf("receipts_check.js: %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "ok "); n < 6 {
		t.Errorf("only %d of the 6 checks ran:\n%s", n, out)
	}
}

package desktop

import (
	"os/exec"
	"strings"
	"testing"
)

// The page's picture cache (web/blobs.js) is checked by node: eviction by bytes, sharing, and that
// no object URL is left un-revoked. Where there is no node, it is skipped.
func TestBlobCache(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node: web/blobs.js is not checked")
	}
	out, err := exec.Command(node, "testdata/blobcache_check.js").CombinedOutput()
	if err != nil {
		t.Fatalf("blobcache_check.js: %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "ok "); n < 7 {
		t.Errorf("only %d of the 7 checks ran:\n%s", n, out)
	}
}

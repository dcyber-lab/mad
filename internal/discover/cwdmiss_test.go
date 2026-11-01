package discover

import (
	"testing"
	"time"
)

// A process whose cwd can't be read is asked again only after cwdRetry,
// and forgotten once it is gone.
func TestScanExternalCwdMissed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	ps := "301 ttys001 claude"
	now := time.Now()
	asked, cwd := 0, ""
	oldPS, oldCwd, oldNow := listProcesses, processCwd, scanNow
	t.Cleanup(func() { listProcesses, processCwd, scanNow = oldPS, oldCwd, oldNow })
	listProcesses = func() ([]byte, error) { return []byte(ps), nil }
	processCwd = func(int) string { asked++; return cwd }
	scanNow = func() time.Time { return now }
	externalMu.Lock()
	externalCache, cwdMissed = map[int]External{}, map[int]time.Time{}
	externalMu.Unlock()

	ScanExternal(nil, nil)
	ScanExternal(nil, nil)
	if asked != 1 {
		t.Fatalf("cwd asked %d times within %v, want once", asked, cwdRetry)
	}
	now, cwd = now.Add(cwdRetry), dir
	if ext := ScanExternal(nil, nil); asked != 2 || len(ext) != 1 || ext[0].Cwd != dir {
		t.Fatalf("after %v: asked %d, found %+v", cwdRetry, asked, ext)
	}

	cwd = ""
	ps = "302 ttys002 claude"
	ScanExternal(nil, nil)
	ps = ""
	ScanExternal(nil, nil)
	externalMu.Lock()
	defer externalMu.Unlock()
	if len(cwdMissed) != 0 || len(externalCache) != 0 {
		t.Fatalf("gone processes kept: %v %v", cwdMissed, externalCache)
	}
}

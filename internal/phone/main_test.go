package phone

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain puts a tailscale on PATH that does nothing, for every test: a
// test that forgets fakeTailscale must not reach the machine's own
// tailscale. Switching to Cloudflare runs `tailscale serve --https=7443
// off`, which turned off a live deck's phone when the tests were run.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mad-phone-test")
	if err != nil {
		panic(err)
	}
	script := "#!/bin/sh\necho 'tailscale: not in tests' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		panic(err)
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

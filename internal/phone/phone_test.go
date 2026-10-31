package phone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
	"rsc.io/qr"
)

// fakeTailscale puts a tailscale on PATH answering status --json with
// status, and noting what else it is asked in the returned file.
func fakeTailscale(t *testing.T, status string) (calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = status ]; then cat <<'EOF'\n%s\nEOF\nexit 0; fi\necho \"$@\" >> '%s'\n", status, calls)
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

const running = `{"BackendState":"Running","Self":{"DNSName":"box.tail1.ts.net."},"CertDomains":["box.tail1.ts.net"]}`

func TestHost(t *testing.T) {
	for _, c := range []struct {
		status string
		want   string
		err    error
	}{
		{running, "box.tail1.ts.net", nil},
		{`{"BackendState":"Stopped","Self":{"DNSName":"box.tail1.ts.net."}}`, "", ErrOffline},
		{`{"BackendState":"Running","Self":{"DNSName":"box.tail1.ts.net."},"CertDomains":null}`, "", ErrNoHTTPS},
		{`not json`, "", ErrNoTailscale},
	} {
		fakeTailscale(t, c.status)
		got, err := host(context.Background())
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%s: %q, %v; want %q, %v", c.status, got, err, c.want, c.err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := host(context.Background()); !errors.Is(err, ErrNoTailscale) {
		t.Errorf("none installed: %v", err)
	}
}

func TestToken(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, err := Token()
	if err != nil || len(a) != 32 {
		t.Fatalf("token %q, %v", a, err)
	}
	if b, _ := Token(); b != a {
		t.Errorf("a second token %q: the phone's must stay", b)
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "*", "phone_token"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "*", "*", "phone_token"))
	}
	if len(matches) != 1 {
		t.Fatalf("token file: %v", matches)
	}
	if fi, err := os.Stat(matches[0]); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, %v", fi.Mode().Perm(), err)
	}
}

// On serves the phone from a pool pane and has tailscale pass it on; Off
// undoes both.
func TestOnOff(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	old, oldSelf := tmux.Socket, deck.SelfCommand
	tmux.Socket = fmt.Sprintf("mad-phone-test-%d", time.Now().UnixNano())
	// Not a comment: the pane's loop goes on after it.
	deck.SelfCommand = func(sub string) string { return "sleep 600; : mad " + sub }
	oldAwake := awake
	awake = ": awake;"
	t.Cleanup(func() { awake = oldAwake })
	t.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand = old, oldSelf
	})
	calls := fakeTailscale(t, running)
	ctx := context.Background()
	if _, err := On(ctx); err == nil || !strings.Contains(err.Error(), "deck is not running") {
		t.Errorf("on with no deck: %v", err)
	}
	if err := tmux.Run("new-session", "-d", "-s", tmux.PoolSession); err != nil {
		t.Fatal(err)
	}

	if s := Get(ctx); s.On || s.Host != "box.tail1.ts.net" || s.URL != "" {
		t.Errorf("before: %+v", s)
	}
	s, err := On(ctx)
	if err != nil || !s.On {
		t.Fatalf("on: %+v, %v", s, err)
	}
	tok, _ := Token()
	if s.URL != "https://box.tail1.ts.net:7443/#token="+tok {
		t.Errorf("url %q", s.URL)
	}
	panes, _ := tmux.ListPanes()
	p, ok := tmux.FindPane(panes, tmux.IDPhone)
	if !ok {
		t.Fatal("no phone pane")
	}
	cmd, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd")
	if !strings.Contains(cmd, "desktop -serve -addr 127.0.0.1:7484 -origin 'box.tail1.ts.net:7443'") {
		t.Errorf("pane runs %q", cmd)
	}
	// Again: the same pane, left running.
	if _, err := On(ctx); err != nil {
		t.Fatal(err)
	}
	panes, _ = tmux.ListPanes()
	if p2, _ := tmux.FindPane(panes, tmux.IDPhone); p2.ID != p.ID {
		t.Errorf("pane %s replaced by %s", p.ID, p2.ID)
	}
	if g := Get(ctx); !g.On || g.URL != s.URL {
		t.Errorf("get: %+v", g)
	}
	if !strings.Contains(cmd, ": awake; sleep 600") {
		t.Errorf("not kept awake: %q", cmd)
	}

	// A mad rebuilt: the deck opening starts the server again on it, in
	// the same pane, from what the pane says, even a pane of a mad that
	// kept only its command.
	deck.SelfCommand = func(sub string) string { return "sleep 601; : mad " + sub }
	if err := tmux.Run("set-option", "-p", "-u", "-t", p.ID, "@mad_phone_host"); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	panes, _ = tmux.ListPanes()
	p3, _ := tmux.FindPane(panes, tmux.IDPhone)
	cmd, _ = tmux.Out("show-options", "-p", "-v", "-t", p3.ID, "@mad_cmd")
	if p3.ID != p.ID || !strings.Contains(cmd, "sleep 601; : mad desktop -serve -addr 127.0.0.1:7484 -origin 'box.tail1.ts.net:7443'") {
		t.Errorf("refreshed: pane %s, %q", p3.ID, cmd)
	}

	if err := Off(ctx); err != nil {
		t.Fatal(err)
	}
	panes, _ = tmux.ListPanes()
	if _, ok := tmux.FindPane(panes, tmux.IDPhone); ok {
		t.Error("the phone's pane is still there")
	}
	b, _ := os.ReadFile(calls)
	want := "serve --bg --https=7443 http://127.0.0.1:7484\nserve --bg --https=7443 http://127.0.0.1:7484\nserve --https=7443 off\n"
	if string(b) != want {
		t.Errorf("tailscale asked:\n%s\nwant:\n%s", b, want)
	}
}

func TestTerminal(t *testing.T) {
	q, err := Terminal("https://box.tail1.ts.net:7443/#token=0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(q, "\n"), "\n")
	// Its modules and a border of 2 each side, two rows a line.
	c, _ := qr.Encode("https://box.tail1.ts.net:7443/#token=0123456789abcdef0123456789abcdef", qr.M)
	if len(lines) != (c.Size+4+1)/2 || !strings.Contains(q, "█") || !strings.HasPrefix(lines[0], "\x1b[30;107m") {
		t.Errorf("%d lines:\n%s", len(lines), q)
	}
	if png, err := PNG("x"); err != nil || len(png) < 100 || string(png[1:4]) != "PNG" {
		t.Errorf("png: %d bytes, %v", len(png), err)
	}
}

// fakeCloudflared puts a cloudflared on PATH that notes its arguments in
// the returned file and, as a quick tunnel does, prints its name.
func fakeCloudflared(t *testing.T) (calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> '%s'\necho 'INF |  https://quiet-fox-1234.trycloudflare.com  |' >&2\nexec sleep 600\n", calls)
	if err := os.WriteFile(filepath.Join(dir, "cloudflared"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func TestChoose(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if c := LoadChoice(); c.Via != ViaTailscale {
		t.Errorf("first choice %+v", c)
	}
	if err := Choose(Choice{Via: ViaCloudflare, Tunnel: "mine"}); !errors.Is(err, ErrNoHostname) {
		t.Errorf("named without hostname: %v", err)
	}
	if err := Choose(Choice{Via: ViaCloudflare, Hostname: "a.example.com"}); !errors.Is(err, ErrHostnameAlone) {
		t.Errorf("hostname without tunnel: %v", err)
	}
	if err := Choose(Choice{Via: ViaCloudflare, Tunnel: "mine", Hostname: "x; rm -rf /"}); err == nil {
		t.Error("a hostname with a shell in it was taken")
	}
	if c := LoadChoice(); c.Via != ViaTailscale {
		t.Errorf("a refused choice was kept: %+v", c)
	}
	want := Choice{Via: ViaCloudflare, Tunnel: "mine", Hostname: "mad.example.com"}
	if err := Choose(Choice{Via: ViaCloudflare, Tunnel: "mine", Hostname: "Mad.Example.com"}); err != nil {
		t.Fatal(err)
	}
	if c := LoadChoice(); c != want {
		t.Errorf("kept %+v, want %+v", c, want)
	}
	if err := Choose(Choice{Via: ViaTailscale}); err != nil {
		t.Fatal(err)
	}
	if c := LoadChoice(); c.Via != ViaTailscale || c.Tunnel != "" {
		t.Errorf("back to tailscale: %+v", c)
	}
}

func TestTunnelCommand(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	q := tunnelCommand(Choice{Via: ViaCloudflare})
	if !strings.Contains(q, "cloudflared tunnel --no-autoupdate --url http://127.0.0.1:7484 --metrics 127.0.0.1:7485") {
		t.Errorf("quick: %s", q)
	}
	n := tunnelCommand(Choice{Via: ViaCloudflare, Tunnel: "mine", Hostname: "mad.example.com"})
	if !strings.Contains(n, "cloudflared tunnel --no-autoupdate run --url http://127.0.0.1:7484 'mine' 2>>") {
		t.Errorf("named: %s", n)
	}
}

// A quick tunnel's name comes from the metrics server, else the log, and
// a tunnel that never says fails with the log's end.
func TestQuickHostname(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	oldAddr, oldWait := metricsAddr, quickWait
	t.Cleanup(func() { metricsAddr, quickWait = oldAddr, oldWait })
	metricsAddr = "127.0.0.1:1" // nothing there
	quickWait = 300 * time.Millisecond
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Dir(paths.PhoneTunnelLog()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PhoneTunnelLog(), []byte("INF starting\nERR boom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := quickHostname(ctx); err == nil || !strings.Contains(err.Error(), "ERR boom") {
		t.Errorf("no name: %v", err)
	}

	os.WriteFile(paths.PhoneTunnelLog(), []byte("INF |  https://old-one.trycloudflare.com  |\nINF |  https://new-one.trycloudflare.com  |\n"), 0o600)
	if h, err := quickHostname(ctx); err != nil || h != "new-one.trycloudflare.com" {
		t.Errorf("from the log: %q, %v", h, err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quicktunnel" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"hostname":"from-metrics.trycloudflare.com"}`)
	}))
	defer srv.Close()
	metricsAddr = strings.TrimPrefix(srv.URL, "http://")
	if h, err := quickHostname(ctx); err != nil || h != "from-metrics.trycloudflare.com" {
		t.Errorf("from metrics: %q, %v", h, err)
	}
}

// Through Cloudflare: On runs the tunnel and the server in their own
// panes, Refresh restarts only the server, Off ends both.
func TestCloudflareOnOff(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	old, oldSelf := tmux.Socket, deck.SelfCommand
	tmux.Socket = fmt.Sprintf("mad-phone-test-%d", time.Now().UnixNano())
	deck.SelfCommand = func(sub string) string { return "sleep 600; : mad " + sub }
	oldAwake, oldAddr := awake, metricsAddr
	awake = ": awake;"
	metricsAddr = "127.0.0.1:1"
	t.Cleanup(func() {
		awake, metricsAddr = oldAwake, oldAddr
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand = old, oldSelf
	})
	calls := fakeCloudflared(t)
	ctx := context.Background()
	if err := Choose(Choice{Via: ViaCloudflare}); err != nil {
		t.Fatal(err)
	}
	if err := tmux.Run("new-session", "-d", "-s", tmux.PoolSession); err != nil {
		t.Fatal(err)
	}

	if s := Get(ctx); s.On || s.Via != ViaCloudflare || s.URL != "" {
		t.Errorf("before: %+v", s)
	}
	s, err := On(ctx)
	if err != nil || !s.On {
		t.Fatalf("on: %+v, %v", s, err)
	}
	tok, _ := Token()
	const host = "quiet-fox-1234.trycloudflare.com"
	if s.Via != ViaCloudflare || s.Host != host || s.URL != "https://"+host+"/#token="+tok {
		t.Errorf("status %+v", s)
	}
	panes, _ := tmux.ListPanes()
	tp, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel)
	sp, ok2 := tmux.FindPane(panes, tmux.IDPhone)
	if !ok || !ok2 {
		t.Fatalf("panes: tunnel %v, server %v", ok, ok2)
	}
	cmd, _ := tmux.Out("show-options", "-p", "-v", "-t", sp.ID, "@mad_cmd")
	if !strings.Contains(cmd, "-origin '"+host+"'") || strings.Contains(cmd, host+":") {
		t.Errorf("server runs %q", cmd)
	}
	if g := Get(ctx); !g.On || g.URL != s.URL {
		t.Errorf("get: %+v", g)
	}
	// Again: both left running, the name kept.
	if s2, err := On(ctx); err != nil || s2.URL != s.URL {
		t.Fatalf("again: %+v, %v", s2, err)
	}
	panes, _ = tmux.ListPanes()
	if p, _ := tmux.FindPane(panes, tmux.IDPhoneTunnel); p.ID != tp.ID {
		t.Errorf("tunnel pane %s replaced by %s", tp.ID, p.ID)
	}
	if b, _ := os.ReadFile(calls); strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), "tunnel --no-autoupdate --url http://127.0.0.1:7484 --metrics 127.0.0.1:1") {
		t.Errorf("cloudflared run as:\n%s", b)
	}

	// A rebuilt mad: the server restarts, the tunnel stays.
	deck.SelfCommand = func(sub string) string { return "sleep 601; : mad " + sub }
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	panes, _ = tmux.ListPanes()
	p, _ := tmux.FindPane(panes, tmux.IDPhone)
	cmd, _ = tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd")
	if !strings.Contains(cmd, "sleep 601; : mad desktop -serve -addr 127.0.0.1:7484 -origin '"+host+"'") {
		t.Errorf("refreshed server: %q", cmd)
	}
	if q, _ := tmux.FindPane(panes, tmux.IDPhoneTunnel); q.ID != tp.ID || tunnelHost() != host {
		t.Errorf("tunnel touched by Refresh: %+v, %q", q, tunnelHost())
	}
	if b, _ := os.ReadFile(calls); strings.Count(string(b), "\n") != 1 {
		t.Errorf("cloudflared started again:\n%s", b)
	}

	if err := Off(ctx); err != nil {
		t.Fatal(err)
	}
	panes, _ = tmux.ListPanes()
	if _, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel); ok {
		t.Error("the tunnel's pane is still there")
	}
	if _, ok := tmux.FindPane(panes, tmux.IDPhone); ok {
		t.Error("the server's pane is still there")
	}
}

// A named tunnel runs under its own name and serves at the name given;
// going back to Tailscale ends the tunnel.
func TestCloudflareNamed(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("MAD_SOCKET", "")
	old, oldSelf := tmux.Socket, deck.SelfCommand
	tmux.Socket = fmt.Sprintf("mad-phone-test-%d", time.Now().UnixNano())
	deck.SelfCommand = func(sub string) string { return "sleep 600; : mad " + sub }
	oldAwake := awake
	awake = ": awake;"
	t.Cleanup(func() {
		awake = oldAwake
		_ = tmux.Run("kill-server")
		tmux.Socket, deck.SelfCommand = old, oldSelf
	})
	calls := fakeCloudflared(t)
	fakeTailscale(t, running)
	ctx := context.Background()
	if err := tmux.Run("new-session", "-d", "-s", tmux.PoolSession); err != nil {
		t.Fatal(err)
	}
	if err := Choose(Choice{Via: ViaCloudflare, Tunnel: "mine", Hostname: "mad.example.com"}); err != nil {
		t.Fatal(err)
	}
	s, err := On(ctx)
	tok, _ := Token()
	if err != nil || s.URL != "https://mad.example.com/#token="+tok {
		t.Fatalf("on: %+v, %v", s, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(calls); len(b) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if b, _ := os.ReadFile(calls); string(b) != "tunnel --no-autoupdate run --url http://127.0.0.1:7484 mine\n" {
		t.Errorf("cloudflared run as %q", b)
	}

	if err := Choose(Choice{Via: ViaTailscale}); err != nil {
		t.Fatal(err)
	}
	if s, err := On(ctx); err != nil || s.Via != ViaTailscale {
		t.Fatalf("tailscale again: %+v, %v", s, err)
	}
	panes, _ := tmux.ListPanes()
	if _, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel); ok {
		t.Error("the tunnel runs on after going back to Tailscale")
	}
}

func TestNoCloudflared(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	if err := Choose(Choice{Via: ViaCloudflare}); err != nil {
		t.Fatal(err)
	}
	s, err := On(context.Background())
	if !errors.Is(err, ErrNoCloudflared) || !strings.Contains(s.Problem, "brew install cloudflared") {
		t.Errorf("%+v, %v", s, err)
	}
}

package phone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// The phone's second way in: a Cloudflare Tunnel, run by cloudflared in
// a pool pane of its own, so the phone (or the iOS app) reaches the Mac
// from anywhere, not only from the tailnet. Quick: no account, a new
// *.trycloudflare.com name each time the tunnel starts. Named: a tunnel
// of the user's own account and domain, made beforehand with cloudflared
// tunnel login, create and route dns.

// The two ways in.
const (
	ViaTailscale  = "tailscale"
	ViaCloudflare = "cloudflare"
)

// Choice is the way in the person picked, kept in the state directory so
// the window's button, mad phone and Refresh all use it.
type Choice struct {
	Via      string `json:"via"`
	Tunnel   string `json:"tunnel,omitempty"`   // a named tunnel's name; "" is quick
	Hostname string `json:"hostname,omitempty"` // the named tunnel's public name
}

// cloudflared is the CLI; tests replace it.
var cloudflared = "cloudflared"

// metricsAddr is where a quick tunnel's cloudflared tells its name.
var metricsAddr = "127.0.0.1:7485"

// quickWait is how long a quick tunnel has to come up.
var quickWait = 20 * time.Second

// ErrNoCloudflared and the like say what to set up first.
var (
	ErrNoCloudflared = errors.New("cloudflared is not installed: brew install cloudflared")
	ErrNoHostname    = errors.New("a named tunnel needs the name it is served at: --hostname <host>")
	ErrHostnameAlone = errors.New("--hostname goes with --tunnel: a quick tunnel makes its own name")
)

var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// LoadChoice is the way in last chosen; Tailscale before any was.
func LoadChoice() Choice {
	var c Choice
	if b, err := os.ReadFile(paths.PhoneVia()); err == nil && json.Unmarshal(b, &c) == nil && c.Via == ViaCloudflare {
		return c
	}
	return Choice{Via: ViaTailscale}
}

// Choose checks c and keeps it as the way in.
func Choose(c Choice) error {
	switch c.Via {
	case ViaTailscale:
		c = Choice{Via: ViaTailscale}
	case ViaCloudflare:
		c.Hostname = strings.ToLower(strings.TrimSpace(c.Hostname))
		c.Tunnel = strings.TrimSpace(c.Tunnel)
		if c.Tunnel != "" && c.Hostname == "" {
			return ErrNoHostname
		}
		if c.Tunnel == "" && c.Hostname != "" {
			return ErrHostnameAlone
		}
		if c.Hostname != "" && !hostnameRe.MatchString(c.Hostname) {
			return fmt.Errorf("%q is not a host name", c.Hostname)
		}
	default:
		return fmt.Errorf("unknown way in %q", c.Via)
	}
	b, _ := json.Marshal(c)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(paths.PhoneVia(), append(b, '\n'), 0o600)
}

// tunnelCommand keeps cloudflared alive as the server's is.
func tunnelCommand(c Choice) string {
	local := fmt.Sprintf("http://127.0.0.1:%d", Port)
	var run string
	if c.Tunnel == "" {
		run = fmt.Sprintf("%s tunnel --no-autoupdate --url %s --metrics %s", cloudflared, local, metricsAddr)
	} else {
		run = fmt.Sprintf("%s tunnel --no-autoupdate run --url %s %s", cloudflared, local, paths.ShellQuote(c.Tunnel))
	}
	return fmt.Sprintf("while :; do %s 2>>%s; sleep 1; done", run, paths.ShellQuote(paths.PhoneTunnelLog()))
}

// storedTunnelHost is the pane running the tunnel and the name noted on
// it, "" if none runs.
func storedTunnelHost() (id, host string) {
	panes, err := tmux.ListPanes()
	if err != nil {
		return "", ""
	}
	p, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel)
	if !ok || p.Dead {
		return "", ""
	}
	h, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_phone_host")
	return p.ID, h
}

// tunnelHost is the name the running tunnel is reached at, "" if none.
// A quick tunnel's name changes whenever its cloudflared starts again (the
// pane's loop does that when it exits), so cloudflared's own word is the
// truth, and the name noted on the pane only a fallback while it does not
// answer. When the name moved, the note and the server's origin follow it.
func tunnelHost() string {
	id, stored := storedTunnelHost()
	if stored == "" || LoadChoice().Tunnel != "" {
		return stored
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	live := fromMetrics(ctx, &http.Client{Timeout: time.Second})
	if live == "" || live == stored {
		return stored
	}
	if err := tmux.Run("set-option", "-p", "-t", id, "@mad_phone_host", live); err != nil {
		return stored
	}
	if panes, err := tmux.ListPanes(); err == nil {
		if p, ok := tmux.FindPane(panes, tmux.IDPhone); ok {
			_ = respawn(p.ID, serveCommand(live), live)
		}
	}
	return live
}

var quickRe = regexp.MustCompile(`https://([a-z0-9-]+(?:\.[a-z0-9-]+)*\.trycloudflare\.com)`)

// quickHostname asks cloudflared for the quick tunnel's name: its metrics
// server says, else its log does. It waits for the tunnel to come up.
func quickHostname(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(quickWait)
	for {
		if h := fromMetrics(ctx, client); h != "" {
			return h, nil
		}
		if b, err := os.ReadFile(paths.PhoneTunnelLog()); err == nil {
			if all := quickRe.FindAllSubmatch(b, -1); len(all) > 0 {
				return string(all[len(all)-1][1]), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("cloudflared gave no tunnel address in %v; its log:\n%s", quickWait, logTail(paths.PhoneTunnelLog(), 8))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func fromMetrics(ctx context.Context, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+metricsAddr+"/quicktunnel", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var q struct{ Hostname string }
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&q) != nil {
		return ""
	}
	return q.Hostname
}

func logTail(path string, n int) string {
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ensureTunnel runs the tunnel, unless it runs already as chosen, and
// says the name it is reached at. A quick tunnel left running keeps its
// name: it changes only when cloudflared starts again.
func ensureTunnel(ctx context.Context, c Choice) (string, error) {
	cmd := tunnelCommand(c)
	panes, err := tmux.ListPanes()
	if err != nil || !tmux.HasSession(tmux.PoolSession) {
		return "", errors.New("the deck is not running: open it first (mad)")
	}
	if p, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel); ok && !p.Dead {
		if had, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd"); had == cmd {
			if h := tunnelHost(); h != "" {
				return h, nil
			}
		}
	}
	_ = os.MkdirAll(paths.StateDir(), 0o755)
	_ = os.WriteFile(paths.PhoneTunnelLog(), nil, 0o600)
	if err := ensurePane(tmux.IDPhoneTunnel, "phone-tunnel", cmd, c.Hostname); err != nil {
		return "", err
	}
	if c.Tunnel != "" {
		return c.Hostname, nil
	}
	h, err := quickHostname(ctx)
	if err != nil {
		killPane(tmux.IDPhoneTunnel)
		return "", err
	}
	panes, _ = tmux.ListPanes()
	if p, ok := tmux.FindPane(panes, tmux.IDPhoneTunnel); ok {
		_ = tmux.Run("set-option", "-p", "-t", p.ID, "@mad_phone_host", h)
	}
	return h, nil
}

// onCloudflare serves the phone's server through the tunnel.
func onCloudflare(ctx context.Context, c Choice) (Status, error) {
	if _, err := exec.LookPath(cloudflared); err != nil {
		return Status{Problem: ErrNoCloudflared.Error()}, ErrNoCloudflared
	}
	if _, err := Token(); err != nil {
		return Status{}, err
	}
	code, err := PairCode()
	if err != nil {
		return Status{}, err
	}
	h, err := ensureTunnel(ctx, c)
	if err != nil {
		return Status{}, err
	}
	if err := ensurePane(tmux.IDPhone, "phone", serveCommand(h), h); err != nil {
		return Status{}, err
	}
	// Not also on the tailnet, if it was.
	if _, err := exec.LookPath(tailscale); err == nil {
		_ = exec.CommandContext(ctx, tailscale, "serve", fmt.Sprintf("--https=%d", HTTPSPort), "off").Run()
	}
	return Status{On: true, Via: ViaCloudflare, Host: h, URL: cloudflareURL(h, code)}, nil
}

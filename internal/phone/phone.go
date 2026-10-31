// Package phone puts the desktop window on the user's phone. A server of
// the deck's own (`mad desktop -serve`) runs in a pool pane, as mad
// stream does, on a port of 127.0.0.1; `tailscale serve` passes it on to
// the user's tailnet over HTTPS, with a certificate for this machine's
// name there. Only the user's own devices reach it, and they need its
// token too: kept in the state directory, so a phone that has it keeps
// working, and handed over in a QR code to scan.
package phone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

const (
	// Port is the server's, on 127.0.0.1.
	Port = 7484
	// HTTPSPort is the tailnet's way in, served by tailscale.
	HTTPSPort = 7443
)

// Status says whether the phone can reach the deck, and how.
type Status struct {
	On   bool   `json:"on"`             // its server runs
	Via  string `json:"via,omitempty"`  // ViaTailscale or ViaCloudflare
	Host string `json:"host,omitempty"` // this machine's name on the tailnet, or the tunnel's
	URL  string `json:"url,omitempty"`  // to open on the phone, token and all
	// Problem is why it can't be on: no Tailscale, HTTPS off, no cloudflared.
	Problem string `json:"problem,omitempty"`
}

// tailscale is the CLI; tests replace it.
var tailscale = "tailscale"

// ErrNoTailscale and the like say what to set up first.
var (
	ErrNoTailscale = errors.New("Tailscale is not running on this machine: install it (tailscale.com), sign in, and do the same on the phone")
	ErrOffline     = errors.New("Tailscale is not connected: run tailscale up")
	ErrNoHTTPS     = errors.New("HTTPS is off in this tailnet: turn on HTTPS certificates in the Tailscale admin console (DNS page)")
)

// host is this machine's name on the tailnet, one it can have an HTTPS
// certificate for.
func host(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, tailscale, "status", "--json").Output()
	if err != nil {
		return "", ErrNoTailscale
	}
	var st struct {
		BackendState string
		Self         struct{ DNSName string }
		CertDomains  []string
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", ErrNoTailscale
	}
	if st.BackendState != "Running" {
		return "", ErrOffline
	}
	name := strings.TrimSuffix(st.Self.DNSName, ".")
	for _, d := range st.CertDomains {
		if d == name {
			return name, nil
		}
	}
	return "", ErrNoHTTPS
}

// Origin is where the phone's page comes from, for host's name: the
// server takes its websockets.
func Origin(host string) string {
	return fmt.Sprintf("%s:%d", host, HTTPSPort)
}

func pageURL(host, token string) string {
	return fmt.Sprintf("https://%s/#token=%s", Origin(host), token)
}

// cloudflareURL is the page through a tunnel: port 443, so none in it.
func cloudflareURL(host, token string) string {
	return fmt.Sprintf("https://%s/#token=%s", host, token)
}

// ExistingToken is the phone's token if one was made, and never makes
// one: ok is false before the phone was first turned on.
func ExistingToken() (string, bool) {
	b, err := os.ReadFile(paths.PhoneToken())
	t := strings.TrimSpace(string(b))
	return t, err == nil && len(t) >= 32
}

// Token is the phone's token, made the first time.
func Token() (string, error) {
	if b, err := os.ReadFile(paths.PhoneToken()); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return "", err
	}
	return t, os.WriteFile(paths.PhoneToken(), []byte(t+"\n"), 0o600)
}

// Get says how things stand.
func Get(ctx context.Context) Status {
	var s Status
	panes, err := tmux.ListPanes()
	if err == nil {
		p, ok := tmux.FindPane(panes, tmux.IDPhone)
		s.On = ok && !p.Dead
	}
	c := LoadChoice()
	s.Via = c.Via
	if c.Via == ViaCloudflare {
		s.Host = tunnelHost()
		if s.On && s.Host != "" {
			if t, err := Token(); err == nil {
				s.URL = cloudflareURL(s.Host, t)
			}
		}
		return s
	}
	h, err := host(ctx)
	if err != nil {
		s.Problem = err.Error()
		return s
	}
	s.Host = h
	if s.On {
		if t, err := Token(); err == nil {
			s.URL = pageURL(h, t)
		}
	}
	return s
}

// serveCommand keeps the server alive as the stream's is. On a Mac it
// keeps the Mac from sleeping while it runs, as a sleeping one serves no
// phone: its screen may go dark and lock, on battery too (caffeinate -i);
// a closed lid still puts it to sleep.
func serveCommand(origin string) string {
	sub := fmt.Sprintf("desktop -serve -addr 127.0.0.1:%d -origin %s", Port, paths.ShellQuote(origin))
	run := deck.SelfCommand(sub)
	if awake != "" {
		run = awake + " " + run
	}
	return fmt.Sprintf("while :; do %s 2>>%s; sleep 1; done", run, paths.ShellQuote(paths.PhoneLog()))
}

// awake runs a command keeping the machine awake; "" where there is no
// such thing. Tests replace it.
var awake = func() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	if _, err := exec.LookPath("caffeinate"); err != nil {
		return ""
	}
	return "caffeinate -i"
}()

// Refresh starts the phone's server again, if it runs: on the mad just
// built, its page as that mad has it. The deck does it as it opens.
func Refresh() error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	p, ok := tmux.FindPane(panes, tmux.IDPhone)
	if !ok {
		return nil
	}
	via := LoadChoice().Via
	host := ""
	if via == ViaCloudflare {
		// The tunnel keeps running: its name is the one to serve for.
		host = tunnelHost()
	}
	if host == "" {
		host, _ = tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_phone_host")
	}
	if host == "" {
		// Started by a mad that kept only its command.
		cmd, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd")
		if m := originRe.FindStringSubmatch(cmd); m != nil {
			host = m[1]
		}
	}
	if host == "" {
		return nil
	}
	origin := host
	if via != ViaCloudflare {
		origin = Origin(host)
	}
	return respawn(p.ID, serveCommand(origin), host)
}

var originRe = regexp.MustCompile(`-origin '?([\w.-]+)(?::\d+)?`)

// On starts the server, unless it runs, and has the chosen way in (see
// Choose) serve it: tailscale, or a Cloudflare Tunnel.
func On(ctx context.Context) (Status, error) {
	if c := LoadChoice(); c.Via == ViaCloudflare {
		return onCloudflare(ctx, c)
	}
	h, err := host(ctx)
	if err != nil {
		return Status{Problem: err.Error()}, err
	}
	t, err := Token()
	if err != nil {
		return Status{}, err
	}
	if err := ensurePane(tmux.IDPhone, "phone", serveCommand(Origin(h)), h); err != nil {
		return Status{}, err
	}
	killPane(tmux.IDPhoneTunnel)
	out, err := exec.CommandContext(ctx, tailscale, "serve", "--bg", fmt.Sprintf("--https=%d", HTTPSPort),
		fmt.Sprintf("http://127.0.0.1:%d", Port)).CombinedOutput()
	if err != nil {
		return Status{}, fmt.Errorf("tailscale serve: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return Status{On: true, Via: ViaTailscale, Host: h, URL: pageURL(h, t)}, nil
}

// Off stops tailscale serving it, the tunnel, and the server.
func Off(ctx context.Context) error {
	if _, err := exec.LookPath(tailscale); err == nil {
		_ = exec.CommandContext(ctx, tailscale, "serve", fmt.Sprintf("--https=%d", HTTPSPort), "off").Run()
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	for _, id := range []string{tmux.IDPhoneTunnel, tmux.IDPhone} {
		if p, ok := tmux.FindPane(panes, id); ok {
			if err := tmux.Run("kill-pane", "-t", p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// killPane ends the pool pane tagged id, if there is one.
func killPane(id string) {
	if panes, err := tmux.ListPanes(); err == nil {
		if p, ok := tmux.FindPane(panes, id); ok {
			_ = tmux.Run("kill-pane", "-t", p.ID)
		}
	}
}

// ensurePane runs cmd, for host, in the pool pane tagged id, restarting
// it when it ran something else (another mad, another name).
func ensurePane(id, name, cmd, host string) error {
	panes, err := tmux.ListPanes()
	if err != nil || !tmux.HasSession(tmux.PoolSession) {
		return errors.New("the deck is not running: open it first (mad)")
	}
	if p, ok := tmux.FindPane(panes, id); ok {
		if had, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd"); had == cmd && !p.Dead {
			return nil
		}
		return respawn(p.ID, cmd, host)
	}
	pid, err := tmux.Out("new-window", "-d", "-t", tmux.PoolSession+":", "-n", name, "-P", "-F", "#{pane_id}", cmd)
	if err != nil {
		return err
	}
	if err := note(pid, cmd, host); err != nil {
		return err
	}
	return tmux.Tag(pid, id)
}

func respawn(id, cmd, host string) error {
	if err := tmux.Run("respawn-pane", "-k", "-t", id, cmd); err != nil {
		return err
	}
	return note(id, cmd, host)
}

// note keeps on the pane what it runs, and for which name.
func note(id, cmd, host string) error {
	if err := tmux.Run("set-option", "-p", "-t", id, "@mad_cmd", cmd); err != nil {
		return err
	}
	return tmux.Run("set-option", "-p", "-t", id, "@mad_phone_host", host)
}

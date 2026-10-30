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
	Host string `json:"host,omitempty"` // this machine's name on the tailnet
	URL  string `json:"url,omitempty"`  // to open on the phone, token and all
	// Problem is why it can't be on: no Tailscale, HTTPS off.
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

// serveCommand keeps the server alive as the stream's is.
func serveCommand(host string) string {
	sub := fmt.Sprintf("desktop -serve -addr 127.0.0.1:%d -origin %s", Port, paths.ShellQuote(Origin(host)))
	return fmt.Sprintf("while :; do %s 2>>%s; sleep 1; done", deck.SelfCommand(sub), paths.ShellQuote(paths.PhoneLog()))
}

// On starts the server, unless it runs, and has tailscale serve it.
func On(ctx context.Context) (Status, error) {
	h, err := host(ctx)
	if err != nil {
		return Status{Problem: err.Error()}, err
	}
	t, err := Token()
	if err != nil {
		return Status{}, err
	}
	if err := ensurePane(serveCommand(h)); err != nil {
		return Status{}, err
	}
	out, err := exec.CommandContext(ctx, tailscale, "serve", "--bg", fmt.Sprintf("--https=%d", HTTPSPort),
		fmt.Sprintf("http://127.0.0.1:%d", Port)).CombinedOutput()
	if err != nil {
		return Status{}, fmt.Errorf("tailscale serve: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return Status{On: true, Host: h, URL: pageURL(h, t)}, nil
}

// Off stops tailscale serving it, and the server.
func Off(ctx context.Context) error {
	_ = exec.CommandContext(ctx, tailscale, "serve", fmt.Sprintf("--https=%d", HTTPSPort), "off").Run()
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	if p, ok := tmux.FindPane(panes, tmux.IDPhone); ok {
		return tmux.Run("kill-pane", "-t", p.ID)
	}
	return nil
}

// ensurePane runs cmd in the phone's pool pane, restarting it when it
// ran something else (another mad, another name).
func ensurePane(cmd string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	if p, ok := tmux.FindPane(panes, tmux.IDPhone); ok {
		if had, _ := tmux.Out("show-options", "-p", "-v", "-t", p.ID, "@mad_cmd"); had == cmd && !p.Dead {
			return nil
		}
		if err := tmux.Run("respawn-pane", "-k", "-t", p.ID, cmd); err != nil {
			return err
		}
		return tmux.Run("set-option", "-p", "-t", p.ID, "@mad_cmd", cmd)
	}
	id, err := tmux.Out("new-window", "-d", "-t", tmux.PoolSession+":", "-n", "phone", "-P", "-F", "#{pane_id}", cmd)
	if err != nil {
		return err
	}
	if err := tmux.Run("set-option", "-p", "-t", id, "@mad_cmd", cmd); err != nil {
		return err
	}
	return tmux.Tag(id, tmux.IDPhone)
}

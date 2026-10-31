package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/phone"
)

// phoneCmd puts the desktop window on the user's phone (package phone):
// on serves it and shows the QR code to scan, off stops it; with neither,
// how it stands. The way in is Tailscale or a Cloudflare Tunnel; on picks
// one with --tailscale or --cloudflare, and keeps it for the next time.
func phoneCmd(args []string, stdio IO) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const usage = "usage: mad phone [on [--tailscale | --cloudflare [--tunnel name --hostname host]] | off]"
	what := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		what, args = args[0], args[1:]
	}
	fs := flags("phone", stdio.Err)
	tailscale := fs.Bool("tailscale", false, "serve over Tailscale (the first time's way)")
	cloudflare := fs.Bool("cloudflare", false, "serve through a Cloudflare Tunnel; a quick one unless --tunnel")
	tunnel := fs.String("tunnel", "", "`name` of your own Cloudflare tunnel, made with cloudflared tunnel create")
	hostname := fs.String("hostname", "", "`host` the named tunnel is served at")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return errUsage
	}
	picked := *tailscale || *cloudflare || *tunnel != "" || *hostname != ""
	if what != "on" && picked {
		return fmt.Errorf("%s", usage)
	}
	var s phone.Status
	switch what {
	case "on":
		if picked {
			c := phone.Choice{Via: phone.ViaTailscale}
			if *cloudflare {
				c = phone.Choice{Via: phone.ViaCloudflare, Tunnel: *tunnel, Hostname: *hostname}
			}
			if *tailscale && (*cloudflare || *tunnel != "" || *hostname != "") || !*cloudflare && !*tailscale {
				return fmt.Errorf("%s", usage)
			}
			if err := phone.Choose(c); err != nil {
				return err
			}
		}
		var err error
		if s, err = phone.On(ctx); err != nil {
			return err
		}
	case "off":
		if err := phone.Off(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdio.Out, "mad: the phone's way in is closed")
		return nil
	case "":
		s = phone.Get(ctx)
	default:
		return fmt.Errorf("%s", usage)
	}
	if s.Problem != "" {
		return fmt.Errorf("%s", s.Problem)
	}
	if !s.On {
		fmt.Fprintln(stdio.Out, "mad: not served to the phone; mad phone on serves it")
		return nil
	}
	qr, err := phone.Terminal(s.URL)
	if err != nil {
		return err
	}
	fmt.Fprint(stdio.Out, qr)
	where := "the phone on your tailnet"
	if s.Via == phone.ViaCloudflare {
		where = "through a Cloudflare Tunnel, from anywhere"
	}
	fmt.Fprintf(stdio.Out, "Scan it with the phone's camera, %s, or open:\n%s\n", where, s.URL)
	return nil
}

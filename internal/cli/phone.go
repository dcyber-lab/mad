package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/dcyber-lab/mad/internal/phone"
)

// phoneCmd puts the desktop window on the user's phone (package phone):
// on serves it and shows the QR code to scan, off stops it; with neither,
// how it stands.
func phoneCmd(args []string, stdio IO) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	what := ""
	if len(args) == 1 {
		what = args[0]
	} else if len(args) > 1 {
		return fmt.Errorf("usage: mad phone [on|off]")
	}
	var s phone.Status
	switch what {
	case "on":
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
		return fmt.Errorf("usage: mad phone [on|off]")
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
	fmt.Fprintf(stdio.Out, "Scan it with the phone's camera, the phone on your tailnet, or open:\n%s\n", s.URL)
	return nil
}

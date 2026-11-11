package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A pairing code that is refused (used, or expired) is said when the page loads, in the page's own words: it
// used to be said only after the person's next action, the page showing nothing but that it was offline.
// Where there is no Chrome, it is skipped.
func TestARefusedPairingCodeIsSaidWhenThePageLoads(t *testing.T) {
	bin := chrome()
	if bin == "" {
		t.Skip("no Chrome: the page's pairing error is not checked (MAD_CHROME names one)")
	}
	h, err := newHandler("pairing", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := serve(t, h)
	c := startChrome(t, bin)
	if _, err := c.call("Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	// The fixture takes the codes of all a and all c; any other is refused, as a used one is.
	if _, err := c.call("Page.navigate", map[string]any{"url": srv.URL + "/#pair=" + strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	var said string
	ok := waitFor(15*time.Second, func() bool {
		v, err := c.eval(`[...document.querySelectorAll("#toasts .toast.err")].map((e) => e.textContent).join("\n")`)
		if err != nil || json.Unmarshal(v, &said) != nil {
			return false
		}
		return strings.Contains(said, "配对码")
	})
	if !ok {
		t.Fatalf("the refused code was not said: toasts %q", said)
	}
	// The code is out of the address, so a reload does not try it again.
	v, err := c.eval(`location.hash`)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	if json.Unmarshal(v, &hash) != nil || strings.Contains(hash, "pair=") {
		t.Errorf("the refused code stays in the address: %q", hash)
	}
}

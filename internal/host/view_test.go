package host

import (
	"context"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/live"
)

// wholeEnd is the end of a message as the pane had it before it wrapped
// only the end: all the text joined, trimmed and wrapped, the last n
// lines of that.
func wholeEnd(blocks []live.Block, width, n int) []string {
	var text strings.Builder
	for _, b := range blocks {
		if b.Kind == "text" {
			text.WriteString(b.Text + "\n")
		}
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(text.String()), "\n") {
		out = append(out, strings.Split(ansi.Wrap(l, width, ""), "\n")...)
	}
	return out[len(out)-min(n, len(out)):]
}

// The pane shows the same end of a message as it did wrapping it all.
func TestMessageEnd(t *testing.T) {
	text := func(s ...string) []live.Block {
		var bs []live.Block
		for _, t := range s {
			bs = append(bs, live.Block{Kind: "text", Text: t})
		}
		return bs
	}
	cases := [][]live.Block{
		nil,
		text(""),
		text("   ", "\n\n"),
		text("hello"),
		text("  hello  \n\n"),
		text("\n\n  first\nsecond\n  "),
		text("a\nb\nc"),
		text("a", "  ", "b"), // a block of space between stays
		text("one\r\ntwo\r\n"),
		text("\u00a0\u2028 x \u3000\u0085"),
		{{Kind: "text", Text: "said"}, {Kind: "tool", Text: "{\"command\": \"ls\"}"}, {Kind: "thinking", Text: "hm\n"}, {Kind: "text", Text: "\nthen"}},
		text("\x1b[1mbold, and long enough that it wraps where the pane ends\x1b[0m and goes on\n\x1b[33mwarn\x1b[0m"),
		text(strings.Repeat("世界 Ünïcödé 👍🏽 ", 300)),
		text(longReply(64 << 10)),
		text(longReply(10<<10), longReply(10<<10)),
		text(strings.Repeat("word ", 5000)),
		text(strings.Repeat("x", 5000) + "\n" + strings.Repeat("y", 50)),
	}
	// And messages put together at random from such pieces.
	pieces := []string{"word", " ", "  ", "\n", "\n\n", "\t", "世界", "Ünï", "👍🏽", "\x1b[1m", "\x1b[0m", "\u00a0", "\r\n",
		"a-hyphenated-word", strings.Repeat("long", 30), ".", "\u2028"}
	kinds := []string{"text", "text", "text", "tool", "thinking"}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 300; i++ {
		var bs []live.Block
		for range 1 + r.IntN(4) {
			var s strings.Builder
			for range r.IntN(60) {
				s.WriteString(pieces[r.IntN(len(pieces))])
			}
			bs = append(bs, live.Block{Kind: kinds[r.IntN(len(kinds))], Text: s.String()})
		}
		cases = append(cases, bs)
	}
	for i, bs := range cases {
		for _, width := range []int{10, 37, 96} {
			for _, n := range []int{0, 1, 3, 34, 100000} {
				if got, want := messageEnd(bs, width, n), wholeEnd(bs, width, n); !slices.Equal(got, want) {
					t.Fatalf("case %d, width %d, %d lines:\ngot  %q\nwant %q", i, width, n, got, want)
				}
			}
		}
	}
}

// Redraws asked for faster than the pane is drawn come together, and the
// last one asked for is never lost.
func TestPaced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan struct{}, 1)
	ask := func() {
		select {
		case in <- struct{}{}:
		default:
		}
	}
	const every = 20 * time.Millisecond
	out := paced(ctx, in, every)
	wait := func(what string) {
		t.Helper()
		select {
		case <-out:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no redraw", what)
		}
	}
	ask()
	wait("the first")
	ask() // too soon: it waits its turn
	wait("one asked for too soon")

	// Asked for every millisecond: one each every, and one that was
	// waiting already.
	got, start := 0, time.Now()
	for time.Since(start) < 10*every {
		ask()
		select {
		case <-out:
			got++
		default:
		}
		time.Sleep(time.Millisecond)
	}
	if took := time.Since(start); got > int(took/every)+2 {
		t.Errorf("%d redraws in %v, %v apart", got, took, every)
	}
}

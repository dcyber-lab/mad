package host

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/live"
)

// longReply is n bytes of text as a model writes it: lines of words, a
// paragraph now and then, a few runes wider than a byte.
func longReply(n int) string {
	const para = "The host keeps the agent's state and the windows looking at it; " +
		"each change is sent to them, and the pane is drawn again.\n" +
		"Ünïcödé and 世界 take more than a byte, and a long line wraps " +
		"where the pane ends, word by word, as ansi.Wrap does it, so the end shows.\n\n"
	return strings.Repeat(para, n/len(para)+1)[:n]
}

// claudeLines are what a headless claude prints for a reply of text,
// written in pieces of size bytes: its init, then the reply's events.
func claudeLines(text string, size int) [][]byte {
	line := func(ev map[string]any) []byte {
		b, _ := json.Marshal(map[string]any{"type": "stream_event", "event": ev})
		return b
	}
	out := [][]byte{
		[]byte(`{"type":"system","subtype":"init","session_id":"s1","model":"opus"}`),
		line(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1"}}),
		line(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}),
	}
	for i := 0; i < len(text); {
		j := min(i+size, len(text))
		for j < len(text) && text[j]&0xc0 == 0x80 {
			j++ // a rune is not cut
		}
		out = append(out, line(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text[i:j]}}))
		i = j
	}
	return append(out,
		line(map[string]any{"type": "content_block_stop", "index": 0}),
		line(map[string]any{"type": "message_stop"}))
}

// BenchmarkStreamUnwatched is a 130 KB reply streamed in 32 byte pieces
// to a host no window looks at: the updates alone, and with the pane
// drawn after every piece (as the pane was while it kept up).
//
//	go test -run '^$' -bench StreamUnwatched -benchmem ./internal/host
func BenchmarkStreamUnwatched(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	lines := claudeLines(longReply(130<<10), 32)
	stream := func(b *testing.B, draw bool) {
		b.ReportAllocs()
		for b.Loop() {
			h := newHost("agent-1")
			c := newClaude(h, "claude", "", "")
			for _, l := range lines {
				c.line(l)
				if draw {
					render(h.State(), "✻ claude", "", 100, 40)
				}
			}
		}
	}
	b.Run("update", func(b *testing.B) { stream(b, false) })
	b.Run("draw", func(b *testing.B) { stream(b, true) })
}

// BenchmarkRenderLong draws the pane once for a 130 KB reply.
func BenchmarkRenderLong(b *testing.B) {
	s := State{Status: Running, Model: "opus", Message: &live.Message{ID: "m", Blocks: []live.Block{{Kind: "text", Text: longReply(130 << 10)}}}}
	b.ReportAllocs()
	for b.Loop() {
		render(s, "✻ claude", "", 100, 40)
	}
}

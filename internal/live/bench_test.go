package live

import (
	"encoding/json"
	"strings"
	"testing"
)

// toolCallEvents are the Messages API events of a reply that is one tool
// call, its input n bytes streamed in pieces of size bytes.
func toolCallEvents(n, size int) [][]byte {
	ev := func(v map[string]any) []byte {
		b, _ := json.Marshal(v)
		return b
	}
	input := `{"file_path": "/tmp/x.go", "content": "` + strings.Repeat("func f() { return 42 } // a line of code\\n", n/42+1)[:n-40] + `"}`
	out := [][]byte{
		ev(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1"}}),
		ev(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t1", "name": "Write"}}),
	}
	for i := 0; i < len(input); i += size {
		piece := input[i:min(i+size, len(input))]
		out = append(out, ev(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": piece}}))
	}
	return append(out,
		ev(map[string]any{"type": "content_block_stop", "index": 0}),
		ev(map[string]any{"type": "message_stop"}))
}

// BenchmarkLongToolCall is a 300 KB tool call (a file written, say)
// streamed into the hub in 32 byte pieces, as mad stream takes it.
//
//	go test -run '^$' -bench LongToolCall -benchmem ./internal/live
func BenchmarkLongToolCall(b *testing.B) {
	events := toolCallEvents(300<<10, 32)
	b.ReportAllocs()
	for b.Loop() {
		s := NewStream(NewHub(), "a", "s")
		for _, e := range events {
			s.Event(e)
		}
	}
}

package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
)

// benchTranscript writes a claude transcript of about mb megabytes: a tool
// result after every two tool calls, as a long session has, every line
// logged at.
func benchTranscript(b *testing.B, mb int, at time.Time) string {
	b.Helper()
	p := filepath.Join(b.TempDir(), "s.jsonl")
	f, err := os.Create(p)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	big := strings.Repeat("x", 3000)
	ts := at.UTC().Format(time.RFC3339Nano)
	for i, n := 0, 0; n < mb<<20; i++ {
		var l string
		if i%3 == 0 {
			l = fmt.Sprintf(`{"parentUuid":"p","type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":"%s"}]},"uuid":"u%d","timestamp":"%s"}`+"\n", i, big, i, ts)
		} else {
			l = fmt.Sprintf(`{"parentUuid":"p","message":{"id":"msg_%d","model":"claude-opus-4-5","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...","description":"%s"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":20000,"cache_creation_input_tokens":300,"output_tokens":400}},"type":"assistant","uuid":"u%d","timestamp":"%s"}`+"\n", i, big[:800], i, ts)
		}
		if _, err := f.WriteString(l); err != nil {
			b.Fatal(err)
		}
		n += len(l)
	}
	return p
}

// BenchmarkReaderCold is the first read of a 20 MB transcript.
//
//	go test -run '^$' -bench Cold -benchmem ./internal/transcript
func BenchmarkReaderCold(b *testing.B) {
	p := benchTranscript(b, 20, time.Now())
	b.SetBytes(20 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := &file{}
		f.update(p, discover.Lookup("claude"))
	}
}

// BenchmarkDayCold is the day's first read of a 20 MB transcript resumed
// today, every line of it from an earlier day.
func BenchmarkDayCold(b *testing.B) {
	p := benchTranscript(b, 20, time.Now().AddDate(0, 0, -3))
	b.SetBytes(20 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := NewDay()
		d.written = func(pv discover.Provider, _ time.Time) []string {
			if pv.Kind() == "claude" {
				return []string{p}
			}
			return nil
		}
		d.Read(time.Now())
	}
}

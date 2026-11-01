package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

// BenchmarkBusLook is one look at a bus of 30 channels with 500 messages
// each: when a post moved one file (the look again, the one channel's new
// line parsed) and when none did (the stat).
//
//	go test -run '^$' -bench BusLook -benchtime 10x ./internal/ui
func BenchmarkBusLook(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: idArch, Kind: "claude"}}
	for i := 0; i < 30; i++ {
		n := fmt.Sprintf("c%d", i)
		if _, err := bus.Create(n, "", []string{idArch}, meFrom); err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 500; j++ {
			if err := bus.PostSystem(n, fmt.Sprintf("line %d of the report with some words to fill it out a little", j)); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("moved", func(b *testing.B) {
		loadBus(st, map[string]string{}, time.Now(), "")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			if err := bus.PostSystem("c0", "one more line"); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			loadBus(st, map[string]string{}, time.Now(), "")
		}
	})
	b.Run("unmoved", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bus.Signature()
		}
	})
}

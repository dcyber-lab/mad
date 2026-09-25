package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Benchmarks for what runs on every poll (500ms) and every frame, and for
// switching agents, at deck sizes well past everyday use:
//
//	go test -run '^$' -bench . -benchtime 20x ./internal/ui

var benchSizes = []int{10, 50, 200}

// benchModel builds a deck of n agents over n/5 projects, with a mix of
// statuses and a screenful of text per agent.
func benchModel(b *testing.B, n int) (*model, pollMsg) {
	b.Helper()
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	st := &state.State{}
	msg := pollMsg{screens: map[string]string{}, hooks: map[string]*status.Hook{}}
	screen := strings.Repeat(strings.Repeat("x", 120)+"\n", 40)
	for i := 0; i < n; i++ {
		if i%5 == 0 {
			st.AddProject(fmt.Sprintf("/code/project-%03d", i/5))
		}
		p := st.Projects[len(st.Projects)-1]
		id := fmt.Sprintf("agent-%03d", i)
		p.Agents = append(p.Agents, &state.Agent{ID: id, Kind: "claude"})
		msg.panes = append(msg.panes, tmux.Pane{ID: fmt.Sprintf("%%%d", i+10), MadID: id, Session: tmux.PoolSession, Index: -1})
		msg.screens[id] = fmt.Sprintf("%d\n%s", i, screen)
		msg.hooks[id] = &status.Hook{State: []string{status.Running, status.Idle, status.Waiting}[i%3], At: time.Now()}
	}
	m := newModel(st, agent.Builtin())
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 60})
	m.applyPoll(msg, time.Now())
	return m, msg
}

func BenchmarkApplyPoll(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			m, msg := benchModel(b, n)
			now := time.Now()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				now = now.Add(pollInterval)
				m.applyPoll(msg, now)
			}
		})
	}
}

func BenchmarkView(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			m, _ := benchModel(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.frame++
				_ = m.View()
			}
		})
	}
}

func BenchmarkJumpNext(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			m, _ := benchModel(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.jumpNext()
			}
		})
	}
}

// benchServer starts a throwaway tmux server shaped like the deck: main
// with sidebar and stage, and n agent windows in the pool that keep
// printing, like busy agents do.
func benchServer(b *testing.B, n int) []string {
	b.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		b.Skip("tmux not installed")
	}
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("mad-bench-%d", time.Now().UnixNano())
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	b.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket = old
	})
	must := func(out string, err error) string {
		if err != nil {
			b.Fatal(err)
		}
		return out
	}
	side := must(tmux.Out("new-session", "-d", "-s", tmux.MainSession, "-x", "200", "-y", "50", "-P", "-F", "#{pane_id}", "sleep 600"))
	stage := must(tmux.Out("split-window", "-d", "-h", "-t", side, "-P", "-F", "#{pane_id}", "sleep 600"))
	must("", tmux.Tag(side, tmux.IDSidebar))
	must("", tmux.Tag(stage, tmux.IDPlaceholder))
	must(tmux.Out("new-session", "-d", "-s", tmux.PoolSession, "-x", "160", "-y", "50", "sleep 600"))
	var ids []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("agent-%03d", i)
		pane := must(tmux.Out("new-window", "-d", "-t", tmux.PoolSession+":", "-P", "-F", "#{pane_id}",
			"while :; do date +%s%N; seq 1 40; sleep 0.2; done"))
		must("", tmux.Tag(pane, id))
		ids = append(ids, id)
	}
	time.Sleep(300 * time.Millisecond) // let them draw
	return ids
}

// BenchmarkPoll is one full status poll against a live tmux server: list
// panes, capture every agent's screen, read hooks, check focus.
func BenchmarkPoll(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			ids := benchServer(b, n)
			st := &state.State{}
			st.AddProject("/code/p")
			for _, id := range ids {
				st.Projects[0].Agents = append(st.Projects[0].Agents, &state.Agent{ID: id, Kind: "claude"})
			}
			m := newModel(st, agent.Builtin())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				msg := m.pollCmd()().(pollMsg)
				if msg.err != nil || len(msg.screens) != n {
					b.Fatalf("poll: err=%v screens=%d", msg.err, len(msg.screens))
				}
			}
		})
	}
}

// BenchmarkSwitch is the tmux side of opening an agent: swap it into the
// stage and focus it, alternating between two agents.
func BenchmarkSwitch(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			ids := benchServer(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := deck.ShowPane(ids[i%2*(n-1)], true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

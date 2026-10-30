package bus

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// A Source reads the bus again only when its files or the statuses moved,
// or FullEvery passed.
func TestSourceReadsOnlyOnChange(t *testing.T) {
	setup(t)
	st, _ := state.Load()
	at := time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC)
	mustCreate(t, "ops", idArch)
	if _, err := SetTask(arch, TaskChange{Title: sp("Ship the release"), Owner: sp(idArch), Channel: sp("ops")}); err != nil {
		t.Fatal(err)
	}
	var s Source
	if l, read := s.Look(st, nil, at); !read || l.Cards != nil || len(l.Channels) != 1 {
		t.Fatalf("first look: read %v, %+v", read, l)
	}
	idle := map[string]string{idArch: status.Idle}
	l, read := s.Look(st, idle, at)
	if !read || len(l.Cards) != 1 || l.Cards[0].Column == ColWaitingYou {
		t.Fatalf("with statuses: read %v, %+v", read, l.Cards)
	}
	if _, read := s.Look(st, map[string]string{idArch: status.Idle}, at.Add(time.Second)); read {
		t.Error("read again with nothing moved")
	}
	// Its owner waits: the task waits on the person.
	l, read = s.Look(st, map[string]string{idArch: status.Waiting}, at.Add(2*time.Second))
	if !read || l.Cards[0].Column != ColWaitingYou || l.Cards[0].Why != WhyPermission {
		t.Errorf("status moved: read %v, %+v", read, l.Cards)
	}
	if _, err := Post("ops", arch, "@you shall I ship it tonight or wait?", 0); err != nil {
		t.Fatal(err)
	}
	l, read = s.Look(st, map[string]string{idArch: status.Waiting}, at.Add(3*time.Second))
	if !read || l.Yours["ops"].Unread != 1 || !l.Yours["ops"].Mention {
		t.Errorf("posted: read %v, yours %+v", read, l.Yours)
	}
	if _, read := s.Look(st, map[string]string{idArch: status.Waiting}, at.Add(3*time.Second+FullEvery)); !read {
		t.Error("not read again after FullEvery")
	}
}

// BenchmarkWindowPollBus is the bus part of one window poll, on a bus of
// 30 channels with 500 messages each and 30 tasks: as the poller did it
// (Board, Channels and YourChannels every poll) and through a Source
// when nothing moved.
//
//	go test -run '^$' -bench WindowPollBus -benchtime 20x ./internal/bus
func BenchmarkWindowPollBus(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: idArch, Kind: "claude", Name: "architect"}, {ID: idBuild, Kind: "claude", Name: "builder"}}
	if err := st.Save(); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		n := fmt.Sprintf("c%d", i)
		if _, err := Create(n, "", []string{idArch, idBuild}, me); err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 500; j++ {
			if err := PostSystem(n, fmt.Sprintf("line %d of the report with some words to fill it out a little", j)); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := SetTask(arch, TaskChange{Title: sp("task " + n), Owner: sp(idBuild), Channel: sp(n)}); err != nil {
			b.Fatal(err)
		}
	}
	statuses := map[string]string{idArch: status.Running, idBuild: status.Idle}
	at := time.Now()
	b.Run("derive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Board(st, statuses, at); err != nil {
				b.Fatal(err)
			}
			_, _ = Channels()
			_, _ = YourChannels()
		}
	})
	b.Run("cached", func(b *testing.B) {
		var s Source
		s.Look(st, statuses, at)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, read := s.Look(st, statuses, at); read {
				b.Fatal("read with nothing moved")
			}
		}
	})
}

func TestReply(t *testing.T) {
	setup(t)
	mustCreate(t, "ops", idArch)
	if _, err := Reply("ops", "go ahead with the deploy", 0); err != nil {
		t.Fatal(err)
	}
	dm := DMChannel(idArch, You)
	if _, err := Reply(dm, "the schema looks right to me", 0); err != nil { // made with the first
		t.Fatal(err)
	}
	if msgs, _, _ := Read(dm, 0, 10); len(msgs) != 1 || msgs[0].From.ID != You {
		t.Errorf("your DM = %+v", msgs)
	}
	if _, err := Reply(DMChannel(idArch, idBuild), "not mine to write in", 0); !errors.Is(err, ErrTheirDM) {
		t.Errorf("their DM: %v", err)
	}
}

func TestSpendPart(t *testing.T) {
	turns := spendSetup(t, 10, 100)
	if s := SpendNow(now()); s.Part != 0 || s.Hot {
		t.Fatalf("nothing spent: %+v", s)
	}
	mustCreate(t, "api", idBuild)
	spendTurn(t, turns, idBuild, 0, 8.5)
	if s := SpendNow(now()); s.Part != 0.85 || !s.Hot || s.Closed {
		t.Errorf("near: %+v", s)
	}
}

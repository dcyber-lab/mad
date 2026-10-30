package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

const (
	idArch  = "aaaa1111-0000-0000-0000-000000000001"
	idBuild = "bbbb2222-0000-0000-0000-000000000002"
)

var (
	meFrom   = bus.From{ID: bus.You, Label: bus.You}
	archFrom = bus.From{ID: idArch, Label: "architect", Kind: "claude"}
)

// busDeck is a project with two agents, and the bus around them.
func busDeck(t *testing.T) (*model, *state.State) {
	t.Helper()
	m, st := setup(t, "/p/one")
	st.Projects[0].Agents = []*state.Agent{{ID: idArch, Kind: "claude", Name: "architect"}, {ID: idBuild, Kind: "codex", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	m.rebuildRows()
	return m, st
}

// look reads the bus into the block, as a tick does.
func look(m *model, st *state.State) {
	m.setBus(loadBus(st, map[string]string{}, time.Now(), ""))
}

func busNames(m *model) []string {
	var out []string
	for _, r := range m.rows {
		if r.bus == nil {
			break
		}
		l, rt := m.busRowSegs(r.bus)
		var parts []string
		for _, x := range append(l, rt...) {
			parts = append(parts, strings.TrimSpace(x.s))
		}
		out = append(out, strings.Join(strings.Fields(strings.Join(parts, " ")), " "))
	}
	return out
}

func mkChannel(t *testing.T, name string) {
	t.Helper()
	if _, err := bus.Create(name, "", []string{idArch}, meFrom); err != nil {
		t.Fatal(err)
	}
}

func say(t *testing.T, ch, text string) {
	t.Helper()
	if _, err := bus.Post(ch, archFrom, text, 0); err != nil {
		t.Fatal(err)
	}
}

func TestBusBlockHiddenWhenQuiet(t *testing.T) {
	m, st := busDeck(t)
	look(m, st)
	if len(busNames(m)) != 0 {
		t.Errorf("an empty bus shows %q", busNames(m))
	}
	// A channel with nothing unread stays out as well.
	mkChannel(t, "ops")
	look(m, st)
	if len(busNames(m)) != 0 {
		t.Errorf("a quiet channel shows %q", busNames(m))
	}
	assertRows(t, m, "one", "  claude", "  codex")
}

func TestBusBlockChannelsWithNews(t *testing.T) {
	m, st := busDeck(t)
	for _, n := range []string{"ops", "dev", "quiet"} {
		mkChannel(t, n)
	}
	say(t, "ops", "the deploy went well, take one")
	say(t, "ops", "the deploy went well, take two")
	say(t, "dev", "reviewing the patch for the api")
	say(t, "quiet", "nothing much to say here today")
	if err := bus.MarkRead(bus.You, "quiet", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(meFrom, idBuild, "please look at the build"); err != nil { // you posted: nothing new for you
		t.Fatal(err)
	}
	look(m, st)
	if got, want := strings.Join(busNames(m), "|"), "#dev 1|#ops 2"; got != want {
		t.Errorf("block = %q, want %q", got, want)
	}

	// Paused shows even when read; the person's DM with news comes after the channels.
	if err := bus.MarkRead(bus.You, "ops", 2); err != nil {
		t.Fatal(err)
	}
	if err := bus.MarkRead(bus.You, "dev", 1); err != nil {
		t.Fatal(err)
	}
	say(t, "quiet", "a second thing I should tell you")
	if _, _, err := bus.DM(archFrom, bus.You, "can you decide about the schema please"); err != nil {
		t.Fatal(err)
	}
	pauseChannel(t, "ops")
	_, last, _ := bus.Read("ops", 0, 1)
	if err := bus.MarkRead(bus.You, "ops", last); err != nil {
		t.Fatal(err)
	}
	look(m, st)
	names := busNames(m)
	// The board has the paused channel for you; unread channels, DMs, then the paused.
	if len(names) != 4 || !strings.HasPrefix(names[0], "▤ Board") || names[1] != "#quiet 1" || names[2] != "@architect 1" || names[3] != "#ops paused" {
		t.Errorf("block = %q", names)
	}
}

// pauseChannel posts as the architect until the channel pauses.
func pauseChannel(t *testing.T, ch string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		say(t, ch, fmt.Sprintf("another long update number %d on the work", i))
		if c, _, _ := bus.Find(ch); c.Paused {
			return
		}
	}
	t.Fatal("never paused")
}

func TestBusBlockMoreAndOrder(t *testing.T) {
	m, st := busDeck(t)
	for i := 0; i < 9; i++ {
		n := fmt.Sprintf("c%d", i)
		mkChannel(t, n)
		say(t, n, "something to read in this channel")
	}
	look(m, st)
	names := busNames(m)
	if len(names) != busRowsMax+1 || names[0] != "#c0 1" || names[busRowsMax] != "+3 more" {
		t.Errorf("block = %q", names)
	}
}

func TestBusBlockBoardRow(t *testing.T) {
	m, _ := busDeck(t)
	rows := busSnap{waiting: 2, open: 5}.busRows()
	if len(rows) != 1 || !rows[0].board {
		t.Fatalf("rows = %+v", rows)
	}
	l, r := m.busRowSegs(&rows[0])
	if l[len(l)-1].s != "Board" || r[0].s != "2 for you" {
		t.Errorf("board row = %+v %+v", l, r)
	}
	// Nothing waiting: the open count; nothing open: no row.
	rows = busSnap{open: 3}.busRows()
	if _, r = m.busRowSegs(&rows[0]); r[0].s != "3 open" {
		t.Errorf("open row = %+v", r)
	}
	if rows = (busSnap{}).busRows(); len(rows) != 0 {
		t.Errorf("empty bus has rows %+v", rows)
	}
}

func TestBusBlockBoardCountsWaitingYou(t *testing.T) {
	m, st := busDeck(t)
	mkChannel(t, "ops")
	say(t, "ops", "@you should the release go out today or wait")
	look(m, st)
	if got := strings.Join(busNames(m), "|"); got != "▤ Board 1 for you|#ops 1" {
		t.Errorf("block = %q", got)
	}
}

func TestBusRowsKeysAndOpen(t *testing.T) {
	m, st := busDeck(t)
	mkChannel(t, "ops")
	say(t, "ops", "the deploy went out without trouble")
	look(m, st)
	if len(m.rows) == 0 || m.rows[0].bus == nil || m.rows[0].bus.ch != "ops" {
		t.Fatalf("rows[0] = %+v", m.rows)
	}
	m.cursor = 0
	// Keys about projects and agents do nothing on a bus row; moving works.
	press(m, "n", "w", "x", "v", "t", "1", "z", "f", "o")
	if m.mode != modeNormal || m.cursor != 0 {
		t.Errorf("mode %v cursor %d", m.mode, m.cursor)
	}
	press(m, "j")
	if r, _ := m.current(); !r.isProject() {
		t.Errorf("after j: %+v", r)
	}
	press(m, "k")
	if cmd := m.keyNormal(key("enter")); cmd == nil {
		t.Error("enter on a channel opens its chat")
	}
	if m.taskTag != "chat:ops" || m.taskFor != "" {
		t.Errorf("taskTag = %q taskFor %q", m.taskTag, m.taskFor)
	}
	if v := m.View(); !strings.Contains(v, "#ops") {
		t.Errorf("view lacks the channel:\n%s", v)
	}
}

// The channel whose chat is open stays in the block though its chat has
// read it all: its row is the way to close it.
func TestBusBlockKeepsTheOpenChat(t *testing.T) {
	m, st := busDeck(t)
	mkChannel(t, "ops")
	m.setBus(loadBus(st, map[string]string{}, time.Now(), "ops"))
	if got := busNames(m); len(got) != 1 || !strings.HasPrefix(got[0], "#ops") {
		t.Errorf("the open chat's channel: %q", got)
	}
	look(m, st)
	if len(busNames(m)) != 0 {
		t.Errorf("closed, a quiet channel shows %q", busNames(m))
	}
}

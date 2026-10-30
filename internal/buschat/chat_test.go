package buschat

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

const (
	idArch  = "aaaa1111-0000-0000-0000-000000000001"
	idBuild = "bbbb2222-0000-0000-0000-000000000002"
)

var (
	me    = bus.From{ID: bus.You, Label: bus.You}
	arch  = bus.From{ID: idArch, Label: "architect", Kind: "claude"}
	build = bus.From{ID: idBuild, Label: "builder", Kind: "codex"}
)

func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: idArch, Kind: "claude", Name: "architect"}, {ID: idBuild, Kind: "codex", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
}

func open(t *testing.T, ch string, w, h int) *Model {
	t.Helper()
	m, err := New(ch)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func typeText(m *Model, s string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func press(m *Model, t tea.KeyType) tea.Cmd {
	_, cmd := m.Update(tea.KeyMsg{Type: t})
	return cmd
}

func TestRenderMessages(t *testing.T) {
	setup(t)
	if _, err := bus.Create("dev", "the api work", []string{idArch, idBuild}, me); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Post("dev", arch, "I split the work in two parts, @builder takes the second part of it", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Post("dev", me, "good, go on", 0); err != nil {
		t.Fatal(err)
	}
	if err := bus.PostSystem("dev", "progress: 1 of 2 done"); err != nil {
		t.Fatal(err)
	}
	m := open(t, "dev", 40, 14)
	v := m.View()
	for _, want := range []string{"#dev", "the api work", "architect", "#1", "@builder takes", "you", "#2", "· progress: 1 of 2 done", "› "} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	for i, l := range strings.Split(v, "\n") {
		if w := len([]rune(l)); w > 44 { // runes, not cells: a margin for the box drawing
			t.Errorf("line %d is %d wide: %q", i, w, l)
		}
	}
	if got := len(strings.Split(v, "\n")); got != 14 {
		t.Errorf("view is %d lines, want 14", got)
	}
	// The person has read it.
	if y, _ := bus.YourChannels(); y["dev"].Unread != 0 {
		t.Errorf("unread after showing: %+v", y["dev"])
	}
}

func TestSendPostsAsYou(t *testing.T) {
	setup(t)
	if _, err := bus.Create("dev", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	m := open(t, "dev", 60, 12)
	typeText(m, "hello team")
	press(m, tea.KeyEnter)
	msgs, _, _ := bus.Read("dev", 0, 10)
	if len(msgs) != 1 || msgs[0].From.ID != bus.You || msgs[0].Text != "hello team" {
		t.Fatalf("posted %+v", msgs)
	}
	if m.input.Value() != "" || !strings.Contains(m.View(), "hello team") || !strings.Contains(m.status, "sent #1") {
		t.Errorf("after send: input %q status %q\n%s", m.input.Value(), m.status, m.View())
	}
	// Polling picks up what others say.
	if _, err := bus.Post("dev", arch, "thanks, working on that now", 0); err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	if !strings.Contains(m.View(), "working on that now") {
		t.Errorf("a new message is not shown:\n%s", m.View())
	}
	if y, _ := bus.YourChannels(); y["dev"].Unread != 0 {
		t.Errorf("unread: %+v", y["dev"])
	}
}

func TestSendErrorShowsInStatus(t *testing.T) {
	setup(t)
	if _, err := bus.Create("dev", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	m := open(t, "dev", 60, 12)
	m.channel = "gone" // the channel went away under it
	typeText(m, "is anyone there")
	press(m, tea.KeyEnter)
	if !m.isErr || !strings.Contains(m.status, "no channel") || m.input.Value() == "" {
		t.Errorf("status %q err %v input %q", m.status, m.isErr, m.input.Value())
	}
	if !strings.Contains(m.View(), "no channel") {
		t.Errorf("status not in view:\n%s", m.View())
	}
}

func TestTabCompletesAgentLabels(t *testing.T) {
	setup(t)
	if _, err := bus.Create("dev", "", []string{idArch, idBuild}, me); err != nil {
		t.Fatal(err)
	}
	m := open(t, "dev", 60, 12)
	typeText(m, "hey @bu")
	press(m, tea.KeyTab)
	if got := m.input.Value(); got != "hey @builder " {
		t.Errorf("input = %q", got)
	}
	m.tabbing = false
	m.input.SetValue("@")
	press(m, tea.KeyTab)
	first := m.input.Value()
	press(m, tea.KeyTab)
	if second := m.input.Value(); first == second || !strings.HasPrefix(second, "@") {
		t.Errorf("tab twice: %q then %q", first, second)
	}
}

func TestReadOnlyDMBetweenAgents(t *testing.T) {
	setup(t)
	ch, _, err := bus.DM(arch, idBuild, "can you take the database part of this")
	if err != nil {
		t.Fatal(err)
	}
	m := open(t, ch, 60, 12)
	v := m.View()
	if !m.readOnly || !strings.Contains(v, "architect ↔ builder") || !strings.Contains(v, "read only") || strings.Contains(v, "› ") {
		t.Errorf("view:\n%s", v)
	}
	typeText(m, "hello")
	press(m, tea.KeyEnter)
	if msgs, _, _ := bus.Read(ch, 0, 10); len(msgs) != 1 {
		t.Errorf("posted into a DM of others: %+v", msgs)
	}
	if cmd := press(m, tea.KeyEsc); cmd == nil {
		t.Error("esc closes")
	}
}

func TestDMWithYouIsWritable(t *testing.T) {
	setup(t)
	ch, _, err := bus.DM(arch, bus.You, "which of the two schemas do you prefer")
	if err != nil {
		t.Fatal(err)
	}
	m := open(t, ch, 60, 12)
	if m.readOnly || !strings.Contains(m.View(), "@architect") {
		t.Errorf("view:\n%s", m.View())
	}
	typeText(m, "the second one please")
	press(m, tea.KeyEnter)
	if msgs, _, _ := bus.Read(ch, 0, 10); len(msgs) != 2 || msgs[1].From.ID != bus.You {
		t.Errorf("messages: %+v", msgs)
	}
}

func TestScrollAndClose(t *testing.T) {
	setup(t)
	if _, err := bus.Create("dev", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := bus.Post("dev", me, strings.Repeat("word ", 3)+string(rune('a'+i%26)), 0); err != nil {
			t.Fatal(err)
		}
	}
	m := open(t, "dev", 50, 12)
	bottom := m.View()
	press(m, tea.KeyPgUp)
	if m.scroll == 0 || m.View() == bottom || !strings.Contains(m.View(), "scrolled up") {
		t.Errorf("pgup did not scroll:\n%s", m.View())
	}
	press(m, tea.KeyPgDown)
	if m.scroll != 0 {
		t.Errorf("scroll = %d", m.scroll)
	}
	// Esc clears the input first; q is a letter while typing.
	typeText(m, "quick note")
	if cmd := press(m, tea.KeyEsc); cmd != nil || m.input.Value() != "" {
		t.Errorf("esc with text: cmd %v input %q", cmd != nil, m.input.Value())
	}
	if cmd := press(m, tea.KeyEsc); cmd == nil {
		t.Error("esc on an empty input closes")
	}
}

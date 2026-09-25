package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

// withHome points HOME and XDG_CONFIG_HOME at a temp dir and returns it.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", "")
	return home
}

// written stands in for a provider's lookup of written sessions.
type written map[string]bool

func (w written) exists(sid string) bool { return w[sid] }

func TestClaudeCommand(t *testing.T) {
	home := withHome(t)
	claude := ByName(Builtin(), "claude")
	a := &state.Agent{ID: "id-1", Kind: "claude"}
	w := written{}

	start := claude.Command(a, false, w.exists)
	if !strings.HasPrefix(start, "claude --settings ") || !strings.HasSuffix(start, "--session-id id-1") {
		t.Errorf("start = %q", start)
	}
	if !strings.Contains(start, filepath.Join(home, ".config", "mad", "claude-settings.json")) {
		t.Errorf("start doesn't load mad's hook settings: %q", start)
	}

	// No transcript yet: --resume would fail, so resume starts fresh.
	if got := claude.Command(a, true, w.exists); got != start {
		t.Errorf("resume without transcript = %q, want start", got)
	}

	w["id-1"] = true
	if got := claude.Command(a, true, w.exists); !strings.HasSuffix(got, "--resume id-1") {
		t.Errorf("resume = %q", got)
	}

	// After /clear the session id differs from the agent id.
	a.SessionID = "sid-2"
	w["sid-2"] = true
	if got := claude.Command(a, true, w.exists); !strings.HasSuffix(got, "--resume sid-2") {
		t.Errorf("resume by session id = %q", got)
	}
}

func TestClaudeFork(t *testing.T) {
	withHome(t)
	claude := ByName(Builtin(), "claude")
	a := &state.Agent{ID: "id-1", Kind: "claude", SessionID: "sid-1", Fork: true}
	w := written{"sid-1": true}

	if got := claude.Command(a, true, w.exists); !strings.HasSuffix(got, "--resume sid-1 --fork-session") {
		t.Errorf("fork resume = %q", got)
	}
	if got := claude.Command(a, false, w.exists); strings.Contains(got, "--fork-session") {
		t.Errorf("fork must only apply to resume, got %q", got)
	}
}

func TestCodexCommand(t *testing.T) {
	withHome(t)
	codex := ByName(Builtin(), "codex")
	a := &state.Agent{ID: "id-2", Kind: "codex"}

	start := codex.Command(a, false, nil)
	if !strings.HasPrefix(start, "codex -c 'notify=[") || !strings.Contains(start, `"hook","codex"]`) {
		t.Errorf("start should wire notify to mad: %q", start)
	}
	// codex names its own threads: whether one was written doesn't matter.
	none := written{}.exists
	if got := codex.Command(a, true, none); !strings.HasSuffix(got, "--last") {
		t.Errorf("resume without id should use --last, got %q", got)
	}
	a.SessionID = "thread-9"
	if got := codex.Command(a, true, none); !strings.HasPrefix(got, "codex resume ") || !strings.HasSuffix(got, " thread-9") {
		t.Errorf("resume by id = %q", got)
	}
}

func TestCodexKeepsUsersNotify(t *testing.T) {
	home := withHome(t)
	codex := ByName(Builtin(), "codex")
	a := &state.Agent{ID: "id-3", Kind: "codex"}
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		config string
		wired  bool
	}{
		{"", true},
		{"model = \"o3\"\n# notify = [\"old\"]\n", true},
		{"notify = [\"terminal-notifier\", \"-title\", \"codex\"]\n", false},
		{"model = \"o3\"\n\n[profiles.work]\n  notify = [\"say\"]\n", false},
	}
	for _, c := range cases {
		write(filepath.Join(home, ".codex"), c.config)
		if got := strings.Contains(codex.Command(a, false, nil), "notify="); got != c.wired {
			t.Errorf("config %q: mad's notify wired = %v, want %v", c.config, got, c.wired)
		}
	}
	if got := codex.Command(a, false, nil); got != "codex " {
		t.Errorf("start without mad's notify = %q", got)
	}

	// CODEX_HOME moves codex's config.
	other := t.TempDir()
	t.Setenv("CODEX_HOME", other)
	if !strings.Contains(codex.Command(a, false, nil), "notify=") {
		t.Error("~/.codex should not count once CODEX_HOME is set")
	}
	write(other, "notify = [\"x\"]\n")
	if strings.Contains(codex.Command(a, false, nil), "notify=") {
		t.Error("notify in $CODEX_HOME/config.toml was overridden")
	}
}

func TestPiAndShell(t *testing.T) {
	withHome(t)
	a := &state.Agent{ID: "id-3"}
	pi := ByName(Builtin(), "pi")
	if got := pi.Command(a, true, nil); got != "pi --session-id id-3" {
		t.Errorf("pi resume = %q (pi resumes via the same id)", got)
	}
	if got := ByName(Builtin(), "shell").Command(a, false, nil); !strings.Contains(got, "${SHELL") {
		t.Errorf("shell = %q", got)
	}
}

// A kind from agents.json that names its sessions has no provider to say
// which were written, so it resumes the one it knows of, if any.
func TestNamedKindWithoutProvider(t *testing.T) {
	k := Kind{Name: "gemini", Start: "gemini --session {id}", Resume: "gemini --resume {sid}", Fork: "--fork"}
	a := &state.Agent{ID: "id-5"}
	if got := k.Command(a, true, nil); got != "gemini --session id-5" {
		t.Errorf("no session known = %q", got)
	}
	a.SessionID, a.Fork = "s-5", true
	if got := k.Command(a, true, nil); got != "gemini --resume s-5 --fork" {
		t.Errorf("resume = %q", got)
	}
}

func TestUnknownKindRunsItsName(t *testing.T) {
	k := ByName(Builtin(), "aider")
	if got := k.Command(&state.Agent{ID: "x"}, true, nil); got != "aider" {
		t.Errorf("got %q", got)
	}
}

func TestLoadMergesAgentsJSON(t *testing.T) {
	home := withHome(t)
	cfg := filepath.Join(home, ".config", "mad", "agents.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	json := `[
		{"name": "claude", "start": "claude --model opus", "waiting": ["Proceed\\?"]},
		{"name": "gemini", "start": "gemini", "waiting": ["Allow execution", "(bad regex"]},
		{"start": "nameless is dropped"}
	]`
	if err := os.WriteFile(cfg, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}

	kinds := Load()
	var names []string
	for _, k := range kinds {
		names = append(names, k.Name)
	}
	if strings.Join(names, ",") != "claude,codex,pi,shell,gemini" {
		t.Fatalf("kinds = %v", names)
	}
	claude := ByName(kinds, "claude")
	if claude.Start != "claude --model opus" || claude.Hooks {
		t.Errorf("override should replace the whole kind: %+v", claude)
	}
	if !claude.ScreenWaiting("Proceed?") {
		t.Error("override waiting pattern not compiled")
	}
	gemini := ByName(kinds, "gemini")
	if !gemini.ScreenWaiting("Allow execution of ls") {
		t.Error("gemini waiting pattern not compiled")
	}
}

func TestLoadIgnoresBrokenConfig(t *testing.T) {
	home := withHome(t)
	cfg := filepath.Join(home, ".config", "mad", "agents.json")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o755)
	_ = os.WriteFile(cfg, []byte("not json"), 0o644)
	if got := len(Load()); got != len(Builtin()) {
		t.Errorf("broken config: %d kinds, want built-ins only", got)
	}
}

func TestScreenWaiting(t *testing.T) {
	kinds := Builtin()
	cases := []struct {
		kind, screen string
		want         bool
	}{
		{"claude", "Do you want to make this edit?\n❯ 1. Yes", true},
		{"claude", "Yes, I trust this folder", true},
		{"claude", "> ", false},
		{"codex", "Would you like to run the following command?", true},
		{"codex", "Working (3s)", false},
		{"pi", "anything", false},
	}
	for _, c := range cases {
		if got := ByName(kinds, c.kind).ScreenWaiting(c.screen); got != c.want {
			t.Errorf("%s ScreenWaiting(%q) = %v", c.kind, c.screen, got)
		}
	}
}

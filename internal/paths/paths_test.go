package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirsFollowXDG(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("MAD_SOCKET", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	if got, want := ConfigDir(), "/home/u/.config/mad"; got != want {
		t.Errorf("ConfigDir = %q, want %q", got, want)
	}
	if got, want := StateDir(), "/home/u/.local/state/mad"; got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "/st")
	for got, want := range map[string]string{
		TmuxConf():         "/cfg/mad/tmux.conf",
		AgentsConfig():     "/cfg/mad/agents.json",
		StateFile():        "/st/mad/state.json",
		StatusDir():        "/st/mad/status",
		SidebarWidthFile(): "/st/mad/sidebar_width",
		SidebarLog():       "/st/mad/sidebar.log",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// A deck on a tmux server of its own keeps its state and generated files
// apart, and shares the user's config.
func TestOtherDeck(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "/st")
	for _, s := range []string{"", "mad"} {
		t.Setenv("MAD_SOCKET", s)
		if OtherDeck() != "" || StateDir() != "/st/mad" || GenDir() != "/cfg/mad" {
			t.Errorf("MAD_SOCKET=%q: deck %q, state %q, generated %q", s, OtherDeck(), StateDir(), GenDir())
		}
	}
	t.Setenv("MAD_SOCKET", "mad-pre")
	for got, want := range map[string]string{
		OtherDeck():    "mad-pre",
		StateFile():    "/st/mad/decks/mad-pre/state.json",
		TmuxConf():     "/st/mad/decks/mad-pre/tmux.conf",
		ConfigFile():   "/cfg/mad/config.json",
		AgentsConfig(): "/cfg/mad/agents.json",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestExpandAndShort(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	cases := map[string]string{
		"~":         "/home/u",
		"~/code/x":  "/home/u/code/x",
		" /abs/p ":  "/abs/p",
		"/a/../b/.": "/b",
	}
	for in, want := range cases {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Expand("rel"); !filepath.IsAbs(got) {
		t.Errorf("Expand(rel) = %q, want absolute", got)
	}

	shorts := map[string]string{
		"/home/u":       "~",
		"/home/u/code":  "~/code",
		"/home/user2/x": "/home/user2/x", // prefix of a different user
		"/etc":          "/etc",
	}
	for in, want := range shorts {
		if got := Short(in); got != want {
			t.Errorf("Short(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":       "'plain'",
		"with space":  "'with space'",
		"it's":        `'it'\''s'`,
		"$HOME;rm -r": "'$HOME;rm -r'",
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
	// The quoted word must survive a real shell unchanged.
	in := `a'b "c" $d`
	out, err := exec.Command("sh", "-c", "printf %s "+ShellQuote(in)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("shell round trip = %q, want %q", out, in)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "f.txt")
	if err := WriteFileAtomic(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "two" {
		t.Fatalf("read back %q, %v", data, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestProjectRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if root, ok := ProjectRoot(dir); ok || root != dir {
		t.Errorf("outside git: got (%q, %v), want (%q, false)", root, ok, dir)
	}

	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root, ok := ProjectRoot(sub)
	want, _ := filepath.EvalSymlinks(dir) // macOS: /var → /private/var
	if !ok || root != want {
		t.Errorf("inside git: got (%q, %v), want (%q, true)", root, ok, want)
	}
}

func TestReadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	v := map[string]int{"kept": 1}
	if err := ReadJSON(path, &v); err != nil || v["kept"] != 1 {
		t.Errorf("missing file: %v %v", err, v)
	}
	if !ModTime(path).IsZero() {
		t.Error("missing file has a ModTime")
	}
	os.WriteFile(path, []byte("{\n  \"a\": 1,\n}\n"), 0o644)
	if err := ReadJSON(path, &v); err == nil || !strings.HasPrefix(err.Error(), "config.json:3: invalid character '}'") {
		t.Errorf("syntax error = %v", err)
	}
	os.WriteFile(path, []byte("{\"a\": 1,\n \"b\": \"x\"}"), 0o644)
	if err := ReadJSON(path, &v); err == nil || !strings.HasPrefix(err.Error(), "config.json:2: cannot unmarshal string") {
		t.Errorf("type error = %v", err)
	}
	if ModTime(path).IsZero() {
		t.Error("existing file has no ModTime")
	}
}

func TestWriteFileAtomicKeepsModeAndSymlink(t *testing.T) {
	for name, write := range map[string]func(string, []byte) error{"WriteFileAtomic": WriteFileAtomic, "WriteFileUnsynced": WriteFileUnsynced} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "real.json")
			if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "link.json")
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			if err := write(link, []byte("new")); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("the link was replaced: %v %v", fi, err)
			}
			fi, err := os.Stat(real)
			if err != nil || fi.Mode().Perm() != 0o644 {
				t.Fatalf("mode = %v, %v; want 0644", fi.Mode().Perm(), err)
			}
			if data, _ := os.ReadFile(real); string(data) != "new" {
				t.Fatalf("content = %q", data)
			}
		})
	}
}

// Only WriteFileAtomic waits for the disk: WriteFileUnsynced is what
// every hook writes an agent's status with.
func TestOnlyWriteFileAtomicSyncs(t *testing.T) {
	synced := 0
	orig := syncFile
	syncFile = func(f *os.File) error { synced++; return orig(f) }
	t.Cleanup(func() { syncFile = orig })
	path := filepath.Join(t.TempDir(), "f.json")
	if err := WriteFileUnsynced(path, []byte("a")); err != nil || synced != 0 {
		t.Fatalf("WriteFileUnsynced: %v, synced %d times", err, synced)
	}
	if err := WriteFileAtomic(path, []byte("b")); err != nil || synced != 1 {
		t.Fatalf("WriteFileAtomic: %v, synced %d times", err, synced)
	}
	if data, _ := os.ReadFile(path); string(data) != "b" {
		t.Fatalf("content = %q", data)
	}
}

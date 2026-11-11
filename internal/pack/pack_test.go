package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// home points mad's config at a temporary directory, and returns a
// project's root in it.
func home(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	return filepath.Join(dir, "proj")
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(list []Pack) string {
	var out []string
	for _, p := range list {
		out = append(out, p.Name)
	}
	return strings.Join(out, " ")
}

// mad comes with its packs: the plain one first, whose first flow and
// template are the defaults, and pstack with the skills its roles name.
func TestBuiltin(t *testing.T) {
	list := Builtin()
	if names(list) != "mad pstack" {
		t.Fatalf("built in = %s", names(list))
	}
	for _, p := range list {
		if p.Source != BuiltIn || p.Version == "" || p.Description == "" {
			t.Errorf("%s = %+v", p.Name, p)
		}
		if err := p.CheckLinks(); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
	}
	ps := list[1]
	if flows, err := ps.Files("flows"); err != nil || len(flows) == 0 || flows[0].Name != "flows/pstack-bugfix.json" {
		t.Errorf("pstack's flows = %v, %v", flows, err)
	}
	if a, ok := ps.Agent("strong"); !ok || a != "claude:opus@high" {
		t.Errorf("$strong = %q %v", a, ok)
	}
	if got := ps.Where("flows/x.json"); got != BuiltIn {
		t.Errorf("where = %q", got)
	}
	if got := ps.Label("flows/x.json"); got != "built-in pack pstack, flows/x.json" {
		t.Errorf("label = %q", got)
	}
	if !ps.HasSkill("bugfix") || ps.HasSkill("nothing") || ps.HasSkill("../pack.json") {
		t.Error("pstack's skills")
	}
	for ref, want := range map[string]string{"pstack:bugfix": "bugfix", "pstack:poteto-mode": "", "bugfix": "", "other:bugfix": ""} {
		p, name, ok := SkillOf(list, ref)
		if name != want || ok != (want != "") || ok && p.Name != "pstack" {
			t.Errorf("SkillOf(%s) = %s %q %v", ref, p.Name, name, ok)
		}
	}
}

// A pack in the user's or the project's directory joins mad's own; one
// of a built-in pack's name takes its place where it stood, linked ones
// too; a directory that is no pack is said, and left out.
func TestList(t *testing.T) {
	root := home(t)
	write(t, filepath.Join(UserDir(), "mine", "pack.json"), `{"name": "mine", "version": "3", "agents": {"main": "claude:sonnet"}}`)
	write(t, filepath.Join(UserDir(), "mine", "flows", "b.json"), `{}`)
	write(t, filepath.Join(UserDir(), "mine", "flows", "a.json"), `{"a": 1}`)
	write(t, filepath.Join(UserDir(), "mine", "flows", "notes.txt"), `not a flow`)
	write(t, filepath.Join(UserDir(), "broken", "pack.json"), `{"name": "Broken"}`)
	write(t, filepath.Join(UserDir(), "typo", "pack.json"), `{"name": "typo", "agent": {}}`)
	write(t, filepath.Join(UserDir(), "empty", "readme.md"), `no pack.json`)
	write(t, filepath.Join(UserDir(), "stray.json"), `{}`)
	// The project's pstack, kept elsewhere and linked.
	kept := filepath.Join(filepath.Dir(root), "checkout", "pstack")
	write(t, filepath.Join(kept, "pack.json"), `{"name": "pstack", "version": "9"}`)
	write(t, filepath.Join(kept, "skills", "bugfix", "SKILL.md"), "the project's way")
	if err := os.MkdirAll(ProjectDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kept, filepath.Join(ProjectDir(root), "pstack")); err != nil {
		t.Fatal(err)
	}

	list, err := List(root)
	if names(list) != "mad pstack mine" {
		t.Errorf("packs = %s", names(list))
	}
	for _, want := range []string{`broken: pack name "Broken"`, `typo: pack.json: unknown field "agent"`, "empty: no pack.json"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err lacks %q: %v", want, err)
		}
	}
	ps, _ := Find(list, "pstack")
	if ps.Version != "9" || ps.Replaces != "built in 0.1.0" || ps.Where("pack.json") != filepath.Join(ProjectDir(root), "pstack", "pack.json") {
		t.Errorf("the project's pstack = %+v", ps)
	}
	if got := ps.Label("flows/x.json"); got != "~/proj/.mad/packs/pstack/flows/x.json" {
		t.Errorf("label = %q", got)
	}
	if ps.String() != "pstack 9" || len(ps.Skills()) != 1 || ps.HasSkill("architect") {
		t.Errorf("its skills = %v", ps.Skills())
	}
	mine, _ := Find(list, "mine")
	files, err := mine.Files("flows")
	if err != nil || len(files) != 2 || files[0].Name != "flows/a.json" || string(files[0].Data) != `{"a": 1}` {
		t.Errorf("mine's flows = %v, %v", files, err)
	}
	if got := strings.Join(mine.AgentNames(), " "); got != "main" {
		t.Errorf("agent names = %q", got)
	}
	// Without a project: mad's own and the user's.
	if list, _ := List(""); names(list) != "mad pstack mine" || list[1].Version != "0.1.0" {
		t.Errorf("no project: %s", names(list))
	}
}

func TestReadChecks(t *testing.T) {
	dir := t.TempDir()
	for body, want := range map[string]string{
		`{"name": "p", "agents": {"Main": "claude"}}`: `agents: name "Main"`,
		`{"name": "p", "agents": {"main": "$other"}}`: `agents: main is "$other", which is no agent`,
		`{"name": "p", "agents": {"main": ""}}`:       `agents: main is "", which is no agent`,
		`not json`:                                    `pack.json: invalid character`,
	} {
		write(t, filepath.Join(dir, "pack.json"), body)
		if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(dir, "skills", "half", "notes.md"), "no SKILL.md here")
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "skills/half has no SKILL.md") {
		t.Errorf("a skill without its file: %v", err)
	}
}

// A run or a team keeps the skills of a pack as files beside it, and
// finds a role's skill among them.
func TestCopySkills(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(dir, "skills", "fix", "SKILL.md"), "how to fix; see `./more.md` and `../shared/rules.md`")
	write(t, filepath.Join(dir, "skills", "fix", "more.md"), "more")
	write(t, filepath.Join(dir, "skills", "shared", "SKILL.md"), "shared")
	write(t, filepath.Join(dir, "skills", "shared", "rules.md"), "rules")
	write(t, filepath.Join(dir, "flows", "f.json"), "{}")
	p, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckLinks(); err != nil {
		t.Errorf("links: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "run", "skills")
	if err := p.CopySkills(dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"p/fix/SKILL.md", "p/fix/more.md", "p/shared/rules.md"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("not copied: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "p", "flows")); err == nil {
		t.Error("the flows were copied with the skills")
	}
	for ref, want := range map[string]string{
		"p:fix": filepath.Join("p", "fix", "SKILL.md"), "p:nothing": "", "fix": "", "q:fix": "", "p:../x": "",
	} {
		if got := SkillFile(dst, ref); got != want {
			t.Errorf("SkillFile(%s) = %q, want %q", ref, got, want)
		}
	}
	// The whole pack, to change it.
	whole := filepath.Join(t.TempDir(), "p")
	if err := p.CopyTo(whole); err != nil {
		t.Fatal(err)
	}
	if again, err := Read(whole); err != nil || len(again.Skills()) != 2 {
		t.Errorf("the copy = %+v, %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(whole, "flows", "f.json")); err != nil {
		t.Error("the copy lacks its flows")
	}
	// A pack without skills copies nothing, and that is no error.
	bare := t.TempDir()
	write(t, filepath.Join(bare, "pack.json"), `{"name": "bare"}`)
	b, _ := Read(bare)
	if err := b.CopySkills(dst); err != nil || b.CheckLinks() != nil {
		t.Errorf("a pack without skills: %v", err)
	}
}

// A skill that sends its reader to a file that is not there is found by
// the check, not by a role in the middle of a run.
func TestCheckLinks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(dir, "skills", "fix", "SKILL.md"),
		"Read `./gone.md`, then [the rules](../shared/rules.md) and `../shared/SKILL.md`.\nRun `./repro.sh`, and see `../<name>/SKILL.md`.")
	write(t, filepath.Join(dir, "skills", "shared", "SKILL.md"), "shared")
	p, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = p.CheckLinks()
	if err == nil || !strings.Contains(err.Error(), "skills/fix/SKILL.md points at ./gone.md, which is not there") ||
		!strings.Contains(err.Error(), "points at ../shared/rules.md") {
		t.Errorf("links: %v", err)
	}
	if strings.Contains(err.Error(), "repro.sh") || strings.Contains(err.Error(), "shared/SKILL.md") || strings.Contains(err.Error(), "name") {
		t.Errorf("a path that is no file of the pack counted: %v", err)
	}
}

func TestEnclosing(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(dir, "p", "pack.json"), `{"name": "p", "agents": {"main": "codex"}}`)
	write(t, filepath.Join(dir, "p", "flows", "f.json"), "{}")
	write(t, filepath.Join(dir, "flows", "f.json"), "{}")
	if p, ok := Enclosing(filepath.Join(dir, "p", "flows", "f.json")); !ok || p.Name != "p" {
		t.Errorf("a pack's flow: %+v %v", p, ok)
	}
	if p, ok := Enclosing(filepath.Join(dir, "p", "teams", "t.json")); !ok || p.Agents["main"] != "codex" {
		t.Errorf("a pack's template: %+v %v", p, ok)
	}
	for _, file := range []string{filepath.Join(dir, "flows", "f.json"), filepath.Join(dir, "p", "pack.json"), filepath.Join(dir, "p", "skills", "x", "f.json")} {
		if _, ok := Enclosing(file); ok {
			t.Errorf("%s is in no pack's flows or teams", file)
		}
	}
}

// A skill that is a link, or has one in it, is the pack's as any other:
// it is found, listed, checked and copied, what the link points at with
// it.
func TestLinkedSkills(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(base, "pack")
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(base, "shared", "triage", "SKILL.md"), "triage; see `./more.md`")
	write(t, filepath.Join(base, "shared", "triage", "more.md"), "more")
	write(t, filepath.Join(base, "shared", "own.md"), "own")
	if err := os.MkdirAll(filepath.Join(dir, "skills", "own"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "shared", "triage"), filepath.Join(dir, "skills", "triage")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "shared", "own.md"), filepath.Join(dir, "skills", "own", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	p, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Skills(), " "); got != "own triage" {
		t.Errorf("skills = %q", got)
	}
	if err := p.CheckLinks(); err != nil {
		t.Errorf("links: %v", err)
	}
	dst := filepath.Join(base, "run", "skills")
	if err := p.CopySkills(dst); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{"p:triage": "triage; see `./more.md`", "p:own": "own"} {
		rel := SkillFile(dst, ref)
		if data, _ := os.ReadFile(filepath.Join(dst, rel)); rel == "" || string(data) != want {
			t.Errorf("the copy of %s = %q %q", ref, rel, data)
		}
	}
	if fi, err := os.Lstat(filepath.Join(dst, "p", "triage", "more.md")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("what the link points at was not copied as a file: %v", err)
	}
	// A link that leads back into its own directory ends, and says so.
	if err := os.Symlink(filepath.Join(dir, "skills"), filepath.Join(dir, "skills", "own", "again")); err != nil {
		t.Fatal(err)
	}
	if err := p.CopySkills(filepath.Join(base, "loop")); err == nil || !strings.Contains(err.Error(), "too deep") {
		t.Errorf("a link into itself: %v", err)
	}
}

// One file of a pack that can't be read is said and left out; the rest
// of the pack's files are still there.
func TestFilesSkipsWhatItCantRead(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(dir, "flows", "a.json"), `{"a": 1}`)
	write(t, filepath.Join(dir, "flows", "z.json"), `{"z": 1}`)
	if err := os.Symlink(filepath.Join(dir, "nowhere.json"), filepath.Join(dir, "flows", "old.json")); err != nil {
		t.Fatal(err)
	}
	p, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Files("flows")
	if len(files) != 2 || files[0].Name != "flows/a.json" || files[1].Name != "flows/z.json" {
		t.Errorf("files = %v", files)
	}
	if err == nil || !strings.Contains(err.Error(), "flows/old.json") {
		t.Errorf("err = %v, want the file named", err)
	}
}

// A copy keeps what its files may do: a script stays one. A file of a
// built-in pack, which nobody may write, becomes the owner's to change.
func TestCopyKeepsModes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pack.json"), `{"name": "p"}`)
	write(t, filepath.Join(dir, "skills", "x", "SKILL.md"), "x")
	script := filepath.Join(dir, "skills", "x", "check.sh")
	write(t, script, "#!/bin/sh\necho ok\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := p.CopySkills(dst); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "p", "x", "check.sh")); err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("the script's copy can't be run: %v %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "p", "x", "SKILL.md")); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("a file's copy: %v %v", fi.Mode(), err)
	}
	whole := filepath.Join(t.TempDir(), "pstack")
	ps, _ := Find(Builtin(), "pstack")
	if err := ps.CopyTo(whole); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(whole, "pack.json")); err != nil || fi.Mode().Perm()&0o600 != 0o600 {
		t.Errorf("a built-in pack's copy is not the owner's to change: %v %v", fi.Mode(), err)
	}
}

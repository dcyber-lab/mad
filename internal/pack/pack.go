// Package pack reads packs. A pack holds a way of working as files: the
// flows runs follow, the templates teams start from, and the skills their
// roles work by. Packs are kept apart from the code that runs them, so a
// way of working changes without mad changing: mad's own are directories
// in /packs, built into the binary; a user's are directories in
// ~/.config/mad/packs/ and a project's in .mad/packs/, one named as an
// earlier one taking its place whole.
//
//	pack.json         {"name", "version", "description", "agents"}
//	flows/*.json      run flows (package run)
//	teams/*.json      team templates (package team)
//	skills/<name>/    SKILL.md, and the files it refers to
//
// A role names a pack's skill as "pack:name". Runs and teams copy the
// skills they work by when they start, and go on by the copy whatever
// becomes of the pack.
package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/packs"
)

// BuiltIn is the Source of a pack mad comes with.
const BuiltIn = "built in"

// Pack is a way of working, read from a directory.
type Pack struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	// Agents names agents for the pack's roles to share: a role whose
	// agent is "$strong" is played by Agents["strong"], so who plays what
	// across the pack is said in one place.
	Agents map[string]string `json:"agents,omitempty"`

	// Source is where it was read: BuiltIn, or its directory.
	Source string `json:"-"`
	// Replaces is the pack it took the place of: "built in 0.1.0".
	Replaces string `json:"-"`
	fsys     fs.FS
}

// File is a file of a pack.
type File struct {
	Name string // its path in the pack: flows/review.json
	Data []byte
}

// String is the pack's name and version: "pstack 0.1.0".
func (p Pack) String() string {
	if p.Version == "" {
		return p.Name
	}
	return p.Name + " " + p.Version
}

// Where is where file name of the pack is: BuiltIn, or its path.
func (p Pack) Where(name string) string {
	if p.Source == BuiltIn {
		return BuiltIn
	}
	return filepath.Join(p.Source, filepath.FromSlash(name))
}

// Label is what a message calls file name of the pack: its path, or for
// a pack mad comes with the pack and the file.
func (p Pack) Label(name string) string {
	if p.Source == BuiltIn {
		return "built-in pack " + p.Name + ", " + name
	}
	return paths.Short(p.Where(name))
}

// UserDir is where a user keeps packs of their own.
func UserDir() string { return filepath.Join(paths.ConfigDir(), "packs") }

// ProjectDir is where a project keeps its packs.
func ProjectDir(root string) string { return filepath.Join(root, ".mad", "packs") }

var nameRe = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`) })

// Read reads the pack in directory dir.
func Read(dir string) (Pack, error) {
	return read(os.DirFS(dir), dir)
}

func read(fsys fs.FS, source string) (Pack, error) {
	at := paths.Short(source)
	data, err := fs.ReadFile(fsys, "pack.json")
	if err != nil {
		return Pack{}, fmt.Errorf("%s: no pack.json", at)
	}
	var p Pack
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Pack{}, fmt.Errorf("%s: pack.json: %v", at, strings.TrimPrefix(err.Error(), "json: "))
	}
	p.Source, p.fsys = source, fsys
	if err := p.check(); err != nil {
		return Pack{}, fmt.Errorf("%s: %w", at, err)
	}
	return p, nil
}

// check says what is wrong with the pack itself, or nil: its flows and
// templates are checked by those who read them.
func (p Pack) check() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !nameRe().MatchString(p.Name) {
		bad("pack name %q: lowercase letters, digits and dashes", p.Name)
	}
	for name, agent := range p.Agents {
		switch {
		case !nameRe().MatchString(name):
			bad("agents: name %q: lowercase letters, digits and dashes", name)
		case agent == "" || strings.HasPrefix(agent, "$"):
			bad("agents: %s is %q, which is no agent (claude:opus@high, codex, …)", name, agent)
		}
	}
	ents, _ := fs.ReadDir(p.fsys, "skills")
	for _, e := range ents {
		if e.IsDir() && !p.HasSkill(e.Name()) {
			bad("skills/%s has no SKILL.md", e.Name())
		}
	}
	return errors.Join(errs...)
}

var builtin = sync.OnceValue(func() []Pack {
	var out []Pack
	for _, name := range packs.Names {
		sub, err := fs.Sub(packs.FS, name)
		if err != nil {
			panic(err)
		}
		p, err := read(sub, BuiltIn)
		if err != nil || p.Name != name {
			panic(fmt.Sprintf("built-in pack %s: %v", name, err))
		}
		out = append(out, p)
	}
	return out
})

// Builtin are the packs mad comes with.
func Builtin() []Pack { return append([]Pack(nil), builtin()...) }

// List are the packs a project at root works with ("" for no project):
// mad's own, the user's, the project's. One of the same name as an earlier
// one takes its place, where the earlier one stood. A directory that is
// no pack is left out, and said so in the error.
func List(root string) ([]Pack, error) {
	list := Builtin()
	dirs := []string{UserDir()}
	if root != "" {
		dirs = append(dirs, ProjectDir(root))
	}
	var errs []error
	for _, dir := range dirs {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			sub := filepath.Join(dir, e.Name())
			// Stat, not the entry: a pack may be a link to a checkout.
			if fi, err := os.Stat(sub); err != nil || !fi.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p, err := Read(sub)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			replaced := false
			for i, old := range list {
				if old.Name == p.Name {
					p.Replaces = strings.TrimSpace(paths.Short(old.Source) + " " + old.Version)
					list[i], replaced = p, true
				}
			}
			if !replaced {
				list = append(list, p)
			}
		}
	}
	return list, errors.Join(errs...)
}

// Find finds the pack called name in list.
func Find(list []Pack, name string) (Pack, bool) {
	for _, p := range list {
		if p.Name == name {
			return p, true
		}
	}
	return Pack{}, false
}

// Enclosing is the pack a flow or template file is in: the directory
// above its flows/ or teams/, when that holds a pack.json.
func Enclosing(file string) (Pack, bool) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return Pack{}, false
	}
	in := filepath.Dir(abs)
	if b := filepath.Base(in); b != "flows" && b != "teams" {
		return Pack{}, false
	}
	p, err := Read(filepath.Dir(in))
	return p, err == nil
}

// Files are the pack's JSON files in dir ("flows", "teams"), by name.
func (p Pack) Files(dir string) ([]File, error) {
	names, err := fs.Glob(p.fsys, dir+"/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []File
	for _, name := range names {
		data, err := fs.ReadFile(p.fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Name: name, Data: data})
	}
	return out, nil
}

// Agent is the agent the pack names name.
func (p Pack) Agent(name string) (string, bool) {
	a, ok := p.Agents[name]
	return a, ok
}

// AgentNames are the names the pack gives agents, sorted.
func (p Pack) AgentNames() []string {
	var out []string
	for name := range p.Agents {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HasSkill reports whether the pack has a skill called name.
func (p Pack) HasSkill(name string) bool {
	if !nameRe().MatchString(name) {
		return false
	}
	fi, err := fs.Stat(p.fsys, path.Join("skills", name, "SKILL.md"))
	return err == nil && !fi.IsDir()
}

// Skills are the names of the pack's skills, sorted.
func (p Pack) Skills() []string {
	ents, _ := fs.ReadDir(p.fsys, "skills")
	var out []string
	for _, e := range ents {
		if e.IsDir() && p.HasSkill(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// linkRe finds a path a skill's file points its reader at: one that
// starts with ./ or ../ and ends in .md, in backticks or as a link.
var linkRe = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("(?:`|\\]\\()(\\.{1,2}/[A-Za-z0-9_./-]+\\.md)")
})

// CheckLinks says which files of the pack's skills point at a file that
// is not there, or nil. A skill's reader is sent to the files beside it
// (`./red-flags.md`, `../principles/fix-root-causes.md`): one that is
// missing is found when a role goes looking, mid-run, unless here.
func (p Pack) CheckLinks() error {
	if _, err := fs.Stat(p.fsys, "skills"); err != nil {
		return nil
	}
	var errs []error
	err := fs.WalkDir(p.fsys, "skills", func(name string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || path.Ext(name) != ".md" {
			return err
		}
		data, err := fs.ReadFile(p.fsys, name)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, m := range linkRe().FindAllSubmatch(data, -1) {
			to := path.Join(path.Dir(name), string(m[1]))
			if seen[to] {
				continue
			}
			seen[to] = true
			if fi, err := fs.Stat(p.fsys, to); err != nil || fi.IsDir() {
				errs = append(errs, fmt.Errorf("%s points at %s, which is not there", name, m[1]))
			}
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}

// SkillOf finds the skill a role's ref names among list's packs, and its
// pack: "pstack:bugfix" is pack pstack's skill bugfix. A ref without a
// pack's name, or of a pack or a skill that is not there, is none: the
// role's agent may still have a skill of that name itself.
func SkillOf(list []Pack, ref string) (Pack, string, bool) {
	in, name, ok := strings.Cut(ref, ":")
	if !ok {
		return Pack{}, "", false
	}
	p, ok := Find(list, in)
	if !ok || !p.HasSkill(name) {
		return Pack{}, "", false
	}
	return p, name, true
}

// SkillFile is where the copy of the skill ref names is in dir, a
// directory CopySkills filled per pack: dir/<pack>/<name>/SKILL.md, as a
// path below dir; "" when there is none.
func SkillFile(dir, ref string) string {
	in, name, ok := strings.Cut(ref, ":")
	if !ok || !nameRe().MatchString(in) || !nameRe().MatchString(name) {
		return ""
	}
	rel := filepath.Join(in, name, "SKILL.md")
	if fi, err := os.Stat(filepath.Join(dir, rel)); err != nil || fi.IsDir() {
		return ""
	}
	return rel
}

// CopySkills copies the pack's skills into dst/<pack name>/, for a run or
// a team to keep.
func (p Pack) CopySkills(dst string) error {
	return copyTree(p.fsys, "skills", filepath.Join(dst, p.Name))
}

// CopyTo copies the whole pack into directory dst.
func (p Pack) CopyTo(dst string) error {
	return copyTree(p.fsys, ".", dst)
}

// copyTree copies the files below from in fsys to dst; nothing when from
// is not there.
func copyTree(fsys fs.FS, from, dst string) error {
	if _, err := fs.Stat(fsys, from); err != nil {
		return nil
	}
	return fs.WalkDir(fsys, from, func(name string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(filepath.FromSlash(from), filepath.FromSlash(name))
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		return paths.WriteFileAtomic(filepath.Join(dst, rel), data)
	})
}

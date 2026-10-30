// Package skill is how agents hand skills to the person: an agent saves
// one as pending (the MCP tool skill_save); the person reads, edits and
// approves it; mad then keeps it in a store of its own and copies it
// where claude, codex and pi look for skills.
//
// The stores are <project>/.mad/skills/<name>/ for a project's skills and
// <config>/skills/<name>/ for the person's own. A copy mad made has a
// .mad-skill.json beside it; a skill dir without one is not mad's and is
// not touched unless asked for.
package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/skills"
)

// Scopes of a skill.
const (
	Project = "project"
	User    = "user"
)

// MaxDescription is the longest description, in characters.
const MaxDescription = 400

// Sidecar names the file that marks a skill dir as mad's copy.
const Sidecar = ".mad-skill.json"

// Pending is a saved skill waiting for the person.
type Pending struct {
	Name        string    `json:"name"` // its slug
	Description string    `json:"description"`
	Body        string    `json:"body"`
	By          string    `json:"by"`
	Project     string    `json:"project,omitempty"` // path of the project it was saved in
	Scope       string    `json:"scope"`
	At          time.Time `json:"at"`
	Replace     bool      `json:"replace,omitempty"`
}

// Skill is an approved skill.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Scope       string `json:"scope"`
	Project     string `json:"project,omitempty"`
}

// Slug is name as a skill's directory name: lower case letters, digits
// and dashes.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	return s
}

func pendingRoot() string { return filepath.Join(paths.StateDir(), "skills", "pending") }

// UserStore is where the person's own approved skills are kept.
func UserStore() string { return filepath.Join(paths.ConfigDir(), "skills") }

// ProjectStore is where project's approved skills are kept.
func ProjectStore(project string) string { return filepath.Join(project, ".mad", "skills") }

func reserved(slug string) bool {
	for _, n := range skills.Names {
		if n == slug {
			return true
		}
	}
	return false
}

// Save queues a skill for the person's approval, by agent `by`, from
// project (a path; "" for none). scope is "project" (the default) or
// "user". replace allows the approved copies to overwrite skill dirs
// that are not mad's.
func Save(by, project, scope, name, description, body string, replace bool) (Pending, error) {
	slug := Slug(name)
	description = strings.Join(strings.Fields(description), " ")
	body = strings.TrimSpace(body)
	switch {
	case slug == "":
		return Pending{}, errors.New("a skill needs a name (letters, digits and dashes)")
	case reserved(slug):
		return Pending{}, fmt.Errorf("%q is one of mad's own skills; pick another name", slug)
	case description == "":
		return Pending{}, errors.New("a skill needs a description: when to use it")
	case len([]rune(description)) > MaxDescription:
		return Pending{}, fmt.Errorf("the description is %d characters; keep it to %d", len([]rune(description)), MaxDescription)
	case body == "":
		return Pending{}, errors.New("a skill needs a body: the steps")
	}
	switch scope {
	case "":
		scope = Project
	case Project, User:
	default:
		return Pending{}, fmt.Errorf("scope is %q or %q, not %q", Project, User, scope)
	}
	if scope == Project && project == "" {
		return Pending{}, errors.New("this agent has no project: save it with scope \"user\"")
	}
	p := Pending{Name: slug, Description: description, Body: body, By: by, Project: project, Scope: scope, At: time.Now().UTC().Truncate(time.Second), Replace: replace}
	dir := filepath.Join(pendingRoot(), slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Pending{}, err
	}
	if err := paths.WriteFileAtomic(filepath.Join(dir, "SKILL.md"), render(slug, description, body)); err != nil {
		return Pending{}, err
	}
	meta, _ := json.MarshalIndent(map[string]any{"by": by, "project": project, "scope": scope, "at": p.At, "replace": replace}, "", "  ")
	if err := paths.WriteFileAtomic(filepath.Join(dir, "meta.json"), meta); err != nil {
		return Pending{}, err
	}
	return p, nil
}

// Path is where a pending skill's SKILL.md is.
func (p Pending) Path() string { return filepath.Join(pendingRoot(), p.Name, "SKILL.md") }

// PendingList lists the skills waiting for the person, oldest first.
func PendingList() ([]Pending, error) {
	ents, err := os.ReadDir(pendingRoot())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []Pending
	for _, e := range ents {
		if p, err := readPending(e.Name()); err == nil {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func readPending(slug string) (Pending, error) {
	if slug == "" || Slug(slug) != slug {
		return Pending{}, fmt.Errorf("no pending skill %q", slug)
	}
	dir := filepath.Join(pendingRoot(), slug)
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Pending{}, fmt.Errorf("no pending skill %q (mad skill pending)", slug)
	}
	var meta struct {
		By      string
		Project string
		Scope   string
		At      time.Time
		Replace bool
	}
	_ = paths.ReadJSON(filepath.Join(dir, "meta.json"), &meta)
	if meta.Scope == "" {
		meta.Scope = Project
	}
	desc, body := parse(string(raw))
	return Pending{Name: slug, Description: desc, Body: body, By: meta.By, Project: meta.Project, Scope: meta.Scope, At: meta.At, Replace: meta.Replace}, nil
}

// Discard drops a pending skill.
func Discard(slug string) error {
	if _, err := readPending(slug); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(pendingRoot(), slug))
}

// Approve puts a pending skill in its store and copies it where the
// agents look. edited, when given, is the whole SKILL.md as the person
// changed it. Nothing is written if a target is someone else's dir (and
// replace was not asked for).
func Approve(slug string, edited *string) error {
	p, err := readPending(slug)
	if err != nil {
		return err
	}
	if edited != nil {
		d, b := parse(*edited)
		d = strings.Join(strings.Fields(d), " ")
		switch {
		case d == "":
			return errors.New("the description is empty")
		case len([]rune(d)) > MaxDescription:
			return fmt.Errorf("the description is longer than %d characters", MaxDescription)
		case strings.TrimSpace(b) == "":
			return errors.New("the body is empty")
		}
		p.Description, p.Body = d, strings.TrimSpace(b)
	}
	store := UserStore()
	if p.Scope == Project {
		if p.Project == "" {
			return errors.New("the skill has no project")
		}
		store = ProjectStore(p.Project)
	}
	sk := Skill{Name: slug, Description: p.Description, Body: p.Body, Scope: p.Scope, Project: p.Project}
	if err := checkTargets(sk, p.Replace); err != nil {
		return err
	}
	dir := filepath.Join(store, slug)
	file := filepath.Join(dir, "SKILL.md")
	if old, err := os.ReadFile(file); err == nil {
		hist := filepath.Join(dir, "history")
		if err := os.MkdirAll(hist, 0o755); err != nil {
			return err
		}
		stamp := paths.ModTime(file).UTC().Format("20060102T150405Z")
		name := filepath.Join(hist, stamp+".md")
		for i := 1; exists(name); i++ {
			name = filepath.Join(hist, fmt.Sprintf("%s-%d.md", stamp, i))
		}
		if err := paths.WriteFileAtomic(name, old); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := paths.WriteFileAtomic(file, render(slug, p.Description, p.Body)); err != nil {
		return err
	}
	if err := install(sk, time.Now().UTC(), true); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(pendingRoot(), slug))
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// target is a place a skill is copied to.
type target struct {
	dir  string
	desc string
}

func targets(sk Skill) []target {
	home := paths.Home()
	desc := sk.Description
	var ts []target
	if sk.Scope == User {
		ts = append(ts, target{filepath.Join(home, ".claude", "skills", sk.Name), desc})
	} else {
		ts = append(ts, target{filepath.Join(sk.Project, ".claude", "skills", sk.Name), desc})
		desc = "(for project " + filepath.Base(sk.Project) + ") " + desc
	}
	return append(ts,
		target{filepath.Join(home, ".codex", "skills", sk.Name), desc},
		target{filepath.Join(home, ".pi", "agent", "skills", sk.Name), desc})
}

func checkTargets(sk Skill, replace bool) error {
	if replace {
		return nil
	}
	for _, t := range targets(sk) {
		if exists(t.dir) && !exists(filepath.Join(t.dir, Sidecar)) {
			return fmt.Errorf("%s exists and is not mad's; save the skill again with replace to overwrite it", paths.Short(t.dir))
		}
	}
	return nil
}

// install copies sk to its targets. A target dir without the sidecar is
// left alone unless force.
func install(sk Skill, version time.Time, force bool) error {
	side, _ := json.Marshal(map[string]any{"slug": sk.Name, "scope": sk.Scope, "project": sk.Project, "version": version.Format(time.RFC3339)})
	var errs []error
	for _, t := range targets(sk) {
		if exists(t.dir) && !exists(filepath.Join(t.dir, Sidecar)) && !force {
			continue
		}
		file := filepath.Join(t.dir, "SKILL.md")
		want := render(sk.Name, t.desc, sk.Body)
		if cur, err := os.ReadFile(file); err == nil && string(cur) == string(want) && exists(filepath.Join(t.dir, Sidecar)) {
			continue
		}
		if err := os.MkdirAll(t.dir, 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := paths.WriteFileAtomic(file, want); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := paths.WriteFileAtomic(filepath.Join(t.dir, Sidecar), side); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Sync re-copies every approved skill, from the user store and the
// stores of the projects in the deck, to the agents' dirs, leaving
// alone dirs that are not mad's.
func Sync() error {
	var projects []string
	if st, err := state.Load(); err == nil {
		for _, p := range st.Projects {
			projects = append(projects, p.Path)
		}
	}
	var errs []error
	for _, sk := range allApproved(projects) {
		store := UserStore()
		if sk.Scope == Project {
			store = ProjectStore(sk.Project)
		}
		version := paths.ModTime(filepath.Join(store, sk.Name, "SKILL.md"))
		if err := install(sk, version.UTC(), false); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// readStore lists the skills in a store.
func readStore(store, scope, project string) []Skill {
	ents, _ := os.ReadDir(store)
	var out []Skill
	for _, e := range ents {
		raw, err := os.ReadFile(filepath.Join(store, e.Name(), "SKILL.md"))
		if err != nil || Slug(e.Name()) != e.Name() {
			continue
		}
		d, b := parse(string(raw))
		out = append(out, Skill{Name: e.Name(), Description: d, Body: b, Scope: scope, Project: project})
	}
	return out
}

func allApproved(projects []string) []Skill {
	out := readStore(UserStore(), User, "")
	seen := map[string]bool{}
	for _, p := range projects {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, readStore(ProjectStore(p), Project, p)...)
	}
	return out
}

// List is the approved skills available in project: its own, then the
// person's. project may be "" for the person's only.
func List(project string) []Skill {
	var out []Skill
	if project != "" {
		out = readStore(ProjectStore(project), Project, project)
	}
	out = append(out, readStore(UserStore(), User, "")...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope == Project
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Edit is a SKILL.md as the window shows it for editing.
func Edit(name, description, body string) string {
	return string(render(name, description, body))
}

// render is a SKILL.md.
func render(name, description, body string) []byte {
	return []byte("---\nname: " + name + "\ndescription: " + strconv.Quote(description) + "\n---\n\n" + strings.TrimSpace(body) + "\n")
}

// parse splits a SKILL.md into its description and body.
func parse(s string) (description, body string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return "", strings.TrimSpace(s)
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", strings.TrimSpace(s)
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, "\n"))
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "description" {
			continue
		}
		v = strings.TrimSpace(v)
		if u, err := strconv.Unquote(v); err == nil && strings.HasPrefix(v, `"`) {
			v = u
		} else if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
		return v, body
	}
	return "", body
}

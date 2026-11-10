package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dcyber-lab/mad/internal/paths"
)

// A role in a run or a team can work by a skill: its message tells the
// agent where the skill's SKILL.md is, and to read and follow it. A
// skill is named as claude names it, "name" or "plugin:name" for a
// plugin's; codex has the plugin's skills copied, by their own name.

var refRe = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^([a-z0-9][a-z0-9-]*:)?[a-z0-9][a-z0-9-]*$`)
})

// CheckRef says what is wrong with a skill's name in a template, or nil.
func CheckRef(ref string) error {
	if !refRe().MatchString(ref) {
		return fmt.Errorf("skill %q: a skill's name, or plugin:name for a claude plugin's", ref)
	}
	return nil
}

// Find is the SKILL.md an agent of kind finds for skill ref when it works
// in dir, or "": a claude looks in the project's skills, the user's, then
// the plugins' installed for it ("plugin:" narrows these to that plugin's);
// a codex in its own.
func Find(kind, dir, ref string) string {
	plugin, name, ok := strings.Cut(ref, ":")
	if !ok {
		plugin, name = "", ref
	}
	var dirs []string
	switch kind {
	case "claude":
		home := claudeHome()
		if plugin == "" {
			dirs = append(dirs, filepath.Join(dir, ".claude", "skills"), filepath.Join(home, "skills"))
		}
		dirs = append(dirs, pluginSkillDirs(home, plugin, dir)...)
	case "codex":
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			home = filepath.Join(paths.Home(), ".codex")
		}
		dirs = append(dirs, filepath.Join(home, "skills"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name, "SKILL.md")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func claudeHome() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(paths.Home(), ".claude")
}

// pluginSkillDirs are the skills dirs of the claude plugins installed for
// project dir, those of plugin alone unless it is "".
func pluginSkillDirs(home, plugin, dir string) []string {
	var f struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
			ProjectPath string `json:"projectPath"`
		} `json:"plugins"`
	}
	data, err := os.ReadFile(filepath.Join(home, "plugins", "installed_plugins.json"))
	if err != nil || json.Unmarshal(data, &f) != nil {
		return nil
	}
	keys := make([]string, 0, len(f.Plugins))
	for k := range f.Plugins { // name@marketplace
		if name, _, _ := strings.Cut(k, "@"); plugin == "" || name == plugin {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		for _, in := range f.Plugins[k] {
			if in.InstallPath == "" || in.ProjectPath != "" && !inside(dir, in.ProjectPath) {
				continue
			}
			out = append(out, filepath.Join(in.InstallPath, "skills"))
		}
	}
	return out
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

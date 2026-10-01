package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
)

// readOnlyTools are what a read only claude role runs without asking:
// looking at the work.
var readOnlyTools = strings.Join([]string{
	"Read", "Grep", "Glob",
	"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)",
}, ",")

// builderTools are the commands an implementer runs without asking: tests
// and builds of the common toolchains, and committing its work.
var builderTools = strings.Join([]string{
	"Bash(go test:*)", "Bash(go build:*)", "Bash(go vet:*)", "Bash(gofmt:*)",
	"Bash(npm test:*)", "Bash(npm run:*)", "Bash(pnpm test:*)", "Bash(yarn test:*)",
	"Bash(pytest:*)", "Bash(python3 -m pytest:*)", "Bash(python3 -m unittest:*)", "Bash(cargo test:*)", "Bash(cargo build:*)",
	"Bash(make test:*)", "Bash(make build:*)",
	"Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)",
}, ",")

// Permission levels for a run's claude agents: what they may do without
// asking you.
const (
	PermAuto      = "auto"      // claude judges each action itself
	PermAllowlist = "allowlist" // edits, and the commands in builderTools
	PermBypass    = "bypass"    // everything
)

// Permissions are the levels, the default first.
var Permissions = []string{PermAuto, PermAllowlist, PermBypass}

// PermissionLabel says what a level lets agents do.
func PermissionLabel(p string) string {
	switch p {
	case PermAllowlist:
		return "edits, tests, builds and commits; asks about the rest"
	case PermBypass:
		return "asks nothing (risky, worktree or not)"
	}
	return "claude judges each action and asks only about risky ones (recommended)"
}

// Choice is an agent a role can be played by, and how hard it thinks.
type Choice struct{ Kind, Model, Effort string }

// Choices are the agents the form offers for a role: claude's models,
// then codex as its config has it, then each model codex lists for the
// account (from its models cache).
func Choices() []Choice {
	out := []Choice{{Kind: "claude", Model: "opus"}, {Kind: "claude", Model: "sonnet"}, {Kind: "claude", Model: "haiku"}, {Kind: "codex"}}
	for _, m := range codexModels() {
		out = append(out, Choice{Kind: "codex", Model: m.Slug})
	}
	return out
}

// claudeEfforts are the levels claude's --effort takes.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// Efforts are the levels c's agent can think at, "" (its own setting)
// first: claude's, or those codex lists for the model (the one its
// config sets, for plain codex).
func Efforts(c Choice) []string {
	out := []string{""}
	switch c.Kind {
	case "claude":
		return append(out, claudeEfforts...)
	case "codex":
		model := c.Model
		if model == "" {
			model = codexConfigModel()
		}
		for _, m := range codexModels() {
			if m.Slug == model && len(m.Levels) > 0 {
				return append(out, m.Levels...)
			}
		}
		return append(out, "low", "medium", "high", "xhigh")
	}
	return out
}

type codexModel struct {
	Slug   string
	Levels []string
}

func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	return filepath.Join(paths.Home(), ".codex")
}

// codexModels are the models codex offers this account, in its own
// order, with the reasoning levels each takes: models_cache.json in its
// home, which it keeps up to date.
func codexModels() []codexModel {
	data, err := os.ReadFile(filepath.Join(codexHome(), "models_cache.json"))
	if err != nil {
		return nil
	}
	var cache struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
			Levels     []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	sort.SliceStable(cache.Models, func(i, j int) bool { return cache.Models[i].Priority < cache.Models[j].Priority })
	var out []codexModel
	for _, m := range cache.Models {
		if m.Slug == "" || m.Visibility != "list" {
			continue
		}
		cm := codexModel{Slug: m.Slug}
		for _, l := range m.Levels {
			cm.Levels = append(cm.Levels, l.Effort)
		}
		out = append(out, cm)
	}
	return out
}

var configModelRe = regexp.MustCompile(`^\s*model\s*=\s*"([^"]+)"`)

// codexConfigModel is the model codex's config.toml sets at its top.
func codexConfigModel() string {
	data, err := os.ReadFile(filepath.Join(codexHome(), "config.toml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if m := configModelRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

func (c Choice) String() string {
	s := c.Kind
	if c.Model != "" {
		s += ":" + c.Model
	}
	if c.Effort != "" {
		s += "@" + c.Effort
	}
	return s
}

// ParseChoice reads "kind", "kind:model", and either with "@effort".
func ParseChoice(s string) Choice {
	s, effort, _ := strings.Cut(s, "@")
	kind, model, _ := strings.Cut(s, ":")
	return Choice{kind, model, effort}
}

// Args are the flags role r's agent starts with: under permission p for
// claude; codex works in its sandbox, read only for a reviewer.
func (r Role) Args(p string) []string {
	switch r.Kind {
	case "codex":
		sandbox := "workspace-write"
		if r.ReadOnly {
			sandbox = "read-only"
		}
		// No update notice for a codex a run starts: it would hold the
		// run up.
		args := []string{"-s", sandbox, "-a", "never", "-c", "check_for_update_on_startup=false"}
		if r.Effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+r.Effort)
		}
		return args
	case "claude":
		var args []string
		switch {
		case r.ReadOnly:
			// Whatever the run's permission: no edits, and no commands but
			// reading the diff; the rest asks.
			args = []string{"--permission-mode", "default", "--allowedTools", readOnlyTools,
				"--disallowedTools", "Edit,Write,NotebookEdit"}
		case p == PermAllowlist:
			args = []string{"--permission-mode", "acceptEdits", "--allowedTools", builderTools}
		case p == PermBypass:
			args = []string{"--permission-mode", "bypassPermissions"}
		default:
			args = []string{"--permission-mode", "auto"}
		}
		if r.Effort != "" {
			args = append(args, "--effort", r.Effort)
		}
		return args
	}
	return nil
}

// Permits says in a few words what role r's agent may do.
func (r Role) Permits(p string) string {
	switch {
	case r.Kind == "codex" && r.ReadOnly:
		return "read only (codex sandbox)"
	case r.Kind == "codex":
		return "edits the worktree (codex sandbox)"
	case r.Kind != "claude":
		return "as its own settings say"
	case r.ReadOnly:
		return "read only; asks before any command but git diff, log, show, status"
	case p == PermAllowlist:
		return "edits, tests, builds, commits"
	case p == PermBypass:
		return "anything"
	}
	return "auto"
}

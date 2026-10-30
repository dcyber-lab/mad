package discover

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// The desktop window offers an agent's slash commands as you type / in
// its message box. A kind's TUI has its own, listed here as its source
// has them; the agent finds more where it runs (skills, prompt templates,
// plugins' commands), and claude and pi say which when asked: started
// headless, they answer, as they would their SDK or RPC client, without
// a word sent to a model.

// Command is one slash command: what typing /Name does.
type Command struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Hint        string   `json:"hint,omitempty"` // its arguments, as the agent writes them: "<model>"
	Aliases     []string `json:"aliases,omitempty"`
	// TUI: what it brings up (a picker, a dialog, a report) is on the
	// agent's screen only, not in its transcript.
	TUI bool `json:"tui,omitempty"`
}

// askTimeout bounds asking an agent: a claude connects its MCP servers
// first.
const askTimeout = 30 * time.Second

// Commands lists the slash commands kind's agents take in dir. If asking
// the agent failed (err), the TUI's own are there all the same.
func Commands(ctx context.Context, kind, dir string) ([]Command, error) {
	switch kind {
	case "claude":
		found, err := askClaude(ctx, dir)
		return merge(claudeCommands, found), err
	case "codex":
		return merge(codexCommands, nil), nil
	case "pi":
		found, err := askPi(ctx, dir)
		return merge(piCommands, found), err
	}
	return nil, nil
}

// merge puts what the agent said (found) with its TUI's own (known), by
// name: a known one adds only what found lacks. Sorted by name.
func merge(known, found []Command) []Command {
	byName := map[string]int{}
	var out []Command
	for _, c := range found {
		if c.Name == "" || strings.HasPrefix(c.Name, "_") || strings.HasPrefix(c.Description, "(removed)") {
			continue
		}
		if _, dup := byName[c.Name]; dup {
			continue
		}
		c.Description = strings.Join(strings.Fields(c.Description), " ")
		byName[c.Name] = len(out)
		out = append(out, c)
	}
	for _, k := range known {
		i, ok := byName[k.Name]
		if !ok {
			out = append(out, k)
			continue
		}
		c := &out[i]
		c.TUI = c.TUI || k.TUI
		if c.Description == "" {
			c.Description = k.Description
		}
		if c.Hint == "" {
			c.Hint = k.Hint
		}
		if len(c.Aliases) == 0 {
			c.Aliases = k.Aliases
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// askClaude asks a headless claude in dir what it takes, the way the
// Agent SDK does (an initialize request): its own commands, skills and
// plugins' among them.
func askClaude(ctx context.Context, dir string) ([]Command, error) {
	var res struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
			Response  struct {
				Commands []struct {
					Name         string   `json:"name"`
					Description  string   `json:"description"`
					ArgumentHint string   `json:"argumentHint"`
					Aliases      []string `json:"aliases"`
				} `json:"commands"`
			} `json:"response"`
		} `json:"response"`
	}
	err := ask(ctx, dir,
		[]string{"claude", "-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--no-session-persistence"},
		`{"type":"control_request","request_id":"mad","request":{"subtype":"initialize"}}`,
		func(line []byte) bool {
			return json.Unmarshal(line, &res) == nil && res.Type == "control_response" && res.Response.RequestID == "mad"
		})
	if err != nil {
		return nil, err
	}
	if res.Response.Subtype == "error" {
		return nil, errors.New("claude: " + res.Response.Error)
	}
	var out []Command
	for _, c := range res.Response.Response.Commands {
		out = append(out, Command{Name: c.Name, Description: c.Description, Hint: c.ArgumentHint, Aliases: c.Aliases})
	}
	return out, nil
}

// askPi asks pi in RPC mode, in dir, for the commands it found: its
// extensions', prompt templates and skills (skill:name).
func askPi(ctx context.Context, dir string) ([]Command, error) {
	var res struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Commands []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"commands"`
		} `json:"data"`
	}
	err := ask(ctx, dir, []string{"pi", "--mode", "rpc", "--no-session"}, `{"id":"mad","type":"get_commands"}`,
		func(line []byte) bool {
			return json.Unmarshal(line, &res) == nil && res.Type == "response" && res.ID == "mad"
		})
	if err != nil {
		return nil, err
	}
	if !res.Success {
		return nil, errors.New("pi: " + res.Error)
	}
	var out []Command
	for _, c := range res.Data.Commands {
		out = append(out, Command{Name: c.Name, Description: c.Description})
	}
	return out, nil
}

// ask starts argv in dir, writes request to it, and hands done what it
// prints, a line at a time, until done has its answer. Then it ends the
// process, with whatever it started. It runs in a session of its own,
// without a terminal: not an agent run outside the deck (ScanExternal),
// and a process group to end.
var ask = func(ctx context.Context, dir string, argv []string, request string, done func(line []byte) bool) error {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	// A pipe of our own, which Wait leaves open: what it printed before it
	// ended is still read.
	out, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer out.Close()
	cmd.Stdout = w
	err = cmd.Start()
	w.Close()
	if err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
	}()
	// Kept open: the request is all it gets, and stdin closing ends it.
	if _, err := in.Write([]byte(request + "\n")); err != nil {
		return err
	}
	answered := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			if done(sc.Bytes()) {
				answered <- nil
				return
			}
		}
		answered <- errors.New(argv[0] + " stopped without answering")
	}()
	select {
	case err := <-answered:
		return err
	case <-exited:
		// Ended, though what it started may hold the pipe open: the
		// answer is in it by now, or not coming.
		select {
		case err := <-answered:
			return err
		case <-time.After(time.Second):
			return errors.New(argv[0] + " stopped without answering")
		}
	case <-ctx.Done():
		return errors.New(argv[0] + " did not answer: " + ctx.Err().Error())
	}
}

// claudeCommands are claude's own: the ones its headless self leaves
// out, which only the TUI has, and a few it lists, in case asking fails
// and for TUI. As claude 2.1 describes them.
var claudeCommands = []Command{
	{Name: "add-dir", Description: "Add a new working directory", Hint: "<path>"},
	{Name: "btw", Description: "Ask a quick side question without interrupting the main conversation", Hint: "[question]", TUI: true},
	{Name: "clear", Description: "Start a new session with empty context; previous session stays on disk (resumable with /resume)", Hint: "[name]", Aliases: []string{"reset", "new"}},
	{Name: "compact", Description: "Free up context by summarizing the conversation so far", Hint: "<optional custom summarization instructions>"},
	{Name: "config", Description: "Open settings", Hint: "[key=value]", Aliases: []string{"settings"}, TUI: true},
	{Name: "context", Description: "Show current context usage", TUI: true},
	{Name: "copy", Description: "Copy Claude's last response to clipboard (or /copy N for the Nth-latest)"},
	{Name: "diff", Description: "View uncommitted changes and per-turn diffs", TUI: true},
	{Name: "exit", Description: "Exit Claude Code", Aliases: []string{"quit"}},
	{Name: "export", Description: "Export the current conversation to a file or clipboard", Hint: "[filename]", TUI: true},
	{Name: "feedback", Description: "Send feedback to Anthropic or report a bug", Hint: "[report]", Aliases: []string{"bug"}, TUI: true},
	{Name: "help", Description: "Show help and available commands", TUI: true},
	{Name: "hooks", Description: "View hook configurations for tool events", TUI: true},
	{Name: "ide", Description: "Manage IDE integrations and show status", Hint: "[open]", TUI: true},
	{Name: "init", Description: "Initialize a new CLAUDE.md file with codebase documentation"},
	{Name: "login", Description: "Sign in with your Anthropic account", TUI: true},
	{Name: "logout", Description: "Sign out from your Anthropic account"},
	{Name: "mcp", Description: "Manage MCP servers", Hint: "[reconnect (<server>|all)|enable|disable [<server>|all]]", TUI: true},
	{Name: "memory", Description: "Edit CLAUDE.md files and memory settings", TUI: true},
	{Name: "model", Description: "Set the AI model for Claude Code", Hint: "<model>", TUI: true},
	{Name: "permissions", Description: "Manage allow and deny tool permission rules", Aliases: []string{"allowed-tools"}, TUI: true},
	{Name: "plan", Description: "Enable plan mode or view the current session plan", Hint: "[open|<description>]"},
	{Name: "plugin", Description: "Manage Claude Code plugins", Aliases: []string{"plugins", "marketplace"}, TUI: true},
	{Name: "resume", Description: "Resume a previous conversation", TUI: true},
	{Name: "skills", Description: "List available skills", TUI: true},
	{Name: "status", Description: "Show Claude Code status including version, model, account, API connectivity, and tool statuses", TUI: true},
	{Name: "tasks", Description: "View and manage everything running in the background", Aliases: []string{"bashes"}, TUI: true},
	{Name: "theme", Description: "Change the theme", TUI: true},
	{Name: "usage", Description: "Show session cost, plan usage, and activity stats", Aliases: []string{"cost", "stats"}, TUI: true},
}

// codexCommands are the codex TUI's, as codex 0.157 lists them
// (codex-rs/tui/src/slash_command.rs, Copyright 2025 OpenAI, Apache
// License 2.0), less the ones only its debug builds show.
var codexCommands = []Command{
	{Name: "model", Description: "choose what model and reasoning effort to use", TUI: true},
	{Name: "ide", Description: "include current selection, open files, and other context from your IDE"},
	{Name: "permissions", Description: "choose what Codex is allowed to do", TUI: true},
	{Name: "keymap", Description: "remap TUI shortcuts", TUI: true},
	{Name: "vim", Description: "toggle Vim mode for the composer"},
	{Name: "setup-default-sandbox", Description: "set up elevated agent sandbox", TUI: true},
	{Name: "experimental", Description: "toggle experimental features", TUI: true},
	{Name: "approve", Description: "approve one retry of a recent auto-review denial"},
	{Name: "memories", Description: "configure memory use and generation", TUI: true},
	{Name: "skills", Description: "use skills to improve how Codex performs specific tasks", TUI: true},
	{Name: "import", Description: "import setup, this project, and recent chats from Claude Code", TUI: true},
	{Name: "hooks", Description: "view and manage lifecycle hooks", TUI: true},
	{Name: "review", Description: "review my current changes and find issues"},
	{Name: "rename", Description: "rename the current thread"},
	{Name: "new", Description: "start a new chat during a conversation"},
	{Name: "archive", Description: "archive this session"},
	{Name: "delete", Description: "permanently delete this session", TUI: true},
	{Name: "resume", Description: "resume a saved chat", TUI: true},
	{Name: "fork", Description: "fork the current chat"},
	{Name: "worktree", Description: "start or continue a conversation in a new worktree", TUI: true},
	{Name: "app", Description: "continue this session in the Desktop app"},
	{Name: "init", Description: "create an AGENTS.md file with instructions for Codex"},
	{Name: "compact", Description: "summarize conversation to prevent hitting the context limit"},
	{Name: "recap", Description: "summarize the current conversation now"},
	{Name: "plan", Description: "switch to Plan mode"},
	{Name: "voice", Description: "start or stop voice; use /voice settings to choose a voice", TUI: true},
	{Name: "goal", Description: "set or view the goal for a long-running task"},
	{Name: "agents", Description: "open the agent command center", TUI: true},
	{Name: "side", Description: "start a side conversation in an ephemeral fork", TUI: true},
	{Name: "btw", Description: "start a side conversation in an ephemeral fork", TUI: true},
	{Name: "copy", Description: "copy the last response or part of it"},
	{Name: "export", Description: "export the conversation as markdown"},
	{Name: "raw", Description: "toggle raw scrollback mode for copy-friendly terminal selection"},
	{Name: "tui", Description: "choose the TUI mode for the next launch", TUI: true},
	{Name: "diff", Description: "show git diff (including untracked files)", TUI: true},
	{Name: "mention", Description: "mention a file", TUI: true},
	{Name: "status", Description: "show current session configuration and token usage", TUI: true},
	{Name: "daemon", Description: "Manage the local background server", TUI: true},
	{Name: "warnings", Description: "view retained warnings and diagnostic details", TUI: true},
	{Name: "cd", Description: "change the current working directory"},
	{Name: "pwd", Description: "show the current working directory", Aliases: []string{"cwd"}, TUI: true},
	{Name: "usage", Description: "view account usage or use a usage limit reset", TUI: true},
	{Name: "debug-config", Description: "show config layers and requirement sources for debugging", TUI: true},
	{Name: "title", Description: "configure which items appear in the terminal title", TUI: true},
	{Name: "statusline", Description: "configure which items appear in the status line", TUI: true},
	{Name: "theme", Description: "choose a syntax highlighting theme", TUI: true},
	{Name: "pets", Description: "choose or hide the terminal pet", Aliases: []string{"pet"}, TUI: true},
	{Name: "mcp", Description: "list configured MCP tools; use /mcp verbose for details", TUI: true},
	{Name: "apps", Description: "manage apps", TUI: true},
	{Name: "plugins", Description: "browse plugins", TUI: true},
	{Name: "logout", Description: "log out of Codex"},
	{Name: "quit", Description: "exit Codex"},
	{Name: "exit", Description: "exit Codex"},
	{Name: "feedback", Description: "send logs to maintainers", TUI: true},
	{Name: "ps", Description: "list background terminals", TUI: true},
	{Name: "stop", Description: "stop all background terminals", Aliases: []string{"clean"}},
	{Name: "clear", Description: "clear the terminal and start a new chat"},
	{Name: "subagents", Description: "switch between this session's subagents", TUI: true},
}

// piCommands are pi's built-in ones, which it handles in its TUI only, as
// pi 0.82 lists them (BUILTIN_SLASH_COMMANDS in
// packages/coding-agent/src/core/slash-commands.ts, Copyright (c) 2025
// Mario Zechner, MIT License).
var piCommands = []Command{
	{Name: "settings", Description: "Open settings menu", TUI: true},
	{Name: "model", Description: "Select model (opens selector UI)", Hint: "<provider/model>", TUI: true},
	{Name: "scoped-models", Description: "Enable/disable models for Ctrl+P cycling", TUI: true},
	{Name: "export", Description: "Export session (HTML default, or specify path: .html/.jsonl)"},
	{Name: "import", Description: "Import and resume a session from a JSONL file", TUI: true},
	{Name: "share", Description: "Share session as a secret GitHub gist"},
	{Name: "copy", Description: "Copy last agent message to clipboard"},
	{Name: "name", Description: "Set session display name"},
	{Name: "session", Description: "Show session info and stats", TUI: true},
	{Name: "changelog", Description: "Show changelog entries", TUI: true},
	{Name: "hotkeys", Description: "Show all keyboard shortcuts", TUI: true},
	{Name: "fork", Description: "Create a new fork from a previous user message", TUI: true},
	{Name: "clone", Description: "Duplicate the current session at the current position"},
	{Name: "tree", Description: "Navigate session tree (switch branches)", TUI: true},
	{Name: "trust", Description: "Save project trust decision for future sessions"},
	{Name: "login", Description: "Configure provider authentication", Hint: "<provider>", TUI: true},
	{Name: "logout", Description: "Remove provider authentication", TUI: true},
	{Name: "new", Description: "Start a new session"},
	{Name: "compact", Description: "Manually compact the session context"},
	{Name: "resume", Description: "Resume a different session", TUI: true},
	{Name: "reload", Description: "Reload keybindings, extensions, skills, prompts, themes, and context files"},
	{Name: "quit", Description: "Quit pi"},
}

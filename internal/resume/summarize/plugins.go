package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
)

func init() {
	Register("claude", func(c Config) (Plugin, error) {
		if _, err := exec.LookPath("claude"); err != nil {
			return nil, errors.New(`brief: summarizer "claude" needs the claude CLI on PATH`)
		}
		if c.Model == "" {
			c.Model = "haiku"
		}
		return claudeCLI{c}, nil
	})
	Register("command", func(c Config) (Plugin, error) {
		if strings.TrimSpace(c.Command) == "" {
			return nil, errors.New(`brief: summarizer "command" needs "command"`)
		}
		return command{c}, nil
	})
}

// schema is the Response shape, handed to plugins that can enforce it.
const schema = `{"type":"object","additionalProperties":false,
"required":["summary","open_questions","decision_candidates"],
"properties":{
 "summary":{"type":"string"},
 "open_questions":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["text","source"],
   "properties":{"text":{"type":"string"},"source":{"type":"string"}}}},
 "decision_candidates":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["choice","source","quote"],
   "properties":{"choice":{"type":"string"},"why":{"type":"string"},"rejected":{"type":"string"},"scope":{"type":"string"},
   "source":{"type":"string"},"quote":{"type":"string"}}}},
 "suggested_bookmark":{"type":"object","additionalProperties":false,"required":["text","source"],
   "properties":{"text":{"type":"string"},"source":{"type":"string"}}}}}`

// Instructions is the system prompt of the built-in plugin; command
// plugins may reuse it.
const Instructions = `You help a developer resume work they left while a coding agent kept going.
You get JSON: their goal, bookmark, pinned decisions, facts mad collected (each saying what it rests on), and the conversation turns since they last caught up (T1… oldest first; "you" is the developer, "reply" the agent's last message in that turn).

Fill the schema:
- summary: 2-4 sentences, what happened in these turns and where things stand, relative to the bookmark if there is one. Plain statements. Keep the agent's claims as claims ("the agent says tests pass"), never as facts; only the facts list is fact.
- open_questions: what is still undecided or unverified that the developer must act on. Cite the turn in source.
- decision_candidates: only choices between approaches that the developer clearly made in their own words (what to do about the problem, what to keep or rule out). Requests to act (open a PR, rerun a test, install something) are not decisions. quote must be copied verbatim from that turn's "you" text. Do not turn a vague "ok"/"好" into a decision; do not record what they rejected or deferred as chosen. Keep the scope as narrow as they stated it.
- suggested_bookmark: optional, one line: what to look at next, from their own words.
Never state what the turns don't say: a PR "opened" is not "merged", a fix "tried" is not "working". At most 2 open questions and 3 decision candidates, the most important. Write in the language the developer writes in. Do not invent turn refs. Empty arrays are fine.`

// claudeCLI runs `claude -p` with the user's Claude Code login. It runs
// outside any project with no tools, no settings (so no hooks: mad's own
// would report it as an agent) and no saved session.
type claudeCLI struct{ c Config }

func (claudeCLI) Name() string { return "claude" }

func (p claudeCLI) Summarize(ctx context.Context, r Request) (Response, Meta, error) {
	in, _ := json.Marshal(r)
	dir := filepath.Join(paths.StateDir(), "summarizer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Response{}, Meta{}, err
	}
	args := []string{"-p", "--model", p.c.Model, "--no-session-persistence", "--tools", "",
		"--strict-mcp-config", "--setting-sources", "", "--system-prompt", Instructions,
		"--output-format", "json", "--json-schema", schema}
	if p.c.MaxUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprint(p.c.MaxUSD))
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	// No thinking: a summary doesn't need it, and it was 90% of the time and cost.
	cmd.Env = append(os.Environ(), "MAX_THINKING_TOKENS=0")
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var res struct {
		IsError    bool            `json:"is_error"`
		Result     string          `json:"result"`
		Structured json.RawMessage `json:"structured_output"`
		Cost       float64         `json:"total_cost_usd"`
		APIms      int             `json:"duration_api_ms"`
		Turns      int             `json:"num_turns"`
		Usage      struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
		ModelUsage map[string]struct {
			Canonical string `json:"canonicalModel"`
		} `json:"modelUsage"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		if err != nil {
			return Response{}, Meta{}, fmt.Errorf("claude -p: %v %s", err, lastLine(stderr.String()))
		}
		return Response{}, Meta{}, fmt.Errorf("claude -p: unreadable output: %v", jerr)
	}
	meta := Meta{CostUSD: res.Cost, Model: p.c.Model, APIms: res.APIms, Turns: res.Turns, InTokens: res.Usage.In, OutTokens: res.Usage.Out}
	for _, u := range res.ModelUsage {
		if u.Canonical != "" {
			meta.Model = u.Canonical
		}
	}
	if res.IsError || len(res.Structured) == 0 {
		return Response{}, meta, fmt.Errorf("claude -p: %s", lastLine(res.Result))
	}
	var resp Response
	if err := json.Unmarshal(res.Structured, &resp); err != nil {
		return Response{}, meta, fmt.Errorf("claude -p: %v", err)
	}
	return resp, meta, nil
}

// command runs any executable as a summarizer: Request JSON on stdin,
// Response JSON on stdout (optionally with "model" and "cost_usd").
// MAD_SUMMARIZER_INSTRUCTIONS and MAD_SUMMARIZER_SCHEMA carry the
// built-in prompt and schema for plugins that wrap another model.
type command struct{ c Config }

func (command) Name() string { return "command" }

func (p command) Summarize(ctx context.Context, r Request) (Response, Meta, error) {
	in, _ := json.Marshal(r)
	cmd := exec.CommandContext(ctx, "sh", "-c", p.c.Command)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = append(os.Environ(), "MAD_SUMMARIZER_INSTRUCTIONS="+Instructions, "MAD_SUMMARIZER_SCHEMA="+schema,
		"MAD_SUMMARIZER_VERSION="+fmt.Sprint(Version))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Response{}, Meta{}, fmt.Errorf("%s: %v %s", p.c.Command, err, lastLine(stderr.String()))
	}
	var resp struct {
		Response
		Model string  `json:"model"`
		Cost  float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return Response{}, Meta{}, fmt.Errorf("%s: output isn't a Response: %v", p.c.Command, err)
	}
	return resp.Response, Meta{Model: resp.Model, CostUSD: resp.Cost}, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

package discover

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeProjectDir is where claude keeps the transcripts of sessions run
// in cwd.
func ClaudeProjectDir(cwd string) string { return claudeProjectDir(cwd) }

// ClaudeTranscripts returns the transcript files of claude session sid
// run in dir: the session itself and any subagents it spawned. Nothing is
// returned until the session's first write. A session whose transcript
// lives under another project (an adopted one that ran elsewhere) is
// found by looking through every project.
func ClaudeTranscripts(dir, sid string) []string {
	base := claudeProjectDir(dir)
	main := filepath.Join(base, sid+".jsonl")
	if _, err := os.Stat(main); err != nil {
		main = ""
		entries, _ := os.ReadDir(claudeProjectsDir())
		for _, e := range entries {
			p := filepath.Join(claudeProjectsDir(), e.Name(), sid+".jsonl")
			if _, err := os.Stat(p); err == nil {
				main, base = p, filepath.Dir(p)
				break
			}
		}
		if main == "" {
			return nil
		}
	}
	out := []string{main}
	subs, _ := filepath.Glob(filepath.Join(base, sid, "subagents", "*.jsonl"))
	return append(out, subs...)
}

// CodexTranscript returns the rollout file of codex thread sid, or "".
func CodexTranscript(sid string) string {
	matches, _ := filepath.Glob(filepath.Join(codexSessionsDir(), "*", "*", "*", "rollout-*-"+sid+".jsonl"))
	for _, m := range matches {
		if strings.HasSuffix(m, "-"+sid+".jsonl") {
			return m
		}
	}
	return ""
}

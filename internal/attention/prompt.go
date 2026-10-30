package attention

import (
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A prompt is a TUI's numbered choice as its screen shows it: claude's
// "Do you want to proceed?" and AskUserQuestion, codex's "Would you like
// to run the following command?", a trust or update screen. Read off the
// screen with no rule per kind:
//
//	Do you want to proceed?          the question: the first line with a "?"
//	❯ 1. Yes                         options 1..n, one under the cursor
//	  2. Yes, and always allow …
//	  3. No
//	Esc to cancel · Tab to amend     a footer that says Esc
//
// A screen it cannot read so is not a prompt: its item offers Open only.
type prompt struct {
	Question string
	// Detail is what the prompt says between its question and its options
	// (codex's reason and command).
	Detail  string
	Options []option
	// Cursor is the option the cursor is on, 1-based.
	Cursor int
	// ID names the prompt: its words, the cursor aside (moving it in the
	// pane is not a new prompt). Two looks with one ID show one prompt.
	ID string
}

type option struct {
	N     int    // its number: the key that picks it
	Label string // its words, a key hint like "(y)" or "(esc)" cut
}

// The most lines above the options that are read as the prompt's: past
// them is the conversation.
const promptLines = 8

var (
	optionRe = regexp.MustCompile(`^(\s*)([❯›>]\s*)?([1-9])\.\s+(\S.*)$`)
	// a hint of the key that also picks it: "(y)", "(esc)", "(shift+tab)"
	hintRe = regexp.MustCompile(`\s+\([a-z][a-z+]{0,11}\)$`)
)

// option labels that open a field to type in, which keys from here
// cannot fill: they are not offered.
var typing = []string{"Type something"}

// parsePrompt reads the prompt at the bottom of screen, if one is there.
func parsePrompt(screen string) (prompt, bool) {
	lines := strings.Split(screen, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	// The footer: among the last few lines, one that offers Esc.
	foot := -1
	for i, seen := len(lines)-1, 0; i >= 0 && seen < 3; i-- {
		if lines[i] == "" {
			continue
		}
		seen++
		if strings.Contains(strings.ToLower(lines[i]), "esc") {
			foot = i
			break
		}
	}
	if foot < 0 {
		return prompt{}, false
	}
	// The options, upward from the footer: description lines indented
	// under them, rules and blank lines may come between.
	var opts []option
	cursor, top, col := 0, -1, -1
	for i := foot - 1; i >= 0; i-- {
		l := lines[i]
		if m := optionRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[3])
			opts = append([]option{{N: n, Label: strings.TrimSpace(m[4])}}, opts...)
			if m[2] != "" {
				if cursor != 0 {
					return prompt{}, false // two cursors: not one list
				}
				cursor = n
			}
			col, top = utf8.RuneCountInString(m[1]+m[2]), i
			if n == 1 {
				break
			}
			continue
		}
		if l == "" || rule(l) || len(opts) > 0 && indent(l) > col {
			continue
		}
		break
	}
	if len(opts) < 2 || cursor == 0 {
		return prompt{}, false
	}
	for i, o := range opts {
		if o.N != i+1 {
			return prompt{}, false
		}
	}
	// What is above them, up to a rule (claude draws one over its prompts).
	var above []string
	for i := top - 1; i >= 0 && len(above) < promptLines; i-- {
		if rule(lines[i]) {
			break
		}
		if l := strings.TrimSpace(lines[i]); l != "" {
			above = append([]string{l}, above...)
		}
	}
	if len(above) == 0 || form(above) {
		return prompt{}, false
	}
	p := prompt{Cursor: cursor}
	q := 0 // no line asks: the first is its title
	for i, l := range above {
		if strings.Contains(l, "?") {
			q = i
			break
		}
	}
	p.Question, p.Detail = above[q], strings.Join(above[q+1:], "\n")
	h := fnv.New64a()
	for _, l := range above {
		h.Write([]byte(l + "\n"))
	}
	seen := map[string]bool{}
	for _, o := range opts {
		o.Label = hintRe.ReplaceAllString(o.Label, "")
		if strings.HasPrefix(o.Label, "[") {
			return prompt{}, false // a checkbox: several picks, then submit
		}
		if seen[o.Label] {
			return prompt{}, false // an option is acted on by its words
		}
		seen[o.Label] = true
		h.Write([]byte(strconv.Itoa(o.N) + ". " + o.Label + "\n"))
		if !typed(o.Label) {
			p.Options = append(p.Options, o)
		}
	}
	if len(p.Options) == 0 {
		return prompt{}, false
	}
	p.ID = strconv.FormatUint(h.Sum64(), 36)
	return p, true
}

// form tells a form of several questions or picks from one prompt:
// claude's AskUserQuestion with tabs ("☐ Color  ☐ Size  ✔ Submit"), which
// a key per question cannot fill. Its page, or the pane, can.
func form(above []string) bool {
	for _, l := range above {
		if strings.Count(l, "☐")+strings.Count(l, "☒")+strings.Count(l, "✔") > 1 {
			return true
		}
	}
	return false
}

func typed(label string) bool {
	for _, t := range typing {
		if strings.HasPrefix(label, t) {
			return true
		}
	}
	return false
}

// rule: a line drawn across, of ─ alone.
func rule(l string) bool {
	l = strings.TrimSpace(l)
	return utf8.RuneCountInString(l) >= 10 && strings.Trim(l, "─") == ""
}

func indent(l string) int {
	return utf8.RuneCountInString(l) - utf8.RuneCountInString(strings.TrimLeft(l, " "))
}

// option is p's option labeled label.
func (p prompt) option(label string) (option, bool) {
	for _, o := range p.Options {
		if o.Label == label {
			return o, true
		}
	}
	return option{}, false
}

package run

import (
	"regexp"
	"strings"
)

// The last line of a reply says what comes next.
const (
	Approve = "APPROVE"
	Changes = "CHANGES"
)

var (
	verdictRe  = regexp.MustCompile(`(?i)VERDICT:\s*(APPROVE|CHANGES)`)
	questionRe = regexp.MustCompile(`(?i)^QUESTION\(@?([\w-]+)\):\s*(.+)$`)
)

// lastLine is a reply's last line with text, without markdown around it.
func lastLine(reply string) string {
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	return strings.Trim(strings.TrimSpace(lines[len(lines)-1]), "*`_ ")
}

// body is a reply without its last line, when that line is a control one.
func body(reply string) string {
	reply = strings.TrimSpace(reply)
	if i := strings.LastIndexByte(reply, '\n'); i >= 0 && control(lastLine(reply)) {
		return strings.TrimSpace(reply[:i])
	}
	if control(lastLine(reply)) {
		return ""
	}
	return reply
}

func control(line string) bool {
	return strings.EqualFold(line, "DONE") || verdictRe.MatchString(line) || questionRe.MatchString(line)
}

// verdict is APPROVE, CHANGES, or "" when a review gave none.
func verdict(reply string) string {
	m := verdictRe.FindStringSubmatch(lastLine(reply))
	if m == nil {
		// Some put a closing word after it; the last verdict anywhere counts.
		all := verdictRe.FindAllStringSubmatch(reply, -1)
		if len(all) == 0 {
			return ""
		}
		m = all[len(all)-1]
	}
	return strings.ToUpper(m[1])
}

// question is who a reply asks and what, if it ends in a question.
func question(reply string) (role, text string, ok bool) {
	m := questionRe.FindStringSubmatch(lastLine(reply))
	if m == nil {
		return "", "", false
	}
	return strings.ToLower(m[1]), strings.TrimSpace(m[2]), true
}

// listMark is what starts a list item, heading or quote in markdown.
var listMark = regexp.MustCompile(`^([-*+]\s+|\d+[.)]\s+|#+\s*|>\s*)`)

// summary is one line saying what a reply came to.
func summary(reply string) string {
	for _, l := range strings.Split(body(reply), "\n") {
		if l = strings.TrimSpace(listMark.ReplaceAllString(strings.TrimSpace(l), "")); l != "" {
			return l
		}
	}
	return lastLine(reply)
}

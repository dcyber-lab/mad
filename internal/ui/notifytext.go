package ui

import (
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// firstLine is the first line of a reply that says something, as plain
// text, cut to 120 runes without splitting a character. A heading only
// labels what follows, so it is used only when nothing else says anything.
func firstLine(s string) string {
	fence, heading := "", ""
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if f := fenceOf(l); f != "" {
			switch {
			case fence == "":
				fence = f
			case f == fence:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		isHeading := strings.HasPrefix(l, "#")
		if l = plainLine(l); l != "" {
			if !isHeading {
				return cutRunes(l, 120)
			}
			if heading == "" {
				heading = l
			}
		}
	}
	return cutRunes(heading, 120)
}

func fenceOf(l string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(l, f) {
			return f
		}
	}
	return ""
}

var (
	ruleRe   = regexp.MustCompile(`^(?:-\s*){3,}$|^(?:\*\s*){3,}$|^(?:_\s*){3,}$|^={3,}$`)
	borderRe = regexp.MustCompile(`^[|+:\-=\s]+$`)
	tagRe    = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	listRe   = regexp.MustCompile(`^(?:#{1,6}\s+|>\s*|[-*+]\s+(?:\[[ xX]\]\s+)?|\d{1,3}[.)]\s+)`)
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	underRe  = regexp.MustCompile(`\b__(.+?)__\b`)
)

// plainLine is l without markdown's marks, or "" when the line only
// decorates (a rule, a table border, a bare HTML tag).
func plainLine(l string) string {
	if l == "" || ruleRe.MatchString(l) || (strings.ContainsAny(l, "-=") && borderRe.MatchString(l)) {
		return ""
	}
	if strings.HasPrefix(l, "<details") || strings.HasPrefix(l, "<summary") {
		return ""
	}
	l = strings.TrimSpace(tagRe.ReplaceAllString(l, ""))
	for {
		n := strings.TrimSpace(listRe.ReplaceAllString(l, ""))
		if n == l {
			break
		}
		l = n
	}
	if strings.HasPrefix(l, "|") {
		l = strings.TrimSpace(strings.Trim(l, "| "))
	}
	l = boldRe.ReplaceAllString(l, "$1")
	l = underRe.ReplaceAllString(l, "$1")
	l = strings.ReplaceAll(l, "`", "")
	return strings.Join(strings.Fields(l), " ")
}

// cutRunes cuts s to n runes ending in "…", backing off so the cut never
// leaves half of a joined emoji or a mark parted from its letter.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := n - 1
	for cut > 0 && (continues(r[cut]) || r[cut-1] == '‍') {
		cut--
	}
	return strings.TrimRight(string(r[:cut]), " ") + "…"
}

// continues: the rune belongs to the one before it.
func continues(r rune) bool {
	return r == '‍' || (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0x1F3FB && r <= 0x1F3FF) ||
		(r >= 0xE0020 && r <= 0xE007F) || unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc)
}

// madMarks says which agents' next turn is mad's own: set by the
// dispatcher (another goroutine) as it delivers, taken by the poll that
// sees the turn start.
type madMarks struct {
	mu sync.Mutex
	at map[string]time.Time
}

// madMarkTTL: a mark whose turn never showed up is forgotten.
const madMarkTTL = 10 * time.Minute

func (k *madMarks) mark(id string, now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.at == nil {
		k.at = map[string]time.Time{}
	}
	k.at[id] = now
}

func (k *madMarks) clear(id string) {
	k.mu.Lock()
	delete(k.at, id)
	k.mu.Unlock()
}

// take reports whether id's turn starting at now is mad's, and clears the mark.
func (k *madMarks) take(id string, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	at, ok := k.at[id]
	delete(k.at, id)
	return ok && now.Sub(at) < madMarkTTL
}

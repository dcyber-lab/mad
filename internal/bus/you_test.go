package bus

import (
	"image/color"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The person: every channel, unread until marked read.
func TestYourChannels(t *testing.T) {
	setup(t)
	if _, err := Create("ops", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", arch, "the deploy is green on staging now", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", me, "@architect thanks, ship it to production", 0); err != nil {
		t.Fatal(err)
	}
	y, err := YourChannels()
	if err != nil || y["ops"] != (Yours{Last: 2, Unread: 1, Listed: true, LastLine: "you: @architect thanks, ship it to production", LastAt: now()}) {
		t.Fatalf("yours = %+v %v", y, err)
	}
	if err := MarkRead(You, "ops", 2); err != nil {
		t.Fatal(err)
	}
	if y, _ := YourChannels(); y["ops"].Unread != 0 || y["ops"].Listed {
		t.Errorf("after reading: %+v", y["ops"])
	}
}

// Mention: an unread @you, or anything unread in the person's DM; a DM
// between two agents is never listed.
func TestYourMentions(t *testing.T) {
	setup(t)
	if _, err := Create("ops", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", arch, "@you which region should the deploy go to?", 0); err != nil {
		t.Fatal(err)
	}
	dm, _, err := DM(arch, You, "a word about the schema when you have a moment")
	if err != nil {
		t.Fatal(err)
	}
	theirs, _, err := DM(arch, idBuild, "@you is not a mention here, it is their DM")
	if err != nil {
		t.Fatal(err)
	}
	y, _ := YourChannels()
	if o := y["ops"]; !o.Mention || !o.Listed {
		t.Errorf("ops = %+v", o)
	}
	if d := y[dm]; !d.Mention || !d.Listed {
		t.Errorf("your DM = %+v", d)
	}
	if d := y[theirs]; d.Listed {
		t.Errorf("their DM = %+v", d)
	}
	if _, err := Post("ops", arch, "never mind, I picked one", 0); err != nil {
		t.Fatal(err)
	}
	if err := MarkRead(You, "ops", 1); err != nil {
		t.Fatal(err)
	}
	if o, _ := YourChannels(); o["ops"].Mention || !o["ops"].Listed {
		t.Errorf("the @you read, the rest not: %+v", o["ops"])
	}
}

// A channel's last message as one line of a list: who, and the first line
// of what they said, cut; pictures alone as [图片]; mad's own lines passed
// over.
func TestYourChannelsLastLine(t *testing.T) {
	setup(t)
	if _, err := Create("ops", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	last := func() string {
		t.Helper()
		y, err := YourChannels()
		if err != nil {
			t.Fatal(err)
		}
		return y["ops"].LastLine
	}
	if got := last(); got != "" {
		t.Errorf("nothing said: %q", got)
	}
	if y, _ := YourChannels(); !y["ops"].LastAt.IsZero() {
		t.Errorf("nothing said, said at %v", y["ops"].LastAt)
	}
	mustPost(t, "ops", arch, "the deploy is green on staging now\nand the second line is not shown")
	if got := last(); got != "architect: the deploy is green on staging now" {
		t.Errorf("first line: %q", got)
	}
	long := strings.Repeat("字", 130)
	mustPost(t, "ops", arch, long)
	if got := last(); got != "architect: "+strings.Repeat("字", 119)+"…" {
		t.Errorf("120 runes: %q (%d runes)", got, len([]rune(got)))
	}
	// A system line (the bus's own) does not become the last words.
	if err := PostSystem("ops", "Paused: waiting for you"); err != nil {
		t.Fatal(err)
	}
	if got := last(); !strings.HasPrefix(got, "architect: 字") {
		t.Errorf("after a system line: %q", got)
	}
	im, err := StoreImage(testPNG(t, 4, 4, color.RGBA{9, 0, 0, 255}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PostImages("ops", me, "", 0, []Image{im}); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != "you: [图片]" {
		t.Errorf("pictures alone: %q", got)
	}
	// LastAt is the time of the message the line is of.
	if y, _ := YourChannels(); !y["ops"].LastAt.Equal(now()) {
		t.Errorf("last at %v, want %v", y["ops"].LastAt, now())
	}
}

// put writes m straight into a channel's file, as the bus's own code does for each kind.
func put(t *testing.T, ch string, m Message) {
	t.Helper()
	err := locked(func() error {
		msgs, err := readMsgs(ch)
		if err != nil {
			return err
		}
		_, err = addMsg(ch, &msgs, m)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// What the bus keeps between agents is not the person's: no last line, no unread, no red dot for each of the
// hidden kinds; a lapsed notice (a system line about a handoff) is the same; an @you of any kind counts and
// is a last line; mad's own pause and resume notes are passed over as before.
func TestHiddenKindsAreNotTheirs(t *testing.T) {
	setup(t)
	if _, err := Create("ops", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	if _, err := Post("ops", arch, "the deploy is green", 0); err != nil {
		t.Fatal(err)
	}
	if err := MarkRead(You, "ops", 1); err != nil {
		t.Fatal(err)
	}
	from := From{ID: idArch, Label: "architect", Kind: "claude"}
	mad := From{ID: system, Label: system, Kind: "system"}
	ref := &HandoffRef{ID: "h-1", To: idBuild, Status: "open"}
	for _, kind := range HiddenKinds {
		put(t, "ops", Message{From: from, Kind: kind, Text: "Re-review the handoff, it lapsed", Handoff: ref})
	}
	put(t, "ops", Message{From: mad, Kind: "system", Text: "h-1 lapsed", Handoff: ref, Mentions: []string{idArch}})
	put(t, "ops", Message{From: mad, Kind: "system", Text: "Resumed by a person."})
	y, err := YourChannels()
	if err != nil {
		t.Fatal(err)
	}
	o := y["ops"]
	if o.Unread != 0 || o.Mention || o.Listed {
		t.Errorf("bookkeeping counted for the person: %+v", o)
	}
	if o.LastLine != "architect: the deploy is green" {
		t.Errorf("the last line is %q, want the last visible message", o.LastLine)
	}
	if o.Last != 8 {
		t.Errorf("last = %d: it is still the number of the last message, hidden or not", o.Last)
	}
	// an @you keeps its kind's weight: it is a message like any other, whatever its kind
	for _, kind := range append([]string{"text"}, HiddenKinds...) {
		put(t, "ops", Message{From: from, Kind: kind, Text: "@you your review is needed on " + kind})
		y, _ = YourChannels()
		o = y["ops"]
		if o.Unread == 0 || !o.Mention || o.LastLine != "architect: @you your review is needed on "+kind {
			t.Errorf("an @you of kind %s: %+v", kind, o)
		}
		if err := MarkRead(You, "ops", o.Last); err != nil {
			t.Fatal(err)
		}
	}
	// a channel with nothing visible has no last line at all
	if _, err := Create("quiet", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	put(t, "quiet", Message{From: from, Kind: "handoff", Text: "do it", Handoff: ref})
	put(t, "quiet", Message{From: from, Kind: "done", Text: "done", Handoff: ref})
	y, _ = YourChannels()
	if q := y["quiet"]; q.LastLine != "" || !q.LastAt.IsZero() || q.Unread != 0 || q.Last != 2 {
		t.Errorf("a channel of only bookkeeping: %+v", q)
	}
}

// The page's list of hidden kinds is the server's.
func TestPageHidesTheSameKinds(t *testing.T) {
	b, err := os.ReadFile("../desktop/web/bus.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const QUIET_KINDS = \[([^\]]*)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("bus.js has no QUIET_KINDS")
	}
	var page []string
	for _, k := range strings.Split(string(m[1]), ",") {
		page = append(page, strings.Trim(strings.TrimSpace(k), `"`))
	}
	if !slices.Equal(page, HiddenKinds) {
		t.Errorf("the page hides %v, the server %v", page, HiddenKinds)
	}
}

// The server finds @you as the page and native do: after the start, a space or a (half or full width)
// parenthesis; not before anything else, and not followed by a letter, digit, _ or -.
func TestAtYouRule(t *testing.T) {
	for text, want := range map[string]bool{
		"@you":           true,
		"@you 请看一下":      true,
		"请看 @you，谢谢":     true,
		"(@you)":         true,
		"（@you）看一下":      true,
		"@YOU":           true,
		"@you.":          true,
		"@you-x":         false,
		"@you_x":         false,
		"@youth":         false,
		"@you2":          false,
		"mail@you.com":   false,
		"a@you":          false,
		"请@you":          false,
		"@your reviewer": false,
		"@you\n下一行":      true,
	} {
		if got := youRe.MatchString(text); got != want {
			t.Errorf("%q: found=%v, want %v", text, got, want)
		}
	}
}

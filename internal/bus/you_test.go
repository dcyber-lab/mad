package bus

import (
	"image/color"
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

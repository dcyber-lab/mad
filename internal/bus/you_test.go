package bus

import "testing"

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
	if err != nil || y["ops"] != (Yours{Last: 2, Unread: 1, Listed: true}) {
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

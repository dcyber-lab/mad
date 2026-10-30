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
	if err != nil || y["ops"] != (Yours{Last: 2, Unread: 1}) {
		t.Fatalf("yours = %+v %v", y, err)
	}
	if err := MarkRead(You, "ops", 2); err != nil {
		t.Fatal(err)
	}
	if y, _ := YourChannels(); y["ops"].Unread != 0 {
		t.Errorf("after reading: %+v", y["ops"])
	}
}

package bus

import "testing"

func TestArchive(t *testing.T) {
	setup(t)
	mustCreate(t, "old", idArch)
	mustCreate(t, "new")
	if err := Archive("old"); err != nil {
		t.Fatal(err)
	}
	if chans, _ := Channels(); len(chans) != 1 || chans[0].ID != "new" {
		t.Errorf("channels = %+v", chans)
	}
	if _, err := Post("old", me, "anyone there?", 0); err == nil {
		t.Error("posted in an archived channel")
	}
	if _, err := Create("old", "", nil, me); err == nil {
		t.Error("an archived channel's name was taken again")
	}
	if err := Archive("dm." + idArch + ".you"); err == nil {
		t.Error("archived a DM")
	}
}

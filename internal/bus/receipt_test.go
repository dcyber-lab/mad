package bus

import "testing"

func stateOf(t *testing.T, ch string, seq int) Receipt {
	t.Helper()
	rs, err := Receipts(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Seq == seq {
			return r
		}
	}
	t.Fatalf("no receipt for #%d in %+v", seq, rs)
	return Receipt{}
}

// A message of the person's wakes who it names; its receipt goes from
// waiting to delivered, read and replied as the cursors and the agent's
// words move; a line with no @ wakes nobody.
func TestReceipts(t *testing.T) {
	setup(t)
	mustCreate(t, "ops", idArch, idBuild)
	plain := mustPost(t, "ops", me, "how is it going").Seq
	one := mustPost(t, "ops", me, "@architect status please").Seq
	all := mustPost(t, "ops", me, "@all stand up").Seq

	if r := stateOf(t, "ops", plain); len(r.Targets) != 0 {
		t.Errorf("a line with no @ wakes nobody: %+v", r)
	}
	want := func(seq int, states ...string) {
		t.Helper()
		r := stateOf(t, "ops", seq)
		if len(r.Targets) != len(states) {
			t.Fatalf("#%d targets %+v, want %v", seq, r.Targets, states)
		}
		for i, s := range states {
			if r.Targets[i].State != s {
				t.Errorf("#%d target %d: %+v, want %s", seq, i, r.Targets[i], s)
			}
		}
	}
	want(one, RcWaiting)
	want(all, RcWaiting, RcWaiting)

	// Delivered: the agent's digest covers it.
	if err := locked(func() error {
		return setCursor(idArch, "ops", func(c *cursor) { c.Delivered = one })
	}); err != nil {
		t.Fatal(err)
	}
	want(one, RcDelivered)
	want(all, RcWaiting, RcWaiting)

	// Read.
	if err := locked(func() error {
		return setCursor(idArch, "ops", func(c *cursor) { c.Delivered, c.Read = all, all })
	}); err != nil {
		t.Fatal(err)
	}
	want(one, RcRead)
	want(all, RcRead, RcWaiting)

	// Replied: an answer to it, or an @you after it.
	if _, err := Post("ops", arch, "all green on my side", one); err != nil {
		t.Fatal(err)
	}
	want(one, RcReplied)
	want(all, RcRead, RcWaiting) // a reply to another message is not an answer to it
	if _, err := Post("ops", build, "@you the build is done", 0); err != nil {
		t.Fatal(err)
	}
	want(all, RcRead, RcReplied)
}

// In a DM the other side is woken, and any word back answers.
func TestReceiptsDM(t *testing.T) {
	setup(t)
	ch, res, err := DM(me, idArch, "are you there, architect")
	if err != nil {
		t.Fatal(err)
	}
	if r := stateOf(t, ch, res.Seq); len(r.Targets) != 1 || r.Targets[0] != (Target{idArch, RcWaiting}) {
		t.Fatalf("%+v", r)
	}
	if _, err := Post(ch, arch, "the architect is here, what do you need", 0); err != nil {
		t.Fatal(err)
	}
	if r := stateOf(t, ch, res.Seq); r.Targets[0].State != RcReplied {
		t.Errorf("%+v", r)
	}
}

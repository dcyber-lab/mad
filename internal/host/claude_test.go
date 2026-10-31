package host

import (
	"testing"
)

// A system init (it reports the mode claude started in) that arrives just
// after the person's choice is recorded, before it is shown, leaves the
// choice standing: it was recorded first, so the init finds it.
func TestSetPermissionInitBetweenChoiceAndPublish(t *testing.T) {
	t.Setenv("MAD_FAKE_STALE", "1")
	h, _ := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle })
	c := h.d.(*claude)
	afterChosen = func() {
		c.line([]byte(`{"type":"system","subtype":"init","session_id":"s1","model":"m","permissionMode":"default"}`))
	}
	t.Cleanup(func() { afterChosen = nil })
	if err := h.do(Command{Type: "permission", Mode: "bypassPermissions"}); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Permission != "bypassPermissions" {
		t.Errorf("permission %q: the init's stale mode replaced the person's choice", s.Permission)
	}
	c.mu.Lock()
	chosen := c.chosen
	c.mu.Unlock()
	if chosen != "bypassPermissions" {
		t.Errorf("chosen %q", chosen)
	}
}

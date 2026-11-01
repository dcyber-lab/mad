package attention

import (
	"testing"

	"github.com/dcyber-lab/mad/internal/status"
)

// A client words an act by its verb, Kind and Target; Label stays the TUI's.
func TestActKindAndTarget(t *testing.T) {
	dm := busRoute{channel: "dm.a.b"}.acts()
	if dm[len(dm)-1].Kind != KindDM {
		t.Errorf("a DM's open act has kind %q, want %q", dm[len(dm)-1].Kind, KindDM)
	}
	ch := busRoute{channel: "ios"}.acts()
	if got := ch[len(ch)-1]; got.Verb != VerbOpen || got.Target != "#ios" || got.Kind != "" {
		t.Errorf("a channel's open act = %+v, want target #ios and no kind", got)
	}
	if got := (budgetRoute{}).acts()[2]; got.Kind != KindBoard {
		t.Errorf("open board has kind %q", got.Kind)
	}
	if got := (channelRoute{channel: "ios"}).acts()[1]; got.Target != "#ios" {
		t.Errorf("channel resume's open target = %q", got.Target)
	}
	plan := hostRoute{ask: status.Ask{Kind: status.AskPlan}}.acts()
	if plan[0].Kind != KindPlan || plan[1].Kind != "" {
		t.Errorf("plan acts = %+v, want only the approve to be kind plan", plan)
	}
	if got := (openRoute{label: "Open board", kind: KindBoard}).acts()[0]; got.Kind != KindBoard || got.Target != "" {
		t.Errorf("a board item's open = %+v, want kind board and no target", got)
	}
	if got := (openRoute{label: "Open claude-1", target: "claude-1"}).acts()[0]; got.Target != "claude-1" || got.Kind != "" {
		t.Errorf("a TUI agent's open = %+v, want target claude-1 and no kind", got)
	}
}

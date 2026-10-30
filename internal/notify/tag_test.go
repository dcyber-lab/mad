package notify

import "testing"

// A newer push for an agent replaces its older one, whatever the kind.
func TestTagIsTheAgentNotTheKind(t *testing.T) {
	w := Event{Kind: Waiting, Project: "api", AgentID: "a1"}
	d := Event{Kind: Done, Project: "api", AgentID: "a1"}
	if w.Tag() != d.Tag() || w.Tag() == (Event{Kind: Done, Project: "api", AgentID: "a2"}).Tag() {
		t.Errorf("tags %q %q", w.Tag(), d.Tag())
	}
	if (Event{Kind: Bus, AgentID: "a1", Channel: "dev"}).Tag() == w.Tag() {
		t.Error("a channel's push shares the agent's tag")
	}
}

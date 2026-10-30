package notify

import (
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/status"
)

// A notification says what the sidebar says.
func TestBodyUsesTheStateWords(t *testing.T) {
	for kind, state := range map[string]string{Waiting: status.Waiting, Done: status.Finished} {
		body := Event{Kind: kind, Agent: "a"}.Body()
		want := strings.ToLower(status.LookOf(state).Word)
		if body != "a "+want {
			t.Errorf("%s: body %q, want the word %q", kind, body, want)
		}
	}
}

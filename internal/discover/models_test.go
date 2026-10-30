package discover

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestModels(t *testing.T) {
	for _, c := range []struct {
		kind, script, sent, want string
	}{
		{"claude", `
echo '{"type":"control_response","response":{"subtype":"success","request_id":"other"}}'
echo '{"type":"control_response","response":{"subtype":"success","request_id":"mad","response":{"models":[{"value":"default","displayName":"Default","description":"Opus"},{"value":"haiku","displayName":"Haiku","description":"Fast"}]}}}'
wait
`, `"subtype":"initialize"`, "default Default Opus|haiku Haiku Fast"},
		// Said hello to first; a hidden model left out.
		{"codex", `
read l2
read l3
printf '\n%s\n%s' "$l2" "$l3" >> "$(dirname "$0")/sent"
echo '{"id":1,"result":{"userAgent":"codex"}}'
echo '{"id":2,"result":{"data":[{"model":"gpt-6-sol","displayName":"GPT-6-Sol","description":"Workhorse"},{"model":"old","displayName":"Old","hidden":true}]}}'
wait
`, `"version":"1"}}}` + "\n" + `{"method":"initialized","params":{}}` + "\n" + `{"id":2,"method":"model/list","params":{}}`, "gpt-6-sol GPT-6-Sol Workhorse"},
		// As --model takes them: provider/id.
		{"pi", `
echo '{"type":"response","id":"mad","command":"get_available_models","success":true,"data":{"models":[{"provider":"anthropic","id":"claude-haiku-4-5","name":"Claude Haiku 4.5"}]}}'
wait
`, `"get_available_models"`, "anthropic/claude-haiku-4-5 Claude Haiku 4.5 anthropic"},
	} {
		sent, _ := fakeAgent(t, c.kind, c.script)
		got, err := Models(context.Background(), c.kind, t.TempDir())
		if err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		var s []string
		for _, m := range got {
			s = append(s, fmt.Sprintf("%s %s %s", m.Value, m.Name, m.Description))
		}
		if strings.Join(s, "|") != c.want {
			t.Errorf("%s: models %q, want %q", c.kind, strings.Join(s, "|"), c.want)
		}
		if b, _ := os.ReadFile(sent); !strings.Contains(string(b), c.sent) {
			t.Errorf("%s: sent %q, want it to have %q", c.kind, b, c.sent)
		}
	}
	if got, err := Models(context.Background(), "shell", t.TempDir()); got != nil || err != nil {
		t.Errorf("shell: %v, %v", got, err)
	}
}

package discover

import (
	"context"
	"encoding/json"
	"errors"
)

// A new chat in the desktop window picks its model before there is an
// agent to ask. Each kind is asked as Commands asks it: started
// headless, it answers as it would its SDK or RPC client, without a word
// sent to a model.

// Model is one model an agent can run on: Value is what --model takes.
type Model struct {
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Models lists the models kind's agents can run on in dir.
func Models(ctx context.Context, kind, dir string) ([]Model, error) {
	switch kind {
	case "claude":
		return claudeModels(ctx, dir)
	case "codex":
		return codexModels(ctx, dir)
	case "pi":
		return piModels(ctx, dir)
	}
	return nil, nil
}

// claudeModels: the models an initialize request is answered with, as
// the Agent SDK asks.
func claudeModels(ctx context.Context, dir string) ([]Model, error) {
	var res struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
			Response  struct {
				Models []struct {
					Value       string `json:"value"`
					DisplayName string `json:"displayName"`
					Description string `json:"description"`
				} `json:"models"`
			} `json:"response"`
		} `json:"response"`
	}
	err := ask(ctx, dir,
		[]string{"claude", "-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--no-session-persistence"},
		`{"type":"control_request","request_id":"mad","request":{"subtype":"initialize"}}`,
		func(line []byte) bool {
			return json.Unmarshal(line, &res) == nil && res.Type == "control_response" && res.Response.RequestID == "mad"
		})
	if err != nil {
		return nil, err
	}
	if res.Response.Subtype == "error" {
		return nil, errors.New("claude: " + res.Response.Error)
	}
	var out []Model
	for _, m := range res.Response.Response.Models {
		out = append(out, Model{Value: m.Value, Name: m.DisplayName, Description: m.Description})
	}
	return out, nil
}

// codexModels: app-server's model/list, after it is said hello to; the
// hidden ones left out.
func codexModels(ctx context.Context, dir string) ([]Model, error) {
	var res struct {
		ID     json.RawMessage           `json:"id"`
		Error  *struct{ Message string } `json:"error"`
		Result struct {
			Data []struct {
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
				Hidden      bool   `json:"hidden"`
			} `json:"data"`
		} `json:"result"`
	}
	err := ask(ctx, dir, []string{"codex", "app-server"},
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"mad","title":"mad","version":"1"}}}`+"\n"+
			`{"method":"initialized","params":{}}`+"\n"+
			`{"id":2,"method":"model/list","params":{}}`,
		func(line []byte) bool {
			return json.Unmarshal(line, &res) == nil && string(res.ID) == "2"
		})
	if err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, errors.New("codex: " + res.Error.Message)
	}
	var out []Model
	for _, m := range res.Result.Data {
		if !m.Hidden {
			out = append(out, Model{Value: m.Model, Name: m.DisplayName, Description: m.Description})
		}
	}
	return out, nil
}

// piModels: the models RPC mode says are available (those with a key),
// as provider/id, which --model takes.
func piModels(ctx context.Context, dir string) ([]Model, error) {
	var res struct {
		ID      string          `json:"id"`
		Type    string          `json:"type"`
		Success bool            `json:"success"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	err := ask(ctx, dir, []string{"pi", "--mode", "rpc", "--no-session"}, `{"id":"mad","type":"get_available_models"}`,
		func(line []byte) bool {
			return json.Unmarshal(line, &res) == nil && res.Type == "response" && res.ID == "mad"
		})
	if err != nil {
		return nil, err
	}
	if !res.Success {
		return nil, errors.New("pi: " + res.Error)
	}
	type piModel struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
		Name     string `json:"name"`
	}
	var list []piModel
	if json.Unmarshal(res.Data, &list) != nil {
		var wrapped struct {
			Models []piModel `json:"models"`
		}
		_ = json.Unmarshal(res.Data, &wrapped)
		list = wrapped.Models
	}
	var out []Model
	for _, m := range list {
		out = append(out, Model{Value: m.Provider + "/" + m.ID, Name: m.Name, Description: m.Provider})
	}
	return out, nil
}

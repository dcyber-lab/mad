package state

import (
	"encoding/json"
	"testing"
)

func TestKeepUnknown(t *testing.T) {
	disk := []byte(`{"projects":[{"name":"p","path":"/p","future_p":1,"agents":[
		{"id":"a","kind":"claude","name":"old","team":"t","future_a":"x","created_at":"2026-01-01T00:00:00Z"},
		{"id":"gone","kind":"claude","future_a":"y","created_at":"2026-01-01T00:00:00Z"}]}],
		"future_top":true}`)
	// this version renamed a, cleared its team, removed "gone"
	s := &State{Projects: []*Project{{Name: "p", Path: "/p", Agents: []*Agent{{ID: "a", Kind: "claude", Name: "new"}}}}}
	data, _ := json.Marshal(s)
	var got struct {
		FutureTop bool `json:"future_top"`
		Projects  []struct {
			FutureP int `json:"future_p"`
			Agents  []map[string]any
		}
	}
	if err := json.Unmarshal(keepUnknown(disk, data), &got); err != nil {
		t.Fatal(err)
	}
	if !got.FutureTop || got.Projects[0].FutureP != 1 {
		t.Fatalf("unknown top or project field lost: %+v", got)
	}
	ag := got.Projects[0].Agents
	if len(ag) != 1 {
		t.Fatalf("a removed agent came back: %v", ag)
	}
	if ag[0]["future_a"] != "x" {
		t.Errorf("unknown agent field lost: %v", ag[0])
	}
	if ag[0]["name"] != "new" {
		t.Errorf("a known field was overwritten from disk: %v", ag[0])
	}
	if _, ok := ag[0]["team"]; ok {
		t.Errorf("a known field this version cleared came back: %v", ag[0])
	}
}

func TestKeepUnknownNothingToKeep(t *testing.T) {
	data := []byte(`{"projects":[]}`)
	if got := keepUnknown([]byte(`{"projects":[]}`), data); string(got) != string(data) {
		t.Errorf("unchanged data rewritten: %s", got)
	}
	if got := keepUnknown([]byte(`not json`), data); string(got) != string(data) {
		t.Errorf("a bad file changed the data: %s", got)
	}
}

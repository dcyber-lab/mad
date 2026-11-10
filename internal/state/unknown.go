package state

import (
	"encoding/json"
	"reflect"
	"strings"
)

// A process older than the state file's writers does not know every field
// in it. Saving drops what its structs lack, so a newer mad's fields (an
// agent's team, say) would vanish whenever the older one saved. keepUnknown
// carries them over from the file being replaced.

// keepUnknown is data (s marshalled) with the keys of disk that this
// version does not know put back: at the top, and in each project (by
// path), agent and run (by id) that is in both.
func keepUnknown(disk, data []byte) []byte {
	var old, cur map[string]json.RawMessage
	if json.Unmarshal(disk, &old) != nil || json.Unmarshal(data, &cur) != nil {
		return data
	}
	if !merge(old, cur, reflect.TypeOf(State{})) {
		return data
	}
	out, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return data
	}
	return out
}

// merge puts old's keys that t does not know into cur, then goes on into
// the projects, agents and runs; it reports whether cur changed.
func merge(old, cur map[string]json.RawMessage, t reflect.Type) bool {
	changed := false
	known := jsonKeys(t)
	for k, v := range old {
		if _, ok := cur[k]; !ok && !known[k] {
			cur[k] = v
			changed = true
		}
	}
	for _, list := range []struct {
		key, id string
		t       reflect.Type
	}{
		{"projects", "path", reflect.TypeOf(Project{})},
		{"agents", "id", reflect.TypeOf(Agent{})},
		{"runs", "id", reflect.TypeOf(Run{})},
	} {
		if mergeList(old, cur, list.key, list.id, list.t) {
			changed = true
		}
	}
	return changed
}

// mergeList merges the objects of old[key] into those of cur[key] with the
// same id field.
func mergeList(old, cur map[string]json.RawMessage, key, id string, t reflect.Type) bool {
	if old[key] == nil || cur[key] == nil {
		return false
	}
	var olds, curs []map[string]json.RawMessage
	if json.Unmarshal(old[key], &olds) != nil || json.Unmarshal(cur[key], &curs) != nil {
		return false
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, o := range olds {
		if o != nil && o[id] != nil {
			byID[string(o[id])] = o
		}
	}
	changed := false
	for _, c := range curs {
		if c == nil || c[id] == nil {
			continue
		}
		if o := byID[string(c[id])]; o != nil && merge(o, c, t) {
			changed = true
		}
	}
	if !changed {
		return false
	}
	data, err := json.Marshal(curs)
	if err != nil {
		return false
	}
	cur[key] = data
	return true
}

// jsonKeys are the JSON names of t's fields.
func jsonKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch name {
		case "-":
		case "":
			keys[f.Name] = true
		default:
			keys[name] = true
		}
	}
	return keys
}

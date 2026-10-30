package desktop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

// The scheduled strip: check-ins and routines in the snapshot, and the
// person adding, pausing, firing and deleting through POST /api/timer.
func TestTimerAPI(t *testing.T) {
	isolate(t)
	const a1 = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{{ID: a1, Kind: "claude", Name: "alice"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Create("dev", "", []string{a1}, you); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)
	timers := func() []bus.Timer {
		t.Helper()
		s.poll.Poll(time.Now())
		var snap Snapshot
		if err := json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		if snap.Timers == nil {
			t.Fatal("timers is not an array in the snapshot")
		}
		return snap.Timers
	}
	post := func(body string, want int) string {
		t.Helper()
		w := call(t, s, "POST", "/api/timer", body)
		if w.Code != want {
			t.Fatalf("POST /api/timer %s: %d %s", body, w.Code, w.Body)
		}
		return w.Body.String()
	}

	if n := len(timers()); n != 0 {
		t.Fatalf("timers at the start: %d", n)
	}
	// An agent's check-in shows too.
	if _, err := bus.AddCheckin(a1, "in 20m", "", "look at CI", time.Now()); err != nil {
		t.Fatal(err)
	}
	var r bus.Timer
	if err := json.Unmarshal([]byte(post(`{"op":"add","every":"weekdays 09:00","channel":"dev","text":"@alice what did you do yesterday?"}`, 200)), &r); err != nil || r.Kind != bus.TimerRoutine {
		t.Fatalf("add = %+v %v", r, err)
	}
	post(`{"op":"add","every":"every 30m","channel":"dev","action":"report"}`, 200)
	got := timers()
	kinds := map[string]int{}
	for _, x := range got {
		kinds[x.Kind]++
	}
	if len(got) != 3 || kinds[bus.TimerOnce] != 1 || kinds[bus.TimerRoutine] != 2 {
		t.Fatalf("timers = %+v", got)
	}
	for _, bad := range []string{
		`{"op":"add","every":"every 5m","channel":"dev","text":"too often"}`,
		`{"op":"add","every":"weekdays 09:00","channel":"nowhere","text":"hi"}`,
		`{"op":"add","every":"weekdays 09:00","channel":"dev"}`,
		`{"op":"pause","id":"r-nothing"}`,
		`{"op":"fire","id":"r-nothing"}`,
		`{"op":"frob"}`,
		`not json`,
	} {
		post(bad, 400)
	}

	post(`{"op":"pause","id":"`+r.ID+`"}`, 200)
	paused := false
	for _, x := range timers() {
		paused = paused || x.ID == r.ID && x.Paused
	}
	if !paused {
		t.Error("not paused")
	}
	post(`{"op":"resume","id":"`+r.ID+`"}`, 200)
	post(`{"op":"fire","id":"`+r.ID+`"}`, 200)
	msgs, _, _ := bus.Read("dev", 0, 10)
	if len(msgs) == 0 || msgs[len(msgs)-1].Kind != "timer" || !strings.Contains(msgs[len(msgs)-1].Text, "what did you do yesterday") {
		t.Errorf("channel after fire now: %+v", msgs)
	}
	// The person may cancel an agent's check-in.
	var checkin bus.Timer
	for _, x := range timers() {
		if x.Kind == bus.TimerOnce {
			checkin = x
		}
	}
	post(`{"op":"rm","id":"`+checkin.ID+`"}`, 200)
	post(`{"op":"rm","id":"`+r.ID+`"}`, 200)
	if left := timers(); len(left) != 1 || left[0].Action != bus.ActionReport {
		t.Errorf("after deleting: %+v", left)
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const initLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`

func TestListChangedOnlyWithStale(t *testing.T) {
	s := testServer()
	s.Stale = func() bool { return false }
	m := start(t, s).call(initLine)
	if !strings.Contains(string(m["result"]), `"listChanged":true`) {
		t.Errorf("with Stale: %s", m["result"])
	}
}

func TestReexecBetweenRequests(t *testing.T) {
	var stale atomic.Bool
	got := make(chan string, 4)
	s := testServer()
	s.Stale = stale.Load
	s.Reexec = func(v string) error { got <- v; return errors.New("no") }
	c := start(t, s)
	c.call(initLine)
	select {
	case v := <-got:
		t.Fatalf("exec while not stale: %q", v)
	case <-time.After(50 * time.Millisecond):
	}
	stale.Store(true)
	// Checked before the next read, so after the next request is answered.
	c.call(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	select {
	case v := <-got:
		if v != "2025-03-26" {
			t.Errorf("version %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exec")
	}
	// It failed: serving goes on, and it is not tried again.
	c.call(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	c.call(`{"jsonrpc":"2.0","id":4,"method":"ping"}`)
	select {
	case v := <-got:
		t.Errorf("second exec %q", v)
	default:
	}
}

func TestReexecWaitsForCalls(t *testing.T) {
	release := make(chan struct{})
	var answered, stale atomic.Bool
	reexec := make(chan bool, 1)
	s := &Server{Tools: []Tool{{Name: "slow", Handler: func(context.Context, json.RawMessage) (string, error) {
		stale.Store(true)
		<-release
		answered.Store(true)
		return "done", nil
	}}}}
	s.Stale = stale.Load
	s.Reexec = func(string) error { reexec <- answered.Load(); return errors.New("no") }
	c := start(t, s)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}`)
	c.send(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	// The ping is answered; the call is not, so no exec yet.
	if m := c.recv(); string(m["id"]) != "2" {
		t.Fatalf("first reply %v", m)
	}
	select {
	case <-reexec:
		t.Fatal("exec with a call in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if m := c.recv(); string(m["id"]) != "1" {
		t.Fatalf("reply %v", m)
	}
	select {
	case ok := <-reexec:
		if !ok {
			t.Error("exec before the call finished")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exec")
	}
}

func TestResumed(t *testing.T) {
	s := testServer()
	s.Resumed = "2025-03-26"
	c := start(t, s)
	m := c.recv()
	if string(m["method"]) != `"notifications/tools/list_changed"` || m["id"] != nil {
		t.Fatalf("first message %v", m)
	}
	if m := c.call(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}`); !strings.Contains(string(m["result"]), `"echo"`) {
		t.Errorf("tools/list: %v", m)
	}
	m = c.call(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`)
	if !strings.Contains(string(m["result"]), `got {\"a\":1}`) {
		t.Errorf("tools/call: %v", m)
	}
}

func TestResumedPassesVersionOn(t *testing.T) {
	got := make(chan string, 1)
	s := testServer()
	s.Resumed = "2024-11-05"
	s.Stale = func() bool { return true }
	s.Reexec = func(v string) error { got <- v; return errors.New("no") }
	c := start(t, s)
	c.recv() // list_changed
	select {
	case v := <-got:
		if v != "2024-11-05" {
			t.Errorf("version %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exec")
	}
}

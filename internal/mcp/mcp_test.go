package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type client struct {
	t    *testing.T
	in   io.WriteCloser
	out  *bufio.Reader
	done chan error
}

func start(t *testing.T, s *Server) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, out: bufio.NewReader(outR), done: make(chan error, 1)}
	go func() {
		err := s.Serve(context.Background(), inR, outW)
		outW.Close()
		c.done <- err
	}()
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *client) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() map[string]json.RawMessage {
	c.t.Helper()
	ch := make(chan string, 1)
	go func() { l, _ := c.out.ReadString('\n'); ch <- l }()
	select {
	case l := <-ch:
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			c.t.Fatalf("bad reply %q: %v", l, err)
		}
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("no reply")
		return nil
	}
}

func (c *client) call(line string) map[string]json.RawMessage {
	c.t.Helper()
	c.send(line)
	return c.recv()
}

func errCode(t *testing.T, m map[string]json.RawMessage) int {
	t.Helper()
	var e rpcError
	if err := json.Unmarshal(m["error"], &e); err != nil {
		t.Fatalf("no error in %v", m)
	}
	return e.Code
}

func testServer() *Server {
	return &Server{
		Name: "test", Version: "1.2.3", Instructions: "be nice",
		Tools: []Tool{
			{Name: "echo", Description: "Echo.", InputSchema: json.RawMessage(`{"type":"object"}`),
				Handler: func(_ context.Context, a json.RawMessage) (string, error) { return "got " + string(a), nil }},
			{Name: "bad", Description: "Fails.",
				Handler: func(context.Context, json.RawMessage) (string, error) { return "", errors.New("boom") }},
		},
	}
}

func TestInitialize(t *testing.T) {
	c := start(t, testServer())
	for _, tc := range []struct{ ask, want string }{
		{"2024-11-05", "2024-11-05"},
		{"2025-03-26", "2025-03-26"},
		{"1999-01-01", "2025-06-18"},
	} {
		m := c.call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + tc.ask + `"}}`)
		var r struct {
			ProtocolVersion string
			Instructions    string
			ServerInfo      struct{ Name, Version string }
		}
		if err := json.Unmarshal(m["result"], &r); err != nil {
			t.Fatal(err)
		}
		if r.ProtocolVersion != tc.want || r.Instructions != "be nice" ||
			r.ServerInfo.Name != "test" || r.ServerInfo.Version != "1.2.3" {
			t.Errorf("ask %s: %+v", tc.ask, r)
		}
		if string(m["id"]) != "1" {
			t.Errorf("id %s", m["id"])
		}
		if !strings.Contains(string(m["result"]), `"listChanged":false`) {
			t.Errorf("capabilities: %s", m["result"])
		}
	}
	s := testServer()
	s.Instructions = ""
	m := start(t, s).call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if strings.Contains(string(m["result"]), "instructions") {
		t.Errorf("unexpected instructions: %s", m["result"])
	}
}

func TestNotificationAndPing(t *testing.T) {
	c := start(t, testServer())
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	c.send(`{"jsonrpc":"2.0","method":"whatever/else"}`)
	// If either notification had been answered, that reply would come first.
	m := c.call(`{"jsonrpc":"2.0","id":"p-1","method":"ping"}`)
	if string(m["id"]) != `"p-1"` || string(m["result"]) != "{}" {
		t.Errorf("ping: %v", m)
	}
}

func TestToolsList(t *testing.T) {
	c := start(t, testServer())
	m := c.call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var r struct {
		Tools []struct {
			Name, Description string
			InputSchema       json.RawMessage
		}
	}
	if err := json.Unmarshal(m["result"], &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Tools) != 2 || r.Tools[0].Name != "echo" || r.Tools[0].Description != "Echo." ||
		string(r.Tools[0].InputSchema) != `{"type":"object"}` || string(r.Tools[1].InputSchema) != `{"type":"object"}` {
		t.Errorf("tools: %+v", r.Tools)
	}
}

type callResult struct {
	Content []struct{ Type, Text string }
	IsError bool
}

func TestToolsCall(t *testing.T) {
	c := start(t, testServer())
	m := c.call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`)
	var r callResult
	if err := json.Unmarshal(m["result"], &r); err != nil {
		t.Fatal(err)
	}
	if r.IsError || len(r.Content) != 1 || r.Content[0].Type != "text" || r.Content[0].Text != `got {"a":1}` {
		t.Errorf("result: %+v", r)
	}

	m = c.call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"bad"}}`)
	r = callResult{}
	if err := json.Unmarshal(m["result"], &r); err != nil {
		t.Fatal(err)
	}
	if !r.IsError || r.Content[0].Text != "boom" {
		t.Errorf("error result: %+v", r)
	}

	m = c.call(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`)
	if code := errCode(t, m); code != -32602 {
		t.Errorf("unknown tool code %d", code)
	}
}

func TestErrors(t *testing.T) {
	c := start(t, testServer())
	m := c.call(`{"jsonrpc":"2.0","id":7,"method":"nope"}`)
	if code := errCode(t, m); code != -32601 || string(m["id"]) != "7" {
		t.Errorf("unknown method: %v", m)
	}
	m = c.call(`{not json`)
	if code := errCode(t, m); code != -32700 || string(m["id"]) != "null" {
		t.Errorf("malformed: %v", m)
	}
	// The server still works afterwards.
	m = c.call(`{"jsonrpc":"2.0","id":8,"method":"ping"}`)
	if string(m["id"]) != "8" {
		t.Errorf("after malformed: %v", m)
	}
}

func TestServeWaitsForCalls(t *testing.T) {
	release := make(chan struct{})
	s := &Server{Name: "t", Tools: []Tool{{Name: "slow",
		Handler: func(context.Context, json.RawMessage) (string, error) {
			<-release
			return "done", nil
		}}}}
	c := start(t, s)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}`)
	c.in.Close()
	select {
	case err := <-c.done:
		t.Fatalf("Serve returned early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	m := c.recv()
	if string(m["id"]) != "1" || !strings.Contains(string(m["result"]), "done") {
		t.Errorf("reply: %v", m)
	}
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

// Serial: calls sent together run in the order they came.
func TestSerial(t *testing.T) {
	var order []string
	srv := &Server{Name: "t", Serial: true, Tools: []Tool{{
		Name: "note",
		Handler: func(_ context.Context, args json.RawMessage) (string, error) {
			order = append(order, string(args))
			return "ok", nil
		},
	}}}
	var in strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&in, `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"note","arguments":%d}}`+"\n", i, i)
	}
	var out strings.Builder
	if err := srv.Serve(context.Background(), strings.NewReader(in.String()), &out); err != nil {
		t.Fatal(err)
	}
	for i, got := range order {
		if got != fmt.Sprint(i) {
			t.Fatalf("call %d ran as %s: %v", i, got, order)
		}
	}
}

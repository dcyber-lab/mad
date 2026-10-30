// Package mcp is a small Model Context Protocol server for tools. It speaks
// JSON-RPC 2.0 over the stdio transport: one JSON message per line, in and
// out. Only what tools need is here: initialize, ping, tools/list and
// tools/call.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Tool is one thing the client can call.
type Tool struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object describing the arguments.
	InputSchema json.RawMessage
	// Handler runs the call. Its text is the result; an error is reported to
	// the model as a failed call, not as a protocol error.
	Handler func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server answers one client on one pair of streams.
type Server struct {
	Name    string
	Version string
	// Instructions is sent to the client at initialize when set.
	Instructions string
	Tools        []Tool
	// Serial runs calls one at a time, in the order they came: for tools
	// where one call needs what an earlier one did (a channel made, then
	// posted in), which a client may send together.
	Serial bool

	// Stale reports that a newer server is installed. The server then asks
	// to be replaced (Reexec) as soon as it can do so with no request lost:
	// between requests, with nothing read and left unanswered.
	Stale func() bool
	// Reexec replaces this process with the newer server. It returns only
	// when that failed, and serving goes on here. protocolVersion is what
	// the client negotiated at initialize, to pass on as Resumed; "" if
	// there was no initialize yet.
	Reexec func(protocolVersion string) error
	// Resumed is set when this process continues a session an earlier one
	// began: the protocol version that session negotiated. It needs no
	// initialize, and tells the client at once that the tools changed.
	Resumed string

	protocol string // negotiated at initialize, or Resumed
}

// How often an idle server looks for a newer one, when stdin allows it.
const staleEvery = 10 * time.Second

// The protocol versions we accept from a client, newest first.
var versions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in and writes replies to out until in ends, then
// waits for calls still running. It returns nil at end of input.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		werr error
	)
	write := func(b []byte) {
		b = append(b, '\n')
		mu.Lock()
		defer mu.Unlock()
		if _, err := out.Write(b); err != nil && werr == nil {
			werr = err
		}
	}
	send := func(r response) {
		r.JSONRPC = "2.0"
		b, err := json.Marshal(r)
		if err != nil {
			b, _ = json.Marshal(response{JSONRPC: "2.0", ID: r.ID,
				Error: &rpcError{-32603, err.Error()}})
		}
		write(b)
	}

	if s.Resumed != "" {
		s.protocol = s.Resumed
		write([]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`))
	}

	// With a deadline on stdin an idle server wakes to look for a newer
	// one; without (a file, a terminal, a test pipe) it looks between
	// requests only.
	dl, _ := in.(interface{ SetReadDeadline(time.Time) error })
	if dl != nil && (s.Stale == nil || s.Reexec == nil || dl.SetReadDeadline(time.Time{}) != nil) {
		dl = nil
	}
	br := bufio.NewReaderSize(in, 64*1024)
	var part []byte // a line begun, not yet whole
	failed := false // Reexec failed: stay on this process
	for {
		// Nothing read is unanswered or unprocessed here: safe to be replaced.
		if len(part) == 0 && br.Buffered() == 0 && !failed && s.Stale != nil && s.Reexec != nil && s.Stale() {
			wg.Wait()
			failed = s.Reexec(s.protocol) != nil
		}
		if dl != nil {
			_ = dl.SetReadDeadline(time.Now().Add(staleEvery))
		}
		line, err := br.ReadBytes('\n')
		if dl != nil && errors.Is(err, os.ErrDeadlineExceeded) {
			// Idle. Keep what came of a line, and look again.
			part = append(part, line...)
			continue
		}
		if len(part) > 0 {
			line = append(part, line...)
			part = nil
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			s.handle(ctx, line, send, &wg)
		}
		if err != nil {
			wg.Wait()
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, io.EOF) {
				return werr
			}
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, line []byte, send func(response), wg *sync.WaitGroup) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		send(response{ID: json.RawMessage("null"), Error: &rpcError{codeParse, "parse error"}})
		return
	}
	notification := len(req.ID) == 0
	fail := func(code int, msg string) {
		if !notification {
			send(response{ID: req.ID, Error: &rpcError{code, msg}})
		}
	}
	reply := func(v any) {
		if !notification {
			send(response{ID: req.ID, Result: v})
		}
	}
	if req.Method == "" {
		// Not a request: a reply to something we never asked, or junk.
		fail(codeInvalidRequest, "missing method")
		return
	}

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := versions[0]
		for _, k := range versions {
			if p.ProtocolVersion == k {
				v = k
			}
		}
		s.protocol = v
		res := map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": s.Stale != nil}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		reply(res)
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		type item struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		list := make([]item, 0, len(s.Tools))
		for _, t := range s.Tools {
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			list = append(list, item{t.Name, t.Description, schema})
		}
		reply(map[string]any{"tools": list})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			fail(codeInvalidParams, "bad params")
			return
		}
		var tool *Tool
		for i := range s.Tools {
			if s.Tools[i].Name == p.Name {
				tool = &s.Tools[i]
			}
		}
		if tool == nil || tool.Handler == nil {
			fail(codeInvalidParams, "unknown tool: "+p.Name)
			return
		}
		call := func() {
			text, err := tool.Handler(ctx, p.Arguments)
			if err != nil {
				text = err.Error()
			}
			reply(map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": err != nil,
			})
		}
		if s.Serial {
			call()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			call()
		}()
	default:
		fail(codeMethodNotFound, "method not found: "+req.Method)
	}
}

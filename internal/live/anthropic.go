package live

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// The claude tap. A claude agent is started with ANTHROPIC_BASE_URL
// pointing here, under a path of its own (/a/<agent id>): its calls go on
// to the API as they came, and the streamed replies of its conversation
// are read on their way back.

const agentPrefix = "/a/"

var agentIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// tapKey keys the tap's agent and session in a call's context.
type tapKey struct{}

type tapped struct{ agent, session string }

// anthropic forwards agents' calls to upstream, tapping their turns.
func (h *Hub) anthropic(upstream *url.URL) http.Handler {
	rp := &httputil.ReverseProxy{
		FlushInterval: -1, // a stream goes on as it comes
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			// Read as it comes: not compressed. The transport still asks
			// for gzip upstream, and undoes it.
			pr.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: func(res *http.Response) error {
			t, ok := res.Request.Context().Value(tapKey{}).(tapped)
			if ok && res.StatusCode == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
				res.Body = &sseTap{ReadCloser: res.Body, ev: &anthropicEvents{hub: h, tapped: t}}
			}
			return nil
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, agentPrefix), "/")
		if !agentIDRe.MatchString(agent) {
			http.NotFound(w, r)
			return
		}
		r.URL.Path, r.URL.RawPath = "/"+rest, ""
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/messages") && conversation(r) {
			t := tapped{agent: agent, session: r.Header.Get("X-Claude-Code-Session-Id")}
			r = r.WithContext(context.WithValue(r.Context(), tapKey{}, t))
		}
		rp.ServeHTTP(w, r)
	})
}

// conversation reports whether a call is a turn of the agent's own
// conversation: streamed, with its tools. Claude's other calls (a title,
// a subagent's turns, which say cc_is_subagent) are left alone. The body
// is read, and put back as it was.
func conversation(r *http.Request) bool {
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false
	}
	var req struct {
		Stream bool              `json:"stream"`
		Tools  []json.RawMessage `json:"tools"`
		System json.RawMessage   `json:"system"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	return req.Stream && len(req.Tools) > 0 && !bytes.Contains(req.System, []byte("cc_is_subagent=true"))
}

// anthropicEvents reads the Messages API's events into the hub.
type anthropicEvents struct {
	hub *Hub
	tapped
	msg string // the message's id, from message_start
}

func (a *anthropicEvents) end() {
	if a.msg != "" {
		a.hub.End(a.agent, a.msg)
	}
}

func (a *anthropicEvents) event(data []byte) {
	var ev struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
		Block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		a.msg = ev.Message.ID
		a.hub.Begin(a.agent, a.msg, a.session)
	case "content_block_start":
		a.hub.Block(a.agent, a.msg, ev.Index, func(b *Block) {
			*b = Block{Kind: blockKind(ev.Block.Type), Tool: ev.Block.Name, ID: ev.Block.ID, Text: ev.Block.Text}
		})
	case "content_block_delta":
		more := ev.Delta.Text + ev.Delta.Thinking + ev.Delta.PartialJSON
		if more == "" {
			return // a signature: nothing to show
		}
		a.hub.Block(a.agent, a.msg, ev.Index, func(b *Block) { b.Text += more })
	case "content_block_stop":
		a.hub.Block(a.agent, a.msg, ev.Index, func(b *Block) { b.Done = true })
	case "message_stop", "error":
		a.end()
	}
}

// blockKind names an API content block the way Block does.
func blockKind(typ string) string {
	switch typ {
	case "thinking", "redacted_thinking":
		return "thinking"
	case "tool_use", "server_tool_use", "mcp_tool_use":
		return "tool"
	}
	return typ
}

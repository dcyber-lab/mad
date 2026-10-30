package live

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// The codex tap. A codex agent is started with openai_base_url pointing
// here (/o/<agent id>, see Vars): its calls go on to ChatGPT's codex
// backend when it signs in with ChatGPT, else to the OpenAI API, and the
// replies of its own thread, not a subagent's, are read on their way
// back. codex streams them over a websocket, or as server-sent events.

const codexPrefix = "/o/"

var (
	chatgptCodex = &url.URL{Scheme: "https", Host: "chatgpt.com", Path: "/backend-api/codex"}
	openaiAPI    = &url.URL{Scheme: "https", Host: "api.openai.com", Path: "/v1"}
)

func (h *Hub) openai(chatgpt, api *url.URL) http.Handler {
	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			up := api
			if pr.In.Header.Get("Chatgpt-Account-Id") != "" {
				up = chatgpt
			}
			pr.SetURL(up)
			pr.Out.Header.Del("Accept-Encoding")
			// Frames as sent: none deflated.
			pr.Out.Header.Del("Sec-Websocket-Extensions")
		},
		ModifyResponse: func(res *http.Response) error {
			t, ok := res.Request.Context().Value(tapKey{}).(tapped)
			if !ok {
				return nil
			}
			ev := &responsesEvents{hub: h, tapped: t}
			switch {
			case res.StatusCode == http.StatusSwitchingProtocols:
				if rwc, ok := res.Body.(io.ReadWriteCloser); ok {
					res.Body = &wsTap{ReadWriteCloser: rwc, ev: ev}
				}
			case res.StatusCode == http.StatusOK && res.Request.Method == http.MethodPost:
				// ChatGPT's backend streams without saying so.
				res.Body = &sseTap{ReadCloser: res.Body, ev: ev}
			}
			return nil
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, codexPrefix), "/")
		if !agentIDRe.MatchString(agent) {
			http.NotFound(w, r)
			return
		}
		r.URL.Path, r.URL.RawPath = "/"+rest, ""
		if strings.HasSuffix(r.URL.Path, "/responses") && ownThread(r.Header) {
			t := tapped{agent: agent, session: r.Header.Get("Session-Id")}
			r = r.WithContext(context.WithValue(r.Context(), tapKey{}, t))
		}
		rp.ServeHTTP(w, r)
	})
}

// ownThread reports whether a call is the agent's own thread: a subagent
// (a reviewer, say) says so, and runs in a thread of its own session.
func ownThread(h http.Header) bool {
	thread, session := h.Get("Thread-Id"), h.Get("Session-Id")
	return h.Get("X-Openai-Subagent") == "" && (thread == "" || session == "" || thread == session)
}

// responsesEvents reads the Responses API's events into the hub: a
// response is a message, its output items the blocks.
type responsesEvents struct {
	hub *Hub
	tapped
	resp string
}

func (r *responsesEvents) end() {
	if r.resp != "" {
		r.hub.End(r.agent, r.resp)
	}
}

func (r *responsesEvents) event(data []byte) {
	var ev struct {
		Type     string `json:"type"`
		Index    int    `json:"output_index"`
		Delta    string `json:"delta"`
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
		Item struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"item"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	switch ev.Type {
	case "response.created":
		r.resp = ev.Response.ID
		r.hub.Begin(r.agent, r.resp, r.session)
	case "response.output_item.added":
		r.hub.Block(r.agent, r.resp, ev.Index, func(b *Block) {
			*b = Block{Kind: itemKind(ev.Item.Type), ID: ev.Item.ID, Tool: ev.Item.Name}
			if b.Kind == "tool" && b.Tool == "" {
				b.Tool = ev.Item.Type
			}
		})
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta",
		"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		if ev.Delta != "" {
			r.hub.Block(r.agent, r.resp, ev.Index, func(b *Block) { b.Text += ev.Delta })
		}
	case "response.output_item.done":
		r.hub.Block(r.agent, r.resp, ev.Index, func(b *Block) { b.Done = true })
	case "response.completed", "response.failed", "response.incomplete", "error":
		r.end()
	}
}

// itemKind names a Responses API output item the way Block does.
func itemKind(typ string) string {
	switch typ {
	case "message":
		return "text"
	case "reasoning":
		return "thinking"
	}
	if strings.HasSuffix(typ, "_call") {
		return "tool"
	}
	return typ
}

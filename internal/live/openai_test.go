package live

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The events of one response: commentary, then a call to exec.
var responseEvents = []string{
	`{"type":"response.created","response":{"id":"resp_1"}}`,
	`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
	`{"type":"response.output_item.done","output_index":0}`,
	`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1"}}`,
	`{"type":"response.output_text.delta","output_index":1,"delta":"Listing "}`,
	`{"type":"response.output_text.delta","output_index":1,"delta":"them."}`,
	`{"type":"response.output_item.done","output_index":1}`,
	`{"type":"response.output_item.added","output_index":2,"item":{"type":"custom_tool_call","id":"ctc_1","name":"exec"}}`,
	`{"type":"response.custom_tool_call_input.delta","output_index":2,"delta":"tools.exec_command({\"cmd\":\"ls"}`,
	`{"type":"response.completed","response":{"id":"resp_1"}}`,
}

func checkResponse(t *testing.T, h *Hub, agent, session string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var m Message
	for m = h.Wait(ctx, agent, 0); !m.Done && ctx.Err() == nil; m = h.Wait(ctx, agent, m.Version) {
	}
	if m.ID != "resp_1" || m.Session != session || !m.Done || len(m.Blocks) != 3 {
		t.Fatalf("message = %+v", m)
	}
	if b := m.Blocks; b[0].Kind != "thinking" || b[1].Kind != "text" || b[1].Text != "Listing them." || b[1].ID != "msg_1" ||
		b[2].Kind != "tool" || b[2].Tool != "exec" || b[2].ID != "ctc_1" || b[2].Text != `tools.exec_command({"cmd":"ls` {
		t.Errorf("blocks = %+v", b)
	}
}

// codex streams its replies over a websocket: the proxy asks for no
// compression and reads the frames from the server; a subagent's
// connection is passed on unread.
func TestOpenAIProxyWebsocket(t *testing.T) {
	var ext []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext = append(ext, r.URL.Path+" ext="+r.Header.Get("Sec-Websocket-Extensions")+" auth="+r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		if _, _, err := c.Read(ctx); err != nil { // the request: response.create
			return
		}
		for _, ev := range responseEvents {
			if err := c.Write(ctx, websocket.MessageText, []byte(ev)); err != nil {
				return
			}
		}
		_, _, _ = c.Read(ctx) // until the client goes
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL + "/backend-api/codex")
	h := NewHub()
	proxy := httptest.NewServer(h.openai(u, u))
	defer proxy.Close()

	run := func(agent string, hdr http.Header) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hdr.Set("Authorization", "Bearer secret")
		hdr.Set("Chatgpt-Account-Id", "acct")
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(proxy.URL, "http")+"/o/"+agent+"/responses",
			&websocket.DialOptions{HTTPHeader: hdr, CompressionMode: websocket.CompressionContextTakeover})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create"}`)); err != nil {
			t.Fatal(err)
		}
		for range responseEvents {
			if _, data, err := c.Read(ctx); err != nil {
				t.Fatal(err)
			} else if !strings.HasPrefix(string(data), `{"type":"response.`) {
				t.Fatalf("the agent got %q", data)
			}
		}
		c.Close(websocket.StatusNormalClosure, "")
	}
	run("a1", http.Header{"Session-Id": {"s1"}, "Thread-Id": {"s1"}})
	checkResponse(t, h, "a1", "s1")

	run("a2", http.Header{"Session-Id": {"s1"}, "Thread-Id": {"t2"}, "X-Openai-Subagent": {"guardian"}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if m := h.Wait(ctx, "a2", 0); m.ID != "" {
		t.Errorf("a subagent was read: %+v", m)
	}
	for _, e := range ext {
		if want := "/backend-api/codex/responses ext= auth=Bearer secret"; e != want {
			t.Errorf("upstream saw %q, want %q", e, want)
		}
	}
}

// Without a websocket, the reply is server-sent events, which ChatGPT's
// backend sends with no content type.
func TestOpenAIProxySSE(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		for _, ev := range responseEvents {
			_, _ = io.WriteString(w, "event: x\ndata: "+ev+"\n\n")
		}
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	h := NewHub()
	proxy := httptest.NewServer(h.openai(u, u))
	defer proxy.Close()
	res, err := http.Post(proxy.URL+"/o/a1/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()
	checkResponse(t, h, "a1", "")
}

type recorder struct{ got []string }

func (r *recorder) event(data []byte) { r.got = append(r.got, string(data)) }
func (r *recorder) end()              { r.got = append(r.got, "end") }

// frame builds a websocket frame, masked when mask is given.
func frame(fin bool, op byte, payload []byte, mask []byte) []byte {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	var out []byte
	m := byte(0)
	if mask != nil {
		m = 0x80
	}
	switch {
	case len(payload) < 126:
		out = []byte{b0, m | byte(len(payload))}
	case len(payload) < 1<<16:
		out = []byte{b0, m | 126, 0, 0}
		binary.BigEndian.PutUint16(out[2:], uint16(len(payload)))
	default:
		out = make([]byte, 10)
		out[0], out[1] = b0, m|127
		binary.BigEndian.PutUint64(out[2:], uint64(len(payload)))
	}
	if mask != nil {
		out = append(out, mask...)
		p := append([]byte(nil), payload...)
		for i := range p {
			p[i] ^= mask[i%4]
		}
		payload = p
	}
	return append(out, payload...)
}

// Frames come in pieces: a message in fragments, a ping between them, a
// masked one, one with a 16-bit length.
func TestWsTapFrames(t *testing.T) {
	long := strings.Repeat("x", 300)
	stream := bytes.Join([][]byte{
		frame(false, 0x1, []byte(`{"a":`), nil),
		frame(true, 0x9, []byte("ping"), nil),
		frame(true, 0x0, []byte(`1}`), nil),
		frame(true, 0x1, []byte(`{"b":2}`), []byte{1, 2, 3, 4}),
		frame(true, 0x2, []byte{0xff}, nil),
		frame(true, 0x1, []byte(long), nil),
	}, nil)
	r := &recorder{}
	tap := &wsTap{ReadWriteCloser: nopRWC{bytes.NewReader(stream)}, ev: r}
	out, err := io.ReadAll(iotestOneByte{tap})
	if err != nil || !bytes.Equal(out, stream) {
		t.Fatalf("passed on %d of %d bytes, %v", len(out), len(stream), err)
	}
	if want := []string{`{"a":1}`, `{"b":2}`, long, "end"}; strings.Join(r.got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %q", r.got)
	}
}

type nopRWC struct{ io.Reader }

func (nopRWC) Write(p []byte) (int, error) { return len(p), nil }
func (nopRWC) Close() error                { return nil }

// iotestOneByte reads one byte at a time: frames split anywhere.
type iotestOneByte struct{ r io.Reader }

func (o iotestOneByte) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

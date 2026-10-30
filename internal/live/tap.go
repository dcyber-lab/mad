package live

import (
	"bytes"
	"encoding/binary"
	"io"
)

// A reply is read on its way back to the agent, as server-sent events
// (sseTap) or websocket messages (wsTap). Each event is JSON, handed to
// what reads that API's events into the hub (anthropicEvents,
// responsesEvents).
type events interface {
	event(data []byte)
	end() // the reply stopped, however
}

// sseTap reads server-sent events.
type sseTap struct {
	io.ReadCloser
	ev    events
	buf   []byte
	ended bool
}

func (t *sseTap) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.buf = append(t.buf, bytes.ReplaceAll(p[:n], []byte("\r"), nil)...)
	for {
		i := bytes.Index(t.buf, []byte("\n\n"))
		if i < 0 {
			break
		}
		// An event's data line is the whole of it.
		for _, line := range bytes.Split(t.buf[:i], []byte("\n")) {
			if d, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				t.ev.event(bytes.TrimSpace(d))
			}
		}
		t.buf = t.buf[i+2:]
	}
	if err != nil {
		t.end()
	}
	return n, err
}

func (t *sseTap) Close() error {
	t.end()
	return t.ReadCloser.Close()
}

func (t *sseTap) end() {
	if !t.ended {
		t.ended = true
		t.ev.end()
	}
}

// wsTap reads the server's side of a websocket: its text messages. The
// agent was made to ask for no compression (permessage-deflate), so the
// frames are as sent.
type wsTap struct {
	io.ReadWriteCloser
	ev    events
	buf   []byte // frames not whole yet
	msg   []byte // a message in fragments
	off   bool   // passed on only, from a message too big to read on
	ended bool
}

// maxMessage bounds a message read: a bigger one is passed on unread.
const maxMessage = 16 << 20

func (t *wsTap) Read(p []byte) (int, error) {
	n, err := t.ReadWriteCloser.Read(p)
	if !t.off {
		t.buf = append(t.buf, p[:n]...)
		for t.frame() {
		}
	}
	if err != nil {
		t.end()
	}
	return n, err
}

// frame takes one whole frame off buf, if there is one.
func (t *wsTap) frame() bool {
	b := t.buf
	if len(b) < 2 {
		return false
	}
	fin, op := b[0]&0x80 != 0, b[0]&0x0f
	masked, size, at := b[1]&0x80 != 0, uint64(b[1]&0x7f), 2
	switch size {
	case 126:
		if len(b) < 4 {
			return false
		}
		size, at = uint64(binary.BigEndian.Uint16(b[2:])), 4
	case 127:
		if len(b) < 10 {
			return false
		}
		size, at = binary.BigEndian.Uint64(b[2:]), 10
	}
	var mask []byte
	if masked {
		if len(b) < at+4 {
			return false
		}
		mask, at = b[at:at+4], at+4
	}
	if size > maxMessage {
		t.end()
		t.off, t.buf = true, nil
		return false
	}
	if uint64(len(b)-at) < size {
		return false
	}
	payload := append([]byte(nil), b[at:at+int(size)]...)
	for i := range payload {
		if mask != nil {
			payload[i] ^= mask[i%4]
		}
	}
	t.buf = b[at+int(size):]
	switch op {
	case 0x1: // text
		t.msg = payload
	case 0x0: // continuation
		if t.msg != nil {
			t.msg = append(t.msg, payload...)
		}
	default: // binary, close, ping, pong: nothing to read
		return true
	}
	if fin && t.msg != nil {
		if len(t.msg) <= maxMessage {
			t.ev.event(t.msg)
		}
		t.msg = nil
	}
	return true
}

func (t *wsTap) Close() error {
	t.end()
	return t.ReadWriteCloser.Close()
}

func (t *wsTap) end() {
	if !t.ended {
		t.ended = true
		t.ev.end()
	}
}

package bus

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// A channel's file is only ever appended to (addMsg) or replaced whole by
// a rename (a compaction). So the messages read from it can be kept, and a
// later read parses only what was appended. A poller looking at thirty
// channels when one moved then parses one line, not every channel again.

// msgCache is what was read of one channel's file.
type msgCache struct {
	fi   os.FileInfo // as of the read, for os.SameFile
	size int64       // bytes seen, the unterminated tail included
	mod  time.Time
	off  int64  // bytes consumed: through the last newline
	last []byte // the last consumed line, newline included
	msgs []Message
	tail []Message // a last line without its newline yet; not built on
}

var (
	msgCacheMu sync.Mutex
	msgCaches  = map[string]*msgCache{}
)

// readMsgs is every message of channel ch, oldest first; a line that does
// not parse is skipped. The slice is the caller's: appending to it or
// replacing its elements leaves the cache alone (the messages' own slices
// are shared, and no caller changes them).
func readMsgs(ch string) ([]Message, error) {
	path := msgFile(ch)
	msgCacheMu.Lock()
	defer msgCacheMu.Unlock()
	c, err := readMsgsCached(path, msgCaches[path])
	if err != nil || c == nil {
		delete(msgCaches, path)
		return nil, err
	}
	msgCaches[path] = c
	out := make([]Message, 0, len(c.msgs)+len(c.tail))
	return append(append(out, c.msgs...), c.tail...), nil
}

// readMsgsCached brings c up to date with the file at path: as it is when
// nothing moved, the appended lines parsed when the file grew, and the
// whole file read again otherwise. nil when there is no file.
func readMsgsCached(path string, c *msgCache) (*msgCache, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c != nil && os.SameFile(c.fi, fi) && fi.Size() == c.size && fi.ModTime().Equal(c.mod) {
		return c, nil
	}
	if c != nil && os.SameFile(c.fi, fi) && fi.Size() >= c.off && c.off > 0 {
		if n, ok, err := appendMsgs(path, c, fi); err != nil {
			return nil, err
		} else if ok {
			return n, nil
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n := &msgCache{fi: fi, mod: fi.ModTime()}
	n.take(data)
	return n, nil
}

// appendMsgs reads what follows c.off, checking first that the line before
// it is still the one c read last (a rename can bring back an inode). ok
// false: the file is not c's plus lines, read it whole.
func appendMsgs(path string, c *msgCache, fi os.FileInfo) (*msgCache, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	start := c.off - int64(len(c.last))
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}
	if !bytes.HasPrefix(data, c.last) {
		return nil, false, nil
	}
	n := &msgCache{fi: fi, mod: fi.ModTime(), off: c.off, last: c.last}
	// The old messages are shared: a slice of them is only ever copied out.
	n.msgs = c.msgs[:len(c.msgs):len(c.msgs)]
	n.size = start
	n.take(data[len(c.last):])
	return n, true, nil
}

// take parses data, which follows c.off in the file, into c.
func (c *msgCache) take(data []byte) {
	c.size += int64(len(data))
	end := bytes.LastIndexByte(data, '\n') + 1
	for rest := data[:end]; len(rest) > 0; {
		i := bytes.IndexByte(rest, '\n')
		line := rest[:i+1]
		rest = rest[i+1:]
		if m, ok := parseMsg(line); ok {
			c.msgs = append(c.msgs, m)
		}
		c.last = line
	}
	c.off += int64(end)
	c.last = bytes.Clone(c.last)
	c.tail = nil
	if m, ok := parseMsg(data[end:]); ok {
		c.tail = []Message{m}
	}
}

func parseMsg(line []byte) (Message, bool) {
	line = bytes.TrimSuffix(line, []byte("\n"))
	var m Message
	if len(line) == 0 || json.Unmarshal(line, &m) != nil {
		return m, false
	}
	return m, true
}

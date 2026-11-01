package bus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// line is message seq with text as a line of a channel's file.
func line(t *testing.T, seq int, text string) string {
	t.Helper()
	b, err := json.Marshal(Message{Seq: seq, Kind: "text", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func texts(t *testing.T, ch string) string {
	t.Helper()
	msgs, err := readMsgs(ch)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range msgs {
		out = append(out, fmt.Sprintf("%d:%s", m.Seq, m.Text))
	}
	return strings.Join(out, " ")
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// TestReadMsgsCache: what is read of a channel's file follows every way
// the file changes, and what one caller does to its slice leaves the next
// caller's alone.
func TestReadMsgsCache(t *testing.T) {
	setup(t)
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := msgFile("c")
	check := func(step, want string) {
		t.Helper()
		if got := texts(t, "c"); got != want {
			t.Fatalf("%s: got %q, want %q", step, got, want)
		}
	}

	check("no file", "")
	appendTo(t, path, line(t, 1, "a")+line(t, 2, "b"))
	check("first read", "1:a 2:b")
	check("unmoved", "1:a 2:b")

	appendTo(t, path, line(t, 3, "c"))
	check("appended", "1:a 2:b 3:c")

	// A line being written: shown once it parses, not taken twice.
	l4 := line(t, 4, "d")
	appendTo(t, path, l4[:10])
	check("half a line", "1:a 2:b 3:c")
	appendTo(t, path, l4[10:len(l4)-1])
	check("no newline yet", "1:a 2:b 3:c 4:d")
	appendTo(t, path, "\n"+line(t, 5, "e"))
	check("line ended", "1:a 2:b 3:c 4:d 5:e")

	appendTo(t, path, "not json\n"+line(t, 6, "f"))
	check("a bad line is skipped", "1:a 2:b 3:c 4:d 5:e 6:f")

	// A compaction: a new file under the name.
	if err := writeAtomic(path, []byte(line(t, 5, "e")+line(t, 6, "f")+line(t, 7, "trimmed"))); err != nil {
		t.Fatal(err)
	}
	check("compacted", "5:e 6:f 7:trimmed")

	// The same inode written over, longer than before: not taken as an append.
	if err := os.WriteFile(path, []byte(line(t, 1, "x")+line(t, 2, "y")+line(t, 3, "z")+line(t, 4, "w")), 0o644); err != nil {
		t.Fatal(err)
	}
	check("written over", "1:x 2:y 3:z 4:w")

	// A caller's changes stay its own.
	msgs, _ := readMsgs("c")
	msgs[0].Text = "changed"
	_ = append(msgs[:1], Message{Seq: 99})
	check("after a caller changed its slice", "1:x 2:y 3:z 4:w")

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check("removed", "")
	appendTo(t, path, line(t, 1, "new"))
	check("made again", "1:new")
	if len(msgCaches) == 0 || msgCaches[filepath.Clean(path)] == nil {
		t.Fatalf("the channel is not cached")
	}
}

// TestReadMsgsCachePost: posts through the bus, a compaction among them,
// read the same through the cache as from the file afresh.
func TestReadMsgsCachePost(t *testing.T) {
	setup(t)
	maxLines, keepLines = 20, 5
	t.Cleanup(func() { maxLines, keepLines = 2000, 500 })
	if _, err := Create("c", "", []string{idArch}, me); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := PostSystem("c", fmt.Sprintf("m%d", i)); err != nil {
			t.Fatal(err)
		}
		got := texts(t, "c")
		msgCacheMu.Lock()
		delete(msgCaches, msgFile("c"))
		msgCacheMu.Unlock()
		if fresh := texts(t, "c"); got != fresh {
			t.Fatalf("after post %d: cached %q, afresh %q", i, got, fresh)
		}
	}
}

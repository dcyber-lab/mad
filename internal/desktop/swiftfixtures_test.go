package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The native apps' tests read copies of the server's answers
// (ios/*/Tests/*/Fixtures): each must still be what the server sends, and
// a fixture kept in both packages the same in both, or the two apps are
// tested against two servers.

// swiftFixtureDirs are the native test packages' fixtures, from here.
var swiftFixtureDirs = []string{
	"../../ios/MadKit/Tests/MadKitTests/Fixtures",
	"../../ios/MadShell/Tests/MadShellTests/Fixtures",
}

// swiftNotReplies are fixtures that are not an answer of the server.
var swiftNotReplies = map[string]bool{
	"at-you.json": true, // the @you rule's table, shared with the page's tests
}

// swiftKnownDifferent are fixture names allowed to differ between the
// two packages, each with why. None today.
var swiftKnownDifferent = map[string]string{}

// swiftReplyType is the type the server encodes the answer in fixture
// name with, by the name's prefix; nil when it is none known.
func swiftReplyType(name string) any {
	switch {
	case strings.HasPrefix(name, "deck-"):
		return &Snapshot{}
	case strings.HasPrefix(name, "messages-"):
		return &BusMessages{}
	case strings.HasPrefix(name, "chat-"):
		return &Chat{}
	}
	return nil
}

func TestSwiftFixturesAreServerAnswers(t *testing.T) {
	seen := map[string][]byte{} // name → the first copy's bytes
	where := map[string]string{}
	for _, dir := range swiftFixtureDirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			t.Logf("no %s: skipped", dir)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".json" {
				continue
			}
			n++
			path := filepath.Join(dir, name)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if prev, ok := seen[name]; ok {
				if why, known := swiftKnownDifferent[name]; !bytes.Equal(prev, b) && !known {
					t.Errorf("%s differs from %s: make them the same, or list it in swiftKnownDifferent", path, where[name])
				} else if known && bytes.Equal(prev, b) {
					t.Errorf("%s is the same in both packages now: take it out of swiftKnownDifferent (%s)", name, why)
				}
			} else {
				seen[name], where[name] = b, path
			}
			if swiftNotReplies[name] {
				continue
			}
			v := swiftReplyType(name)
			if v == nil {
				t.Errorf("%s: no server type known for it: name it deck-, messages- or chat-, or list it in swiftNotReplies", path)
				continue
			}
			d := json.NewDecoder(bytes.NewReader(b))
			d.DisallowUnknownFields()
			if err := d.Decode(v); err != nil {
				t.Errorf("%s does not decode into %T: %v", path, v, err)
			}
		}
		if n == 0 {
			t.Errorf("%s has no fixtures", dir)
		}
	}
	if len(seen) == 0 {
		t.Skip("no native fixtures here")
	}
}

package jqgo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This runs jq's own test suites (testdata/jq, copied from jq 1.7.1 under
// its MIT license) against jqgo. Outputs are compared as values, so object
// key order and number spelling (1.0 vs 1) do not matter. Cases that
// depend on things jqgo deliberately does differently are listed in
// testdata/jq/skip.txt with the reason.

type jqCase struct {
	file    string
	line    int
	program string
	input   string
	outputs []string
	fail    bool   // %%FAIL: the program must not compile
	failMsg string // expected compile error (informational)
}

func readJQTests(t *testing.T, path string) []jqCase {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	var cases []jqCase
	i := 0
	skipBlank := func() {
		for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], "#")) {
			i++
		}
	}
	for {
		skipBlank()
		if i >= len(lines) {
			return cases
		}
		c := jqCase{file: filepath.Base(path), line: i + 1}
		if strings.HasPrefix(lines[i], "%%FAIL") {
			c.fail = true
			i++
			c.program = lines[i]
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				c.failMsg += lines[i]
				i++
			}
			cases = append(cases, c)
			continue
		}
		c.program = lines[i]
		i++
		if i < len(lines) {
			c.input = lines[i]
			i++
		}
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !strings.HasPrefix(lines[i], "#") {
			c.outputs = append(c.outputs, lines[i])
			i++
		}
		cases = append(cases, c)
	}
}

func loadSkips(t *testing.T) map[string]string {
	skips := map[string]string{}
	f, err := os.Open("testdata/jq/skip.txt")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return skips
		}
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	reason := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "## "):
			reason = strings.TrimPrefix(line, "## ")
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#"):
		default:
			skips[line] = reason
		}
	}
	return skips
}

func runJQCase(c jqCase) error {
	q, err := Compile(c.program, WithEnviron([]string{"PAGER=less"}), WithDebugWriter(io.Discard))
	if c.fail {
		if err == nil {
			return fmt.Errorf("compiled, but jq rejects it (%s)", c.failMsg)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("compile: %v", err)
	}
	input, err := parseJSON([]byte(c.input))
	if err != nil {
		return fmt.Errorf("bad input %q: %v", c.input, err)
	}
	var want []any
	for _, o := range c.outputs {
		v, err := parseJSON([]byte(o))
		if err != nil {
			return fmt.Errorf("bad expected output %q: %v", o, err)
		}
		want = append(want, v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []any
	var runErr error
	for v, err := range q.Run(ctx, input) {
		if err != nil {
			runErr = err
			break
		}
		got = append(got, v)
		if len(got) > len(want)+10 {
			break
		}
	}
	if runErr != nil && len(got) < len(want) {
		return fmt.Errorf("error after %d output(s): %v", len(got), runErr)
	}
	if len(got) != len(want) {
		return fmt.Errorf("got %d outputs %s, want %d %s", len(got), dumpAll(got), len(want), dumpAll(want))
	}
	for i := range got {
		if !sameResult(got[i], want[i]) {
			return fmt.Errorf("output %d: got %s, want %s", i, toJSON(got[i]), toJSON(want[i]))
		}
	}
	return nil
}

// sameResult is equality where NaN equals NaN (jq prints both as null).
func sameResult(a, b any) bool {
	return equal(a, b) || toJSON(a) == toJSON(b)
}

func dumpAll(vs []any) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = toJSON(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func TestJQSuite(t *testing.T) {
	skips := loadSkips(t)
	used := map[string]bool{}
	for _, file := range []string{"jq/jq.test", "jq/man.test", "jq/onig.test", "extra.test"} {
		cases := readJQTests(t, filepath.Join("testdata", file))
		passed, skipped, failed := 0, 0, 0
		for _, c := range cases {
			if _, ok := skips[c.program]; ok {
				used[c.program] = true
				skipped++
				continue
			}
			if err := runJQCase(c); err != nil {
				failed++
				t.Errorf("%s:%d: %s\n    %v", c.file, c.line, c.program, err)
				continue
			}
			passed++
		}
		t.Logf("%s: %d passed, %d skipped, %d failed", file, passed, skipped, failed)
	}
	for prog := range skips {
		if !used[prog] {
			t.Errorf("skip.txt entry matches no test: %s", prog)
		}
	}
}

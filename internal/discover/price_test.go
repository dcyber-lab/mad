package discover

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestClaudePrice(t *testing.T) {
	cases := map[string]string{
		"claude-opus-5-5":                              "claude-opus-5-5",
		"claude-opus-5-5[1m]":                          "claude-opus-5-5",
		"claude-opus-5":                                "claude-opus-5",
		"claude-haiku-4-5-20251001":                    "claude-haiku-4-5",
		"claude-sonnet-4-20250514":                     "claude-sonnet-4",
		"us.anthropic.claude-sonnet-4-5-20250929-v1:0": "claude-sonnet-4-5",
		"claude-opus-6":                                "claude-opus", // not yet listed: the family's latest
		"claude-fable-7":                               "claude-fable",
		"<synthetic>":                                  "",
		"gpt-5.5":                                      "",
		"":                                             "",
	}
	for model, want := range cases {
		p, ok := claudePrice(model)
		if ok != (want != "") || ok && p != claudePrices[want] {
			t.Errorf("claudePrice(%q) = %v, %v; want %s", model, p, ok, want)
		}
	}
}

func TestClaudeCost(t *testing.T) {
	// claude-opus-5-5: $4 in, $20 out, $0.20 cache read per million.
	line := `{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[],
		"usage":{"input_tokens":1000,"cache_creation_input_tokens":3000,"cache_read_input_tokens":10000,"output_tokens":500,
		"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":2000},
		"server_tool_use":{"web_search_requests":2},"speed":"standard"}}}`
	// 1000×4 + 1000×4×1.25 + 2000×4×2 + 10000×0.20 + 500×20 = 37000 per
	// million, and two searches at a cent each.
	cases := map[string]float64{
		line: 0.057,
		strings.Replace(line, `"standard"`, `"fast"`, 1):         0.094,
		strings.Replace(line, `claude-opus-5-5`, `mystery-9`, 1): 0,
		// No breakdown: every cache write is for five minutes.
		strings.Replace(line, `"ephemeral_1h_input_tokens":2000`, `"x":0`, 1): 0.051,
	}
	for l, want := range cases {
		e, ok := claude{}.Parse([]byte(l))
		if !ok || e.Usage == nil {
			t.Fatalf("not parsed: %s", l)
		}
		if math.Abs(e.Usage.Cost-want) > 1e-9 {
			t.Errorf("cost %v, want %v, for %s", e.Usage.Cost, want, l)
		}
	}
}

// A response is dated by the line's timestamp, for the day's spending.
func TestClaudeParseTime(t *testing.T) {
	line := `{"type":"assistant","timestamp":"2026-09-29T16:05:07.123Z","message":{"id":"m1","model":"claude-opus-5-5",
		"content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`
	e, ok := claude{}.Parse([]byte(line))
	if want := time.Date(2026, 9, 29, 16, 5, 7, 123e6, time.UTC); !ok || !e.At.Equal(want) {
		t.Errorf("At = %v, want %v", e.At, want)
	}
}

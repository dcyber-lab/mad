package discover

import "strings"

// price is what a model charges, in USD per million tokens. Cache writes
// cost 1.25 times the input price for five minutes, twice for an hour.
type price struct{ in, out, cacheRead float64 }

// claudePrices are Anthropic's API list prices. A model not listed takes
// the price of the longest listed name it starts with, so a new release
// is priced like the latest of its family until it is added here.
var claudePrices = map[string]price{
	"claude-fable":      {10, 50, 0.25},
	"claude-fable-5-1":  {10, 50, 0.25},
	"claude-fable-5":    {10, 50, 1},
	"claude-opus":       {4, 20, 0.20},
	"claude-opus-5-5":   {4, 20, 0.20},
	"claude-opus-5":     {5, 25, 0.50},
	"claude-opus-4-8":   {5, 25, 0.50},
	"claude-opus-4-7":   {5, 25, 0.50},
	"claude-opus-4-6":   {5, 25, 0.50},
	"claude-opus-4-5":   {5, 25, 0.50},
	"claude-opus-4-1":   {15, 75, 1.50},
	"claude-opus-4":     {15, 75, 1.50},
	"claude-sonnet":     {2, 10, 0.20},
	"claude-sonnet-5-5": {2, 10, 0.20},
	"claude-sonnet-5":   {2, 10, 0.20},
	"claude-sonnet-4-6": {3, 15, 0.30},
	"claude-sonnet-4-5": {3, 15, 0.30},
	"claude-sonnet-4":   {3, 15, 0.30},
	"claude-haiku":      {1, 5, 0.10},
	"claude-haiku-4-5":  {1, 5, 0.10},
}

// webSearch is what one server-side web search costs.
const webSearch = 0.01

// claudePrice finds model's price: "claude-haiku-4-5-20251001",
// "claude-opus-5-5[1m]" and "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
// all name a listed model.
func claudePrice(model string) (price, bool) {
	i := strings.Index(model, "claude-")
	if i < 0 {
		return price{}, false
	}
	name, _, _ := strings.Cut(model[i:], "[")
	for {
		if p, ok := claudePrices[name]; ok {
			return p, true
		}
		j := strings.LastIndexByte(name, '-')
		if j < 0 {
			return price{}, false
		}
		name = name[:j]
	}
}

// claudeUsage is the usage claude logs with a response.
type claudeUsage struct {
	Input         int64  `json:"input_tokens"`
	CacheWrite    int64  `json:"cache_creation_input_tokens"`
	CacheRead     int64  `json:"cache_read_input_tokens"`
	Output        int64  `json:"output_tokens"`
	Speed         string `json:"speed"`
	CacheCreation *struct {
		Hour int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerToolUse *struct {
		WebSearches int64 `json:"web_search_requests"`
	} `json:"server_tool_use"`
}

// cost is what a response of model with usage u comes to, or 0 when the
// model has no known price. Fast mode doubles every rate.
func (u claudeUsage) cost(model string) float64 {
	p, ok := claudePrice(model)
	if !ok {
		return 0
	}
	hour := int64(0)
	if u.CacheCreation != nil {
		hour = min(u.CacheCreation.Hour, u.CacheWrite)
	}
	usd := (float64(u.Input)*p.in +
		float64(u.CacheWrite-hour)*p.in*1.25 +
		float64(hour)*p.in*2 +
		float64(u.CacheRead)*p.cacheRead +
		float64(u.Output)*p.out) / 1e6
	if u.Speed == "fast" {
		usd *= 2
	}
	if u.ServerToolUse != nil {
		usd += float64(u.ServerToolUse.WebSearches) * webSearch
	}
	return usd
}

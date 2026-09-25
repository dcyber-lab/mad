package probe

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// kv parses key=value lines. Repeated keys keep every value in order.
func kv(s string) map[string][]string {
	m := map[string][]string{}
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "="); ok {
			m[k] = append(m[k], v)
		}
	}
	return m
}

func first(m map[string][]string, k string) string {
	if v := m[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func parseFacts(s string) Facts {
	m := kv(s)
	f := Facts{
		Hostname: first(m, "hostname"),
		Arch:     first(m, "arch"),
		Kernel:   first(m, "kernel"),
		User:     first(m, "user"),
		Tools:    map[string]bool{},
	}
	for _, t := range m["tool"] {
		f.Tools[t] = true
	}
	for _, a := range m["addr"] {
		ip, dev, _ := strings.Cut(a, " ")
		f.Addrs = append(f.Addrs, Addr{IP: ip, Dev: dev})
	}
	return f
}

// Route is the parsed first line of `ip route get`.
type Route struct {
	Dev, Src, Via string
	MTU           int
}

func (r Route) String() string {
	s := "dev " + r.Dev
	if r.Src != "" {
		s += " src " + r.Src
	}
	if r.Via != "" {
		s += " via " + r.Via
	} else {
		s += " (directly connected)"
	}
	if r.MTU > 0 {
		s += fmt.Sprintf(", mtu %d", r.MTU)
	}
	return s
}

func parseRoute(line string, mtu string) Route {
	var r Route
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "dev":
			r.Dev = f[i+1]
		case "src":
			r.Src = f[i+1]
		case "via":
			r.Via = f[i+1]
		}
	}
	r.MTU, _ = strconv.Atoi(strings.TrimSpace(mtu))
	return r
}

// Ping is the summary of a ping run.
type Ping struct {
	Sent, Received int
	AvgMS          float64
}

var (
	pingCounts = regexp.MustCompile(`(\d+) packets transmitted, (\d+) (?:packets )?received`)
	pingRTT    = regexp.MustCompile(`= [\d.]+/([\d.]+)/`)
)

func parsePing(out string) (Ping, bool) {
	var p Ping
	m := pingCounts.FindStringSubmatch(out)
	if m == nil {
		return p, false
	}
	p.Sent, _ = strconv.Atoi(m[1])
	p.Received, _ = strconv.Atoi(m[2])
	if r := pingRTT.FindStringSubmatch(out); r != nil {
		p.AvgMS, _ = strconv.ParseFloat(r[1], 64)
	}
	return p, true
}

// Conn is how a TCP connect attempt ended.
type Conn string

const (
	ConnOpen    Conn = "open"
	ConnTimeout Conn = "timeout"
	ConnRefused Conn = "refused"
	ConnNoRoute Conn = "no-route"
	ConnError   Conn = "error"
)

func classifyConnect(out string) (Conn, int, string) {
	m := kv(out)
	rc, _ := strconv.Atoi(first(m, "rc"))
	ms, _ := strconv.Atoi(first(m, "ms"))
	msg := first(m, "err")
	// bash prefixes errors with "bash: connect: " / "bash: line 1: ...".
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	low := strings.ToLower(msg)
	switch {
	case first(m, "rc") == "":
		return ConnError, 0, "no result from connect script"
	case rc == 0:
		return ConnOpen, ms, ""
	case rc == 124:
		return ConnTimeout, ms, ""
	case strings.Contains(low, "refused"):
		return ConnRefused, ms, msg
	case strings.Contains(low, "no route"), strings.Contains(low, "unreachable"):
		return ConnNoRoute, ms, msg
	default:
		return ConnError, ms, msg
	}
}

func parseIperf(out string) (float64, error) {
	var r struct {
		Error string `json:"error"`
		End   struct {
			SumReceived struct {
				BitsPerSecond float64 `json:"bits_per_second"`
			} `json:"sum_received"`
		} `json:"end"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		return 0, fmt.Errorf("%s", firstLine(out))
	}
	if err := json.Unmarshal([]byte(out[start:]), &r); err != nil {
		return 0, fmt.Errorf("unreadable iperf3 output: %v", err)
	}
	if r.Error != "" {
		return 0, fmt.Errorf("%s", r.Error)
	}
	return r.End.SumReceived.BitsPerSecond, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func humanBits(bps float64) string {
	units := []string{"bit/s", "Kbit/s", "Mbit/s", "Gbit/s", "Tbit/s"}
	i := 0
	for bps >= 1000 && i < len(units)-1 {
		bps /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", bps, units[i])
}

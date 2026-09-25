package probe

import "testing"

func TestParsePing(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want Ping
		ok   bool
	}{
		{"iputils all replies", `PING 10.9.0.2 (10.9.0.2) 56(84) bytes of data.
--- 10.9.0.2 ping statistics ---
5 packets transmitted, 5 received, 0% packet loss, time 816ms
rtt min/avg/max/mdev = 0.031/0.045/0.061/0.010 ms`, Ping{5, 5, 0.045}, true},
		{"iputils loss", `5 packets transmitted, 2 received, 60% packet loss, time 4ms
rtt min/avg/max/mdev = 1.0/2.5/4.0/1.0 ms`, Ping{5, 2, 2.5}, true},
		{"busybox", `5 packets transmitted, 0 packets received, 100% packet loss`, Ping{5, 0, 0}, true},
		{"not permitted", `ping: socket: Operation not permitted`, Ping{}, false},
	}
	for _, c := range cases {
		got, ok := parsePing(c.out)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestClassifyConnect(t *testing.T) {
	cases := []struct {
		out  string
		want Conn
	}{
		{"rc=0\nms=3\nerr=\n", ConnOpen},
		{"rc=124\nms=3001\nerr=\n", ConnTimeout},
		{"rc=1\nms=1\nerr=bash: line 1: /dev/tcp/10.0.0.1/80: Connection refused\n", ConnRefused},
		{"rc=1\nms=1\nerr=bash: line 1: /dev/tcp/10.0.0.1/80: No route to host\n", ConnNoRoute},
		{"rc=1\nms=1\nerr=bash: connect: Network is unreachable\n", ConnNoRoute},
		{"rc=1\nms=1\nerr=bash: example.invalid: Name or service not known\n", ConnError},
		{"", ConnError},
	}
	for _, c := range cases {
		if got, _, _ := classifyConnect(c.out); got != c.want {
			t.Errorf("classifyConnect(%q) = %s, want %s", c.out, got, c.want)
		}
	}
}

func TestParseRoute(t *testing.T) {
	r := parseRoute("10.9.0.2 via 10.0.0.1 dev eth0 src 10.0.0.5 uid 0", "9001")
	if r != (Route{Dev: "eth0", Src: "10.0.0.5", Via: "10.0.0.1", MTU: 9001}) {
		t.Fatalf("got %+v", r)
	}
	if s := r.String(); s != "dev eth0 src 10.0.0.5 via 10.0.0.1, mtu 9001" {
		t.Fatalf("String() = %q", s)
	}
}

func TestParseFacts(t *testing.T) {
	f := parseFacts("hostname=db1\narch=aarch64\ntool=ping\ntool=ip\naddr=10.0.0.5 eth0\naddr=fd00::5 eth1\n")
	if f.Hostname != "db1" || f.Arch != "aarch64" || !f.Tools["ping"] || !f.Tools["ip"] || f.Tools["nc"] {
		t.Fatalf("got %+v", f)
	}
	if len(f.Addrs) != 2 || f.Addrs[1] != (Addr{"fd00::5", "eth1"}) {
		t.Fatalf("addrs %+v", f.Addrs)
	}
}

func TestParseIperf(t *testing.T) {
	bps, err := parseIperf(`{"start":{},"end":{"sum_received":{"bits_per_second":9.41e9}}}`)
	if err != nil || bps != 9.41e9 {
		t.Fatalf("got %v %v", bps, err)
	}
	if _, err := parseIperf(`{"error":"unable to connect to server: Connection refused"}`); err == nil {
		t.Fatal("want error")
	}
	if got := humanBits(9.41e9); got != "9.41 Gbit/s" {
		t.Fatalf("humanBits = %q", got)
	}
}

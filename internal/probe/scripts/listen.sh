# listen.sh PORT SECONDS -- accept one TCP connection on PORT.
# Prints one of: INUSE, ERR <why>, READY <tool>; after READY one of
# PEER <addr> (addr is "?" when the tool can't tell), NOCONN.
main() {
	export LC_ALL=C PATH="$PATH:/usr/sbin:/sbin"
	port=$1 secs=$2
	if command -v ss >/dev/null 2>&1 && [ -n "$(ss -Hltn "sport = :$port" 2>/dev/null)" ]; then
		echo INUSE; return 0
	fi
	if command -v python3 >/dev/null 2>&1; then
		python3 - "$port" "$secs" <<'PY'
import errno, socket, sys
port, secs = int(sys.argv[1]), float(sys.argv[2])
def bind():
    try:
        s = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
        s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        s.bind(("::", port))
        return s
    except OSError as e:
        if e.errno in (errno.EADDRINUSE, errno.EACCES):
            raise
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("0.0.0.0", port))
    return s
try:
    s = bind()
except OSError as e:
    print("INUSE" if e.errno == errno.EADDRINUSE else "ERR " + e.strerror, flush=True)
    sys.exit(0)
s.listen(1)
s.settimeout(secs)
print("READY python3", flush=True)
try:
    c, addr = s.accept()
    peer = addr[0]
    if peer.startswith("::ffff:"):
        peer = peer[7:]
    print("PEER " + peer, flush=True)
    c.close()
except socket.timeout:
    print("NOCONN", flush=True)
PY
		return 0
	fi
	if command -v socat >/dev/null 2>&1; then
		timeout "$secs" socat -T1 TCP-LISTEN:"$port",reuseaddr SYSTEM:'echo PEER $SOCAT_PEERADDR >&2' 2>&1 </dev/null &
		pid=$!
		tool=socat
	elif command -v nc >/dev/null 2>&1; then
		# OpenBSD nc takes the port as an argument, traditional nc wants -p.
		(
			timeout "$secs" nc -l "$port"
			r=$?
			# 124 is a real timeout; anything else is probably the wrong dialect.
			[ "$r" = 0 ] || [ "$r" = 124 ] && exit "$r"
			timeout "$secs" nc -l -p "$port"
		) >/dev/null 2>&1 </dev/null &
		pid=$!
		tool=nc
	else
		echo "ERR no python3, socat or nc to listen with"; return 0
	fi
	sleep 0.3
	kill -0 "$pid" 2>/dev/null || { echo "ERR $tool could not listen on $port"; return 0; }
	echo "READY $tool"
	wait "$pid"
	rc=$?
	if [ "$rc" = 124 ]; then echo NOCONN; elif [ "$tool" = nc ]; then echo "PEER ?"; fi
}
main "$@" </dev/null

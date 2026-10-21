package remote

import (
	"encoding/hex"
	"net/netip"
	"strconv"
	"strings"
)

// Listener is a TCP socket in the LISTEN state on the remote host.
type Listener struct {
	// Addr is the local address of the socket: the unspecified address for
	// a wildcard socket, or the zero Addr if it could not be parsed.
	Addr netip.Addr
	Port int
	UID  int // owner of the socket, or -1 if unknown
}

// ParseListeners parses the lines of a snapshot that was made with method.
func ParseListeners(method string, bigEndian bool, lines []string) []Listener {
	switch method {
	case "proc":
		return parseProc(lines, bigEndian)
	case "ss":
		return parseSS(lines)
	case "netstat":
		return parseNetstat(lines)
	case "lsof":
		return parseLsof(lines)
	}
	return nil
}

// parseProc parses lines such as "0100007F:1F90 1000": the local address
// from /proc/net/tcp or /proc/net/tcp6, followed by the owner's uid.
func parseProc(lines []string, bigEndian bool) []Listener {
	var out []Listener
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		addr, port, ok := parseProcAddr(f[0], bigEndian)
		if !ok {
			continue
		}
		uid, err := strconv.Atoi(f[1])
		if err != nil {
			uid = -1
		}
		out = append(out, Listener{Addr: addr, Port: port, UID: uid})
	}
	return out
}

// parseProcAddr decodes an address such as "0100007F:1F90". The kernel
// prints the address as 32-bit words in host byte order, and the port as a
// plain number.
func parseProcAddr(s string, bigEndian bool) (netip.Addr, int, bool) {
	hexAddr, hexPort, ok := strings.Cut(s, ":")
	if !ok {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil || port == 0 {
		return netip.Addr{}, 0, false
	}
	raw, err := hex.DecodeString(hexAddr)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.Addr{}, 0, false
	}
	if !bigEndian {
		for i := 0; i < len(raw); i += 4 {
			raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
		}
	}
	addr, _ := netip.AddrFromSlice(raw)
	return addr.Unmap(), int(port), true
}

// parseSS parses the output of "ss -tln".
func parseSS(lines []string) []Listener {
	var out []Listener
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == "tcp" { // Netid column
			f = f[1:]
		}
		if len(f) < 4 || f[0] != "LISTEN" {
			continue
		}
		if l, ok := parseEndpoint(f[3], false, -1); ok {
			out = append(out, l)
		}
	}
	return out
}

// parseNetstat parses lines such as "tcp4 127.0.0.1.8080": the protocol and
// local address columns of "netstat -an", as BSD and Linux print them.
func parseNetstat(lines []string) []Listener {
	var out []Listener
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if l, ok := parseEndpoint(f[1], f[0] == "tcp6", -1); ok {
			out = append(out, l)
		}
	}
	return out
}

// parseLsof parses the output of "lsof -F ptun": process sets that start
// with a "p" line and carry the owner in a "u" line, followed by file sets
// with the address type in a "t" line and the address in an "n" line.
func parseLsof(lines []string) []Listener {
	var out []Listener
	uid, ipv6 := -1, false
	for _, line := range lines {
		if line == "" {
			continue
		}
		value := line[1:]
		switch line[0] {
		case 'p':
			uid = -1
		case 'u':
			if n, err := strconv.Atoi(value); err == nil {
				uid = n
			}
		case 'f':
			ipv6 = false
		case 't':
			ipv6 = value == "IPv6"
		case 'n':
			if strings.Contains(value, "->") {
				continue
			}
			if l, ok := parseEndpoint(value, ipv6, uid); ok {
				out = append(out, l)
			}
		}
	}
	return out
}

// parseEndpoint parses an address and port such as "127.0.0.1:80",
// "[::1]:80", "*:80" and ":::80", or "127.0.0.1.80" and "*.80" as BSD
// netstat prints them. ipv6 says how to read the wildcard "*".
func parseEndpoint(s string, ipv6 bool, uid int) (Listener, bool) {
	i := strings.LastIndexAny(s, ":.")
	if i < 0 {
		return Listener{}, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil || port < 1 || port > 65535 {
		return Listener{}, false
	}
	return Listener{Addr: parseHost(s[:i], ipv6), Port: port, UID: uid}, true
}

func parseHost(h string, ipv6 bool) netip.Addr {
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if h == "*" || h == "" {
		if ipv6 {
			return netip.IPv6Unspecified()
		}
		return netip.IPv4Unspecified()
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		// IPv4 addresses may come with a zone, as in "127.0.0.53%lo".
		zone := strings.IndexByte(h, '%')
		if zone < 0 {
			return netip.Addr{}
		}
		if addr, err = netip.ParseAddr(h[:zone]); err != nil {
			return netip.Addr{}
		}
	}
	return addr.Unmap()
}

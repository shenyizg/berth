package forward

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"time"
)

// A forward listens on every loopback address. A local port is only free if
// nothing else receives connections to localhost on it: the forward must not
// take over a local service, which it could do on macOS, where a socket
// bound to 127.0.0.1 may share its port with one bound to the wildcard
// address. ssh also counts a forward as set up if any loopback address
// works, and a port taken on just one of them would reach different services
// over IPv4 and IPv6.

var haveIPv6Loopback = sync.OnceValue(func() bool {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	l.Close()
	return true
})

// LocalPortFree reports whether port is free for a forward: nothing accepts
// connections on it at 127.0.0.1 or ::1, and it can be bound on both.
func LocalPortFree(port int) bool {
	if accepting("127.0.0.1", port) || haveIPv6Loopback() && accepting("::1", port) {
		return false
	}
	l4, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	defer l4.Close()
	if !haveIPv6Loopback() {
		return true
	}
	l6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	l6.Close()
	return true
}

// accepting reports whether something accepts TCP connections on host:port.
func accepting(host string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// FreeLocalPort returns a port chosen by the system that is free for a
// forward.
func FreeLocalPort() (int, error) {
	for range 20 {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if LocalPortFree(port) {
			return port, nil
		}
	}
	return 0, errors.New("no free local port")
}

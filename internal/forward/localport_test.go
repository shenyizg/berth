package forward

import (
	"net"
	"strconv"
	"testing"
)

func listenPort(t *testing.T, network, addr string) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func TestLocalPortFree(t *testing.T) {
	ln, port := listenPort(t, "tcp4", "127.0.0.1:0")
	if LocalPortFree(port) {
		t.Errorf("port %d is in use on 127.0.0.1 but reported free", port)
	}
	ln.Close()
	if !LocalPortFree(port) {
		t.Errorf("port %d was released but is reported busy", port)
	}
}

func TestLocalPortFreeIPv6(t *testing.T) {
	if !haveIPv6Loopback() {
		t.Skip("no IPv6 loopback address")
	}
	ln, port := listenPort(t, "tcp6", "[::1]:0")
	defer ln.Close()
	if LocalPortFree(port) {
		t.Errorf("port %d is in use on ::1 but reported free", port)
	}
}

func TestAccepting(t *testing.T) {
	ln, port := listenPort(t, "tcp4", "127.0.0.1:0")
	if !accepting("127.0.0.1", port) {
		t.Errorf("accepting(127.0.0.1, %d) = false for an open listener", port)
	}
	ln.Close()
	if accepting("127.0.0.1", port) {
		t.Errorf("accepting(127.0.0.1, %d) = true after the listener closed", port)
	}
}

func TestFreeLocalPort(t *testing.T) {
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("FreeLocalPort returned %d, which cannot be bound: %v", port, err)
	}
	ln.Close()
}

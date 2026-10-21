package remote

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func l(addr string, port, uid int) Listener {
	return Listener{Addr: netip.MustParseAddr(addr), Port: port, UID: uid}
}

func lines(s string) []string {
	return strings.Split(strings.Trim(s, "\n"), "\n")
}

func TestParseProc(t *testing.T) {
	got := ParseListeners("proc", false, lines(`
0100007F:1F90 1000
00000000:0016 0
3500007F:0035 101
00000000000000000000000001000000:0277 0
00000000000000000000000000000000:0050 1000
0000000000000000FFFF00000100007F:1F91 1000
0100007F:0000 0
not-an-address 0
0100007F:1F92
`))
	want := []Listener{
		l("127.0.0.1", 8080, 1000),
		l("0.0.0.0", 22, 0),
		l("127.0.0.53", 53, 101),
		l("::1", 631, 0),
		l("::", 80, 1000),
		l("127.0.0.1", 8081, 1000),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestParseProcBigEndian(t *testing.T) {
	got := ParseListeners("proc", true, []string{"7F000001:1F90 1000", "00000000000000000000000000000001:0277 0"})
	want := []Listener{l("127.0.0.1", 8080, 1000), l("::1", 631, 0)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestParseSS(t *testing.T) {
	got := ParseListeners("ss", false, lines(`
State  Recv-Q Send-Q Local Address:Port  Peer Address:PortProcess
LISTEN 0      4096   127.0.0.53%lo:53         0.0.0.0:*
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*
LISTEN 0      128             [::]:22            [::]:*
LISTEN 0      511            [::1]:5173          [::]:*
LISTEN 0      511                *:8080             *:*
LISTEN     0      128         :::2222                      :::*
LISTEN     0      100        ::1:25                      :::*
tcp   LISTEN 0      511      127.0.0.1:3000       0.0.0.0:*
ESTAB  0      0      10.0.0.2:22        10.0.0.1:50000
`))
	want := []Listener{
		l("127.0.0.53", 53, -1),
		l("0.0.0.0", 22, -1),
		l("::", 22, -1),
		l("::1", 5173, -1),
		l("0.0.0.0", 8080, -1),
		l("::", 2222, -1),
		l("::1", 25, -1),
		l("127.0.0.1", 3000, -1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestParseNetstat(t *testing.T) {
	got := ParseListeners("netstat", false, lines(`
tcp4 127.0.0.1.22422
tcp46 *.7874
tcp6 *.49217
tcp6 ::1.5432
tcp6 fe80::1%lo0.3000
tcp 0.0.0.0:22
tcp6 :::22
tcp 127.0.0.1:631
tcp4 *.*
`))
	want := []Listener{
		l("127.0.0.1", 22422, -1),
		l("0.0.0.0", 7874, -1),
		l("::", 49217, -1),
		l("::1", 5432, -1),
		l("fe80::1%lo0", 3000, -1),
		l("0.0.0.0", 22, -1),
		l("::", 22, -1),
		l("127.0.0.1", 631, -1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestParseLsof(t *testing.T) {
	got := ParseListeners("lsof", false, lines(`
p667
u501
f10
tIPv4
n*:49217
f12
tIPv6
n*:49217
p700
u0
f5
tIPv4
n127.0.0.1:631
f6
tIPv6
n[::1]:631
p800
f3
tIPv4
n127.0.0.1:9000->127.0.0.1:50000
`))
	want := []Listener{
		l("0.0.0.0", 49217, 501),
		l("::", 49217, 501),
		l("127.0.0.1", 631, 0),
		l("::1", 631, 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestParseUnknownHost(t *testing.T) {
	got := ParseListeners("ss", false, []string{"LISTEN 0 128 some-name:8080 *:*"})
	if len(got) != 1 || got[0].Addr.IsValid() || got[0].Port != 8080 {
		t.Errorf("got %v, want one listener on port 8080 with no address", got)
	}
}

func TestParseUnknownMethod(t *testing.T) {
	if got := ParseListeners("magic", false, []string{"tcp 0.0.0.0:22"}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

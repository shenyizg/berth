package forward

import (
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shenyizg/berth/internal/remote"
)

type fakeForwarder struct {
	forwarded []Spec
	canceled  []Spec
	failLocal map[int]bool // Forward fails for these local ports
	failAll   bool
}

func (f *fakeForwarder) Forward(s Spec) error {
	if f.failAll || f.failLocal[s.Local] {
		return errors.New("port forwarding failed")
	}
	f.forwarded = append(f.forwarded, s)
	return nil
}

func (f *fakeForwarder) Cancel(s Spec) error {
	f.canceled = append(f.canceled, s)
	return nil
}

type harness struct {
	m    *Manager
	fw   *fakeForwarder
	now  time.Time
	busy map[int]bool // local ports that are taken
	next int          // last port handed out as a free port
}

func newHarness(cfg Config) *harness {
	h := &harness{
		fw:   &fakeForwarder{failLocal: map[int]bool{}},
		now:  time.Unix(1_000_000, 0),
		busy: map[int]bool{},
		next: 50000,
	}
	h.m = NewManager(cfg, h.fw)
	h.m.now = func() time.Time { return h.now }
	h.m.localFree = func(port int) bool { return !h.busy[port] }
	h.m.freePort = func() (int, error) {
		h.next++
		return h.next, nil
	}
	return h
}

// update applies a snapshot and summarizes the events: "+3000" for a
// forward to the same local port, "+5000>50001" for one to another port,
// "-3000" for a removed forward and "!3000" for a failure.
func (h *harness) update(listeners ...remote.Listener) string {
	var out []string
	for _, ev := range h.m.Update(listeners) {
		switch ev.Kind {
		case Forwarded:
			s := fmt.Sprintf("+%d", ev.Spec.Port)
			if ev.Spec.Local != ev.Spec.Port {
				s += fmt.Sprintf(">%d", ev.Spec.Local)
			}
			out = append(out, s)
		case Stopped:
			out = append(out, fmt.Sprintf("-%d", ev.Spec.Port))
		case Failed:
			out = append(out, fmt.Sprintf("!%d", ev.Spec.Port))
		}
	}
	return strings.Join(out, " ")
}

func (h *harness) expect(t *testing.T, want string, listeners ...remote.Listener) {
	t.Helper()
	if got := h.update(listeners...); got != want {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func lis(port, uid int) remote.Listener {
	return remote.Listener{Addr: netip.MustParseAddr("127.0.0.1"), Port: port, UID: uid}
}

func TestScopeMine(t *testing.T) {
	h := newHarness(Config{Scope: ScopeMine, UID: 1000})
	h.expect(t, "+3000", lis(22, 0), lis(3000, 1000), lis(5432, 999))
	h.expect(t, "+8080", lis(22, 0), lis(3000, 1000), lis(5432, 999), lis(8080, 1000))
}

func TestScopeNew(t *testing.T) {
	h := newHarness(Config{Scope: ScopeNew})
	h.expect(t, "", lis(22, -1), lis(5432, -1))
	h.expect(t, "+3000", lis(22, -1), lis(5432, -1), lis(3000, -1))
	// A port from the first snapshot that closes and opens again is new.
	h.expect(t, "", lis(22, -1), lis(3000, -1))
	h.expect(t, "+5432", lis(22, -1), lis(3000, -1), lis(5432, -1))
}

func TestScopeAllAndNone(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll})
	h.expect(t, "+22 +80", lis(80, 0), lis(22, 0))

	var include PortSet
	include.Add(8888, 8888)
	h = newHarness(Config{Scope: ScopeNone, Include: include})
	h.expect(t, "+8888", lis(22, 0), lis(8888, 1000), lis(3000, 1000))
}

func TestIncludeOverridesScope(t *testing.T) {
	var include PortSet
	include.Add(8888, 8888)
	h := newHarness(Config{Scope: ScopeNew, Include: include})
	h.expect(t, "+8888", lis(22, -1), lis(8888, -1))
}

func TestExcludeAndReserved(t *testing.T) {
	var exclude, reserved PortSet
	exclude.Add(22, 22)
	reserved.Add(8080, 8080)
	exclude.Add(8888, 8888)
	h := newHarness(Config{Scope: ScopeAll, Exclude: exclude, Reserved: reserved, Include: PortSet{ranges: []portRange{{8888, 8888}}}})
	h.expect(t, "+3000", lis(22, 0), lis(8080, 0), lis(3000, 0), lis(8888, 0))
}

func TestBusyLocalPort(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll})
	h.busy[5000] = true
	h.expect(t, "+5000>50001", lis(5000, 0))
	want := []Spec{{Local: 50001, Host: "127.0.0.1", Port: 5000}}
	if got := h.m.Forwards(); !reflect.DeepEqual(got, want) {
		t.Errorf("Forwards() = %v, want %v", got, want)
	}
}

func TestSamePort(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll, SamePort: true})
	h.busy[5000] = true
	h.expect(t, "!5000", lis(5000, 0))
	h.expect(t, "", lis(5000, 0)) // not retried while it stays open
	h.expect(t, "")
	h.expect(t, "!5000", lis(5000, 0)) // retried after it reopens
}

func TestForwardRaceFallsBack(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll})
	h.fw.failLocal[3000] = true // taken between the check and ssh's bind
	h.expect(t, "+3000>50001", lis(3000, 0))
}

func TestForwardFailure(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll})
	h.fw.failAll = true
	h.expect(t, "!3000", lis(3000, 0))
	h.expect(t, "", lis(3000, 0))
	h.fw.failAll = false
	h.expect(t, "")
	h.expect(t, "+3000", lis(3000, 0))
}

func TestLinger(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll, Linger: 5 * time.Second})
	h.expect(t, "+3000", lis(3000, 0))

	// A restart within the linger time keeps the forward.
	h.now = h.now.Add(time.Second)
	h.expect(t, "")
	h.now = h.now.Add(3 * time.Second)
	h.expect(t, "", lis(3000, 0))

	h.now = h.now.Add(time.Second)
	h.expect(t, "")
	h.now = h.now.Add(4 * time.Second)
	h.expect(t, "")
	h.now = h.now.Add(time.Second)
	h.expect(t, "-3000")
	if want := []Spec{{3000, "127.0.0.1", 3000}}; !reflect.DeepEqual(h.fw.canceled, want) {
		t.Errorf("canceled %v, want %v", h.fw.canceled, want)
	}
	if got := h.m.Forwards(); len(got) != 0 {
		t.Errorf("Forwards() = %v, want none", got)
	}
}

// When the remote host shares the local network, berth's own forwards show
// up among the remote ports. They must not be forwarded again.
func TestOwnForwardsAreIgnored(t *testing.T) {
	h := newHarness(Config{Scope: ScopeAll})
	h.busy[5000] = true
	h.expect(t, "+5000>50001", lis(5000, 0))
	h.expect(t, "", lis(5000, 0), lis(50001, 0))

	// The remote host may still report the local end after the forward is
	// gone.
	h.expect(t, "-5000", lis(50001, 0))
	h.now = h.now.Add(10 * time.Second)
	h.expect(t, "", lis(50001, 0))
	h.now = h.now.Add(releaseGuard)
	h.expect(t, "+50001", lis(50001, 0))
}

func TestTarget(t *testing.T) {
	addr := func(s string) remote.Listener {
		var a netip.Addr
		if s != "" {
			a = netip.MustParseAddr(s)
		}
		return remote.Listener{Addr: a, Port: 1}
	}
	tests := []struct {
		addrs []string
		want  string
	}{
		{[]string{"0.0.0.0"}, "127.0.0.1"},
		{[]string{"127.0.0.1"}, "127.0.0.1"},
		{[]string{"127.0.0.53"}, "127.0.0.53"},
		{[]string{"::"}, "::1"},
		{[]string{"::1"}, "::1"},
		{[]string{"10.0.0.5"}, "10.0.0.5"},
		{[]string{"fe80::1%eth0"}, "fe80::1%eth0"},
		{[]string{"::1", "127.0.0.1"}, "127.0.0.1"},
		{[]string{"10.0.0.5", "::"}, "::1"},
		{[]string{""}, "localhost"},
		{[]string{"", "10.0.0.5"}, "10.0.0.5"},
	}
	for _, tt := range tests {
		var ls []remote.Listener
		for _, a := range tt.addrs {
			ls = append(ls, addr(a))
		}
		if got := target(ls); got != tt.want {
			t.Errorf("target(%q) = %q, want %q", tt.addrs, got, tt.want)
		}
	}
}

func TestSpecArg(t *testing.T) {
	tests := map[Spec]string{
		{3000, "127.0.0.1", 3000}:  "localhost:3000:127.0.0.1:3000",
		{50001, "::1", 5000}:       "localhost:50001:[::1]:5000",
		{8080, "fe80::1%eth0", 80}: "localhost:8080:[fe80::1%eth0]:80",
		{9000, "localhost", 9000}:  "localhost:9000:localhost:9000",
	}
	for spec, want := range tests {
		if got := spec.Arg(); got != want {
			t.Errorf("%+v.Arg() = %q, want %q", spec, got, want)
		}
	}
}

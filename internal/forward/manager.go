// Package forward decides which remote ports to forward and keeps the
// forwards of an SSH connection in line with the ports that are listening on
// the remote host.
package forward

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/shenyizg/berth/internal/remote"
)

// Spec is a local forward: connections to localhost:Local are forwarded to
// Host:Port as seen from the remote host.
type Spec struct {
	Local int
	Host  string
	Port  int
}

// Arg formats the spec as an argument for ssh -L. The forward listens on
// the loopback addresses only, whatever GatewayPorts says.
func (s Spec) Arg() string {
	host := s.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("localhost:%d:%s:%d", s.Local, host, s.Port)
}

// Forwarder adds and removes forwards on an SSH connection.
type Forwarder interface {
	Forward(Spec) error
	Cancel(Spec) error
}

// EventKind is the kind of an Event.
type EventKind int

const (
	// Forwarded means that a forward was set up.
	Forwarded EventKind = iota + 1
	// Stopped means that a forward was removed because its remote port
	// closed.
	Stopped
	// Failed means that a port could not be forwarded. The port is tried
	// again after it closes and opens again.
	Failed
)

// Event reports a change made by Manager.Update.
type Event struct {
	Kind EventKind
	Spec Spec
	Err  error // set for Failed
}

// Config controls a Manager.
type Config struct {
	// Scope selects the ports to forward. It must be resolved, not
	// ScopeAuto.
	Scope Scope
	// UID is the remote login user, for ScopeMine.
	UID int
	// Include lists ports that are forwarded whenever they listen,
	// whatever the scope.
	Include PortSet
	// Exclude lists ports that are never forwarded.
	Exclude PortSet
	// Reserved lists local ports that the SSH connection uses already,
	// such as the user's own -L forwards. Remote ports with these numbers
	// are ignored, so that a connection to the local host does not forward
	// its own forwards.
	Reserved PortSet
	// SamePort disables the fallback to another local port when the
	// remote port number is not available locally.
	SamePort bool
	// Linger is how long a forward is kept after its remote port closes,
	// so that a server that restarts keeps its forward.
	Linger time.Duration
}

// Manager keeps the forwards of an SSH connection in line with the ports
// that are listening on the remote host.
type Manager struct {
	cfg Config
	fw  Forwarder

	// Replaceable in tests.
	now       func() time.Time
	localFree func(port int) bool
	freePort  func() (int, error)

	started  bool
	baseline map[int]bool // ports that listened before the first snapshot
	active   map[int]*entry
	failed   map[int]bool
	released map[int]time.Time // local ends of removed forwards, see isOwnLocalPort
}

// releaseGuard is how long the local end of a removed forward still counts
// as one of berth's own ports, since the remote host may keep reporting it
// for a moment after it closed.
const releaseGuard = 30 * time.Second

type entry struct {
	spec   Spec
	closed time.Time // when the remote port was last seen closed
}

// NewManager returns a Manager that sets up forwards with fw.
func NewManager(cfg Config, fw Forwarder) *Manager {
	return &Manager{
		cfg:       cfg,
		fw:        fw,
		now:       time.Now,
		localFree: LocalPortFree,
		freePort:  FreeLocalPort,
		baseline:  make(map[int]bool),
		active:    make(map[int]*entry),
		failed:    make(map[int]bool),
		released:  make(map[int]time.Time),
	}
}

// Update brings the forwards in line with a snapshot of the sockets that
// listen on the remote host, and reports what it changed. Calling it again
// with the same snapshot removes forwards whose linger time has passed.
func (m *Manager) Update(listeners []remote.Listener) []Event {
	now := m.now()
	byPort := make(map[int][]remote.Listener)
	for _, l := range listeners {
		byPort[l.Port] = append(byPort[l.Port], l)
	}
	if !m.started {
		m.started = true
		for port := range byPort {
			m.baseline[port] = true
		}
	}
	// A port that closes is no longer part of the baseline: if it opens
	// again, it is new.
	for port := range m.baseline {
		if _, ok := byPort[port]; !ok {
			delete(m.baseline, port)
		}
	}
	for port := range m.failed {
		if _, ok := byPort[port]; !ok {
			delete(m.failed, port)
		}
	}
	for port, t := range m.released {
		if now.Sub(t) >= releaseGuard {
			delete(m.released, port)
		}
	}

	var events []Event
	for _, port := range sortedKeys(m.active) {
		a := m.active[port]
		if ls, ok := byPort[port]; ok && m.wanted(port, ls) {
			a.closed = time.Time{}
			continue
		}
		if a.closed.IsZero() {
			a.closed = now
		}
		if now.Sub(a.closed) < m.cfg.Linger {
			continue
		}
		delete(m.active, port)
		_ = m.fw.Cancel(a.spec) // the forward is useless anyway
		if a.spec.Local != a.spec.Port {
			m.released[a.spec.Local] = now
		}
		events = append(events, Event{Kind: Stopped, Spec: a.spec})
	}
	for _, port := range sortedKeys(byPort) {
		if m.active[port] != nil || m.failed[port] || !m.wanted(port, byPort[port]) {
			continue
		}
		events = append(events, m.add(port, target(byPort[port])))
	}
	return events
}

// Forwards returns the active forwards, ordered by remote port.
func (m *Manager) Forwards() []Spec {
	var specs []Spec
	for _, port := range sortedKeys(m.active) {
		specs = append(specs, m.active[port].spec)
	}
	return specs
}

// wanted reports whether a remote port that listens on ls should be
// forwarded.
func (m *Manager) wanted(port int, ls []remote.Listener) bool {
	if m.cfg.Exclude.Contains(port) || m.cfg.Reserved.Contains(port) || m.isOwnLocalPort(port) {
		return false
	}
	if m.cfg.Include.Contains(port) {
		return true
	}
	switch m.cfg.Scope {
	case ScopeAll:
		return true
	case ScopeNew:
		return !m.baseline[port]
	case ScopeMine:
		for _, l := range ls {
			if l.UID >= 0 && l.UID == m.cfg.UID {
				return true
			}
		}
	}
	return false
}

// isOwnLocalPort reports whether port is the local end of a current or
// recently removed forward that uses a different port number remotely. If
// the remote host shares the local network, such a port shows up among the
// remote ports, and forwarding it would make a forward to itself.
func (m *Manager) isOwnLocalPort(port int) bool {
	if _, ok := m.released[port]; ok {
		return true
	}
	for remotePort, a := range m.active {
		if a.spec.Local == port && remotePort != port {
			return true
		}
	}
	return false
}

func (m *Manager) add(port int, host string) Event {
	spec := Spec{Local: port, Host: host, Port: port}
	if !m.localFree(port) {
		if m.cfg.SamePort {
			m.failed[port] = true
			return Event{Kind: Failed, Spec: spec, Err: fmt.Errorf("local port %d is not available", port)}
		}
		p, err := m.freePort()
		if err != nil {
			m.failed[port] = true
			return Event{Kind: Failed, Spec: spec, Err: err}
		}
		spec.Local = p
	}
	err := m.fw.Forward(spec)
	if err != nil && spec.Local == port && !m.cfg.SamePort {
		// The port may have been taken since it was checked.
		if p, perr := m.freePort(); perr == nil {
			spec.Local = p
			err = m.fw.Forward(spec)
		}
	}
	if err != nil {
		m.failed[port] = true
		return Event{Kind: Failed, Spec: spec, Err: err}
	}
	m.active[port] = &entry{spec: spec}
	return Event{Kind: Forwarded, Spec: spec}
}

// target returns the address that the SSH server should connect to in order
// to reach a port that listens on the given sockets.
func target(ls []remote.Listener) string {
	best, bestRank := "localhost", 99
	for _, l := range ls {
		if host, rank := targetFor(l.Addr); rank < bestRank {
			best, bestRank = host, rank
		}
	}
	return best
}

func targetFor(a netip.Addr) (host string, rank int) {
	switch {
	case !a.IsValid():
		return "localhost", 4
	case a.Is4() && a.IsUnspecified():
		return "127.0.0.1", 0
	case a.Is4() && a.IsLoopback():
		return a.String(), 1
	case a.IsUnspecified() || a.IsLoopback():
		return "::1", 2
	default:
		return a.String(), 3
	}
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

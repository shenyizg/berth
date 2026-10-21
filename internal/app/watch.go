package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shenyizg/berth/internal/forward"
	"github.com/shenyizg/berth/internal/mux"
	"github.com/shenyizg/berth/internal/remote"
)

// linger is how long a forward stays after its remote port closes, so that
// a server that restarts keeps its forward.
const linger = 5 * time.Second

// watch waits for the master connection, starts the watcher script on the
// remote host and forwards ports as it reports them, until ctx is canceled
// or the script ends.
func (s *session) watch(ctx context.Context) {
	if !s.waitForMaster(ctx) {
		return
	}
	s.out.debugf("connected; starting the port watcher on the remote host")

	cmd := s.mux.Session(remote.Command)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.out.warnf("cannot start the port watcher: %v", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.out.warnf("cannot start the port watcher: %v", err)
		return
	}
	stderr := &tail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		s.out.warnf("cannot start the port watcher: %v", err)
		return
	}
	// The script reads itself from its standard input, and exits when that
	// is closed, so it stays open until the end.
	io.WriteString(stdin, remote.Script(remote.ScriptOptions{Interval: s.opts.interval}))

	msgs := make(chan watchMsg)
	quit := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		readWatcher(remote.NewReader(stdout), msgs, quit)
	}()
	defer func() {
		close(quit)
		stdin.Close()
		select {
		case <-readDone:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
			<-readDone
		}
		cmd.Wait()
	}()

	s.follow(ctx, msgs, stderr)
}

// waitForMaster waits until the master connection is up, which is when the
// user has logged in. It returns false if ctx is canceled first.
func (s *session) waitForMaster(ctx context.Context) bool {
	delay := 50 * time.Millisecond
	for {
		if _, err := os.Stat(s.mux.Socket); err == nil && s.check(ctx) == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
		delay = min(2*delay, 250*time.Millisecond)
	}
}

// watchMsg is a message from the watcher script, or the error that ended
// its output.
type watchMsg struct {
	hello    *remote.Hello
	snapshot []remote.Listener
	err      error
	preamble []string // with err, when the script did not start
}

// readWatcher reads the watcher's messages and sends them on msgs until the
// output ends, which it reports as a last message with err set.
func readWatcher(r *remote.Reader, msgs chan<- watchMsg, quit <-chan struct{}) {
	defer close(msgs)
	send := func(m watchMsg) bool {
		select {
		case msgs <- m:
			return true
		case <-quit:
			return false
		}
	}
	hello, err := r.ReadHello()
	if err != nil {
		send(watchMsg{err: err, preamble: r.Preamble()})
		return
	}
	if !send(watchMsg{hello: &hello}) {
		return
	}
	for {
		snapshot, err := r.ReadSnapshot()
		if err != nil {
			send(watchMsg{err: err})
			return
		}
		if !send(watchMsg{snapshot: snapshot}) {
			return
		}
	}
}

// follow applies the watcher's reports until ctx is canceled or the
// watcher ends.
func (s *session) follow(ctx context.Context, msgs <-chan watchMsg, stderr *tail) {
	var (
		mgr  *forward.Manager
		last []remote.Listener
		seen bool
	)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			// Nothing new from the remote host, but forwards of closed
			// ports may be due for removal.
			if seen {
				s.report(mgr.Update(last))
			}
		case m, ok := <-msgs:
			switch {
			case !ok:
				return
			case m.err != nil:
				if ctx.Err() == nil {
					s.watcherEnded(m, mgr != nil, stderr.String())
				}
				return
			case m.hello != nil:
				if sameHost(*m.hello) {
					s.out.noticef("%s is this machine, so its ports are here already; not forwarding them", s.args.Destination)
					return
				}
				mgr = s.newManager(*m.hello)
			default:
				last = m.snapshot
				s.report(mgr.Update(last))
				if !seen {
					seen = true
					s.out.debugf("port watcher ready; %d listening sockets", len(last))
				}
			}
		}
	}
}

// sameHost reports whether the remote host is this machine, in the same
// network namespace. Its ports are reachable already, and forwarding them
// could make a forward connect to itself. BERTH_TEST_SAME_HOST=1 turns the
// check off for berth's own tests, which connect to the local host.
func sameHost(h remote.Hello) bool {
	if os.Getenv("BERTH_TEST_SAME_HOST") == "1" {
		return false
	}
	return h.HostID != "" && h.HostID == remote.LocalHostID()
}

func (s *session) newManager(h remote.Hello) *forward.Manager {
	scope := s.opts.scope.Resolve(h.Owners(), h.UID)
	if s.opts.scope == forward.ScopeMine && scope != forward.ScopeMine {
		s.out.warnf("%s does not report who owns each port; forwarding ports that open from now on instead", s.args.Destination)
	}
	s.out.debugf("remote host: %s, uid %d, ports listed with %s; forwarding scope %s", h.OS, h.UID, h.Method, scope)

	// The user's remote forwards and sshd's X11 display ports listen on
	// the remote host on behalf of this connection; they are not services
	// to bring back.
	var exclude forward.PortSet
	exclude.AddSet(s.opts.exclude)
	for _, p := range s.args.RemoteForwardPorts() {
		exclude.Add(p, p)
	}
	if s.args.X11() {
		exclude.Add(6010, 6099)
	}
	var reserved forward.PortSet
	for _, p := range s.args.LocalForwardPorts() {
		reserved.Add(p, p)
	}
	return forward.NewManager(forward.Config{
		Scope:    scope,
		UID:      h.UID,
		Include:  s.opts.include,
		Exclude:  exclude,
		Reserved: reserved,
		SamePort: s.opts.samePort,
		Linger:   linger,
	}, muxForwarder{s.mux})
}

// report tells the user about forwards that were added or failed.
func (s *session) report(events []forward.Event) {
	var same []string
	for _, ev := range events {
		sp := ev.Spec
		switch ev.Kind {
		case forward.Forwarded:
			s.out.debugf("forwarding localhost:%d to %s:%d", sp.Local, sp.Host, sp.Port)
			if sp.Local == sp.Port {
				same = append(same, strconv.Itoa(sp.Port))
			} else {
				s.out.event(fmt.Sprintf("forwarded remote port %d to localhost:%d (local port %d is not available)", sp.Port, sp.Local, sp.Port))
			}
		case forward.Stopped:
			s.out.debugf("remote port %d closed; stopped forwarding localhost:%d", sp.Port, sp.Local)
		case forward.Failed:
			s.out.event(fmt.Sprintf("could not forward remote port %d: %v", sp.Port, ev.Err))
		}
	}
	switch len(same) {
	case 0:
	case 1:
		s.out.event(fmt.Sprintf("forwarded remote port %s to localhost:%s", same[0], same[0]))
	default:
		s.out.event(fmt.Sprintf("forwarded remote ports %s to the same local ports", strings.Join(same, ", ")))
	}
}

// watcherEnded explains why the watcher stopped on its own.
func (s *session) watcherEnded(m watchMsg, started bool, stderr string) {
	var remoteErr remote.Error
	switch {
	case errors.As(m.err, &remoteErr):
		s.out.warnf("cannot watch ports on %s: %v", s.args.Destination, remoteErr)
	case !started:
		detail := m.err.Error()
		if errors.Is(m.err, io.EOF) {
			detail = lastLine(append(m.preamble, strings.Split(stderr, "\n")...))
		}
		msg := "the port watcher did not start on " + s.args.Destination + "; ports will not be forwarded"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		s.out.warnf("%s", msg)
	default:
		s.out.debugf("port watcher stopped: %v", m.err)
	}
}

func lastLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// muxForwarder adds and removes forwards through the master connection.
type muxForwarder struct{ c *mux.Client }

func (f muxForwarder) Forward(spec forward.Spec) error { return f.do(f.c.Forward, spec) }
func (f muxForwarder) Cancel(spec forward.Spec) error  { return f.do(f.c.Cancel, spec) }

func (f muxForwarder) do(op func(context.Context, string) error, spec forward.Spec) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return op(ctx, spec.Arg())
}

// tail keeps the last few kilobytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailSize = 4096

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailSize:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

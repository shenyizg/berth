package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/shenyizg/berth/internal/mux"
	"github.com/shenyizg/berth/internal/sshargs"
)

// session is one run of ssh with automatic port forwarding.
type session struct {
	opts options
	ssh  string   // path of the ssh program
	argv []string // ssh arguments, as the user gave them
	args *sshargs.Args
	out  *console
	mux  *mux.Client
}

// run starts ssh with connection sharing, watches the remote host for
// listening ports while ssh runs, and cleans up afterwards. It returns ssh's
// exit status.
func (s *session) run() int {
	dir, err := makeSocketDir()
	if err != nil {
		s.out.warnf("cannot create a control socket (%v); running ssh without port forwarding", err)
		return runPlain(s.ssh, s.argv)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "ctl")
	s.mux = &mux.Client{Program: s.ssh, Socket: socket, Prepare: detach}
	s.out.debugf("control socket %s", socket)

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, terminationSignals...)
	defer signal.Stop(sigs)

	cmd := exec.Command(s.ssh, s.sshArgs(socket)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		s.out.warnf("%v", err)
		return 255
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ctx, stopWatching := context.WithCancel(context.Background())
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		s.watch(ctx)
	}()

	code := waitForSSH(cmd, exited, sigs)
	if code == 0 && s.args.NoSession() {
		code = s.holdOpen(sigs)
	}

	stopWatching()
	s.closeMaster()
	select {
	case <-watching:
	case <-time.After(5 * time.Second):
		s.out.debugf("the port watcher did not stop")
	}
	return code
}

// sshArgs returns the arguments for ssh: the user's, preceded by options
// that share the connection through socket. ssh uses the first value it
// gets for an option, so these take precedence over the user's settings.
//
// With ControlPersist, the master connection moves to the background after
// login, and the user's ssh process continues as a client of it. That way
// the user's session ends as soon as they log out, even though the watcher
// session is still open. berth closes the master when ssh exits, so the
// timeout only matters if berth is killed. With -N there may be no other
// session to keep the master busy, so it must not time out.
func (s *session) sshArgs(socket string) []string {
	persist := "5"
	if s.args.NoSession() {
		persist = "yes"
	}
	return append([]string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + socket,
		"-o", "ControlPersist=" + persist,
	}, s.argv...)
}

// waitForSSH waits for ssh to exit and passes on the signals that berth
// receives meanwhile. Signals from the terminal, such as Ctrl-C, reach ssh
// directly as well; getting them twice does no harm.
func waitForSSH(cmd *exec.Cmd, exited <-chan error, sigs <-chan os.Signal) int {
	for {
		select {
		case err := <-exited:
			return exitCode(err)
		case sig := <-sigs:
			_ = cmd.Process.Signal(sig)
		}
	}
}

// holdOpen keeps forwarding ports after "ssh -N" has moved to the
// background, until berth is interrupted or the connection closes.
func (s *session) holdOpen(sigs <-chan os.Signal) int {
	if err := s.check(context.Background()); err != nil {
		s.out.warnf("connection to %s closed", s.args.Destination)
		return 255
	}
	s.out.noticef("connected to %s; forwarding ports until interrupted", s.args.Destination)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sigs:
			return 0
		case <-tick.C:
			if err := s.check(context.Background()); err != nil {
				s.out.warnf("connection to %s closed", s.args.Destination)
				return 255
			}
		}
	}
}

// check reports whether the master connection is running.
func (s *session) check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.mux.Check(ctx)
}

// closeMaster closes the master connection if it is running. That ends the
// watcher session and removes all forwards.
func (s *session) closeMaster() {
	if _, err := os.Stat(s.mux.Socket); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.mux.Exit(ctx); err != nil {
		s.out.debugf("closing the connection: %v", err)
	}
}

// maxSocketPath is the longest control socket path that works everywhere.
// A socket path has room for 103 bytes on macOS and the BSDs, and ssh
// first creates the socket under a name that is 17 bytes longer.
const maxSocketPath = 103 - 17

// makeSocketDir creates a private directory for the control socket "ctl".
// The path must be short, and must not contain characters that ssh would
// split or expand in a ControlPath.
func makeSocketDir() (string, error) {
	for _, base := range []string{os.TempDir(), "/tmp"} {
		if strings.ContainsAny(base, " \t\n\"'%$") {
			continue
		}
		dir, err := os.MkdirTemp(base, "berth-")
		if err != nil {
			continue
		}
		if len(filepath.Join(dir, "ctl")) <= maxSocketPath {
			return dir, nil
		}
		os.Remove(dir)
	}
	return "", errors.New("no usable temporary directory")
}

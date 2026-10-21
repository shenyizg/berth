//go:build unix

package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// multiplexing reports whether ssh connection sharing works on this system.
const multiplexing = true

// terminationSignals are the signals that berth passes on to ssh, or that
// make it stop when ssh is already in the background.
var terminationSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// detach starts cmd in a process group of its own, so that signals from the
// terminal, such as Ctrl-C and Ctrl-Z, do not reach berth's helper processes.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// execSSH replaces berth with ssh. It returns only if that fails.
func execSSH(path string, args []string) error {
	return syscall.Exec(path, append([]string{filepath.Base(path)}, args...), os.Environ())
}

// exitCode converts the result of running ssh to an exit status. A process
// killed by signal n gets 128+n, as in the shell.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 255
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}

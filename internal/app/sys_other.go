//go:build !unix

package app

import (
	"errors"
	"os"
	"os/exec"
)

// multiplexing reports whether ssh connection sharing works on this system.
// The Windows port of OpenSSH does not support it.
const multiplexing = false

var terminationSignals = []os.Signal{os.Interrupt}

func detach(*exec.Cmd) {}

// execSSH runs ssh and exits with its status.
func execSSH(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(exitCode(cmd.Wait()))
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode()
	}
	return 255
}

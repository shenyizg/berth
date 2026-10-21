// Package app implements the berth command.
package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/shenyizg/berth/internal/sshargs"
)

const usage = `berth runs ssh and forwards remote TCP ports to this machine as soon as
something starts listening on them.

Usage:
  berth [options] [ssh options] destination [command [argument ...]]

The options below must come before the ssh arguments, which berth passes to
ssh unchanged.

Options:
  --scope MODE      which listening ports to forward (default auto):
                      mine  ports opened by the user you log in as
                      new   ports that start listening after you connect
                      all   all listening ports
                      none  only the ports given with --include
                      auto  mine if the server reports port owners and you
                            are not root, new otherwise
  --include PORTS   always forward these ports, e.g. 8888,9000-9010
  --exclude PORTS   never forward these ports
  --same-port       only forward to the same port number locally; skip ports
                    that are busy here instead of picking another number
  --interval TIME   how often the server is checked for ports (default 1s)
  --notify MODE     how to report new forwards: terminal (default), desktop
                    or none
  --quiet           same as --notify none
  --ssh PROGRAM     ssh program to run (default: $BERTH_SSH or ssh)
  --verbose         explain what berth is doing
  --version         print the version and exit
  -h, --help        print this help and exit

Examples:
  berth dev.example.com
  berth -p 2222 -J bastion me@10.0.0.7
  berth --exclude 5432,6379 dev
  berth -N dev          no shell, just forward ports until Ctrl-C
`

// Main runs berth with the given arguments and returns its exit status.
// version is the version set at build time, if any.
func Main(args []string, version string) int {
	opts, sshArgv, err := parseOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "berth: %v\nRun 'berth --help' for usage.\n", err)
		return 2
	}
	switch {
	case opts.help:
		io.WriteString(os.Stdout, usage)
		return 0
	case opts.version:
		fmt.Printf("berth %s\n", resolveVersion(version))
		return 0
	case len(sshArgv) == 0:
		io.WriteString(os.Stderr, usage)
		return 2
	}

	sshPath, err := exec.LookPath(opts.ssh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "berth: %v\n", err)
		return 255
	}
	out := newConsole(opts.notify, opts.verbose)
	parsed, err := sshargs.Parse(sshArgv)
	if err != nil {
		// Let ssh report the problem in its own words.
		out.debugf("running ssh unchanged: %v", err)
		return runPlain(sshPath, sshArgv)
	}
	if reason := parsed.Unsupported(); reason != "" {
		out.debugf("running ssh without port forwarding: %s", reason)
		return runPlain(sshPath, sshArgv)
	}
	if !multiplexing {
		out.warnf("ssh connection sharing is not available on this system; running ssh without port forwarding")
		return runPlain(sshPath, sshArgv)
	}
	s := &session{opts: opts, ssh: sshPath, argv: sshArgv, args: parsed, out: out}
	return s.run()
}

// runPlain runs ssh with the given arguments in place of berth.
func runPlain(ssh string, args []string) int {
	err := execSSH(ssh, args)
	fmt.Fprintf(os.Stderr, "berth: cannot run %s: %v\n", ssh, err)
	return 255
}

func resolveVersion(v string) string {
	if v == "" {
		v = "dev"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	return strings.TrimPrefix(v, "v")
}

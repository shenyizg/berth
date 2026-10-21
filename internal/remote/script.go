// Package remote contains the script that berth runs on the remote host to
// watch for listening TCP sockets, and the code that reads its output.
package remote

import (
	_ "embed"
	"strconv"
	"strings"
	"time"
)

//go:embed watch.sh
var scriptTemplate string

// Command is the remote command that runs the watcher script, which is sent
// on its standard input. The argument makes the process easy to recognize in
// ps(1) on the remote host.
const Command = "sh -s berth-watcher"

// Methods are the ways in which the script can list listening sockets.
var Methods = []string{"proc", "ss", "netstat", "lsof"}

// ScriptOptions configure the watcher script.
type ScriptOptions struct {
	// Interval is the time between two scans. The default is one second.
	Interval time.Duration
	// Method forces a way of listing sockets. By default the script picks
	// the first one of Methods that works on the remote host.
	Method string
	// ProcNet is the directory with the tcp and tcp6 tables of the proc
	// method. The default is /proc/net.
	ProcNet string
}

// Script returns the watcher script.
func Script(opts ScriptOptions) string {
	interval := opts.Interval
	if interval <= 0 {
		interval = time.Second
	}
	method := "auto"
	for _, m := range Methods {
		if opts.Method == m {
			method = m
		}
	}
	procNet := opts.ProcNet
	if procNet == "" {
		procNet = "/proc/net"
	}
	return strings.NewReplacer(
		"@INTERVAL@", strconv.FormatFloat(interval.Seconds(), 'f', -1, 64),
		"@METHOD@", method,
		"@PROCNET@", shellQuote(procNet),
	).Replace(scriptTemplate)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

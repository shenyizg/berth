package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// How changes to the forwarded ports are reported.
const (
	notifyTerminal = "terminal"
	notifyDesktop  = "desktop"
	notifyNone     = "none"
)

// console prints berth's messages on standard error. While ssh runs, the
// terminal may be in raw mode, where a newline does not return the cursor to
// the start of the line, so lines end in "\r\n" on a terminal.
type console struct {
	mu            sync.Mutex
	w             io.Writer
	eol           string
	notify        string
	verbose       bool
	desktopFailed atomic.Bool
}

func newConsole(notify string, verbose bool) *console {
	c := &console{w: os.Stderr, eol: "\n", notify: notify, verbose: verbose}
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		c.eol = "\r\n"
	}
	return c
}

func (c *console) print(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	io.WriteString(c.w, "berth: "+msg+c.eol)
}

// debugf prints a diagnostic message if --verbose is on.
func (c *console) debugf(format string, args ...any) {
	if c.verbose {
		c.print(fmt.Sprintf(format, args...))
	}
}

// warnf prints a problem that the user should know about.
func (c *console) warnf(format string, args ...any) {
	c.print(fmt.Sprintf(format, args...))
}

// noticef prints a status message unless notifications are turned off.
func (c *console) noticef(format string, args ...any) {
	if c.notify != notifyNone || c.verbose {
		c.print(fmt.Sprintf(format, args...))
	}
}

// event reports a change to the forwarded ports in the way the user chose.
func (c *console) event(msg string) {
	if c.notify == notifyDesktop && !c.desktopFailed.Load() {
		err := desktopNotify(msg)
		if err == nil {
			c.debugf("%s", msg)
			return
		}
		c.desktopFailed.Store(true)
		c.warnf("cannot show desktop notifications (%v); showing them here instead", err)
	}
	if c.notify != notifyNone || c.verbose {
		c.print(msg)
	}
}

// desktopNotify shows msg as a desktop notification.
func desktopNotify(msg string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		script := "display notification " + appleScriptString(msg) + ` with title "berth"`
		cmd = exec.Command("osascript", "-e", script)
	} else {
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return errors.New("notify-send is not installed")
		}
		cmd = exec.Command(path, "berth", msg)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

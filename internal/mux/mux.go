// Package mux controls an OpenSSH master connection (see ControlMaster in
// ssh_config(5)) by running ssh as a client of its control socket.
package mux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// placeholder is the destination given to ssh. ssh insists on one, but does
// not use it when it talks to a master connection.
const placeholder = "berth"

// Client runs ssh commands through the master connection that listens on a
// control socket.
type Client struct {
	Program string // the ssh executable
	Socket  string // path of the control socket

	// Prepare, if set, is called with every command before it starts.
	Prepare func(*exec.Cmd)
}

// command returns an ssh command that uses the master connection. The
// user's configuration files are ignored: nothing in them is needed to talk
// to the master, and Match exec blocks would run for every command.
func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	args = append([]string{"-F", "/dev/null", "-o", "ControlMaster=no", "-S", c.Socket}, args...)
	cmd := exec.CommandContext(ctx, c.Program, args...)
	if c.Prepare != nil {
		c.Prepare(cmd)
	}
	return cmd
}

// Check reports whether the master connection is running.
func (c *Client) Check(ctx context.Context) error {
	return c.control(ctx, "check")
}

// Exit asks the master connection to close. This ends all of its sessions
// and forwards.
func (c *Client) Exit(ctx context.Context) error {
	return c.control(ctx, "exit")
}

// Forward adds a local forward, given as an argument for ssh -L.
func (c *Client) Forward(ctx context.Context, spec string) error {
	return c.control(ctx, "forward", "-L", spec)
}

// Cancel removes a local forward that was added with the same spec.
func (c *Client) Cancel(ctx context.Context, spec string) error {
	return c.control(ctx, "cancel", "-L", spec)
}

// Session returns a command that runs remoteCommand on the remote host in a
// new session of the master connection, without a terminal.
func (c *Client) Session(remoteCommand string) *exec.Cmd {
	return c.command(context.Background(), "-T", placeholder, remoteCommand)
}

func (c *Client) control(ctx context.Context, op string, args ...string) error {
	args = append([]string{"-O", op}, args...)
	cmd := c.command(ctx, append(args, placeholder)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	msg := strings.TrimSpace(out.String())
	if ctx.Err() != nil {
		return fmt.Errorf("ssh -O %s: %w", op, ctx.Err())
	}
	// "ssh -O cancel" exits with status 0 even if the master refuses.
	if err == nil && strings.Contains(msg, "request failed") {
		err = errors.New("request failed")
	}
	if err != nil {
		if reason := reason(msg); reason != "" {
			return fmt.Errorf("ssh -O %s: %s", op, reason)
		}
		return fmt.Errorf("ssh -O %s: %w", op, err)
	}
	return nil
}

// reason extracts the cause from ssh's error output, such as "Port
// forwarding failed" from "mux_client_forward: forwarding request failed:
// Port forwarding failed".
func reason(msg string) string {
	line, _, _ := strings.Cut(msg, "\n")
	if i := strings.LastIndex(line, ": "); i >= 0 {
		line = line[i+2:]
	}
	return strings.TrimSpace(line)
}

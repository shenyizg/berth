package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenyizg/berth/internal/forward"
	"github.com/shenyizg/berth/internal/sshargs"
)

func testSession(notify string, verbose bool) (*session, *bytes.Buffer) {
	var buf bytes.Buffer
	out := &console{w: &buf, eol: "\n", notify: notify, verbose: verbose}
	return &session{out: out, args: &sshargs.Args{Destination: "dev"}}, &buf
}

func TestReport(t *testing.T) {
	s, buf := testSession(notifyTerminal, false)
	s.report([]forward.Event{
		{Kind: forward.Forwarded, Spec: forward.Spec{Local: 3000, Host: "127.0.0.1", Port: 3000}},
		{Kind: forward.Forwarded, Spec: forward.Spec{Local: 50001, Host: "::1", Port: 5000}},
		{Kind: forward.Stopped, Spec: forward.Spec{Local: 4000, Host: "127.0.0.1", Port: 4000}},
		{Kind: forward.Forwarded, Spec: forward.Spec{Local: 8080, Host: "127.0.0.1", Port: 8080}},
		{Kind: forward.Failed, Spec: forward.Spec{Local: 9000, Port: 9000}, Err: errors.New("boom")},
	})
	want := `berth: forwarded remote port 5000 to localhost:50001 (local port 5000 is not available)
berth: could not forward remote port 9000: boom
berth: forwarded remote ports 3000, 8080 to the same local ports
`
	if got := buf.String(); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}

	buf.Reset()
	s.report([]forward.Event{{Kind: forward.Forwarded, Spec: forward.Spec{Local: 3000, Host: "127.0.0.1", Port: 3000}}})
	if got, want := buf.String(), "berth: forwarded remote port 3000 to localhost:3000\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestReportQuietAndVerbose(t *testing.T) {
	events := []forward.Event{
		{Kind: forward.Forwarded, Spec: forward.Spec{Local: 3000, Host: "127.0.0.1", Port: 3000}},
		{Kind: forward.Stopped, Spec: forward.Spec{Local: 4000, Host: "127.0.0.1", Port: 4000}},
	}
	s, buf := testSession(notifyNone, false)
	s.report(events)
	if buf.Len() != 0 {
		t.Errorf("--quiet printed %q", buf.String())
	}

	s, buf = testSession(notifyNone, true)
	s.report(events)
	for _, want := range []string{
		"forwarding localhost:3000 to 127.0.0.1:3000",
		"remote port 4000 closed; stopped forwarding localhost:4000",
		"forwarded remote port 3000 to localhost:3000",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("verbose output %q lacks %q", buf.String(), want)
		}
	}
}

func TestWatcherEnded(t *testing.T) {
	s, buf := testSession(notifyTerminal, false)
	s.watcherEnded(watchMsg{err: errors.New("EOF wrapped"), preamble: nil}, true, "")
	if buf.Len() != 0 {
		t.Errorf("a watcher that ends after starting should be quiet, printed %q", buf.String())
	}
	s.watcherEnded(watchMsg{err: io.EOF, preamble: []string{"Welcome!", "sh: not found"}}, false, "")
	if got := buf.String(); !strings.Contains(got, "did not start on dev") || !strings.Contains(got, "(sh: not found)") {
		t.Errorf("output = %q", got)
	}
}

func TestMakeSocketDir(t *testing.T) {
	dir, err := makeSocketDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dir)
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", fi.Mode().Perm())
	}
	if n := len(filepath.Join(dir, "ctl")); n > maxSocketPath {
		t.Errorf("socket path is %d bytes long", n)
	}
}

package remote

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestScriptSyntax(t *testing.T) {
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(Script(ScriptOptions{}))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}

func TestScriptOptions(t *testing.T) {
	s := Script(ScriptOptions{Interval: 250 * time.Millisecond, Method: "lsof", ProcNet: "/it's/here"})
	for _, want := range []string{"interval=0.25\n", "method=lsof\n", `procnet='/it'\''s/here'` + "\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("script does not contain %q", want)
		}
	}
	if strings.Contains(s, "@INTERVAL@") || strings.Contains(s, "@METHOD@") || strings.Contains(s, "@PROCNET@") {
		t.Error("script contains placeholders")
	}

	s = Script(ScriptOptions{Method: "rm -rf"})
	if !strings.Contains(s, "interval=1\n") || !strings.Contains(s, "method=auto\n") {
		t.Error("defaults not applied")
	}
}

// watcher runs the script with sh, as berth does on the remote host.
type watcher struct {
	snapshots chan []Listener
	hello     Hello
	stdin     io.WriteCloser
	done      chan struct{} // closed when the output ends
}

func startWatcher(t *testing.T, opts ScriptOptions) *watcher {
	t.Helper()
	cmd := exec.Command("sh", "-s", "berth-watcher")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, Script(opts)); err != nil {
		t.Fatal(err)
	}
	w := &watcher{snapshots: make(chan []Listener, 100), stdin: stdin, done: make(chan struct{})}
	r := NewReader(stdout)
	if w.hello, err = r.ReadHello(); err != nil {
		t.Fatalf("ReadHello: %v (preamble %q)", err, r.Preamble())
	}
	go func() {
		defer close(w.done)
		for {
			snap, err := r.ReadSnapshot()
			if err != nil {
				return
			}
			w.snapshots <- snap
		}
	}()
	t.Cleanup(func() {
		stdin.Close()
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-w.done
		}
		cmd.Wait()
	})
	return w
}

// waitFor returns the first snapshot that satisfies ok.
func (w *watcher) waitFor(t *testing.T, what string, ok func([]Listener) bool) []Listener {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case snap := <-w.snapshots:
			if ok(snap) {
				return snap
			}
		case <-w.done:
			t.Fatalf("watcher stopped while waiting for %s", what)
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func hasPort(port int) func([]Listener) bool {
	return func(ls []Listener) bool {
		for _, l := range ls {
			if l.Port == port {
				return true
			}
		}
		return false
	}
}

func available(method string) bool {
	if method == "proc" {
		_, err := os.Stat("/proc/net/tcp")
		return err == nil
	}
	_, err := exec.LookPath(method)
	return err == nil
}

// visible reports whether the tool behind method shows a listening port when
// it runs as a child of this process. Some sandboxes hide sockets from it,
// which is not the script's fault.
func visible(t *testing.T, method string, port int) bool {
	t.Helper()
	var out []byte
	switch method {
	case "proc":
		tcp, _ := os.ReadFile("/proc/net/tcp")
		return strings.Contains(string(tcp), fmt.Sprintf(":%04X ", port))
	case "ss":
		out, _ = exec.Command("ss", "-tln").Output()
	case "netstat":
		out, _ = exec.Command("netstat", "-an").Output()
	case "lsof":
		out, _ = exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN").Output()
	}
	return strings.Contains(string(out), strconv.Itoa(port))
}

// TestScriptOnThisHost runs the script with each method that works on this
// machine and checks that it notices a new listening socket.
func TestScriptOnThisHost(t *testing.T) {
	for _, method := range append([]string{"auto"}, Methods...) {
		t.Run(method, func(t *testing.T) {
			if method != "auto" && !available(method) {
				t.Skipf("%s is not available here", method)
			}
			w := startWatcher(t, ScriptOptions{Interval: 100 * time.Millisecond, Method: method})
			if method != "auto" && w.hello.Method != method {
				t.Fatalf("method = %q, want %q", w.hello.Method, method)
			}
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			port := ln.Addr().(*net.TCPAddr).Port
			if !visible(t, w.hello.Method, port) {
				t.Skipf("%s does not show this process's sockets here", w.hello.Method)
			}

			snap := w.waitFor(t, "the new port", hasPort(port))
			for _, l := range snap {
				if l.Port != port {
					continue
				}
				if l.Addr.String() != "127.0.0.1" {
					t.Errorf("address = %v, want 127.0.0.1", l.Addr)
				}
				if w.hello.Owners() && l.UID != os.Getuid() {
					t.Errorf("uid = %d, want %d", l.UID, os.Getuid())
				}
			}

			ln.Close()
			w.waitFor(t, "the port to close", func(ls []Listener) bool { return !hasPort(port)(ls) })
		})
	}
}

func TestScriptExitsWhenInputCloses(t *testing.T) {
	w := startWatcher(t, ScriptOptions{Interval: 100 * time.Millisecond})
	w.stdin.Close()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		t.Fatal("script still running 5s after its input closed")
	}
}

func TestScriptHostID(t *testing.T) {
	w := startWatcher(t, ScriptOptions{})
	if local := LocalHostID(); local != "" && w.hello.HostID != local {
		t.Errorf("script reports host id %q, berth computes %q", w.hello.HostID, local)
	}
}

// TestScriptProc runs the proc method on made-up socket tables, so that it is
// tested on systems without /proc as well.
func TestScriptProc(t *testing.T) {
	dir := t.TempDir()
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 3500007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000   101        0 20123 1 0000000000000000 100 0 0 10 5
   1: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22345 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000000000000000 100 0 0 10 0
   3: 0F02000A:0016 0202000A:C35A 01 00000000:00000000 02:000AFB6C 00000000     0        0 44444 4 0000000000000000 20 4 30 10 -1
`
	tcp6 := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0016 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22350 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000001000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 55555 1 0000000000000000 100 0 0 10 0
`
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tcp", tcp)
	write("tcp6", tcp6)

	w := startWatcher(t, ScriptOptions{Interval: 100 * time.Millisecond, ProcNet: dir})
	if w.hello.Method != "proc" {
		t.Fatalf("method = %q, want proc", w.hello.Method)
	}
	if w.hello.BigEndian {
		t.Skip("the fixtures are little-endian")
	}
	want := []Listener{
		l("127.0.0.53", 53, 101),
		l("0.0.0.0", 22, 0),
		l("127.0.0.1", 8080, 1000),
		l("::", 22, 0),
		l("::1", 5173, 1000),
	}
	if got := w.waitFor(t, "a snapshot", func([]Listener) bool { return true }); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}

	write("tcp", tcp+"   4: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 66666 1 0000000000000000 100 0 0 10 0\n")
	w.waitFor(t, "port 3000", hasPort(3000))
}

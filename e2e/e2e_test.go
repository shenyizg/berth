// Package e2e tests the berth binary against a real sshd that runs as the
// current user on the local host. The tests need ssh, sshd and ssh-keygen,
// and run only if BERTH_E2E=1.
package e2e

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var berthBin string

func TestMain(m *testing.M) {
	if os.Getenv("BERTH_E2E") != "1" {
		fmt.Println("skipping end-to-end tests; set BERTH_E2E=1 to run them")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "berth-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	berthBin = filepath.Join(dir, "berth")
	build := exec.Command("go", "build", "-o", berthBin, "github.com/shenyizg/berth/cmd/berth")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building berth:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// server is an sshd for the current user, reachable as host "e2e" with the
// client configuration in config.
type server struct {
	config string
}

func startServer(t *testing.T) *server {
	t.Helper()
	sshd := "/usr/sbin/sshd"
	if _, err := os.Stat(sshd); err != nil {
		if sshd, err = exec.LookPath("sshd"); err != nil {
			t.Skip("sshd not found")
		}
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, key := range []string{"host_key", "user_key"} {
		out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, key)).CombinedOutput()
		if err != nil {
			t.Fatalf("ssh-keygen: %v\n%s", err, out)
		}
	}
	pub, err := os.ReadFile(filepath.Join(dir, "user_key.pub"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "authorized_keys"), string(pub))
	port := freePort(t)
	writeFile(t, filepath.Join(dir, "sshd_config"), fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile %s
UsePAM no
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AllowTcpForwarding yes
`, port, filepath.Join(dir, "host_key"), filepath.Join(dir, "sshd.pid"), filepath.Join(dir, "authorized_keys")))

	cmd := exec.Command(sshd, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	var log syncBuffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("sshd log:\n%s", log.String())
		}
	})
	eventually(t, 10*time.Second, "sshd to listen", func() bool {
		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			c.Close()
		}
		return err == nil
	})

	config := filepath.Join(dir, "ssh_config")
	writeFile(t, config, fmt.Sprintf(`Host e2e
  HostName 127.0.0.1
  Port %d
  User %s
  IdentityFile %s
  IdentitiesOnly yes
  UserKnownHostsFile %s
  StrictHostKeyChecking accept-new
  BatchMode yes
  LogLevel ERROR
`, port, me.Username, filepath.Join(dir, "user_key"), filepath.Join(dir, "known_hosts")))
	return &server{config: config}
}

// berth is a running berth process.
type berth struct {
	cmd    *exec.Cmd
	stderr *lineBuffer
	done   chan struct{}
	err    error
}

// startBerth runs berth with its own options, followed by ssh arguments
// for the test server. The scope is "new", so that the ports that were open
// before the test, such as the ones of the developer's own programs, are
// left alone.
func startBerth(t *testing.T, s *server, opts []string, sshArgs ...string) *berth {
	t.Helper()
	args := append([]string{"--scope", "new"}, opts...)
	args = append(args, "-F", s.config)
	cmd := exec.Command(berthBin, append(args, sshArgs...)...)
	// The "remote" host is this machine, which berth would detect and
	// leave alone.
	cmd.Env = append(os.Environ(), "BERTH_TEST_SAME_HOST=1")
	b := &berth{cmd: cmd, stderr: &lineBuffer{}, done: make(chan struct{})}
	cmd.Stderr = b.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		b.err = cmd.Wait()
		close(b.done)
	}()
	t.Cleanup(func() {
		select {
		case <-b.done:
		default:
			cmd.Process.Kill()
			<-b.done
		}
		if t.Failed() {
			t.Logf("berth output:\n%s", b.stderr.String())
		}
	})
	return b
}

// waitFor waits until a line of berth's error output matches pattern, and
// returns the submatches.
func (b *berth) waitFor(t *testing.T, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	var m []string
	eventually(t, 30*time.Second, fmt.Sprintf("berth to print %q", pattern), func() bool {
		m = b.stderr.find(re)
		return m != nil
	})
	return m
}

// wait waits for berth to exit and returns its exit status.
func (b *berth) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-b.done:
	case <-time.After(30 * time.Second):
		t.Fatal("berth did not exit")
	}
	var ee *exec.ExitError
	switch {
	case b.err == nil:
		return 0
	case errors.As(b.err, &ee):
		return ee.ExitCode()
	}
	t.Fatal(b.err)
	return -1
}

// serve accepts connections on 127.0.0.1 and greets each one.
func serve(t *testing.T, greeting string) (port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			io.WriteString(c, greeting+"\n")
			c.Close()
		}
	}()
	var once sync.Once
	stop = func() { once.Do(func() { ln.Close() }) }
	t.Cleanup(stop)
	return ln.Addr().(*net.TCPAddr).Port, stop
}

func greeting(t *testing.T, addr string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("connecting to %s: %v", addr, err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatalf("reading from %s: %v", addr, err)
	}
	return strings.TrimSpace(line)
}

func TestForwardsNewPort(t *testing.T) {
	s := startServer(t)
	b := startBerth(t, s, []string{"--verbose", "--interval", "200ms"}, "e2e", "sleep 60")
	b.waitFor(t, `port watcher ready`)

	port, stop := serve(t, "hello from the remote host")
	m := b.waitFor(t, fmt.Sprintf(`forwarded remote port %d to localhost:(\d+)`, port))
	local := m[1]
	// The remote host is this machine, so the port number is taken here.
	if local == strconv.Itoa(port) {
		t.Fatalf("forwarded to the same port %s, which is in use locally", local)
	}
	for _, host := range []string{"127.0.0.1", "::1"} {
		if host == "::1" && !haveIPv6Loopback() {
			continue
		}
		if got := greeting(t, net.JoinHostPort(host, local)); got != "hello from the remote host" {
			t.Errorf("via %s: got %q", host, got)
		}
	}

	stop()
	b.waitFor(t, fmt.Sprintf(`remote port %d closed; stopped forwarding localhost:%s`, port, local))
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+local, time.Second); err == nil {
		c.Close()
		t.Errorf("localhost:%s still accepts connections after the forward was removed", local)
	}
	if strings.Contains(b.stderr.String(), "forwarded remote port "+local) {
		t.Errorf("berth forwarded its own local port %s", local)
	}

	b.cmd.Process.Signal(syscall.SIGTERM)
	b.wait(t)
}

func TestExitStatus(t *testing.T) {
	s := startServer(t)
	b := startBerth(t, s, nil, "e2e", "exit 7")
	if code := b.wait(t); code != 7 {
		t.Errorf("exit status = %d, want 7", code)
	}
}

func TestNoSession(t *testing.T) {
	s := startServer(t)
	b := startBerth(t, s, []string{"--verbose", "--interval", "200ms"}, "-N", "e2e")
	socket := b.waitFor(t, `control socket (\S+)`)[1]
	b.waitFor(t, `connected to e2e; forwarding ports until interrupted`)
	b.waitFor(t, `port watcher ready`)

	port, _ := serve(t, "hello again")
	local := b.waitFor(t, fmt.Sprintf(`forwarded remote port %d to localhost:(\d+)`, port))[1]
	if got := greeting(t, "127.0.0.1:"+local); got != "hello again" {
		t.Errorf("got %q", got)
	}

	b.cmd.Process.Signal(os.Interrupt)
	if code := b.wait(t); code != 0 {
		t.Errorf("exit status = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Dir(socket)); !os.IsNotExist(err) {
		t.Errorf("control socket directory still exists: %v", err)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+local, time.Second); err == nil {
		c.Close()
		t.Errorf("localhost:%s still accepts connections after berth exited", local)
	}
}

// TestCleanupAfterKill checks that nothing is left behind when berth is
// killed: the watcher exits when its input closes, and the master
// connection times out after the session ends.
func TestCleanupAfterKill(t *testing.T) {
	s := startServer(t)
	before := countWatchers(t)
	b := startBerth(t, s, []string{"--verbose"}, "e2e", "sleep 2")
	socket := b.waitFor(t, `control socket (\S+)`)[1]
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(socket)) }) // berth cannot do it
	b.waitFor(t, `port watcher ready`)
	if n := countWatchers(t); n <= before {
		t.Fatalf("no watcher process found (%d before, %d now)", before, n)
	}

	b.cmd.Process.Kill()
	b.wait(t)
	eventually(t, 15*time.Second, "the watcher to exit", func() bool { return countWatchers(t) <= before })
	eventually(t, 30*time.Second, "the master connection to exit", func() bool {
		_, err := os.Stat(socket)
		return os.IsNotExist(err)
	})
}

func TestSameHost(t *testing.T) {
	if !haveHostID() {
		t.Skip("no host id on this system")
	}
	s := startServer(t)
	cmd := exec.Command(berthBin, "-F", s.config, "e2e", "sleep 3")
	cmd.Env = withoutEnv(os.Environ(), "BERTH_TEST_SAME_HOST")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "e2e is this machine") {
		t.Errorf("berth did not notice that it is connected to this machine:\n%s", out)
	}
}

func TestPassThrough(t *testing.T) {
	s := &server{config: filepath.Join(t.TempDir(), "ssh_config")}
	writeFile(t, s.config, "Host e2e\n  HostName 192.0.2.1\n  Port 2222\n")
	out, err := exec.Command(berthBin, "-F", s.config, "-G", "e2e").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hostname 192.0.2.1") || !strings.Contains(string(out), "port 2222") {
		t.Errorf("ssh -G output not passed through:\n%s", out)
	}
}

// countWatchers counts the processes of watcher scripts on this machine. A
// watcher may show up as several processes: the login shell, sh and the sh
// that waits for the end of its input.
func countWatchers(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", "sh -s berth-watcher").Output()
	if err != nil && len(out) == 0 {
		return 0 // pgrep exits with 1 when nothing matches
	}
	return len(strings.Fields(string(out)))
}

func haveHostID() bool {
	out, _ := exec.Command("sh", "-c", "{ cat /proc/sys/kernel/random/boot_id; sysctl -n kern.bootsessionuuid; } 2>/dev/null").Output()
	return len(bytes.TrimSpace(out)) > 0
}

func withoutEnv(env []string, name string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

func haveIPv6Loopback() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lineBuffer collects output and finds lines in it.
type lineBuffer struct {
	syncBuffer
}

func (b *lineBuffer) find(re *regexp.Regexp) []string {
	for _, line := range strings.Split(b.String(), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			return m
		}
	}
	return nil
}

// syncBuffer is a bytes.Buffer that can be written and read concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

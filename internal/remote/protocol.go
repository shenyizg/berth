package remote

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Hello describes the remote host. It is the first message of the script.
type Hello struct {
	UID       int    // id of the login user, or -1 if unknown
	Method    string // how sockets are listed: proc, ss, netstat or lsof
	OS        string // name of the operating system, as printed by uname -s
	BigEndian bool   // byte order of the addresses in /proc/net/tcp
	// HostID identifies the machine and network namespace, or is empty.
	// It matches LocalHostID when the remote host is the local host.
	HostID string
}

// Owners reports whether the snapshots include the owner of each socket.
func (h Hello) Owners() bool {
	return h.Method == "proc" || h.Method == "lsof"
}

// Error is a problem reported by the script itself.
type Error string

func (e Error) Error() string { return string(e) }

// maxPreamble is how many lines from before the hello message are kept.
const maxPreamble = 10

// Reader reads the output of the watcher script.
type Reader struct {
	sc       *bufio.Scanner
	hello    Hello
	preamble []string
}

// NewReader returns a Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return &Reader{sc: sc}
}

// Preamble returns the last lines that arrived before the hello message.
// They usually come from the remote user's shell startup files, or explain
// why the script did not start.
func (r *Reader) Preamble() []string {
	return append([]string(nil), r.preamble...)
}

// ReadHello reads up to and including the hello message.
func (r *Reader) ReadHello() (Hello, error) {
	for r.sc.Scan() {
		line := r.sc.Text()
		if msg, ok := strings.CutPrefix(line, "@berth "); ok {
			kind, rest, _ := strings.Cut(msg, " ")
			switch kind {
			case "hello":
				h, err := parseHello(rest)
				r.hello = h
				return h, err
			case "error":
				return Hello{}, Error(rest)
			}
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(r.preamble) == maxPreamble {
			r.preamble = r.preamble[1:]
		}
		r.preamble = append(r.preamble, line)
	}
	return Hello{}, r.err()
}

// ReadSnapshot reads the next list of listening sockets. It must be called
// after ReadHello.
func (r *Reader) ReadSnapshot() ([]Listener, error) {
	var lines []string
	inside := false
	for r.sc.Scan() {
		line := r.sc.Text()
		switch {
		case line == "@berth snapshot":
			inside = true
			lines = lines[:0]
		case line == "@berth end" && inside:
			return ParseListeners(r.hello.Method, r.hello.BigEndian, lines), nil
		case strings.HasPrefix(line, "@berth error "):
			return nil, Error(strings.TrimPrefix(line, "@berth error "))
		case inside && line != "":
			lines = append(lines, line)
		}
	}
	return nil, r.err()
}

func (r *Reader) err() error {
	if err := r.sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

func parseHello(s string) (Hello, error) {
	f := strings.Fields(s)
	if len(f) < 3 {
		return Hello{}, fmt.Errorf("malformed hello message %q", s)
	}
	if f[0] != "1" {
		return Hello{}, fmt.Errorf("unsupported watcher protocol %q", f[0])
	}
	h := Hello{UID: -1, Method: f[2]}
	if uid, err := strconv.Atoi(f[1]); err == nil {
		h.UID = uid
	}
	if len(f) > 3 {
		h.OS = f[3]
	}
	if len(f) > 4 {
		h.BigEndian = f[4] == "be"
	}
	if len(f) > 5 && f[5] != "-" {
		h.HostID = f[5]
	}
	return h, nil
}

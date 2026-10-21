package remote

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestReader(t *testing.T) {
	r := NewReader(strings.NewReader(`Welcome to the server!

@berth hello 1 1000 proc Linux le 0d3b6a2e-4c2fnet:[4026531840]
@berth snapshot
0100007F:1F90 1000
@berth end
@berth snapshot

@berth end
@berth error something broke
`))
	h, err := r.ReadHello()
	if err != nil {
		t.Fatal(err)
	}
	want := Hello{UID: 1000, Method: "proc", OS: "Linux", HostID: "0d3b6a2e-4c2fnet:[4026531840]"}
	if h != want {
		t.Errorf("hello = %+v, want %+v", h, want)
	}
	if got := r.Preamble(); !reflect.DeepEqual(got, []string{"Welcome to the server!"}) {
		t.Errorf("preamble = %q", got)
	}

	snap, err := r.ReadSnapshot()
	if err != nil || !reflect.DeepEqual(snap, []Listener{l("127.0.0.1", 8080, 1000)}) {
		t.Errorf("first snapshot = %v, %v", snap, err)
	}
	snap, err = r.ReadSnapshot()
	if err != nil || len(snap) != 0 {
		t.Errorf("second snapshot = %v, %v; want empty", snap, err)
	}
	_, err = r.ReadSnapshot()
	var remoteErr Error
	if !errors.As(err, &remoteErr) || remoteErr != "something broke" {
		t.Errorf("third read: err = %v, want remote error", err)
	}
	if _, err = r.ReadSnapshot(); err != io.EOF {
		t.Errorf("fourth read: err = %v, want EOF", err)
	}
}

func TestReadHello(t *testing.T) {
	tests := []struct {
		input string
		want  Hello
		err   string
	}{
		{"@berth hello 1 0 netstat Darwin le 02C7A23B\n", Hello{UID: 0, Method: "netstat", OS: "Darwin", HostID: "02C7A23B"}, ""},
		{"@berth hello 1 - ss Linux be -\n", Hello{UID: -1, Method: "ss", OS: "Linux", BigEndian: true}, ""},
		{"@berth hello 1 1000 lsof\n", Hello{UID: 1000, Method: "lsof"}, ""},
		{"@berth hello 2 1000 proc Linux le x\n", Hello{}, "unsupported watcher protocol"},
		{"@berth hello 1\n", Hello{}, "malformed hello"},
		{"@berth error no tools\n", Hello{}, "no tools"},
		{"sh: not found\n", Hello{}, "EOF"},
	}
	for _, tt := range tests {
		h, err := NewReader(strings.NewReader(tt.input)).ReadHello()
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ReadHello(%q): err = %v, want %q", tt.input, err, tt.err)
			}
			continue
		}
		if err != nil || h != tt.want {
			t.Errorf("ReadHello(%q) = %+v, %v; want %+v", tt.input, h, err, tt.want)
		}
	}
}

func TestPreambleIsBounded(t *testing.T) {
	var input strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&input, "line %d\n", i)
	}
	r := NewReader(strings.NewReader(input.String()))
	if _, err := r.ReadHello(); err != io.EOF {
		t.Fatalf("err = %v, want EOF", err)
	}
	p := r.Preamble()
	if len(p) != maxPreamble || p[len(p)-1] != "line 49" {
		t.Errorf("preamble = %q, want the last %d lines", p, maxPreamble)
	}
}

func TestOwners(t *testing.T) {
	for method, want := range map[string]bool{"proc": true, "lsof": true, "ss": false, "netstat": false} {
		if got := (Hello{Method: method}).Owners(); got != want {
			t.Errorf("Owners() for %s = %v, want %v", method, got, want)
		}
	}
}

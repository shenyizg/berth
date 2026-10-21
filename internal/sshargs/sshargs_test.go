package sshargs

import (
	"reflect"
	"strings"
	"testing"
)

func letters(a *Args) string {
	var b strings.Builder
	for _, o := range a.Options {
		b.WriteByte(o.Letter)
	}
	return b.String()
}

func TestParse(t *testing.T) {
	tests := []struct {
		argv    string
		dest    string
		command []string
		letters string
	}{
		{"host", "host", nil, ""},
		{"-p 2222 me@host", "me@host", nil, "p"},
		{"-p2222 host ls -la", "host", []string{"ls", "-la"}, "p"},
		{"host -L 8080:localhost:80 top", "host", []string{"top"}, "L"},
		{"-NfL 8080:localhost:80 host", "host", nil, "NfL"},
		{"-vvv host", "host", nil, "vvv"},
		{"-- host -v", "host", []string{"-v"}, ""},
		{"host -- -v", "host", []string{"-v"}, ""},
		{"-t host -- top -d 1", "host", []string{"top", "-d", "1"}, "t"},
		{"-o ControlMaster=no ssh://me@host:22", "ssh://me@host:22", nil, "o"},
		{"-V", "", nil, "V"},
		{"", "", nil, ""},
	}
	for _, tt := range tests {
		a, err := Parse(strings.Fields(tt.argv))
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.argv, err)
			continue
		}
		if a.Destination != tt.dest || !reflect.DeepEqual(a.Command, tt.command) || letters(a) != tt.letters {
			t.Errorf("Parse(%q) = destination %q, command %q, options %q; want %q, %q, %q",
				tt.argv, a.Destination, a.Command, letters(a), tt.dest, tt.command, tt.letters)
		}
	}
}

func TestParseValues(t *testing.T) {
	a, err := Parse([]string{"-p2222", "-NfL", "8080:localhost:80", "-o", "User me", "host"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Option{{'p', "2222"}, {'N', ""}, {'f', ""}, {'L', "8080:localhost:80"}, {'o', "User me"}}
	if !reflect.DeepEqual(a.Options, want) {
		t.Errorf("options = %v, want %v", a.Options, want)
	}
}

func TestParseErrors(t *testing.T) {
	for _, argv := range []string{"-p", "host -L", "-Z host", "-vZ host"} {
		if _, err := Parse(strings.Fields(argv)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", argv)
		}
	}
}

func TestConfig(t *testing.T) {
	a, err := Parse([]string{
		"-o", "SessionType none",
		"-osessiontype=default",
		"-o", "ForwardX11 = yes",
		"-o", `RemoteForward="9000 localhost:9000"`,
		"host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Config("SessionType"); !reflect.DeepEqual(got, []string{"none", "default"}) {
		t.Errorf("SessionType = %q", got)
	}
	if got := a.Config("forwardx11"); !reflect.DeepEqual(got, []string{"yes"}) {
		t.Errorf("ForwardX11 = %q", got)
	}
	if got := a.Config("RemoteForward"); !reflect.DeepEqual(got, []string{"9000 localhost:9000"}) {
		t.Errorf("RemoteForward = %q", got)
	}
	if got := a.Config("User"); got != nil {
		t.Errorf("User = %q, want nothing", got)
	}
}

func TestNoSession(t *testing.T) {
	tests := map[string]bool{
		"host":                        false,
		"-N host":                     true,
		"-o SessionType=none host":    true,
		"-o SessionType=default host": false,
		"host -N":                     true,
	}
	for argv, want := range tests {
		a, err := Parse(strings.Fields(argv))
		if err != nil {
			t.Fatal(err)
		}
		if got := a.NoSession(); got != want {
			t.Errorf("NoSession(%q) = %v, want %v", argv, got, want)
		}
	}
}

func TestUnsupported(t *testing.T) {
	tests := map[string]bool{
		"host":                                true,
		"-N -L 8080:localhost:80 host":        true,
		"-t -A -J bastion host top":           true,
		"-G host":                             false,
		"-V":                                  false,
		"-Q cipher":                           false,
		"-O check host":                       false,
		"-M host":                             false,
		"-S /tmp/ctl host":                    false,
		"-W localhost:22 host":                false,
		"-f host sleep 1":                     false,
		"-o ForkAfterAuthentication=yes host": false,
		"-s host sftp":                        false,
		"-o SessionType=subsystem host sftp":  false,
		"-p 22":                               false,
	}
	for argv, supported := range tests {
		a, err := Parse(strings.Fields(argv))
		if err != nil {
			t.Fatal(err)
		}
		if reason := a.Unsupported(); (reason == "") != supported {
			t.Errorf("Unsupported(%q) = %q, want supported=%v", argv, reason, supported)
		}
	}
}

func TestForwardPorts(t *testing.T) {
	a, err := Parse([]string{
		"-L", "8080:localhost:80",
		"-L", "127.0.0.1:8081:localhost:80",
		"-L", "[::1]:8082:localhost:80",
		"-L", "/tmp/local.sock:localhost:80",
		"-L", "0:localhost:80",
		"-D", "1080",
		"-D", "localhost:1081",
		"-o", "LocalForward 8083 localhost:80",
		"-o", "DynamicForward=1082",
		"-R", "9000:localhost:9000",
		"-R", "0.0.0.0:9001:localhost:80",
		"-R", "9002",
		"-R", "/tmp/remote.sock:localhost:80",
		"-R", "0:localhost:80",
		"-o", "RemoteForward 9003 localhost:80",
		"host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.LocalForwardPorts(), []int{8080, 8081, 8082, 1080, 1081, 8083, 1082}; !reflect.DeepEqual(got, want) {
		t.Errorf("LocalForwardPorts() = %v, want %v", got, want)
	}
	if got, want := a.RemoteForwardPorts(), []int{9000, 9001, 9002, 9003}; !reflect.DeepEqual(got, want) {
		t.Errorf("RemoteForwardPorts() = %v, want %v", got, want)
	}
}

func TestX11(t *testing.T) {
	tests := map[string]bool{
		"host":                     false,
		"-X host":                  true,
		"-Y host":                  true,
		"-o ForwardX11=yes host":   true,
		"-o ForwardX11=no -X host": true,
	}
	for argv, want := range tests {
		a, err := Parse(strings.Fields(argv))
		if err != nil {
			t.Fatal(err)
		}
		if got := a.X11(); got != want {
			t.Errorf("X11(%q) = %v, want %v", argv, got, want)
		}
	}
}

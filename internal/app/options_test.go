package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shenyizg/berth/internal/forward"
)

func TestParseOptions(t *testing.T) {
	o, rest, err := parseOptions(strings.Fields(
		"--scope all --exclude=22,80 --include 8888 --exclude 9000-9010 --same-port --interval 2 --notify=desktop --verbose -p 2222 host --scope"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p", "2222", "host", "--scope"}; !reflect.DeepEqual(rest, want) {
		t.Errorf("ssh arguments = %q, want %q", rest, want)
	}
	if o.scope != forward.ScopeAll || !o.samePort || !o.verbose || o.notify != notifyDesktop || o.interval != 2*time.Second {
		t.Errorf("options = %+v", o)
	}
	if o.exclude.String() != "22,80,9000-9010" || o.include.String() != "8888" {
		t.Errorf("exclude = %q, include = %q", o.exclude, o.include)
	}
}

func TestParseOptionsDefaults(t *testing.T) {
	t.Setenv("BERTH_SSH", "")
	o, rest, err := parseOptions([]string{"host", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rest, []string{"host", "--verbose"}) {
		t.Errorf("ssh arguments = %q", rest)
	}
	if o.scope != forward.ScopeAuto || o.interval != time.Second || o.notify != notifyTerminal || o.ssh != "ssh" || o.verbose {
		t.Errorf("options = %+v", o)
	}
}

func TestParseOptionsSeparator(t *testing.T) {
	o, rest, err := parseOptions([]string{"--quiet", "-h", "--", "--odd-host-name"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rest, []string{"--odd-host-name"}) || o.notify != notifyNone || !o.help {
		t.Errorf("options = %+v, ssh arguments = %q", o, rest)
	}
}

func TestParseOptionsSSH(t *testing.T) {
	t.Setenv("BERTH_SSH", "/opt/ssh/bin/ssh")
	o, _, err := parseOptions([]string{"host"})
	if err != nil || o.ssh != "/opt/ssh/bin/ssh" {
		t.Errorf("ssh = %q, %v; want the value of BERTH_SSH", o.ssh, err)
	}
	o, _, err = parseOptions([]string{"--ssh", "my-ssh", "host"})
	if err != nil || o.ssh != "my-ssh" {
		t.Errorf("ssh = %q, %v; want my-ssh", o.ssh, err)
	}
}

func TestParseOptionsErrors(t *testing.T) {
	tests := map[string]string{
		"--nope host":          "unknown option --nope",
		"--scope":              "needs a value",
		"--scope=everything":   "unknown scope",
		"--interval=10ms host": "not between",
		"--interval=soon host": "invalid duration",
		"--verbose=yes host":   "does not take a value",
		"--exclude x host":     "invalid port",
		"--notify email host":  "unknown mode",
		"--ssh= host":          "empty program name",
	}
	for args, want := range tests {
		_, _, err := parseOptions(strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseOptions(%q): err = %v, want %q", args, err, want)
		}
	}
}

func TestParseInterval(t *testing.T) {
	good := map[string]time.Duration{"1s": time.Second, "500ms": 500 * time.Millisecond, "2": 2 * time.Second, "0.5": 500 * time.Millisecond}
	for in, want := range good {
		if got, err := parseInterval(in); err != nil || got != want {
			t.Errorf("parseInterval(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"50ms", "2h", "abc", "-1"} {
		if _, err := parseInterval(in); err == nil {
			t.Errorf("parseInterval(%q) succeeded", in)
		}
	}
}

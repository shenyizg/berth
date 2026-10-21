// Package sshargs understands just enough of an ssh(1) command line for berth
// to wrap it: where the destination is, what kind of session is requested and
// which ports the user forwards explicitly. The arguments themselves are
// always passed to ssh unchanged.
package sshargs

import (
	"fmt"
	"strconv"
	"strings"
)

// Option letters, from the getopt string in OpenSSH's ssh.c.
const (
	lettersWithValue = "bceilmopBDEFIJLOPQRSwW"
	lettersFlag      = "1246afgknqstvxACGKMNTVXYy"
)

// Option is a single command-line option such as -p 2222 or -N.
type Option struct {
	Letter byte
	Value  string // empty for options without an argument
}

// Args is a parsed ssh command line.
type Args struct {
	Options     []Option
	Destination string   // empty if none was given
	Command     []string // remote command, if any
}

// Parse parses ssh's arguments. Like ssh, it accepts options both before and
// after the destination; the first non-option argument after the destination
// starts the remote command, and "--" ends option processing.
func Parse(argv []string) (*Args, error) {
	a := &Args{}
	i := 0
	for {
		n, ended, err := a.parseOptions(argv[i:])
		if err != nil {
			return nil, err
		}
		i += n
		if a.Destination != "" || i == len(argv) {
			break
		}
		a.Destination = argv[i]
		i++
		if ended {
			break
		}
	}
	if i < len(argv) {
		a.Command = argv[i:]
	}
	return a, nil
}

// parseOptions consumes options from the start of args. It returns how many
// arguments it used and whether option processing ended with "--".
func (a *Args) parseOptions(args []string) (int, bool, error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			return i + 1, true, nil
		}
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		i++
		for j := 1; j < len(arg); j++ {
			c := arg[j]
			switch {
			case strings.IndexByte(lettersFlag, c) >= 0:
				a.Options = append(a.Options, Option{Letter: c})
			case strings.IndexByte(lettersWithValue, c) >= 0:
				value := arg[j+1:]
				if value == "" {
					if i == len(args) {
						return 0, false, fmt.Errorf("option -%c requires an argument", c)
					}
					value = args[i]
					i++
				}
				a.Options = append(a.Options, Option{Letter: c, Value: value})
				j = len(arg)
			default:
				return 0, false, fmt.Errorf("unknown option -%c", c)
			}
		}
	}
	return i, false, nil
}

// Has reports whether the option letter appears on the command line.
func (a *Args) Has(letter byte) bool {
	for _, o := range a.Options {
		if o.Letter == letter {
			return true
		}
	}
	return false
}

// Values returns the arguments given to an option letter, in order.
func (a *Args) Values(letter byte) []string {
	var values []string
	for _, o := range a.Options {
		if o.Letter == letter {
			values = append(values, o.Value)
		}
	}
	return values
}

// Config returns the values given with -o for an ssh_config(5) keyword, in
// order. Keywords are case-insensitive.
func (a *Args) Config(keyword string) []string {
	var values []string
	for _, o := range a.Options {
		if o.Letter != 'o' {
			continue
		}
		if k, v := splitConfig(o.Value); strings.EqualFold(k, keyword) {
			values = append(values, v)
		}
	}
	return values
}

// configFirst returns the first value given for keyword. Like ssh, which uses
// the first value it obtains for most keywords.
func (a *Args) configFirst(keyword string) string {
	if values := a.Config(keyword); len(values) > 0 {
		return values[0]
	}
	return ""
}

// splitConfig splits "Keyword=value", "Keyword value" or "Keyword = value".
func splitConfig(s string) (keyword, value string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, "= \t")
	if i < 0 {
		return s, ""
	}
	value = strings.TrimSpace(s[i:])
	value = strings.TrimSpace(strings.TrimPrefix(value, "="))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return s[:i], value
}

func isYes(v string) bool {
	return strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
}

// NoSession reports whether no shell or command is requested (-N).
func (a *Args) NoSession() bool {
	return a.Has('N') || strings.EqualFold(a.configFirst("SessionType"), "none")
}

// X11 reports whether X11 forwarding is requested.
func (a *Args) X11() bool {
	return a.Has('X') || a.Has('Y') || isYes(a.configFirst("ForwardX11"))
}

// Unsupported returns why berth should leave this command line to ssh alone,
// or "" if berth can add port forwarding to it.
func (a *Args) Unsupported() string {
	for _, o := range a.Options {
		switch o.Letter {
		case 'G':
			return "-G only prints the configuration"
		case 'V':
			return "-V only prints the version"
		case 'Q':
			return "-Q only queries supported algorithms"
		case 'O':
			return "-O controls an existing connection"
		case 'M', 'S':
			return fmt.Sprintf("-%c configures connection sharing, which berth uses itself", o.Letter)
		case 'W':
			return "-W forwards standard input and output"
		case 'f':
			return "-f puts ssh in the background"
		case 's':
			return "-s requests a subsystem"
		}
	}
	switch {
	case isYes(a.configFirst("ForkAfterAuthentication")):
		return "ForkAfterAuthentication puts ssh in the background"
	case strings.EqualFold(a.configFirst("SessionType"), "subsystem"):
		return "SessionType requests a subsystem"
	case a.Destination == "":
		return "no destination"
	}
	return ""
}

// LocalForwardPorts returns the local ports that the user forwards
// explicitly with -L, -D, LocalForward or DynamicForward.
func (a *Args) LocalForwardPorts() []int {
	var specs []string
	specs = append(specs, a.Values('L')...)
	specs = append(specs, a.Values('D')...)
	specs = append(specs, firstFields(a.Config("LocalForward"))...)
	specs = append(specs, firstFields(a.Config("DynamicForward"))...)
	return listenPorts(specs)
}

// RemoteForwardPorts returns the remote ports that the user forwards
// explicitly with -R or RemoteForward.
func (a *Args) RemoteForwardPorts() []int {
	var specs []string
	specs = append(specs, a.Values('R')...)
	specs = append(specs, firstFields(a.Config("RemoteForward"))...)
	return listenPorts(specs)
}

// firstFields returns the first word of each value. In ssh_config(5),
// "LocalForward 8080 localhost:80" lists the listening side first.
func firstFields(values []string) []string {
	var out []string
	for _, v := range values {
		if f := strings.Fields(v); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func listenPorts(specs []string) []int {
	var ports []int
	for _, spec := range specs {
		if p, ok := listenPort(spec); ok {
			ports = append(ports, p)
		}
	}
	return ports
}

// listenPort returns the port that a forwarding specification listens on.
// "8080:host:80", "127.0.0.1:8080:host:80", "[::1]:8080:host:80", "8080"
// and "localhost:8080" all listen on 8080. Unix socket paths and port 0 (a
// port chosen by the server) have no fixed port.
func listenPort(spec string) (int, bool) {
	fields := splitSpec(spec)
	for i := 0; i < len(fields) && i < 2; i++ {
		f := fields[i]
		if strings.Contains(f, "/") {
			return 0, false
		}
		if p, err := strconv.Atoi(f); err == nil {
			return p, p > 0 && p <= 65535
		}
	}
	return 0, false
}

// splitSpec splits a forwarding specification at colons, keeping IPv6
// addresses in square brackets together and removing the brackets.
func splitSpec(spec string) []string {
	var fields []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(spec); i++ {
		switch c := spec[i]; {
		case c == '[':
			depth++
		case c == ']' && depth > 0:
			depth--
		case c == ':' && depth == 0:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(fields, cur.String())
}

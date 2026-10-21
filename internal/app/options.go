package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shenyizg/berth/internal/forward"
)

// options are berth's own command-line options.
type options struct {
	scope    forward.Scope
	include  forward.PortSet
	exclude  forward.PortSet
	samePort bool
	interval time.Duration
	notify   string
	ssh      string
	verbose  bool
	help     bool
	version  bool
}

// valueOptions are the options that take a value.
var valueOptions = map[string]bool{
	"scope":    true,
	"include":  true,
	"exclude":  true,
	"interval": true,
	"notify":   true,
	"ssh":      true,
}

// parseOptions parses berth's options and returns the arguments for ssh.
// berth's options are long options that come before the ssh arguments, so
// parsing stops at the first argument that does not start with "--", or
// after "--".
func parseOptions(args []string) (options, []string, error) {
	o := options{
		scope:    forward.ScopeAuto,
		interval: time.Second,
		notify:   notifyTerminal,
		ssh:      "ssh",
	}
	if v := os.Getenv("BERTH_SSH"); v != "" {
		o.ssh = v
	}
	i := 0
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "-h" {
			o.help = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			break
		}
		name, value, hasValue := strings.Cut(arg[2:], "=")
		if !valueOptions[name] {
			if err := o.setFlag(name); err != nil {
				return o, nil, err
			}
			if hasValue {
				return o, nil, fmt.Errorf("option --%s does not take a value", name)
			}
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return o, nil, fmt.Errorf("option --%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if err := o.setValue(name, value); err != nil {
			return o, nil, fmt.Errorf("--%s: %v", name, err)
		}
	}
	return o, args[i:], nil
}

func (o *options) setFlag(name string) error {
	switch name {
	case "same-port":
		o.samePort = true
	case "quiet":
		o.notify = notifyNone
	case "verbose":
		o.verbose = true
	case "version":
		o.version = true
	case "help":
		o.help = true
	default:
		return fmt.Errorf("unknown option --%s", name)
	}
	return nil
}

func (o *options) setValue(name, value string) error {
	var err error
	switch name {
	case "scope":
		o.scope, err = forward.ParseScope(value)
	case "include", "exclude":
		var ports forward.PortSet
		if ports, err = forward.ParsePortSet(value); err == nil && name == "include" {
			o.include.AddSet(ports)
		} else if err == nil {
			o.exclude.AddSet(ports)
		}
	case "interval":
		o.interval, err = parseInterval(value)
	case "notify":
		switch value {
		case notifyTerminal, notifyDesktop, notifyNone:
			o.notify = value
		default:
			err = fmt.Errorf("unknown mode %q (want terminal, desktop or none)", value)
		}
	case "ssh":
		if value == "" {
			err = errors.New("empty program name")
		}
		o.ssh = value
	}
	return err
}

// parseInterval parses a duration such as "2s" or "500ms". A plain number
// is a number of seconds.
func parseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		seconds, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return 0, err
		}
		d = time.Duration(seconds * float64(time.Second))
	}
	if d < 100*time.Millisecond || d > time.Hour {
		return 0, fmt.Errorf("%v is not between 100ms and 1h", d)
	}
	return d, nil
}

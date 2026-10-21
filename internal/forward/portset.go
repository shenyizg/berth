package forward

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PortSet is a set of TCP ports, written as a comma-separated list of ports
// and ranges such as "3000,8000-8100". The zero value is the empty set.
type PortSet struct {
	ranges []portRange
}

type portRange struct{ lo, hi int }

// ParsePortSet parses a list of ports and ranges such as "3000,8000-8100".
func ParsePortSet(s string) (PortSet, error) {
	var ps PortSet
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		loText, hiText, isRange := strings.Cut(item, "-")
		lo, err := parsePort(loText)
		if err != nil {
			return PortSet{}, fmt.Errorf("invalid port %q", item)
		}
		hi := lo
		if isRange {
			if hi, err = parsePort(hiText); err != nil || hi < lo {
				return PortSet{}, fmt.Errorf("invalid port range %q", item)
			}
		}
		ps.Add(lo, hi)
	}
	return ps, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 || p > 65535 {
		return 0, errors.New("invalid port")
	}
	return p, nil
}

// Add adds the ports from lo to hi, inclusive.
func (ps *PortSet) Add(lo, hi int) {
	ps.ranges = append(ps.ranges, portRange{lo, hi})
}

// AddSet adds all ports of other.
func (ps *PortSet) AddSet(other PortSet) {
	ps.ranges = append(ps.ranges, other.ranges...)
}

// Contains reports whether port is in the set.
func (ps PortSet) Contains(port int) bool {
	for _, r := range ps.ranges {
		if r.lo <= port && port <= r.hi {
			return true
		}
	}
	return false
}

// Empty reports whether the set contains no ports.
func (ps PortSet) Empty() bool {
	return len(ps.ranges) == 0
}

// String returns the set in the syntax that ParsePortSet accepts.
func (ps PortSet) String() string {
	items := make([]string, len(ps.ranges))
	for i, r := range ps.ranges {
		items[i] = strconv.Itoa(r.lo)
		if r.hi != r.lo {
			items[i] += "-" + strconv.Itoa(r.hi)
		}
	}
	return strings.Join(items, ",")
}

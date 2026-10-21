package forward

import "fmt"

// Scope selects which listening ports are forwarded automatically.
type Scope int

const (
	// ScopeAuto is ScopeMine if the remote host reports who owns each socket
	// and the login user is not root, and ScopeNew otherwise.
	ScopeAuto Scope = iota
	// ScopeMine forwards ports that belong to the remote login user.
	ScopeMine
	// ScopeNew forwards ports that start listening after the connection is
	// made.
	ScopeNew
	// ScopeAll forwards every listening port.
	ScopeAll
	// ScopeNone forwards only ports that are included explicitly.
	ScopeNone
)

var scopeNames = []string{"auto", "mine", "new", "all", "none"}

// ParseScope parses the name of a scope.
func ParseScope(s string) (Scope, error) {
	for i, name := range scopeNames {
		if s == name {
			return Scope(i), nil
		}
	}
	return 0, fmt.Errorf("unknown scope %q (want auto, mine, new, all or none)", s)
}

func (s Scope) String() string {
	if s >= 0 && int(s) < len(scopeNames) {
		return scopeNames[s]
	}
	return fmt.Sprintf("Scope(%d)", int(s))
}

// Resolve returns the scope to use on a remote host. owners reports whether
// the host tells who owns each socket, and uid is the login user's id.
// ScopeAuto, and ScopeMine on a host without owner information, become
// ScopeMine or ScopeNew.
func (s Scope) Resolve(owners bool, uid int) Scope {
	switch {
	case s == ScopeAuto && owners && uid > 0:
		return ScopeMine
	case s == ScopeAuto, s == ScopeMine && (!owners || uid < 0):
		return ScopeNew
	}
	return s
}

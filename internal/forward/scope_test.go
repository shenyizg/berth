package forward

import "testing"

func TestParseScope(t *testing.T) {
	for _, name := range []string{"auto", "mine", "new", "all", "none"} {
		s, err := ParseScope(name)
		if err != nil || s.String() != name {
			t.Errorf("ParseScope(%q) = %v, %v", name, s, err)
		}
	}
	if _, err := ParseScope("everything"); err == nil {
		t.Error("ParseScope(everything) succeeded")
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		scope  Scope
		owners bool
		uid    int
		want   Scope
	}{
		{ScopeAuto, true, 1000, ScopeMine},
		{ScopeAuto, true, 0, ScopeNew},
		{ScopeAuto, true, -1, ScopeNew},
		{ScopeAuto, false, 1000, ScopeNew},
		{ScopeMine, true, 0, ScopeMine},
		{ScopeMine, false, 1000, ScopeNew},
		{ScopeMine, true, -1, ScopeNew},
		{ScopeNew, true, 1000, ScopeNew},
		{ScopeAll, false, 1000, ScopeAll},
		{ScopeNone, true, 1000, ScopeNone},
	}
	for _, tt := range tests {
		if got := tt.scope.Resolve(tt.owners, tt.uid); got != tt.want {
			t.Errorf("%v.Resolve(%v, %d) = %v, want %v", tt.scope, tt.owners, tt.uid, got, tt.want)
		}
	}
}

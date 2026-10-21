package forward

import "testing"

func TestParsePortSet(t *testing.T) {
	tests := []struct {
		in, out string
		has     []int
		hasNot  []int
	}{
		{"3000", "3000", []int{3000}, []int{2999, 3001}},
		{"3000,8000-8100", "3000,8000-8100", []int{3000, 8000, 8050, 8100}, []int{7999, 8101}},
		{" 22 , 80 ,", "22,80", []int{22, 80}, []int{23}},
		{"", "", nil, []int{1, 65535}},
		{"1-65535", "1-65535", []int{1, 65535}, nil},
	}
	for _, tt := range tests {
		ps, err := ParsePortSet(tt.in)
		if err != nil {
			t.Errorf("ParsePortSet(%q): %v", tt.in, err)
			continue
		}
		if got := ps.String(); got != tt.out {
			t.Errorf("ParsePortSet(%q).String() = %q, want %q", tt.in, got, tt.out)
		}
		for _, p := range tt.has {
			if !ps.Contains(p) {
				t.Errorf("%q does not contain %d", tt.in, p)
			}
		}
		for _, p := range tt.hasNot {
			if ps.Contains(p) {
				t.Errorf("%q contains %d", tt.in, p)
			}
		}
		if ps.Empty() != (tt.out == "") {
			t.Errorf("ParsePortSet(%q).Empty() = %v", tt.in, ps.Empty())
		}
	}
}

func TestParsePortSetErrors(t *testing.T) {
	for _, in := range []string{"abc", "0", "65536", "9000-8000", "1-x", "-5", "80-", "1,2,three"} {
		if _, err := ParsePortSet(in); err == nil {
			t.Errorf("ParsePortSet(%q) succeeded, want error", in)
		}
	}
}

func TestAddSet(t *testing.T) {
	var a PortSet
	a.Add(22, 22)
	b, _ := ParsePortSet("8000-8010")
	a.AddSet(b)
	if !a.Contains(22) || !a.Contains(8005) || a.Contains(23) {
		t.Errorf("AddSet result %q is wrong", a)
	}
}

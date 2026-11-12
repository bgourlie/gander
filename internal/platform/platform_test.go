package platform

import "testing"

func TestAtLeast(t *testing.T) {
	for _, tc := range []struct {
		have, want string
		ok         bool
	}{
		{"13.3", "13.3", true},
		{"13.3.1", "13.3", true},
		{"13.2.1", "13.3", false},
		{"14.0", "13.3", true},
		{"13", "13.3", false},
		{"26.1", "13.3", true},
		{"2.34", "2.35", false},
		{"2.39", "2.35", true},
		{"2.4", "2.35", false},
	} {
		if got := atLeast(tc.have, tc.want); got != tc.ok {
			t.Errorf("atLeast(%q, %q) = %v", tc.have, tc.want, got)
		}
	}
}

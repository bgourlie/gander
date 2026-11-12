package version

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"v2026.4", "v2026.4"},
		{"2026.4", "v2026.4"},
		{"v1.2.3", "v1.2.3"},
		{"v2.0-rc1", "v2.0-rc1"},
		{"v2.0-rc.1", "v2.0-rc.1"},
		{"v7", "v7"},
	} {
		v, err := Parse(tc.in)
		if err != nil || v.String() != tc.want {
			t.Errorf("Parse(%q) = %q, %v; want %q", tc.in, v, err, tc.want)
		}
	}
	for _, in := range []string{
		"", "v", "vv1", "latest", "v1.", "v.1", "v1..2", "v1.2-", "v1.2-rc 1", "v1.2-rc/1",
		"../v1", "v1/..", "v1.2-rc-1", "v1.2 ", " v1.2", "v-1", "v1.x", "v99999999999999999999",
		"v１.2", "v1.2+meta",
	} {
		if v, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %q, want an error", in, v)
		}
	}
	// Versions are equal exactly when their tags are; the "v" is optional.
	if MustParse("2026.4") != MustParse("v2026.4") || MustParse("v1.2") == MustParse("v1.2.0") {
		t.Error("versions should be equal exactly when their tags are")
	}
}

func TestCompare(t *testing.T) {
	// In ascending order.
	want := []string{
		"v1.2-alpha", "v1.2-beta", "v1.2-rc1", "v1.2-rc2", "v1.2-rc9", "v1.2-rc10", "v1.2",
		"v1.2.0", "v1.2.1", "v1.10", "v2.0-rc1", "v2.0", "v2026.1", "v2026.4", "v2026.10",
	}
	vs := make([]Version, len(want))
	for i, s := range want {
		vs[i] = MustParse(s)
	}
	for range 20 {
		rand.Shuffle(len(vs), func(i, j int) { vs[i], vs[j] = vs[j], vs[i] })
		slices.SortFunc(vs, Compare)
		for i, v := range vs {
			if v.String() != want[i] {
				t.Fatalf("sorted = %v, want %v", vs, want)
			}
		}
	}
	if c := Compare(MustParse("v2026.4"), MustParse("2026.4")); c != 0 {
		t.Errorf("Compare(v2026.4, 2026.4) = %d, want 0", c)
	}
}

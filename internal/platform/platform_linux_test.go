package platform

import "testing"

func TestMaxGlibc(t *testing.T) {
	v := ""
	for _, n := range []string{"GLIBC_2.2.5", "GLIBC_PRIVATE", "GLIBC_2.35", "GLIBC_2.4", "GCC_3.0", "GLIBC_2.34"} {
		v = maxGlibc(v, n)
	}
	if v != "2.35" {
		t.Errorf("maxGlibc = %q, want 2.35", v)
	}
}

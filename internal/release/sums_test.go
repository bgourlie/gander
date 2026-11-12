package release

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseSums(t *testing.T) {
	const name = "goose-v2026.4-linux-x64.tar.gz"
	hash := strings.Repeat("ab", 32)
	want := bytes.Repeat([]byte{0xab}, 32)
	for _, line := range []string{
		hash + "  " + name,
		hash + " *" + name,
		hash + "  ./" + name,
		"  " + hash + "  " + name + "\r",
	} {
		data := []byte(strings.Repeat("00", 32) + "  other.zip\n" + line + "\n")
		sum, err := parseSums(data, name)
		if err != nil || !bytes.Equal(sum, want) {
			t.Errorf("line %q: %x, %v", line, sum, err)
		}
	}
	if _, err := parseSums([]byte(hash+"  other\n"), name); err == nil {
		t.Error("want an error for a missing name")
	}
	if _, err := parseSums([]byte("xyz  "+name+"\n"), name); err == nil {
		t.Error("want an error for a bad hash")
	}
}

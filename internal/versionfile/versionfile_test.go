package versionfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16LE(s string, bom bool) []byte {
	var b []byte
	if bom {
		b = []byte{0xFF, 0xFE}
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func utf16BE(s string) []byte {
	b := []byte{0xFE, 0xFF}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u>>8), byte(u))
	}
	return b
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"plain", []byte("v2026.4\n")},
		{"no v", []byte("2026.4")},
		{"no newline", []byte("v2026.4")},
		{"CRLF", []byte("v2026.4\r\n")},
		{"spaces", []byte("  v2026.4 \t\n")},
		{"UTF-8 BOM", []byte("\xEF\xBB\xBFv2026.4\n")},
		{"UTF-16LE, as PowerShell 5.1 writes it", utf16LE("v2026.4\r\n", true)},
		{"UTF-16BE", utf16BE("v2026.4\n")},
		{"comments and blank lines", []byte("# the project's compiler\n\n  # indented comment\nv2026.4\n\n# trailing\n")},
	} {
		v, err := Parse(tc.data)
		if err != nil || v.String() != "v2026.4" {
			t.Errorf("%s: Parse = %q, %v; want v2026.4", tc.name, v, err)
		}
	}
	for name, data := range map[string]string{
		"empty":          "",
		"only comments":  "# nothing\n\n",
		"two versions":   "v2026.4\nv2026.3\n",
		"two tokens":     "v2026.4 v2026.3\n",
		"inline comment": "v2026.4 # pinned\n",
		"not a version":  "stable\n",
		"path traversal": "../../etc\n",
		"latest":         "latest\n",
		"binary":         "\x00\x80\xfd",
		"bad UTF-8":      "v2026.4\xff\n",
		"odd UTF-16":     "\xFF\xFEv",
	} {
		if v, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: Parse = %q, want an error", name, v)
		}
	}
}

func TestReadNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, Name)
	os.WriteFile(p, []byte("nonsense\n"), 0o644)
	_, err := Read(p)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("Read error = %v, want one naming %s", err, p)
	}

	_, err = Read(filepath.Join(dir, "missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of a missing file = %v, want fs.ErrNotExist", err)
	}

	// A folder where the file should be is an error, not a missing file.
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(filepath.Join(sub, Name), 0o755)
	_, err = Read(filepath.Join(sub, Name))
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of a folder = %v, want an error that isn't fs.ErrNotExist", err)
	}
}

func TestWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), Name)
	os.WriteFile(p, []byte("v1.0\n"), 0o644)
	v, _ := Parse([]byte("2026.4"))
	if err := Write(p, v); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "v2026.4\n" {
		t.Errorf("wrote %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("left %d files behind, want just %s", len(entries), Name)
	}
}

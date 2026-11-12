// Package versionfile reads and writes the one-version files gander uses: a project's
// .goose-version and <home>/default.
package versionfile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/version"
)

// Name is the file a project pins its version in.
const Name = ".goose-version"

// Read reads a version file. A file that doesn't exist returns an error matching
// fs.ErrNotExist; any other problem is an error that names the file.
func Read(path string) (version.Version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return version.Version{}, err
		}
		return version.Version{}, fmt.Errorf("can't read %s: %w", path, err)
	}
	v, err := Parse(data)
	if err != nil {
		return version.Version{}, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// Parse reads the contents of a version file: exactly one version, with an optional
// "v". Blank lines and lines starting with "#" are skipped.
func Parse(data []byte) (version.Version, error) {
	text, err := decode(data)
	if err != nil {
		return version.Version{}, err
	}
	var found string
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line) // also drops the CR of a CRLF file
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if found != "" {
			return version.Version{}, fmt.Errorf("line %d: a version file holds one version, but it also has %q; remove the extra line", i+1, line)
		}
		if _, err := version.Parse(line); err != nil {
			return version.Version{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		found = line
	}
	if found == "" {
		return version.Version{}, errors.New("holds no version; write one, such as v2026.4, or delete the file")
	}
	return version.Parse(found)
}

// decode turns the file's bytes into text. Windows PowerShell 5.1's ">" writes UTF-16LE
// with a BOM, and rustup silently ignored such files (rustup#2302), so UTF-16 is decoded
// by its BOM and a UTF-8 BOM is dropped.
func decode(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], true)
	}
	if !utf8.Valid(data) {
		return "", errors.New("isn't UTF-8 or UTF-16 text; save it as plain text holding one version, such as v2026.4")
	}
	return string(data), nil
}

func decodeUTF16(data []byte, bigEndian bool) (string, error) {
	if len(data)%2 != 0 {
		return "", errors.New("is UTF-16 with an odd number of bytes; save it again as plain text")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		hi, lo := data[2*i], data[2*i+1]
		if !bigEndian {
			hi, lo = lo, hi
		}
		units[i] = uint16(hi)<<8 | uint16(lo)
	}
	return string(utf16.Decode(units)), nil
}

// Write writes v to path as a new file renamed over any old one.
func Write(path string, v version.Version) error {
	return fsutil.WriteFile(path, []byte(v.String()+"\n"), 0o644)
}

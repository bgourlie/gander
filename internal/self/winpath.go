package self

import (
	"slices"
	"strings"
)

// pathWith returns path, a Windows PATH value, with dir first, and whether that changed
// it. A path that already has dir, however it's spelled there, stays as it is. expand
// expands %VARS%.
func pathWith(path, dir string, expand func(string) string) (string, bool) {
	entries := strings.Split(path, ";")
	if slices.ContainsFunc(entries, func(e string) bool { return samePathEntry(e, dir, expand) }) {
		return path, false
	}
	if path == "" {
		return dir, true
	}
	return dir + ";" + path, true
}

// pathWithout returns path without dir, and whether that changed it. The other entries
// stay as they were, %VARS% and all.
func pathWithout(path, dir string, expand func(string) string) (string, bool) {
	entries := strings.Split(path, ";")
	kept := slices.DeleteFunc(slices.Clone(entries), func(e string) bool { return samePathEntry(e, dir, expand) })
	if len(kept) == len(entries) {
		return path, false
	}
	return strings.Join(kept, ";"), true
}

// samePathEntry compares PATH entries as Windows reads them: after expanding %VARS%, and
// ignoring case, quotes and a trailing backslash.
func samePathEntry(entry, dir string, expand func(string) string) bool {
	norm := func(s string) string {
		return strings.TrimRight(strings.Trim(strings.TrimSpace(expand(s)), `"`), `\`)
	}
	return norm(entry) != "" && strings.EqualFold(norm(entry), norm(dir))
}

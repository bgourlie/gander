package self

import "testing"

// Windows PATH entries match however they're spelled, and the rest stay as they were.
func TestWindowsPathValue(t *testing.T) {
	const dir = `C:\Users\me\AppData\Local\gander\bin`
	expand := func(s string) string {
		if s == `%LOCALAPPDATA%\gander\bin\` {
			return `C:\Users\me\AppData\Local\gander\bin\`
		}
		return s
	}
	for _, c := range []struct {
		path, with, without string
	}{
		{path: "", with: dir, without: ""},
		{path: `%USERPROFILE%\tools;C:\other`, with: dir + `;%USERPROFILE%\tools;C:\other`, without: `%USERPROFILE%\tools;C:\other`},
		{path: `C:\a;%LOCALAPPDATA%\gander\bin\;C:\b`, with: `C:\a;%LOCALAPPDATA%\gander\bin\;C:\b`, without: `C:\a;C:\b`},
		{path: `"c:\users\ME\appdata\local\GANDER\bin";C:\b;` + dir, with: `"c:\users\ME\appdata\local\GANDER\bin";C:\b;` + dir, without: `C:\b`},
	} {
		if got, _ := pathWith(c.path, dir, expand); got != c.with {
			t.Errorf("pathWith(%q) = %q, want %q", c.path, got, c.with)
		}
		if got, _ := pathWithout(c.path, dir, expand); got != c.without {
			t.Errorf("pathWithout(%q) = %q, want %q", c.path, got, c.without)
		}
	}
}

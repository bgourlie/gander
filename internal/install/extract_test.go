//go:debug tarinsecurepath=0
//go:debug zipinsecurepath=0

package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/bgourlie/gander/internal/releasetest"
)

const prefix = "goose-v1.0-macos-arm64"

func writeArchive(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// rawTar builds a .tar.gz from headers exactly as given.
func rawTar(hs ...*tar.Header) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range hs {
		tw.WriteHeader(h)
		if h.Typeflag == tar.TypeReg {
			tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtract(t *testing.T) {
	files := append(releasetest.Tree("goose v1.0"), releasetest.File{Name: "._goose", Body: "macOS metadata", Mode: 0o644})
	for _, archive := range []string{
		writeArchive(t, prefix+".tar.gz", releasetest.TarGz(prefix, files)),
		writeArchive(t, prefix+".zip", releasetest.Zip(prefix, files)),
	} {
		name := filepath.Base(archive)
		dest := t.TempDir()
		if err := extract(archive, dest, prefix); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi, err := os.Stat(filepath.Join(dest, "goose")); err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s: goose isn't executable: %v", name, err)
		}
		if fi, err := os.Stat(filepath.Join(dest, "stdlib", "std.goose")); err != nil || fi.Mode().Perm()&0o111 != 0 {
			t.Errorf("%s: std.goose should be a plain file: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dest, "._goose")); err == nil {
			t.Errorf("%s: unpacked the AppleDouble file", name)
		}
	}
}

// Each archive is otherwise valid, with one entry gander must refuse.
func TestExtractRefusesUnsafeEntries(t *testing.T) {
	reg := func(name string) *tar.Header {
		return &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: 3}
	}
	top := &tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0o755}
	var zipTraversal bytes.Buffer
	zw := zip.NewWriter(&zipTraversal)
	w, _ := zw.Create(prefix + "/../evil")
	w.Write([]byte("x"))
	zw.Close()

	for name, data := range map[string][]byte{
		"parent.tar.gz":             rawTar(top, reg(prefix+"/../evil")),
		"escaping parent.tar.gz":    rawTar(top, reg("../evil")),
		"absolute.tar.gz":           rawTar(top, reg("/tmp/evil")),
		"outside the prefix.tar.gz": rawTar(reg("other/file")),
		"symlink.tar.gz":            rawTar(top, &tar.Header{Name: prefix + "/lib", Typeflag: tar.TypeSymlink, Linkname: "/etc"}),
		"hard link.tar.gz":          rawTar(top, &tar.Header{Name: prefix + "/x", Typeflag: tar.TypeLink, Linkname: prefix + "/goose"}),
		"device.tar.gz":             rawTar(top, &tar.Header{Name: prefix + "/dev", Typeflag: tar.TypeChar}),
		"backslash.tar.gz":          rawTar(top, reg(prefix+`/a\..\..\evil`)),
		"duplicate.tar.gz":          rawTar(top, reg(prefix+"/goose"), reg(prefix+"/goose")),
		"empty.tar.gz":              rawTar(top),
		"parent.zip":                zipTraversal.Bytes(),
	} {
		dir := t.TempDir()
		dest := filepath.Join(dir, "dest")
		os.Mkdir(dest, 0o755)
		if err := extract(writeArchive(t, name, data), dest, prefix); err == nil {
			t.Errorf("%s: extract succeeded", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
			t.Errorf("%s: wrote outside the destination", name)
		}
	}
}

package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// extract unpacks a Goose release archive into dir, which must exist and be empty.
//
// Go's archive/tar accepts "../" and absolute names by default, and other managers' tar
// libraries wrote outside the target through link entries (RUSTSEC-2026-0067). So every
// write goes through os.Root, only files and folders under the one top-level prefix are
// accepted, and modes are gander's, not the archive's.
func extract(archive, dir, prefix string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	x := &extractor{root: root, prefix: prefix + "/"}
	if strings.HasSuffix(archive, ".zip") {
		err = x.zip(archive)
	} else {
		err = x.tarGz(archive)
	}
	if err != nil {
		return fmt.Errorf("can't unpack %s: %w", archive, err)
	}
	if x.files == 0 {
		return fmt.Errorf("can't unpack %s: it has nothing under %s", archive, x.prefix)
	}
	return nil
}

type extractor struct {
	root   *os.Root
	prefix string
	files  int
}

func (x *extractor) tarGz(archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var isDir bool
		switch h.Typeflag {
		case tar.TypeDir:
			isDir = true
		case tar.TypeReg:
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("%s is a %s, and Goose's archives hold only files and folders", h.Name, typeName(h.Typeflag))
		}
		if err := x.entry(h.Name, isDir, h.Mode, h.Size, tr); err != nil {
			return err
		}
	}
}

func (x *extractor) zip(archive string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		mode := zf.Mode()
		isDir := mode.IsDir()
		if !isDir && !mode.IsRegular() {
			return fmt.Errorf("%s is a %s, and Goose's archives hold only files and folders", zf.Name, mode.Type())
		}
		var r io.ReadCloser
		if !isDir {
			if r, err = zf.Open(); err != nil {
				return err
			}
		}
		err := x.entry(zf.Name, isDir, int64(mode.Perm()), int64(zf.UncompressedSize64), r)
		if r != nil {
			r.Close()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (x *extractor) entry(name string, isDir bool, mode, size int64, r io.Reader) error {
	rel, ok := strings.CutPrefix(name, x.prefix)
	if !ok {
		if strings.TrimSuffix(name, "/") == strings.TrimSuffix(x.prefix, "/") && isDir {
			return nil // the top-level folder itself
		}
		return fmt.Errorf("%s isn't under %s", name, x.prefix)
	}
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return nil
	}
	if !fs.ValidPath(rel) || strings.ContainsAny(rel, `\:`) {
		return fmt.Errorf("%s isn't a safe path", name)
	}
	// AppleDouble files ("._name") carry macOS metadata and aren't part of the release.
	if strings.HasPrefix(path.Base(rel), "._") {
		return nil
	}
	if isDir {
		return x.root.MkdirAll(rel, 0o755)
	}
	if d := path.Dir(rel); d != "." {
		if err := x.root.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	perm := os.FileMode(0o644)
	if mode&0o100 != 0 {
		perm = 0o755
	}
	f, err := x.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("%s holds %d bytes, not the %d its header says", name, n, size)
	}
	x.files++
	return nil
}

func typeName(t byte) string {
	switch t {
	case tar.TypeSymlink:
		return "symbolic link"
	case tar.TypeLink:
		return "hard link"
	case tar.TypeChar, tar.TypeBlock:
		return "device"
	case tar.TypeFifo:
		return "named pipe"
	}
	return fmt.Sprintf("entry of type %q", t)
}

package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// MaxBinarySize caps the termocode binary taken out of a tarball (and the
// tarball download): a release binary is far below it.
const MaxBinarySize = 200 << 20

// ExtractBinary takes binDir/termocode out of the .tar.gz at tgz and
// writes it to dest (a new file, mode 0600; the caller sets the mode).
// It refuses the whole archive when any entry is absolute or has a ".."
// part, when the binary is a link or not a plain file, appears twice, is
// missing, or is larger than max. Nothing else is written. On error dest
// does not exist.
func ExtractBinary(tgz, binDir, dest string, max int64) (err error) {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("tarball: %w", err)
	}
	defer gz.Close()
	want := binDir + "/termocode"
	tr := tar.NewReader(gz)
	found := false
	defer func() {
		if err != nil && found {
			_ = os.Remove(dest)
		}
	}()
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tarball: %w", err)
		}
		name := h.Name
		if unsafeEntry(name) {
			return fmt.Errorf("tarball entry %q leaves its folder", name)
		}
		if path.Clean(name) != want {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("tarball entry %q is not a plain file", name)
		}
		if found {
			return fmt.Errorf("tarball has %q twice", name)
		}
		if h.Size > max {
			return fmt.Errorf("tarball entry %q is too large (%d bytes)", name, h.Size)
		}
		if err := writeLimited(dest, tr, max); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("tarball has no %s", want)
	}
	return nil
}

// unsafeEntry reports whether a tar entry name is absolute or has a ".."
// part.
func unsafeEntry(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return true
	}
	for _, p := range strings.Split(name, "/") {
		if p == ".." {
			return true
		}
	}
	return false
}

// writeLimited copies at most max bytes of r into a new file dest.
func writeLimited(dest string, r io.Reader, max int64) error {
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, max+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("binary is larger than %d bytes", max)
	}
	if err != nil {
		_ = os.Remove(dest)
	}
	return err
}

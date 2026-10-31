package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// maxChecksums caps checksums.txt (a few lines).
const maxChecksums = 1 << 20

// sanityTimeout caps the `<new> --version` run.
const sanityTimeout = 10 * time.Second

// Staged is a downloaded, checked new binary next to the one it replaces.
type Staged struct {
	Tag     string
	Exe     string // the binary Apply replaces
	NewPath string // the checked new binary (same folder, hidden name)
	Size    int64  // bytes downloaded (the tarball)
}

// Discard removes the staged binary (Cancel on the confirm card).
func (s Staged) Discard() {
	if s.NewPath != "" {
		_ = os.Remove(s.NewPath)
	}
}

// Prepare downloads rel's tarball for goos/goarch and checksums.txt,
// checks the signature of checksums.txt (when a release key is set) and the SHA-256, takes out the binary into a hidden file next to
// exe with exe's mode, and runs it once with --version: it must answer
// "termocode <tag>". Nothing is left behind on error.
func (c Client) Prepare(ctx context.Context, rel Release, exe, goos, goarch string) (Staged, error) {
	tarball, sums, err := PickAsset(rel, goos, goarch)
	if err != nil {
		return Staged{}, err
	}
	info, err := os.Stat(exe)
	if err != nil {
		return Staged{}, err
	}
	tmp, err := os.MkdirTemp("", "termocode-update-")
	if err != nil {
		return Staged{}, err
	}
	defer os.RemoveAll(tmp)

	tgz := filepath.Join(tmp, tarball.Name)
	if err := c.Download(ctx, tarball.URL, tgz, MaxBinarySize); err != nil {
		return Staged{}, err
	}
	sumData, err := c.readSmall(ctx, sums.URL, maxChecksums)
	if err != nil {
		return Staged{}, err
	}
	if err := c.verifySignature(ctx, rel, sumData); err != nil {
		return Staged{}, err
	}
	if err := VerifyChecksum(tgz, tarball.Name, sumData); err != nil {
		return Staged{}, err
	}
	size := int64(0)
	if st, err := os.Stat(tgz); err == nil {
		size = st.Size()
	}

	newPath, err := freshName(filepath.Dir(exe))
	if err != nil {
		return Staged{}, err
	}
	if err := ExtractBinary(tgz, BinaryDir(rel.Tag, goos, goarch), newPath, MaxBinarySize); err != nil {
		return Staged{}, err
	}
	staged := Staged{Tag: rel.Tag, Exe: exe, NewPath: newPath, Size: size}
	if err := os.Chmod(newPath, info.Mode().Perm()); err != nil {
		staged.Discard()
		return Staged{}, err
	}
	if err := sanityCheck(ctx, newPath, rel.Tag); err != nil {
		staged.Discard()
		return Staged{}, err
	}
	return staged, nil
}

// freshName is an unused hidden file name in dir (it also proves dir is
// writable).
func freshName(dir string) (string, error) {
	f, err := os.CreateTemp(dir, ".termocode-new-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

// sanityCheck runs bin --version and wants "termocode <tag>".
func sanityCheck(ctx context.Context, bin, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, sanityTimeout)
	defer cancel()
	var out bytes.Buffer
	var err error
	// A file just written can be "text file busy" for a moment while
	// another goroutine forks; try again a few times.
	for try := 0; try < 5; try++ {
		out.Reset()
		cmd := spawn.CommandContext(ctx, bin, "--version")
		cmd.Env = append(os.Environ(), "TERMOCODE_HEADLESS=1")
		cmd.Stdout = &out
		if err = cmd.Run(); !errors.Is(err, syscall.ETXTBSY) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("the new binary does not run here: %w", err)
	}
	got := strings.TrimSpace(out.String())
	if got != "termocode "+tag {
		return fmt.Errorf("the new binary says %q, want %q", got, "termocode "+tag)
	}
	return nil
}

// readSmall downloads a small file (checksums.txt) into memory.
func (c Client) readSmall(ctx context.Context, raw string, max int64) ([]byte, error) {
	resp, err := c.get(ctx, raw, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err == nil && int64(len(b)) > max {
		err = fmt.Errorf("%s is too large", raw)
	}
	return b, err
}

// Package selfwatch notices when the program file termocode runs from is
// replaced by another build (a developer rebuilt it), so the UI can
// reload itself into it.
//
// Poll is cheap: one stat. Only a file that changed and then stayed the
// same for one more poll (a build that is still writing changes from one
// poll to the next) is looked at: it must be executable, carry a Go build
// id that differs from the running one, and print termocode's version
// line when run with --version. Anything else (a partial write, a broken
// or foreign binary, the same build copied again) is ignored, and a file
// once judged is not run again until it changes.
package selfwatch

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Interval is how often the UI polls the file.
const Interval = 3 * time.Second

// FileState is what one stat says about the file. Two equal states mean
// the file did not change in between.
type FileState struct {
	Dev, Ino uint64
	Size     int64
	Mod      time.Time
	Mode     fs.FileMode
}

// Deps are the watcher's ways to look at the file (tests swap them).
type Deps struct {
	Stat    func(path string) (FileState, error)
	BuildID func(path string) (string, error)
	// Probe runs the file with --version and returns its output line.
	Probe func(path string) (string, error)
}

// DefaultDeps look at the real file.
func DefaultDeps() Deps { return Deps{Stat: Stat, BuildID: ReadBuildID, Probe: Probe} }

// Build is a new build on disk: its Go build id and its version (what
// `termocode --version` prints after "termocode ").
type Build struct {
	ID      string
	Version string
}

// Watcher watches one program file. It is not safe for concurrent use:
// the UI polls it from one command at a time.
type Watcher struct {
	path string
	self string // the build id of the running image
	deps Deps

	last    FileState // the newest stat
	haveLst bool
	judged  FileState // the state the verdict below is about
	haveJdg bool
	ready   Build // ID "" while there is no new build
	// Why is why the last file looked at was ignored ("" when it was a
	// new build or the running one), for the log.
	Why string
}

// New watches path; self is the build id of the running program ("" when
// unknown: then every complete termocode build counts as new).
func New(path, self string, d Deps) *Watcher {
	if d.Stat == nil {
		d.Stat = Stat
	}
	if d.BuildID == nil {
		d.BuildID = ReadBuildID
	}
	if d.Probe == nil {
		d.Probe = Probe
	}
	return &Watcher{path: path, self: self, deps: d}
}

// Path is the watched file.
func (w *Watcher) Path() string { return w.path }

// Poll looks at the file once. ok is true while a new, complete build is
// on disk (the newest one when it was replaced again).
func (w *Watcher) Poll() (Build, bool) {
	st, err := w.deps.Stat(w.path)
	if err != nil {
		w.haveLst = false // gone for now (mid-replace): look again when it is back
		return w.ready, w.ready.ID != ""
	}
	if w.haveJdg && st == w.judged {
		return w.ready, w.ready.ID != ""
	}
	if !w.haveLst || st != w.last {
		w.last, w.haveLst = st, true // changed: wait one poll for it to settle
		return w.ready, w.ready.ID != ""
	}
	w.judged, w.haveJdg = st, true
	b, why := w.judge(st)
	w.Why = why
	if b.ID != "" {
		w.ready = b
	} else if why == "" {
		w.ready = Build{} // the running build is back in place
	}
	return w.ready, w.ready.ID != ""
}

// judge looks at a settled file: the new build, or why not ("" with no
// build: it is the running one).
func (w *Watcher) judge(st FileState) (Build, string) {
	if !st.Mode.IsRegular() {
		return Build{}, "not a regular file"
	}
	if st.Mode.Perm()&0o111 == 0 {
		return Build{}, "not executable"
	}
	id, err := w.deps.BuildID(w.path)
	if err != nil || id == "" {
		return Build{}, "no Go build id (not complete?)"
	}
	if id == w.self {
		return Build{}, ""
	}
	out, err := w.deps.Probe(w.path)
	if err != nil {
		return Build{}, "--version failed: " + err.Error()
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(out), "termocode ")
	if !ok || v == "" || strings.ContainsAny(v, "\n\r") {
		return Build{}, "--version did not print termocode's line"
	}
	return Build{ID: id, Version: v}, ""
}

// CleanPath drops the " (deleted)" Linux adds to the name of a program
// file that was replaced or removed while it runs.
func CleanPath(p string) string { return strings.TrimSuffix(p, " (deleted)") }

// probeTimeout bounds one --version run.
var probeTimeout = 5 * time.Second

// probeMax caps what is read of its output.
const probeMax = 4 << 10

// Probe runs `path --version` and returns its first output line.
func Probe(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, path, "--version")
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, n: probeMax}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("no answer in %s", probeTimeout)
		}
		return "", err
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	return strings.TrimSpace(line), nil
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	_, _ = l.w.Write(p)
	return n, nil
}

// errNoBuildID: the file carries no Go build id.
var errNoBuildID = errors.New("no Go build id")

// ReadBuildID returns the Go build id of the program at path: the ELF
// note .note.go.buildid, else the marker the linker puts at the start of
// the text of other formats (Mach-O, PE). It reads only those bytes.
func ReadBuildID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if ef, err := elf.NewFile(f); err == nil {
		if s := ef.Section(".note.go.buildid"); s != nil {
			data, err := s.Data()
			if err != nil {
				return "", err
			}
			return elfNoteID(data)
		}
	}
	head := make([]byte, 64<<10)
	n, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return markerID(head[:n])
}

// elfNoteID reads the build id from a Go ELF note: namesz, descsz, type
// (4 bytes each), the name "Go\x00\x00", the id.
func elfNoteID(data []byte) (string, error) {
	if len(data) < 16 || !bytes.Equal(data[12:16], []byte("Go\x00\x00")) {
		return "", errNoBuildID
	}
	descsz := int(data[4]) | int(data[5])<<8 | int(data[6])<<16 | int(data[7])<<24
	if descsz <= 0 || 16+descsz > len(data) {
		return "", errNoBuildID
	}
	return string(data[16 : 16+descsz]), nil
}

var (
	markerStart = []byte("\xff Go build ID: \"")
	markerEnd   = []byte("\"\n \xff")
)

// markerID finds the linker's build id marker in b.
func markerID(b []byte) (string, error) {
	i := bytes.Index(b, markerStart)
	if i < 0 {
		return "", errNoBuildID
	}
	rest := b[i+len(markerStart):]
	j := bytes.Index(rest, markerEnd)
	if j <= 0 {
		return "", errNoBuildID
	}
	return string(rest[:j]), nil
}

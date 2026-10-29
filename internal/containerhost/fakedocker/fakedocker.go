// Package fakedocker is an in-process fake of the container engine CLI
// for containerhost tests. It implements services.Exec and answers
// `cp`, `exec` and `inspect` for fake containers whose "/" is a temp
// folder. It never starts a real engine and never reads the user's
// engine config.
package fakedocker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Call is one logged call of the fake.
type Call struct {
	// Context is the value of a global --context (docker) or
	// --connection (podman) flag ("" = none).
	Context string
	// Args are the arguments after the binary and the global flags.
	Args []string
}

// Docker is one fake engine with its containers.
type Docker struct {
	// Kind is "docker" or "podman": it decides what `cp` does with the
	// owner in a tar (docker keeps it with -a, podman with
	// --archive=false).
	Kind string

	mu       sync.Mutex
	cs       map[string]*Container
	calls    []Call
	received []tar.Header
	delays   map[string]time.Duration
	streams  int
}

// Container is one fake container. Its "/" is the local folder Root.
type Container struct {
	Name string
	Root string

	d      *Docker
	owners map[string][2]int // container path → uid, gid
}

// New is a fake docker with no containers.
func New() *Docker {
	return &Docker{Kind: "docker", cs: map[string]*Container{}, delays: map[string]time.Duration{}}
}

// Add creates a running container name whose "/" is a new temp folder.
func (d *Docker) Add(t testing.TB, name string) *Container {
	t.Helper()
	c := &Container{Name: name, Root: t.TempDir(), d: d, owners: map[string][2]int{}}
	d.mu.Lock()
	d.cs[name] = c
	d.mu.Unlock()
	return c
}

// Local is the local path of the container path p.
func (c *Container) Local(p string) string {
	return filepath.Join(c.Root, filepath.FromSlash(path.Clean("/"+p)))
}

// WriteFile writes a file inside the container (parents 0755).
func (c *Container) WriteFile(t testing.TB, p string, data []byte, mode fs.FileMode) {
	t.Helper()
	l := c.Local(p)
	if err := os.MkdirAll(filepath.Dir(l), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(l, mode); err != nil {
		t.Fatal(err)
	}
}

// Mkdir makes a folder (and parents) inside the container.
func (c *Container) Mkdir(t testing.TB, p string) {
	t.Helper()
	if err := os.MkdirAll(c.Local(p), 0o755); err != nil {
		t.Fatal(err)
	}
}

// SetOwner sets the uid and gid `cp` reports for p.
func (c *Container) SetOwner(p string, uid, gid int) {
	c.d.mu.Lock()
	c.owners[path.Clean(p)] = [2]int{uid, gid}
	c.d.mu.Unlock()
}

// Owner is the uid and gid of p (ok false = never set).
func (c *Container) Owner(p string) (uid, gid int, ok bool) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	o, ok := c.owners[path.Clean(p)]
	return o[0], o[1], ok
}

// SetDelay makes every call of the sub command sub wait d first (or
// until its context ends).
func (d *Docker) SetDelay(sub string, dur time.Duration) {
	d.mu.Lock()
	d.delays[sub] = dur
	d.mu.Unlock()
}

// Calls returns the logged calls.
func (d *Docker) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Call(nil), d.calls...)
}

// Received returns the headers of every tar entry `cp - c:dir` got.
func (d *Docker) Received() []tar.Header {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]tar.Header(nil), d.received...)
}

// OpenStreams is how many Streams were not stopped yet.
func (d *Docker) OpenStreams() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.streams
}

func (d *Docker) container(name string) (*Container, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.cs[name]
	return c, ok
}

type result struct {
	out, err string
	code     int
}

func fail(format string, a ...any) result {
	return result{err: fmt.Sprintf(format, a...) + "\n", code: 1}
}

// Capture runs one fake call.
func (d *Docker) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	return d.CaptureInput(ctx, dir, nil, argv...)
}

// CaptureInput runs one fake call with input on stdin.
func (d *Docker) CaptureInput(ctx context.Context, _ string, input []byte, argv ...string) (string, string, int, error) {
	if len(argv) == 0 {
		return "", "", -1, errors.New("no command")
	}
	call := parseGlobal(argv[1:])
	d.mu.Lock()
	d.calls = append(d.calls, call)
	delay := time.Duration(0)
	if len(call.Args) > 0 {
		delay = d.delays[call.Args[0]]
	}
	d.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", "", -1, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return "", "", -1, err
	}
	r := d.run(ctx, call.Args, input)
	if err := ctx.Err(); err != nil {
		return r.out, r.err, -1, err
	}
	return r.out, r.err, r.code, nil
}

// Stream runs one fake call and hands its stdout as a reader.
func (d *Docker) Stream(ctx context.Context, dir string, argv ...string) (io.ReadCloser, func(), error) {
	out, _, _, err := d.Capture(ctx, dir, argv...)
	if err != nil {
		return nil, nil, err
	}
	d.mu.Lock()
	d.streams++
	d.mu.Unlock()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			d.mu.Lock()
			d.streams--
			d.mu.Unlock()
		})
	}
	return io.NopCloser(strings.NewReader(out)), stop, nil
}

func parseGlobal(args []string) Call {
	var c Call
	for len(args) >= 2 && (args[0] == "--context" || args[0] == "--connection") {
		c.Context = args[1]
		args = args[2:]
	}
	c.Args = append([]string(nil), args...)
	return c
}

func (d *Docker) run(ctx context.Context, args []string, input []byte) result {
	if len(args) == 0 {
		return fail("no sub command")
	}
	switch args[0] {
	case "cp":
		return d.cp(args[1:], input)
	}
	return fail("fakedocker: unknown command %q", args[0])
}

func (d *Docker) cp(args []string, input []byte) result {
	archive, follow := false, false
	var pos []string
	for _, a := range args {
		switch a {
		case "-a", "--archive", "--archive=true":
			archive = d.Kind == "docker"
		case "--archive=false":
			archive = d.Kind == "podman"
		case "-L", "--follow-link":
			follow = true
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return fail("unknown flag: %s", a)
			}
			pos = append(pos, a)
		}
	}
	if len(pos) != 2 {
		return fail("\"docker cp\" requires exactly 2 arguments")
	}
	src, dst := pos[0], pos[1]
	switch {
	case dst == "-":
		name, p, ok := strings.Cut(src, ":")
		if !ok {
			return fail("fakedocker: only container-to-stdout copies")
		}
		c, ok := d.container(name)
		if !ok {
			return fail("Error response from daemon: No such container: %s", name)
		}
		return c.cpOut(p, follow)
	case src == "-":
		name, p, ok := strings.Cut(dst, ":")
		if !ok {
			return fail("fakedocker: only stdin-to-container copies")
		}
		c, ok := d.container(name)
		if !ok {
			return fail("Error response from daemon: No such container: %s", name)
		}
		return c.cpIn(p, input, archive)
	}
	return fail("fakedocker: one side of cp must be -")
}

func (c *Container) notFound(p string) result {
	if c.d.Kind == "podman" {
		return fail("Error: %q could not be found on container %s: no such file or directory", p, c.Name)
	}
	return fail("Error response from daemon: Could not find the file %s in container %s", p, c.Name)
}

func (c *Container) header(cp string, fi fs.FileInfo, name string) (*tar.Header, error) {
	link := ""
	if fi.Mode()&fs.ModeSymlink != 0 {
		l, err := os.Readlink(c.Local(cp))
		if err != nil {
			return nil, err
		}
		link = l
	}
	h, err := tar.FileInfoHeader(fi, link)
	if err != nil {
		return nil, err
	}
	h.Name = name
	h.Uname, h.Gname = "", ""
	h.Uid, h.Gid = 1000, 1000
	c.d.mu.Lock()
	if o, ok := c.owners[path.Clean(cp)]; ok {
		h.Uid, h.Gid = o[0], o[1]
	}
	c.d.mu.Unlock()
	return h, nil
}

// cpOut is `cp c:p -`: a tar of p named by its base name.
func (c *Container) cpOut(p string, follow bool) result {
	p = path.Clean("/" + p)
	stat := os.Lstat
	if follow {
		stat = os.Stat
	}
	fi, err := stat(c.Local(p))
	if err != nil {
		return c.notFound(p)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var add func(cp, name string, fi fs.FileInfo) error
	add = func(cp, name string, fi fs.FileInfo) error {
		if fi.IsDir() {
			name += "/"
		}
		h, err := c.header(cp, fi, name)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		switch {
		case fi.Mode().IsRegular():
			data, err := os.ReadFile(c.Local(cp))
			if err != nil {
				return err
			}
			_, err = tw.Write(data)
			return err
		case fi.IsDir():
			ents, err := os.ReadDir(c.Local(cp))
			if err != nil {
				return err
			}
			for _, e := range ents {
				info, err := e.Info()
				if err != nil {
					return err
				}
				if err := add(path.Join(cp, e.Name()), path.Join(name, e.Name()), info); err != nil {
					return err
				}
			}
		}
		return nil
	}
	base := path.Base(p)
	if err := add(p, base, fi); err != nil {
		return fail("fakedocker: %v", err)
	}
	if err := tw.Close(); err != nil {
		return fail("fakedocker: %v", err)
	}
	return result{out: buf.String()}
}

// cpIn is `cp - c:dir`: extract the tar on stdin into dir.
func (c *Container) cpIn(dir string, input []byte, archive bool) result {
	dir = path.Clean("/" + dir)
	if fi, err := os.Stat(c.Local(dir)); err != nil || !fi.IsDir() {
		return c.notFound(dir)
	}
	tr := tar.NewReader(bytes.NewReader(input))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail("Error response from daemon: bad tar: %v", err)
		}
		name := path.Clean(h.Name)
		if name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return fail("Error response from daemon: invalid tar entry %q", h.Name)
		}
		c.d.mu.Lock()
		c.d.received = append(c.d.received, *h)
		c.d.mu.Unlock()
		cp := path.Join(dir, name)
		l := c.Local(cp)
		mode := fs.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(l, mode); err != nil {
				return fail("Error response from daemon: %v", err)
			}
			_ = os.Chmod(l, mode)
		case tar.TypeReg:
			data, err := io.ReadAll(tr)
			if err != nil {
				return fail("Error response from daemon: %v", err)
			}
			if fi, err := os.Lstat(l); err == nil && fi.IsDir() {
				return fail("Error response from daemon: cannot overwrite directory %q with non-directory", cp)
			}
			_ = os.Remove(l) // the engine replaces the entry, like tar -x
			if err := os.WriteFile(l, data, mode); err != nil {
				return fail("Error response from daemon: %v", err)
			}
			_ = os.Chmod(l, mode)
		default:
			return fail("fakedocker: tar entry type %c not supported", h.Typeflag)
		}
		owner := [2]int{0, 0}
		if archive {
			owner = [2]int{h.Uid, h.Gid}
		}
		c.d.mu.Lock()
		c.owners[cp] = owner
		c.d.mu.Unlock()
	}
	return result{}
}

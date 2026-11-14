package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Needle is the text that fixtures put at the very end (and once in a file
// of each repo) so search measures have a known target.
const Needle = "BENCH_NEEDLE_7f3a"

// FixtureRepos are the repos `bench fixtures` writes, by name.
var FixtureRepos = map[string]RepoSpec{
	"small": {Files: 200, Seed: 1, Git: true},
	"large": {Files: 20000, Seed: 2, Git: true},
}

// QuickOpenTarget is the file quick_open types and waits for (without
// ".go"). Its number is below every repo's file count and is a Go file
// (i%4 is 0 or 1 in GenRepo), so each fixture repo has it.
const QuickOpenTarget = "f00161"

// RepoSpec describes a generated repo. No private code is ever used.
type RepoSpec struct {
	Files int    // exact number of files, go.mod and others included
	Seed  uint64 // same seed, same tree
	Git   bool   // git init and one commit
}

// GenRepo writes a deterministic Go + TypeScript repo of spec.Files files.
// It has a go.mod (so gopls works), one file with a type error (so a
// diagnostic must appear) and the Needle in exactly one file.
func GenRepo(dir string, spec RepoSpec) error {
	if spec.Files < 4 {
		return fmt.Errorf("bench: a repo needs at least 4 files, got %d", spec.Files)
	}
	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed^0x9e3779b97f4a7c15))
	write := func(rel, body string) error {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(body), 0o644)
	}
	if err := write("go.mod", "module benchrepo\n\ngo 1.22\n"); err != nil {
		return err
	}
	if err := write("diag/bad.go", "package diag\n\nfunc Bad() int {\n\treturn missingName\n}\n"); err != nil {
		return err
	}
	if err := write("README.md", fmt.Sprintf("# bench repo seed %d\n\n%s\n", spec.Seed, Needle)); err != nil {
		return err
	}
	for i := 3; i < spec.Files; i++ {
		dirN := i / 40
		pkg := fmt.Sprintf("p%03d", dirN)
		var rel, body string
		switch i % 4 {
		case 0, 1:
			rel = fmt.Sprintf("src/%s/f%05d.go", pkg, i)
			body = goFile(rng, pkg, i)
		case 2:
			rel = fmt.Sprintf("web/%s/m%05d.ts", pkg, i)
			body = tsFile(rng, i)
		default:
			rel = fmt.Sprintf("docs/%s/n%05d.md", pkg, i)
			body = fmt.Sprintf("# note %d\n\n%s\n", i, words(rng, 30))
		}
		if err := write(rel, body); err != nil {
			return err
		}
	}
	if spec.Git {
		return gitInit(dir)
	}
	return nil
}

func words(rng *rand.Rand, n int) string {
	vocab := []string{"alpha", "bravo", "delta", "echo", "frame", "grid", "hash", "index", "joint", "kernel", "lane", "merge", "node", "orbit", "pixel", "queue", "route", "stack", "token", "value"}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(vocab[rng.IntN(len(vocab))])
	}
	return b.String()
}

func goFile(rng *rand.Rand, pkg string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	for k := 0; k < 6; k++ {
		fmt.Fprintf(&b, "// Func%d_%d adds %d to its input (%s).\nfunc Func%d_%d(x int) int {\n\treturn x + %d\n}\n\n", n, k, rng.IntN(1000), words(rng, 3), n, k, rng.IntN(1000))
	}
	return b.String()
}

func tsFile(rng *rand.Rand, n int) string {
	var b strings.Builder
	for k := 0; k < 6; k++ {
		fmt.Fprintf(&b, "export function fn%d_%d(x: number): number {\n  // %s\n  return x * %d;\n}\n\n", n, k, words(rng, 3), rng.IntN(1000))
	}
	return b.String()
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := spawn.Command("git", append([]string{"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitInit(dir string) error {
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "fixture"}} {
		if out, err := gitOut(dir, a...); err != nil {
			return fmt.Errorf("git %v: %v: %s", a, err, out)
		}
	}
	return nil
}

// HashTree hashes names and contents of every file (not .git) in dir.
func HashTree(dir string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// BigLines writes a text file of exactly n lines; the last has the Needle.
func BigLines(path string, n int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 1; i <= n; i++ {
		tail := words(rng, 8)
		if i == n {
			tail += " " + Needle
		}
		if _, err := fmt.Fprintf(f, "line %07d %s\n", i, tail); err != nil {
			return err
		}
	}
	return nil
}

// BigBytes writes a log-like file of about size bytes (at most 200 more),
// ending with a line that has the Needle.
func BigBytes(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	rng := rand.New(rand.NewPCG(3, 4))
	var written int64
	for i := 0; written < size; i++ {
		line := fmt.Sprintf("2026-01-01T00:00:%02d INFO req=%d %s\n", i%60, i, words(rng, 12))
		n, err := io.WriteString(f, line)
		if err != nil {
			return err
		}
		written += int64(n)
	}
	_, err = fmt.Fprintf(f, "END %s\n", Needle)
	return err
}

// Pin is one language server version used by every editor in a run.
type Pin struct {
	Name    string
	Version string
	Install string // how a person installs exactly this version
}

// LSPPins are the servers the LSP measures need. They are installed by hand
// (never downloaded by the harness) so every editor talks to the same ones.
var LSPPins = []Pin{
	{"gopls", "v0.20.0", "go install golang.org/x/tools/gopls@v0.20.0"},
	{"typescript-language-server", "4.3.4", "npm install -g typescript-language-server@4.3.4 typescript@5.8.3"},
}

// SlowLink is a TCP proxy that delays every chunk by a fixed time in each
// direction, like a slow network. Point an SSH client at Addr().
type SlowLink struct {
	ln     net.Listener
	target string
	delay  time.Duration
	mu     sync.Mutex
	conns  []net.Conn
	wg     sync.WaitGroup
}

// NewSlowLink listens on a free local port and forwards to target.
func NewSlowLink(target string, delay time.Duration) (*SlowLink, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l := &SlowLink{ln: ln, target: target, delay: delay}
	l.wg.Go(l.accept)
	return l, nil
}

// Addr is the host:port to connect to.
func (l *SlowLink) Addr() string { return l.ln.Addr().String() }

func (l *SlowLink) accept() {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", l.target)
		if err != nil {
			c.Close()
			continue
		}
		l.mu.Lock()
		l.conns = append(l.conns, c, up)
		l.mu.Unlock()
		l.wg.Go(func() { l.pipe(c, up) })
		l.wg.Go(func() { l.pipe(up, c) })
	}
}

type chunk struct {
	b  []byte
	at time.Time
}

// pipe copies src to dst, holding each chunk until delay has passed.
func (l *SlowLink) pipe(src, dst net.Conn) {
	q := make(chan chunk, 1024)
	var w sync.WaitGroup
	w.Go(func() {
		for c := range q {
			time.Sleep(time.Until(c.at.Add(l.delay)))
			if _, err := dst.Write(c.b); err != nil {
				break
			}
		}
		for range q { // drain so the reader never blocks
		}
	})
	buf := make([]byte, 16*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			q <- chunk{append([]byte(nil), buf[:n]...), time.Now()}
		}
		if err != nil {
			break
		}
	}
	close(q)
	w.Wait()
	src.Close()
	dst.Close()
}

// Drop cuts every open connection (a network drop); new ones still work.
func (l *SlowLink) Drop() {
	l.mu.Lock()
	cs := l.conns
	l.conns = nil
	l.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

// Close stops the proxy and cuts every connection.
func (l *SlowLink) Close() {
	l.ln.Close()
	l.Drop()
	l.wg.Wait()
}

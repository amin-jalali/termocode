package bench

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func countFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			n++
		}
		return nil
	})
	return n
}

func TestGenRepoMakesExactFileCountDeterministically(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		if err := GenRepo(d, RepoSpec{Files: 250, Seed: 7}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countFiles(t, a); n != 250 {
		t.Fatalf("files = %d, want 250", n)
	}
	h1, _ := HashTree(a)
	h2, _ := HashTree(b)
	if h1 != h2 {
		t.Fatal("same seed must give the same tree")
	}
	c := t.TempDir()
	_ = GenRepo(c, RepoSpec{Files: 250, Seed: 8})
	if h3, _ := HashTree(c); h3 == h1 {
		t.Fatal("another seed must differ")
	}
}

func TestGenRepoHasSourceFilesWithGoAndTS(t *testing.T) {
	d := t.TempDir()
	if err := GenRepo(d, RepoSpec{Files: 40, Seed: 1}); err != nil {
		t.Fatal(err)
	}
	var goN, tsN int
	_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
		switch filepath.Ext(p) {
		case ".go":
			goN++
		case ".ts":
			tsN++
		}
		return nil
	})
	if goN == 0 || tsN == 0 {
		t.Fatalf("go=%d ts=%d", goN, tsN)
	}
	if _, err := os.Stat(filepath.Join(d, "go.mod")); err != nil {
		t.Fatal("a Go repo needs go.mod for gopls")
	}
}

func TestGenRepoGitHasACleanTree(t *testing.T) {
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		t.Skip("no git")
	}
	d := t.TempDir()
	if err := GenRepo(d, RepoSpec{Files: 30, Seed: 1, Git: true}); err != nil {
		t.Fatal(err)
	}
	out, err := gitOut(d, "status", "--porcelain")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("status %q err %v", out, err)
	}
}

func TestBigLinesAndBigBytes(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "big.txt")
	if err := BigLines(p, 50000); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	sc := bufio.NewScanner(f)
	n, last := 0, ""
	for sc.Scan() {
		n++
		last = sc.Text()
	}
	if n != 50000 || !strings.Contains(last, Needle) {
		t.Fatalf("lines=%d last=%q", n, last)
	}
	q := filepath.Join(d, "huge.log")
	if err := BigBytes(q, 5<<20); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(q)
	if st.Size() < 5<<20 || st.Size() > 5<<20+200 {
		t.Fatalf("size %d", st.Size())
	}
	b, _ := os.ReadFile(q)
	if !bytes.Contains(b[len(b)-200:], []byte(Needle)) {
		t.Fatal("needle must sit at the end")
	}
}

func TestLSPPinsAreExactVersions(t *testing.T) {
	if len(LSPPins) < 2 {
		t.Fatal("need gopls and a TypeScript server")
	}
	for _, p := range LSPPins {
		if p.Name == "" || p.Version == "" || strings.ContainsAny(p.Version, "^~*") || p.Version == "latest" {
			t.Fatalf("pin %+v is not exact", p)
		}
	}
}

func TestSlowLinkDelaysBothWaysAndDrops(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // test echo server
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	link, err := NewSlowLink(ln.Addr().String(), 60*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	c, err := net.Dial("tcp", link.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	_, _ = c.Write([]byte("x"))
	buf := make([]byte, 1)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if rtt := time.Since(start); rtt < 110*time.Millisecond {
		t.Fatalf("round trip %v, want >= 2x delay", rtt)
	}
	link.Drop()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(buf); err == nil {
		t.Fatal("connection must be cut by Drop")
	}
}

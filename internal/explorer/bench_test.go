package explorer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// manyDir is a workspace with one folder of n files ("many/").
func manyDir(b *testing.B, n int) string {
	b.Helper()
	dir := b.TempDir()
	many := filepath.Join(dir, "many")
	if err := os.MkdirAll(many, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(many, fmt.Sprintf("f%04d.txt", i)), []byte("x\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

// BenchmarkLoad5k reads a folder of 5 000 files (what a first click on
// it does).
func BenchmarkLoad5k(b *testing.B) {
	dir := manyDir(b, 5000)
	n := &Node{Path: filepath.Join(dir, "many"), IsDir: true}
	b.ReportAllocs()
	for b.Loop() {
		if err := n.Load(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkView5kExpanded draws the panel after a folder of 5 000 files
// opens (the first frame after the click: the cache is cold).
func BenchmarkView5kExpanded(b *testing.B) {
	dir := manyDir(b, 5000)
	root, err := NewRoot(dir)
	if err != nil {
		b.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(30, 38)
	m.SetMeta("ws", "main", true, 5001)
	many := root.Children[0]
	if err := many.Toggle(); err != nil {
		b.Fatal(err)
	}
	git := GitStatus{}
	b.ReportAllocs()
	for b.Loop() {
		touch() // a cold cache, as after the click
		_ = m.View(false, "", false, git)
	}
}

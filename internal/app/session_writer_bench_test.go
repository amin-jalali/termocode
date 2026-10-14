package app

import (
	"path/filepath"
	"testing"
)

// BenchmarkScrollSessionSave is the session part of the StateMsg handler
// while the user scrolls: every state message moves the cursor one line.
// Before N1 each one read and wrote the session files (~100 µs, 168
// allocs per message on an i7-13700H); now ~1.3 µs, 8 allocs.
func BenchmarkScrollSessionSave(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_CONFIG_HOME", dir)
	b.Chdir(dir)
	m := &Model{}
	files := make([]nvimBuffer, 0, 8)
	for i := 0; i < 8; i++ {
		files = append(files, nvimBuffer{Path: filepath.Join(dir, "f"+string(rune('a'+i))+".go")})
	}
	active := files[0].Path
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cur := map[string][2]int{active: {i + 1, 1}}
		for _, f := range files[1:] {
			cur[f.Path] = [2]int{10, 1}
		}
		m.persistStateSession(StateMsg{Path: active, Bufs: files, Cursors: cur})
	}
}

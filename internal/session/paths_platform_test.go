//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SCN-35: the session folder under the homes of a Mac (a name with a space
// and non-ASCII letters), of Termux, and a very long one (macOS sockets
// allow only about 100 bytes).
func TestSCN35SessionDirForPlatformHomes(t *testing.T) {
	cases := []struct {
		name, home string
		short      bool // the home itself is used (the path fits a socket)
	}{
		{"macOS name with a space and umlauts", "Zoë Müller", true},
		{"Termux", "com.termux/files/home", true},
		{"very long home", strings.Repeat("a-long-folder-name/", 6) + "me", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "tcp")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			home := filepath.Join(root, c.home)
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_RUNTIME_DIR", "")
			t.Setenv("XDG_CACHE_HOME", "")
			t.Setenv("TMPDIR", root)
			d, err := Dir()
			if err != nil {
				t.Fatal(err)
			}
			if len(d)+len(longestSocketName) > maxSocketPath {
				t.Errorf("%q + the longest socket name is past %d bytes", d, maxSocketPath)
			}
			if inHome := strings.HasPrefix(d, home); inHome != c.short {
				t.Errorf("dir %q: under the home = %v, want %v", d, inHome, c.short)
			}
			if st, err := os.Stat(d); err != nil || st.Mode().Perm() != 0o700 {
				t.Errorf("dir %q: %v, mode %v, want 0700", d, err, st)
			}
		})
	}
}

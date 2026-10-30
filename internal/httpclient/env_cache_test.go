package httpclient

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PERF-8: env files are read again only when one of them changed
// (size, time, inode) or appeared or went away.
func TestLoadEnvsReadsOnlyChangedFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "api")
	pub := filepath.Join(root, EnvFile)
	writeFile(t, pub, `{"dev": {"host": "one"}}`)
	writeFile(t, filepath.Join(dir, "x.http"), "GET {{host}}\n")
	reads := 0
	orig := envReadFile
	envReadFile = func(p string) ([]byte, error) { reads++; return orig(p) }
	t.Cleanup(func() { envReadFile = orig })

	host := func() string { v, _, _ := LoadEnvs(dir, root).Lookup("dev", "host"); return v }
	if h := host(); h != "one" {
		t.Fatalf("host %q", h)
	}
	first := reads
	if h := host(); h != "one" || reads != first {
		t.Fatalf("unchanged files read again: %d reads -> %d", first, reads)
	}
	// Same size, new time.
	writeFile(t, pub, `{"dev": {"host": "two"}}`)
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(pub, later, later)
	if h := host(); h != "two" {
		t.Fatalf("after an edit: %q", h)
	}
	// A nearer file appears.
	writeFile(t, filepath.Join(dir, EnvFile), `{"dev": {"host": "near"}}`)
	if h := host(); h != "near" {
		t.Fatalf("after a new file: %q", h)
	}
	// It goes away again.
	_ = os.Remove(filepath.Join(dir, EnvFile))
	if h := host(); h != "two" {
		t.Fatalf("after a removed file: %q", h)
	}
	// .env too.
	writeFile(t, filepath.Join(root, DotenvFile), "K=1\n")
	if v := LoadEnvs(dir, root).Dotenv["K"]; v != "1" {
		t.Fatalf(".env K=%q", v)
	}
	writeFile(t, filepath.Join(root, DotenvFile), "K=22\n")
	if v := LoadEnvs(dir, root).Dotenv["K"]; v != "22" {
		t.Fatalf(".env K=%q after an edit", v)
	}
}

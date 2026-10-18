package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRef(t *testing.T) {
	ok := []struct {
		in   string
		kind string
		arg  string
	}{
		{"${env:OPENAI_API_KEY}", RefEnv, "OPENAI_API_KEY"},
		{"  ${env:_X1}  ", RefEnv, "_X1"},
		{"${dotenv:POSTGRES_PASSWORD}", RefDotenv, "POSTGRES_PASSWORD"},
		{"${cmd:pass show termocode/anthropic}", RefCmd, "pass show termocode/anthropic"},
		{"${cmd:awk '{print $1}' f}", RefCmd, "awk '{print $1}' f"},
	}
	for _, c := range ok {
		r, err := ParseRef(c.in)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", c.in, err)
			continue
		}
		if r.Kind != c.kind || r.Arg != c.arg {
			t.Errorf("ParseRef(%q) = %+v", c.in, r)
		}
		if r2, err := ParseRef(r.String()); err != nil || r2 != r {
			t.Errorf("String() round trip of %q: %v %v", c.in, r2, err)
		}
	}
	notRef := []string{"sk-ant-123", "", "$env:X", "{env:X}", "abc${env:X}"}
	for _, in := range notRef {
		if _, err := ParseRef(in); !errors.Is(err, ErrNotRef) {
			t.Errorf("ParseRef(%q) err = %v, want ErrNotRef", in, err)
		}
		if IsRef(in) {
			t.Errorf("IsRef(%q) = true", in)
		}
	}
	bad := []string{
		"${env:}", "${env:1X}", "${env:A-B}", "${env:X", "${env}", "${}",
		"${dotenv:../.env}", "${dotenv:A/B}", "${dotenv:}",
		"${cmd:}", "${cmd:   }", "${cmd:a\nb}",
		"${secret:X}", "${file:/etc/passwd}", "${ENV:X}",
	}
	for _, in := range bad {
		if _, err := ParseRef(in); !errors.Is(err, ErrBadRef) {
			t.Errorf("ParseRef(%q) err = %v, want ErrBadRef", in, err)
		}
	}
}

func TestResolveEnv(t *testing.T) {
	r := &Resolver{Getenv: func(k string) string {
		if k == "MY_KEY" {
			return "env-VALUE"
		}
		return ""
	}}
	v, err := r.Resolve(Ref{RefEnv, "MY_KEY"})
	if err != nil || v != "env-VALUE" {
		t.Fatalf("Resolve = %q %v", v, err)
	}
	_, err = r.Resolve(Ref{RefEnv, "EMPTY"})
	if err == nil || !strings.Contains(err.Error(), "${env:EMPTY}") {
		t.Fatalf("empty env: err = %v, want one naming the reference", err)
	}
}

func TestResolveDotenv(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("# c\nexport DB_PASS=\"dot-VALUE\"\nOTHER=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Workspace: ws}
	v, err := r.Resolve(Ref{RefDotenv, "DB_PASS"})
	if err != nil || v != "dot-VALUE" {
		t.Fatalf("Resolve = %q %v", v, err)
	}
	if _, err := r.Resolve(Ref{RefDotenv, "MISSING"}); err == nil {
		t.Fatal("a missing name must fail")
	}

	// The cache follows the file: a new value after an edit.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("DB_PASS=dot-NEWER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.Resolve(Ref{RefDotenv, "DB_PASS"}); v != "dot-NEWER" {
		t.Fatalf("after edit = %q", v)
	}

	if _, err := (&Resolver{}).Resolve(Ref{RefDotenv, "DB_PASS"}); err == nil {
		t.Fatal("no workspace must fail")
	}
}

func TestResolveDotenvNeverOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	// A parent .env must never be read.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=parent-VALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Workspace: ws}
	if v, err := r.Resolve(Ref{RefDotenv, "SECRET"}); err == nil {
		t.Fatalf("read a parent .env: %q", v)
	}
	if _, err := r.Resolve(Ref{RefDotenv, "../.env"}); !errors.Is(err, ErrBadRef) {
		t.Fatalf("a path as NAME: err = %v, want ErrBadRef", err)
	}
	// A workspace .env that is a link to the parent's is refused too.
	if err := os.Symlink(filepath.Join(root, ".env"), filepath.Join(ws, ".env")); err != nil {
		t.Skip("no symlinks here")
	}
	if v, err := r.Resolve(Ref{RefDotenv, "SECRET"}); err == nil {
		t.Fatalf("followed a link out of the workspace: %q", v)
	}
}

func TestResolveCmd(t *testing.T) {
	r := &Resolver{}
	v, err := r.Resolve(Ref{RefCmd, "printf 'cmd-VALUE  \\nsecond line\\n'"})
	if err != nil || v != "cmd-VALUE" {
		t.Fatalf("Resolve = %q %v, want the first line trimmed", v, err)
	}
}

func TestResolveCmdFails(t *testing.T) {
	r := &Resolver{Redact: func(s string) string { return strings.ReplaceAll(s, "HIDE", "[REDACTED]") }}
	// The words are joined at run time so the reference text (which the
	// error names) does not hold them.
	_, err := r.Resolve(Ref{RefCmd, "printf '%s-%s\\n' out VALUE; printf 'boom %s%s\\n' HI DE >&2; exit 1"})
	if err == nil {
		t.Fatal("exit 1 must fail")
	}
	msg := err.Error()
	if strings.Contains(msg, "out-VALUE") {
		t.Errorf("error leaks stdout: %q", msg)
	}
	if !strings.Contains(msg, "boom [REDACTED]") || strings.Contains(msg, "HIDE") {
		t.Errorf("error must carry redacted stderr: %q", msg)
	}
	if _, err := r.Resolve(Ref{RefCmd, "true"}); err == nil {
		t.Error("empty output must fail")
	}
}

func TestResolveCmdTimeout(t *testing.T) {
	r := &Resolver{Timeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := r.Resolve(Ref{RefCmd, "exec sleep 5"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("timeout took %v", d)
	}
}

func TestResolveCmdStdinClosed(t *testing.T) {
	r := &Resolver{Timeout: 2 * time.Second}
	// cat on a closed stdin ends at once with no output.
	if _, err := r.Resolve(Ref{RefCmd, "cat; echo done"}); err != nil {
		t.Fatalf("stdin must be closed: %v", err)
	}
}

func TestResolveCmdCache(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "n")
	line := "echo x >> " + counter + "; echo cached-VALUE"
	r := &Resolver{}
	for i := 0; i < 3; i++ {
		if v, err := r.Resolve(Ref{RefCmd, line}); err != nil || v != "cached-VALUE" {
			t.Fatalf("Resolve = %q %v", v, err)
		}
	}
	if n := strings.Count(readFile(t, counter), "x"); n != 1 {
		t.Fatalf("command ran %d times, want 1 (cached)", n)
	}
	r.ForgetCache()
	if _, err := r.Resolve(Ref{RefCmd, line}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readFile(t, counter), "x"); n != 2 {
		t.Fatalf("after ForgetCache ran %d times, want 2", n)
	}
}

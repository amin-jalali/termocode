package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHelloCarriesCaps: a new client sends its terminal caps after the
// size in the hello frame.
func TestHelloCarriesCaps(t *testing.T) {
	caps := &ClientCaps{
		Env:      map[string]string{"TERM": "xterm-kitty", "LANG": "C.UTF-8"},
		Graphics: &GraphicsCaps{Protocol: "kitty", CellW: 9, CellH: 18, Reason: "probe"},
	}
	cols, rows, got, err := decodeHello(encodeHello(100, 30, caps))
	if err != nil || cols != 100 || rows != 30 {
		t.Fatalf("size %dx%d err %v", cols, rows, err)
	}
	if got == nil || got.Env["TERM"] != "xterm-kitty" || got.Env["LANG"] != "C.UTF-8" {
		t.Fatalf("env = %+v", got)
	}
	if got.Graphics == nil || *got.Graphics != *caps.Graphics {
		t.Errorf("graphics = %+v", got.Graphics)
	}
}

// TestHelloFromOldClient: an old client sends only the size; that is not
// an error and there are no caps.
func TestHelloFromOldClient(t *testing.T) {
	cols, rows, caps, err := decodeHello(encodeSize(80, 24))
	if err != nil || cols != 80 || rows != 24 || caps != nil {
		t.Errorf("got %dx%d caps=%v err=%v", cols, rows, caps, err)
	}
	if _, _, _, err := decodeHello([]byte{1, 2}); err == nil {
		t.Error("short hello accepted")
	}
	if b := encodeHello(80, 24, nil); len(b) != 4 {
		t.Errorf("hello without caps is %d bytes, want 4 (the old format)", len(b))
	}
}

// TestOldServerReadsNewHello: an old server reads only the size, so the
// caps after it must not change it.
func TestOldServerReadsNewHello(t *testing.T) {
	b := encodeHello(132, 43, &ClientCaps{Env: map[string]string{"TERM": "xterm"}})
	if c, r, err := decodeSize(b); err != nil || c != 132 || r != 43 {
		t.Errorf("old decode: %dx%d %v", c, r, err)
	}
}

// TestHelloBadCaps: broken caps are dropped, the size still counts.
func TestHelloBadCaps(t *testing.T) {
	b := append(encodeSize(90, 20), []byte("{not json")...)
	cols, rows, caps, err := decodeHello(b)
	if err != nil || cols != 90 || rows != 20 || caps != nil {
		t.Errorf("got %dx%d caps=%v err=%v", cols, rows, caps, err)
	}
}

// TestEnvCaps keeps only the set terminal variables.
func TestEnvCaps(t *testing.T) {
	env := map[string]string{"TERM": "xterm-256color", "COLORTERM": "", "HOME": "/h", "TERM_PROGRAM": "WezTerm"}
	c := EnvCaps(func(k string) string { return env[k] })
	if c.Env["TERM"] != "xterm-256color" || c.Env["TERM_PROGRAM"] != "WezTerm" {
		t.Errorf("env = %v", c.Env)
	}
	if _, ok := c.Env["COLORTERM"]; ok {
		t.Error("empty COLORTERM sent")
	}
	if _, ok := c.Env["HOME"]; ok {
		t.Error("HOME is not a terminal variable")
	}
}

// TestCapsFile: written caps read back; nil caps remove the file; no
// file means no caps.
func TestCapsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.caps.json")
	if c, err := ReadCaps(path); err != nil || c != nil {
		t.Fatalf("missing file: %v %v", c, err)
	}
	if err := writeCaps(path, &ClientCaps{Env: map[string]string{"TERM": "foot"}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v %v", fi, err)
	}
	c, err := ReadCaps(path)
	if err != nil || c == nil || c.Env["TERM"] != "foot" {
		t.Fatalf("read back %+v %v", c, err)
	}
	if err := writeCaps(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("nil caps left the file: %v", err)
	}
}

// TestEnvCapsSendsSSH: the client's SSH variables travel with the caps,
// so the UI knows whether the terminal that shows it is on another
// machine (file transfer: local copy or scp).
func TestEnvCapsSendsSSH(t *testing.T) {
	env := map[string]string{"SSH_CONNECTION": "10.0.0.5 50000 10.0.0.9 22", "SSH_CLIENT": "10.0.0.5 50000 22", "SSH_TTY": "/dev/pts/2"}
	c := EnvCaps(func(k string) string { return env[k] })
	for k, v := range env {
		if c.Env[k] != v {
			t.Errorf("%s = %q, want %q", k, c.Env[k], v)
		}
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-6: `termocode lane` prints its usage, refuses a host:folder that
// is not one (no ssh runs), and says when a file is not a pack.
func TestLaneUsageAndRefusals(t *testing.T) {
	env := func(string) string { return "" }
	var out, errw bytes.Buffer
	if code := runLaneIn(nil, nil, &out, &errw, env); code != 2 || !strings.Contains(errw.String(), "termocode lane pull") {
		t.Errorf("no args: %d %q", code, errw.String())
	}
	out.Reset()
	if code := runLaneIn([]string{"--help"}, nil, &out, &errw, env); code != 0 || !strings.Contains(out.String(), "termocode lane push") {
		t.Errorf("help: %d", code)
	}
	errw.Reset()
	if code := runLaneIn([]string{"pull", "-oProxyCommand=x:/a"}, nil, &out, &errw, env); code != 1 || !strings.Contains(errw.String(), "host:folder") {
		t.Errorf("bad remote: %d %q", code, errw.String())
	}
	junk := filepath.Join(t.TempDir(), "x.tlane")
	if err := os.WriteFile(junk, []byte("not a pack"), 0o600); err != nil {
		t.Fatal(err)
	}
	errw.Reset()
	if code := runLaneIn([]string{"info", junk}, nil, &out, &errw, env); code != 1 || !strings.Contains(errw.String(), "not a lane pack") {
		t.Errorf("junk: %d %q", code, errw.String())
	}
	if code := runLaneIn([]string{"unpack"}, nil, &out, &errw, env); code != 2 {
		t.Errorf("unpack without a file: %d", code)
	}
}

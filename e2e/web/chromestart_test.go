//go:build browser

package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A Chrome that starts but never answers (as one did on the macOS runner,
// stuck in Target.attachToTarget) ends the try within its wait, killed,
// instead of hanging the whole suite.
func TestChromeStartThatHangsIsStopped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands for Chrome")
	}
	exe := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 600\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const wait = 2 * time.Second
	start := time.Now()
	p, err := startPage(t, &Server{}, Desktop, "en-US", exe, wait)
	if err == nil {
		t.Fatalf("a Chrome that never answers started: %+v", p)
	}
	if !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v, want it to say Chrome gave no answer", err)
	}
	if took := time.Since(start); took > wait+20*time.Second {
		t.Fatalf("the try took %s, want about %s", took, wait)
	}
}

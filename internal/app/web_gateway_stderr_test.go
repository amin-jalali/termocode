//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (WEB-F1): a gateway that cannot start says why on its stderr
// (the port is busy, another panel runs); the laptop shows that reason,
// not just "did not start".
func TestWebGatewayFailShowsStderrReason(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fake-termocode")
	script := "#!/bin/sh\necho 'termocode web gateway: web port 8123: the port is busy' >&2\nexit 1\n"
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	old := webGatewayExe
	webGatewayExe = func() (string, error) { return exe, nil }
	defer func() { webGatewayExe = old }()

	h := newWebHub()
	defer h.stop()
	if _, err := startWebGatewayProcess(h, []string{"--port", "8123"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case msg := <-h.ch:
			if f, ok := msg.(webPanelFailMsg); ok {
				got := f.err.Error()
				if !strings.Contains(got, "web port 8123: the port is busy") || strings.Contains(got, "termocode web gateway:") {
					t.Fatalf("reason %q", got)
				}
				if !strings.Contains(got, "web_port") {
					t.Errorf("no hint to change the port: %q", got)
				}
				return
			}
		case <-deadline:
			t.Fatal("no fail message")
		}
	}
}

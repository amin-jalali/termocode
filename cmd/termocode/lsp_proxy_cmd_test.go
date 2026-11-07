package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLSPProxyArgs(t *testing.T) {
	m, argv, err := lspProxyArgs([]string{"--map", "/app=/home/u/proj", "--", "gopls", "serve"})
	if err != nil || m.Remote != "/app" || m.Local != "/home/u/proj" || strings.Join(argv, " ") != "gopls serve" {
		t.Fatalf("lspProxyArgs = %+v %q %v", m, argv, err)
	}
	for _, bad := range [][]string{
		nil,
		{"--", "gopls"},
		{"--map", "/app=/x"},
		{"--map", "/app=/x", "--"},
		{"--map", "app=/x", "--", "gopls"},
		{"--weird"},
	} {
		if _, _, err := lspProxyArgs(bad); err == nil {
			t.Errorf("lspProxyArgs(%q) gave no error", bad)
		}
	}
}

func TestLSPProxyUsageAndExitCodes(t *testing.T) {
	var out, errw bytes.Buffer
	if code := lspProxyMain([]string{"--help"}, nil, &out, &errw); code != 0 || !strings.Contains(out.String(), "--map REMOTE=LOCAL") {
		t.Errorf("--help = %d, %q", code, out.String())
	}
	out.Reset()
	if code := lspProxyMain([]string{"--map", "/app=/x"}, nil, &out, &errw); code != 2 || !strings.Contains(errw.String(), "after --") {
		t.Errorf("usage error = %d, %q", code, errw.String())
	}
}

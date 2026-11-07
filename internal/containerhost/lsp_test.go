package containerhost

import (
	"archive/tar"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

func TestFindCommandsOnPathAndProjectBins(t *testing.T) {
	h, _, c := newHost(t)
	c.AddTool(t, "gopls")
	c.WriteFile(t, "/app/node_modules/.bin/typescript-language-server", []byte("#!/bin/sh\n"), 0o755)
	c.WriteFile(t, "/app/.venv/bin/pyright-langserver", []byte("#!/bin/sh\n"), 0o755)
	got, err := h.FindCommands(t.Context(), []string{"gopls", "typescript-language-server", "pyright-langserver", "clangd"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got["gopls"], "/gopls") {
		t.Errorf("gopls = %q", got["gopls"])
	}
	if got["typescript-language-server"] != "/app/node_modules/.bin/typescript-language-server" {
		t.Errorf("ts = %q", got["typescript-language-server"])
	}
	if got["pyright-langserver"] != "/app/.venv/bin/pyright-langserver" {
		t.Errorf("pyright = %q", got["pyright-langserver"])
	}
	if _, ok := got["clangd"]; ok {
		t.Error("clangd is not in the image")
	}
	// Names and root are positional, never inside the script.
	last := c.Execs()[len(c.Execs())-1]
	if last.Argv[0] != "sh" || strings.Contains(last.Argv[2], "gopls") || strings.Contains(last.Argv[2], "/app") {
		t.Errorf("names are not positional: %q", last.Argv)
	}
}

func TestFindCommandsWithoutShell(t *testing.T) {
	d := fakedocker.New()
	c := d.Add(t, "distroless")
	c.Mkdir(t, "/app")
	c.NoShell()
	h, err := New(t.Context(), Config{Container: "distroless", Root: "/app", X: d})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.FindCommands(t.Context(), []string{"gopls"})
	if err != nil || len(got) != 0 {
		t.Errorf("FindCommands = %v, %v; want none", got, err)
	}
}

func TestFindCommandsStoppedContainer(t *testing.T) {
	h, _, c := newHost(t)
	c.Stop()
	if _, err := h.FindCommands(t.Context(), []string{"gopls"}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("err = %v, want ErrNotRunning", err)
	}
}

func TestLSPArgvIsInteractiveWithoutTTY(t *testing.T) {
	h, _, _ := newHost(t, func(c *Config) { c.User = "1000"; c.Context = "prod" })
	got := strings.Join(h.LSPArgv([]string{"gopls", "serve"}), " ")
	want := "docker --context prod exec -i -w /app -u 1000 api-1 gopls serve"
	if got != want {
		t.Errorf("LSPArgv = %q, want %q", got, want)
	}
}

func TestTreeTarStreamsTheRoot(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/main.go", []byte("package main\n"), 0o644)
	r, stop, err := h.TreeTar(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	tr := tar.NewReader(r)
	seen := false
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hd.Name == "app/main.go" {
			seen = true
		}
	}
	if !seen {
		t.Error("app/main.go not in the stream")
	}
}

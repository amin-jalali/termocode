package services

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// composeExtrasExec records the argv of each Capture and answers out.
type composeExtrasExec struct {
	argv [][]string
	out  string
	code int
}

func (e *composeExtrasExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	e.argv = append(e.argv, argv)
	return e.out, "", e.code, nil
}

func (e *composeExtrasExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return e.Capture(ctx, dir, argv...)
}

func (e *composeExtrasExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, func() {}, nil
}

func TestComposeEnvFilesGoBeforeTheVerb(t *testing.T) {
	c := &Compose{Root: "/p", Files: []string{"compose.yaml"}, EnvFiles: []string{".env.dev"}}
	got := c.Watch().Argv
	want := []string{"docker", "compose", "-f", "compose.yaml", "--env-file", ".env.dev", "watch"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestComposeResolvedConfig(t *testing.T) {
	x := &composeExtrasExec{out: "services:\n  web:\n    image: nginx\n"}
	c := &Compose{Root: "/p", X: x}
	text, err := c.ResolvedConfig(context.Background())
	if err != nil || text != x.out {
		t.Fatalf("got %q, %v", text, err)
	}
	if want := []string{"docker", "compose", "config"}; !slices.Equal(x.argv[0], want) {
		t.Fatalf("argv %q", x.argv[0])
	}
	x.code = 1
	if _, err := c.ResolvedConfig(context.Background()); err == nil {
		t.Fatal("a failing compose config: want an error")
	}
}

func TestComposeRunOneOff(t *testing.T) {
	c := &Compose{Root: "/p", Project: "shop"}
	cmd, err := c.RunOneOff("web", []string{"rails", "db:migrate"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "compose", "-p", "shop", "run", "--rm", "web", "rails", "db:migrate"}
	if !slices.Equal(cmd.Argv, want) || cmd.Dir != "/p" || cmd.Label != "run: web" {
		t.Fatalf("got %+v", cmd)
	}
	for _, bad := range []string{"", "-v", "--rm"} {
		if _, err := c.RunOneOff(bad, nil); err == nil {
			t.Fatalf("service %q: want an error (never read as a flag)", bad)
		}
	}
}

func TestFindEnvFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{".env", ".env.dev", "prod.env", "readme.md", "envy.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, ".env.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := FindEnvFiles(dir)
	if want := []string{".env", ".env.dev", "prod.env"}; !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

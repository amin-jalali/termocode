package containers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func TestDetectAndList(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "version.json", `{"Client":{"Version":"27.4"},"Server":{"Version":"27.4"}}`)
	f.Set(t, "ps.json", "{\"ID\":\"abc\",\"Names\":\"web\",\"State\":\"running\"}\n")
	old := os.Getenv("PATH")
	t.Setenv("PATH", f.PathEnv())
	defer os.Setenv("PATH", old)
	e := Detect(context.Background(), DetectOptions{LookPath: func(string) (string, error) { return f.Docker(), nil }, Exec: services.SpawnExec{}})
	if e.Binary != f.Docker() || !e.Reachable || e.Version.Version == "" {
		t.Fatalf("engine = %+v", e)
	}
	cs, err := e.ListContainers(context.Background())
	if err != nil || len(cs) != 1 || cs[0].ID != "abc" {
		t.Fatalf("containers=%+v err=%v", cs, err)
	}
}

func TestLoginPasswordOnlyOnStdin(t *testing.T) {
	f := servicestest.New(t)
	e := Engine{Binary: f.Docker(), Kind: "docker", Reachable: true, X: services.SpawnExec{}}
	if err := e.Login(context.Background(), "ghcr.io", "u", "topsecret"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	for _, c := range calls {
		if strings.Contains(c, "topsecret") {
			t.Fatal("secret in argv")
		}
	}
	b, err := os.ReadFile(filepath.Join(f.Data, "login.stdin"))
	if err != nil || string(b) != "topsecret\n" {
		t.Fatalf("stdin=%q err=%v", b, err)
	}
}

func TestActionsAndVisibleComposeArgv(t *testing.T) {
	f := servicestest.New(t)
	e := Engine{Binary: f.Docker(), Kind: "docker", Reachable: true, X: services.SpawnExec{}}
	for _, action := range []func(context.Context, string) error{e.Start, e.Stop, e.Restart, e.Pause, e.Unpause, e.RemoveContainer} {
		if err := action(context.Background(), "id"); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range f.Calls() {
		if strings.Contains(call, "-f") {
			t.Fatalf("unexpected force flag in %q", call)
		}
	}
	cmd := e.ComposeUp("shop", "/work/shop")
	want := []string{f.Docker(), "compose", "-p", "shop", "--project-directory", "/work/shop", "up", "-d"}
	if strings.Join(cmd.Argv, " ") != strings.Join(want, " ") {
		t.Fatalf("compose argv=%q", cmd.Argv)
	}
	cmd = e.ComposeWithFiles("shop", "/work/shop", []string{"compose.yaml", "compose.prod.yaml"}, "down")
	want = []string{f.Docker(), "compose", "-p", "shop", "--project-directory", "/work/shop", "-f", "compose.yaml", "-f", "compose.prod.yaml", "down"}
	if strings.Join(cmd.Argv, " ") != strings.Join(want, " ") || cmd.Dir != "/work/shop" {
		t.Fatalf("compose down argv=%q dir=%q", cmd.Argv, cmd.Dir)
	}
}

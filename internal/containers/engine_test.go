package containers

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

func TestStorageUserQueriesUseEngineFilters(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "volume-ls.json", "{\"Name\":\"named\",\"Driver\":\"local\"}\n")
	f.Set(t, "ps.json", "{\"ID\":\"abc\",\"Names\":\"web\",\"State\":\"running\"}\n")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	if _, err := e.ListDanglingVolumes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ContainersUsingVolume(context.Background(), "data"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0] != "volume ls --filter dangling=true --format {{json .}}" || calls[1] != "ps --all --filter volume=data --format {{json .}}" {
		t.Fatalf("storage queries = %#v", calls)
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

func TestImageVisibleCommandsUseArgumentLists(t *testing.T) {
	e := Engine{Binary: "docker"}
	cases := []struct {
		name      string
		got, want []string
	}{
		{"push", e.PushCommand("ghcr.io/acme/app:1").Argv, []string{"docker", "push", "ghcr.io/acme/app:1"}},
		{"run", e.RunCommand("alpine:3", "demo", []string{"8080:80"}, []string{"MODE=dev"}).Argv,
			[]string{"docker", "run", "--rm", "-it", "--name", "demo", "-p", "8080:80", "-e", "MODE=dev", "alpine:3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("argv = %#v, want %#v", tc.got, tc.want)
			}
		})
	}
}

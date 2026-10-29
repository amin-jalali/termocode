package containers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, engine, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", engine, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseComposeProjectFiles(t *testing.T) {
	got, err := ParseContainers([]byte(`{"ID":"c1","Names":["api-1"],"Labels":"com.docker.compose.project=shop,com.docker.compose.project.working_dir=/work/shop,com.docker.compose.project.config_files=compose.yaml,compose.prod.yaml"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].WorkingDir != "/work/shop" || strings.Join(got[0].ConfigFiles, ",") != "compose.yaml,compose.prod.yaml" {
		t.Fatalf("compose metadata = %#v", got)
	}
}

func TestParsersEngineFixtures(t *testing.T) {
	for _, engine := range []string{"docker27", "docker28", "podman5"} {
		t.Run(engine, func(t *testing.T) {
			cs, err := ParseContainers(fixture(t, engine, "ps"))
			if err != nil {
				t.Fatal(err)
			}
			if len(cs) != 2 {
				t.Fatalf("containers = %d", len(cs))
			}
			if cs[0].ID == "" || cs[0].Image == "" {
				t.Fatalf("incomplete container: %#v", cs[0])
			}
			if len(cs[0].Ports) == 0 {
				t.Fatalf("container ports missing: %#v", cs[0])
			}
			ims, err := ParseImages(fixture(t, engine, "images"))
			if err != nil {
				t.Fatal(err)
			}
			if len(ims) != 2 || ims[0].ID == "" {
				t.Fatalf("images = %#v", ims)
			}
			vs, err := ParseVolumes(fixture(t, engine, "volume-ls"))
			if err != nil {
				t.Fatal(err)
			}
			if len(vs) != 1 || vs[0].Name == "" {
				t.Fatalf("volumes = %#v", vs)
			}
			ns, err := ParseNetworks(fixture(t, engine, "network-ls"))
			if err != nil {
				t.Fatal(err)
			}
			if len(ns) != 1 || ns[0].Name == "" {
				t.Fatalf("networks = %#v", ns)
			}
			ctx, err := ParseContexts(fixture(t, engine, "context-ls"))
			if err != nil {
				t.Fatal(err)
			}
			if len(ctx) == 0 || !ctx[0].Current {
				t.Fatalf("contexts = %#v", ctx)
			}
			v, err := ParseVersion(fixture(t, engine, "version"))
			if err != nil {
				t.Fatal(err)
			}
			if v.Version == "" {
				t.Fatalf("version = %#v", v)
			}
			stats, err := ParseStats(fixture(t, engine, "stats"))
			if err != nil {
				t.Fatal(err)
			}
			if len(stats) != 1 || stats[0].Container == "" {
				t.Fatalf("stats = %#v", stats)
			}
			events, err := ParseEvents(fixture(t, engine, "events"))
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].Type == "" {
				t.Fatalf("events = %#v", events)
			}
			history, err := ParseHistory(fixture(t, engine, "history"))
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 1 || history[0].CreatedBy == "" {
				t.Fatalf("history = %#v", history)
			}
			inspect, err := ParseInspect(fixture(t, engine, "inspect"))
			if err != nil {
				t.Fatal(err)
			}
			if inspect == nil {
				t.Fatal("empty inspect")
			}
		})
	}
}

func TestStateFromPS(t *testing.T) {
	cases := map[string]string{"running": "running", "Up 5 minutes": "running", "Paused": "paused", "created": "created", "Exited (1) 2 minutes ago": "exited", "dead": "dead", "restarting (1) 2 seconds ago": "restarting", "removing": "removing", "": "unknown", "custom-state": "custom-state"}
	for in, want := range cases {
		if got := StateFromPS(in, ""); got != want {
			t.Errorf("StateFromPS(%q)=%q, want %q", in, got, want)
		}
	}
	if got := StateFromPS("", "Up 3 seconds"); got != "running" {
		t.Errorf("status fallback = %q", got)
	}
}

func TestParsePortsText(t *testing.T) {
	ports := ParsePortsText("0.0.0.0:8081->8080/tcp, :::8443->443/tcp, 53/udp")
	if len(ports) != 3 || ports[0].PublicPort != 8081 || ports[1].IP != "::" || ports[2].Type != "udp" {
		t.Fatalf("ports = %#v", ports)
	}
}

func TestGroupByProject(t *testing.T) {
	input := []Container{{ID: "a", Project: "other-project"}, {ID: "b", Labels: map[string]string{"io.podman.compose.project": "current", "io.podman.compose.project.working_dir": "/work/current"}}, {ID: "c"}, {ID: "d", Project: "current"}}
	got := GroupByProject(input, "current")
	if len(got) != 3 || got[0].Name != "current" || got[0].WorkingDir != "/work/current" || got[1].Name != "other-project" || got[2].Name != "other" || !got[2].IsOther {
		t.Fatalf("groups = %#v", got)
	}
}

func TestReadRegistryConfigDoesNotReturnAuth(t *testing.T) {
	cfg, err := ReadRegistryConfig(filepath.Join("testdata", "registry-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CredsStore != "desktop" || len(cfg.Registries) != 3 {
		t.Fatalf("config = %#v", cfg)
	}
	encoded := fmt.Sprintf("%#v %v", cfg, cfg)
	for _, secret := range []string{"c2VjcmV0LW5ldmVyLXJldHVybg==", "another-secret-value"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("auth leaked into result: %s", encoded)
		}
	}
	if cfg.Registries[0].Host != "ghcr.io" || cfg.Registries[0].CredHelper != "secretservice" {
		t.Fatalf("ghcr registry = %#v", cfg.Registries[0])
	}
}

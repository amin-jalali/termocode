package containers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CP-12: podman connections as CONTEXTS and the Docker Hub search.

func TestParseConnectionsFixtures(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile("testdata/podman5/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	cs, err := ParseConnections(read("connection-ls.json"))
	if err != nil || len(cs) != 2 {
		t.Fatalf("array: %+v %v", cs, err)
	}
	if cs[0].Name != "podman-machine-default" || !cs[0].Current || cs[1].Current || !strings.HasPrefix(cs[1].DockerEndpoint, "ssh://dev@build.example") {
		t.Errorf("array rows = %+v", cs)
	}
	for _, c := range cs {
		if strings.Contains(c.DockerEndpoint+c.Host+c.Description, ".ssh") || strings.Contains(c.Description, "machine/machine") {
			t.Errorf("the identity file path leaked: %+v", c)
		}
	}
	cs, err = ParseConnections(read("connection-ls-lines.json"))
	if err != nil || len(cs) != 2 || cs[0].Name != "local" || !cs[0].Current || cs[1].Name != "lab" || cs[1].Current || cs[1].DockerEndpoint != "ssh://root@10.0.0.7/run/podman/podman.sock" {
		t.Fatalf("lines: %+v %v", cs, err)
	}
	for _, empty := range []string{"", "null", "[]", "\n"} {
		if cs, err := ParseConnections([]byte(empty)); err != nil || len(cs) != 0 {
			t.Errorf("%q: %+v %v", empty, cs, err)
		}
	}
	if _, err := ParseConnections([]byte("not json")); err == nil {
		t.Error("garbage parsed")
	}
	// A row without a name is skipped, not shown as an empty choice.
	if cs, _ := ParseConnections([]byte(`[{"URI":"unix:///x"},{"Name":"ok"}]`)); len(cs) != 1 || cs[0].Name != "ok" {
		t.Errorf("nameless row: %+v", cs)
	}
}

func podmanEngine(t *testing.T) (*servicestest.Fake, Engine) {
	f := servicestest.New(t)
	bin := f.InstallPodman(t)
	f.Set(t, "version.json", `{"Client":{"Version":"5.2.0"},"Server":{"Version":"5.2.0"}}`)
	return f, Engine{Binary: bin, Kind: "podman", Reachable: true, X: services.SpawnExec{}}
}

// A session connection goes on every podman call as
// CONTAINER_CONNECTION; listing connections and making one the default
// never carry it.
func TestPodmanConnectionOnEveryCall(t *testing.T) {
	f, e := podmanEngine(t)
	t.Setenv("CONTAINER_CONNECTION", "")
	t.Setenv("DOCKER_CONTEXT", "")
	f.Set(t, "connection-ls.json", `[{"Name":"a","URI":"unix:///a","Default":true},{"Name":"b","URI":"ssh://b"}]`)
	e.Context = "b"
	ctx := context.Background()
	if _, err := e.ListContainers(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	cs, err := e.ListContexts(ctx)
	if err != nil || len(cs) != 2 {
		t.Fatalf("ListContexts on podman = %+v %v", cs, err)
	}
	if err := e.UseContext(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	got := f.ConnCalls()
	want := []string{"b|ps --all --no-trunc --format {{json .}}", "b|start abc", "|system connection ls --format json", "|system connection default a"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range f.CtxCalls() {
		if !strings.HasPrefix(c, "|") {
			t.Errorf("podman call carried DOCKER_CONTEXT: %q", c)
		}
	}
}

func TestDockerUnaffectedByConnectionEnv(t *testing.T) {
	f := servicestest.New(t)
	t.Setenv("CONTAINER_CONNECTION", "")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, Context: "prod"}
	if _, err := e.ListContainers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := f.ConnCalls(); len(c) != 1 || !strings.HasPrefix(c[0], "|") {
		t.Fatalf("docker call carried CONTAINER_CONNECTION: %q", c)
	}
}

func TestParseSearchDockerAndPodman(t *testing.T) {
	for _, dir := range []string{"docker27", "podman5"} {
		b, err := os.ReadFile("testdata/" + dir + "/search.json")
		if err != nil {
			t.Fatal(err)
		}
		rs, err := ParseSearch(b)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if len(rs) != 2 {
			t.Fatalf("%s: %d results %+v (an unsafe name must be dropped)", dir, len(rs), rs)
		}
		if rs[0].Name != "nginx" || !rs[0].Official || rs[0].Stars != 20512 || rs[0].Description != "Official build of Nginx." {
			t.Errorf("%s: first = %+v", dir, rs[0])
		}
		if rs[1].Name != "bitnami/nginx" || rs[1].Official || rs[1].Stars != 195 {
			t.Errorf("%s: second = %+v", dir, rs[1])
		}
	}
}

func TestSearchArgvAndTermRules(t *testing.T) {
	f := servicestest.New(t)
	b, _ := os.ReadFile("testdata/docker27/search.json")
	f.Set(t, "search.json", string(b))
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	rs, err := e.Search(context.Background(), "NGINX", 25)
	if err != nil || len(rs) != 2 {
		t.Fatalf("search: %+v %v", rs, err)
	}
	if c := f.Calls(); len(c) != 1 || c[0] != "search --format {{json .}} --limit 25 --no-trunc nginx" {
		t.Fatalf("argv = %q", c)
	}
	for _, bad := range []string{"", "-rm", "a b", "a;b", "$(x)", strings.Repeat("a", 101), "a\nb", "../x"} {
		if err := ValidSearchTerm(bad); err == nil {
			t.Errorf("ValidSearchTerm(%q) accepted", bad)
		}
		if _, err := e.Search(context.Background(), bad, 25); err == nil {
			t.Errorf("Search(%q) ran", bad)
		}
	}
	if n := len(f.Calls()); n != 1 {
		t.Errorf("a refused term reached the engine (%d calls)", n)
	}
	// The limit is clamped to the engine's 1..100.
	if _, err := e.Search(context.Background(), "redis", 1000); err != nil {
		t.Fatal(err)
	}
	if c := f.Calls(); !strings.Contains(c[len(c)-1], "--limit 100 ") {
		t.Errorf("limit not clamped: %q", c[len(c)-1])
	}
	f.Set(t, "search.err", "Error response from daemon: Get \"https://index.docker.io/v1/search\": dial tcp: lookup index.docker.io: no such host")
	if _, err := e.Search(context.Background(), "redis", 10); err == nil || !strings.Contains(err.Error(), "no such host") {
		t.Errorf("search error = %v", err)
	}
}

func TestCleanTextDropsEscapes(t *testing.T) {
	got := cleanText("evil \x1b[31mred\x1b[0m\x1b]0;title\x07!\n", 200)
	if got != "evil red!" {
		t.Errorf("cleanText = %q", got)
	}
	if got := cleanText(strings.Repeat("a", 300), 10); len([]rune(got)) != 10 {
		t.Errorf("cut = %q", got)
	}
}

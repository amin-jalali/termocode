package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFileConfig(t *testing.T) {
	src := `{
  // comment
  "compose": { "file": "dev.yaml", "profiles": ["dev"], "project": "p" },
  "services": [
    { "name": "api", "command": "go run ./cmd/api", "cwd": "srv", "env": { "PORT": "8080" },
      "port": 8080, "health": { "http": "http://localhost:8080/healthz" } },
    { "name": "redis", "command": "redis-server --port 6379", "port": 6379 }, /* trailing */
  ]
}`
	fc, err := ParseFileConfig([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Compose.File != "dev.yaml" || fc.Compose.Profiles[0] != "dev" || fc.Compose.Project != "p" {
		t.Errorf("compose = %+v", fc.Compose)
	}
	svcs := fc.LocalServices()
	if len(svcs) != 2 {
		t.Fatalf("services = %+v", svcs)
	}
	api, redis := svcs[0], svcs[1]
	if api.Backend != "local" || api.Command != "go run ./cmd/api" || api.Cwd != "srv" || api.Env["PORT"] != "8080" {
		t.Errorf("api = %+v", api)
	}
	if api.Health.HTTP != "http://localhost:8080/healthz" || api.Ports[0].Published != 8080 {
		t.Errorf("api health/ports = %+v %+v", api.Health, api.Ports)
	}
	if strings.Join(api.EnvKeys, ",") != "PORT" || api.Source != ConfigPath {
		t.Errorf("api keys/source = %v %q", api.EnvKeys, api.Source)
	}
	// port without health → default TCP probe.
	if redis.Health.TCP != 6379 {
		t.Errorf("redis probe = %+v", redis.Health)
	}
}

func TestParseFileConfigInvalid(t *testing.T) {
	cases := map[string]string{
		`{"services":[{"name":"Bad Name","command":"x"}]}`:                      "name",
		`{"services":[{"name":"a","command":""}]}`:                              "command",
		`{"services":[{"name":"a","command":"x"},{"name":"a","command":"y"}]}`:  "twice",
		`{"services":[{"name":"a","command":"x","port":70000}]}`:                "port",
		`{"services":[{"name":"a","command":"x","cwd":"../out"}]}`:              "cwd",
		`{"services":[{"name":"a","command":"x","health":{"http":"ftp://x"}}]}`: "http",
		`{nope`: "services.json",
	}
	for src, want := range cases {
		_, err := ParseFileConfig([]byte(src))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseFileConfig(%s) err = %v, want it to mention %q", src, err, want)
		}
	}
}

func TestTemplateParses(t *testing.T) {
	fc, err := ParseFileConfig([]byte(Template))
	if err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if len(fc.Services) == 0 {
		t.Error("template has no example service")
	}
	if !strings.Contains(Template, "//") {
		t.Error("template should explain itself with comments")
	}
	if json.Valid([]byte(Template)) {
		t.Log("template is plain JSON")
	}
}

func TestParseProcfile(t *testing.T) {
	src := "# comment\nweb: npm run dev\nworker:   bundle exec sidekiq -q $PORT\n\nbad line\nBad-Name: x\n"
	got := ParseProcfile(src, "Procfile")
	if len(got) != 2 {
		t.Fatalf("procfile = %+v", got)
	}
	if got[0].Name != "web" || got[0].Command != "npm run dev" || got[0].Source != "Procfile" || got[0].Backend != "local" {
		t.Errorf("web = %+v", got[0])
	}
	if got[1].Command != "bundle exec sidekiq -q $PORT" {
		t.Errorf("worker = %+v ($PORT is not expanded)", got[1])
	}
}

func TestDetect(t *testing.T) {
	noEnv := func(string) string { return "" }
	hasDocker := func(string) (string, error) { return "/usr/bin/docker", nil }
	noDocker := func(string) (string, error) { return "", os.ErrNotExist }
	write := func(t *testing.T, root, name, body string) {
		t.Helper()
		p := filepath.Join(root, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("nothing", func(t *testing.T) {
		d := Detect(t.TempDir(), noEnv, hasDocker, "linux")
		if !d.Empty() {
			t.Errorf("want empty, got %+v", d)
		}
	})
	t.Run("compose order", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "docker-compose.yml", "services: {}\n")
		write(t, root, "compose.yml", "services: {}\n")
		d := Detect(root, noEnv, hasDocker, "linux")
		if strings.Join(d.ComposeFiles, ",") != "compose.yml" || !d.Compose {
			t.Errorf("files = %v compose=%v", d.ComposeFiles, d.Compose)
		}
	})
	t.Run("COMPOSE_FILE wins", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "compose.yaml", "services: {}\n")
		env := func(k string) string {
			if k == "COMPOSE_FILE" {
				return "a.yaml:b.yaml"
			}
			return ""
		}
		d := Detect(root, env, hasDocker, "linux")
		if strings.Join(d.ComposeFiles, ",") != "a.yaml,b.yaml" {
			t.Errorf("files = %v", d.ComposeFiles)
		}
	})
	t.Run("services.json compose file and profiles", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "compose.yaml", "services: {}\n")
		write(t, root, "dev.yaml", "services: {}\n")
		write(t, root, ConfigPath, `{"compose":{"file":"dev.yaml","profiles":["dev"]},"services":[{"name":"web","command":"vite"}]}`)
		write(t, root, "Procfile", "web: other\nworker: w\n")
		d := Detect(root, noEnv, hasDocker, "linux")
		if strings.Join(d.ComposeFiles, ",") != "dev.yaml" || strings.Join(d.Profiles, ",") != "dev" {
			t.Errorf("compose = %v %v", d.ComposeFiles, d.Profiles)
		}
		if len(d.Local) != 2 || d.Local[0].Command != "vite" || d.Local[1].Name != "worker" {
			t.Errorf("local = %+v (services.json wins over Procfile)", d.Local)
		}
	})
	t.Run("no docker", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "compose.yaml", "services: {}\n")
		d := Detect(root, noEnv, noDocker, "linux")
		if d.Compose || d.Problem.Kind != ProblemNoDocker || len(d.ComposeFiles) != 1 {
			t.Errorf("got %+v", d)
		}
	})
	t.Run("android has no compose and no nag", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "compose.yaml", "services: {}\n")
		write(t, root, "Procfile.dev", "web: python -m http.server\n")
		d := Detect(root, noEnv, hasDocker, "android")
		if d.Compose || len(d.ComposeFiles) != 0 || d.Problem.Kind != ProblemNone {
			t.Errorf("android got %+v", d)
		}
		if len(d.Local) != 1 || d.Local[0].Source != "Procfile.dev" {
			t.Errorf("local = %+v", d.Local)
		}
	})
	t.Run("bad services.json", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, ConfigPath, `{bad`)
		d := Detect(root, noEnv, hasDocker, "linux")
		if d.ConfigErr == nil {
			t.Error("want a config error")
		}
		if d.Empty() {
			t.Error("a broken services.json is not 'no services'")
		}
	})
}

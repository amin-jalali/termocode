package services

import (
	"os"
	"strings"
	"testing"
	"time"
)

func readTD(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePSJSONLines(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	st, err := ParsePS(readTD(t, "ps-lines.json"), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 3 {
		t.Fatalf("got %d statuses, want 3", len(st))
	}
	pg := st["postgres"]
	if pg.State != Healthy || pg.Health != "healthy" || pg.Text != "Up 2 hours (healthy)" || pg.Image != "postgres:16" {
		t.Errorf("postgres = %+v", pg)
	}
	if len(pg.Ports) != 1 || pg.Ports[0].Published != 5432 || pg.Ports[0].Target != 5432 {
		t.Errorf("postgres ports = %+v (IPv6 duplicate must fold)", pg.Ports)
	}
	if got := at.Sub(pg.StartedAt); got != 2*time.Hour {
		t.Errorf("postgres up for %v, want 2h", got)
	}
	if r := st["redis"]; r.State != Running {
		t.Errorf("redis state = %v", r.State)
	}
	api := st["api"]
	if api.State != Exited || api.ExitCode != 1 || len(api.Ports) != 0 {
		t.Errorf("api = %+v (unpublished port must drop)", api)
	}
}

func TestParsePSArrayAndCasing(t *testing.T) {
	st, err := ParsePS(readTD(t, "ps-array.json"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st["postgres"].State != Healthy || st["postgres"].Health != "healthy" {
		t.Errorf("postgres = %+v (Health casing)", st["postgres"])
	}
	if st["mailhog"].State != Restarting {
		t.Errorf("mailhog = %+v", st["mailhog"])
	}
}

func TestParsePSEmptyAndBad(t *testing.T) {
	for _, in := range []string{"", "\n", "[]"} {
		st, err := ParsePS([]byte(in), time.Now())
		if err != nil || len(st) != 0 {
			t.Errorf("ParsePS(%q) = %v, %v", in, st, err)
		}
	}
	if _, err := ParsePS([]byte("not json"), time.Now()); err == nil {
		t.Error("bad input must fail")
	}
}

// One row per state of the plan's §2.7 table.
func TestStateFromPS(t *testing.T) {
	cases := []struct {
		state, health, text string
		code                int
		want                State
	}{
		{"running", "healthy", "Up 2 hours (healthy)", 0, Healthy},
		{"running", "", "Up 2 hours", 0, Running},
		{"running", "unhealthy", "Up 2 hours (unhealthy)", 0, Unhealthy},
		{"running", "starting", "Up 12 seconds (health: starting)", 0, Starting},
		{"restarting", "", "Restarting (1) 3 seconds ago", 1, Restarting},
		{"exited", "", "Exited (0) 5 minutes ago", 0, Stopped},
		{"exited", "", "Exited (1) 5 minutes ago", 1, Exited},
		{"dead", "", "Dead", 137, Exited},
		{"created", "", "Created", 0, Stopped},
		{"paused", "", "Up 1 hour (Paused)", 0, Paused},
		{"removing", "", "Removal In Progress", 0, Stopping},
		{"", "", "Up 3 minutes (healthy)", 0, Healthy},
		{"", "", "Exited (2) 1 second ago", 0, Exited},
		{"", "", "Restarting (1) 3 seconds ago", 0, Restarting},
		{"", "", "", 0, Unknown},
	}
	for _, c := range cases {
		if got := StateFromPS(c.state, c.health, c.text, c.code); got != c.want {
			t.Errorf("StateFromPS(%q,%q,%q,%d) = %v, want %v", c.state, c.health, c.text, c.code, got, c.want)
		}
	}
	if exitCodeFromText("Exited (2) 1 second ago") != 2 {
		t.Error("exit code from text")
	}
}

func TestUpFor(t *testing.T) {
	cases := map[string]time.Duration{
		"Up 2 hours (healthy)":            2 * time.Hour,
		"Up About an hour":                time.Hour,
		"Up About a minute":               time.Minute,
		"Up 12 seconds":                   12 * time.Second,
		"Up Less than a second":           0,
		"Up 3 days":                       72 * time.Hour,
		"Up 2 weeks":                      14 * 24 * time.Hour,
		"Up 5 minutes (health: starting)": 5 * time.Minute,
	}
	for in, want := range cases {
		got, ok := upFor(in)
		if !ok || got != want {
			t.Errorf("upFor(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	if _, ok := upFor("Exited (0) 5 minutes ago"); ok {
		t.Error("exited text has no uptime")
	}
}

func TestFormatUptime(t *testing.T) {
	cases := map[time.Duration]string{
		12 * time.Second:              "12s",
		3*time.Minute + 5*time.Second: "3m",
		2*time.Hour + 14*time.Minute:  "2h 14m",
		2 * time.Hour:                 "2h",
		3*24*time.Hour + 5*time.Hour:  "3d",
		-time.Second:                  "0s",
	}
	for d, want := range cases {
		if got := FormatUptime(d); got != want {
			t.Errorf("FormatUptime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig(readTD(t, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "termocode" {
		t.Errorf("project = %q", cfg.Project)
	}
	var names []string
	for _, s := range cfg.Services {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "api,mailhog,postgres,redis" {
		t.Fatalf("names = %v", names)
	}
	api, mh, pg := cfg.Services[0], cfg.Services[1], cfg.Services[2]
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "postgres" || api.Image != "" {
		t.Errorf("api = %+v", api)
	}
	if strings.Join(api.EnvKeys, ",") != "DATABASE_URL,PORT" {
		t.Errorf("api env keys = %v", api.EnvKeys)
	}
	if !pg.HasHealthcheck || pg.Image != "postgres:16" || pg.Ports[0].Published != 5432 {
		t.Errorf("postgres = %+v", pg)
	}
	if len(mh.Profiles) != 1 || mh.Profiles[0] != "mail" || mh.Ports[0].Host != "127.0.0.1" || mh.Ports[0].Published != 8025 {
		t.Errorf("mailhog = %+v", mh)
	}
	for _, s := range cfg.Services {
		if s.Backend != "compose" {
			t.Errorf("%s backend = %q", s.Name, s.Backend)
		}
	}
	// Secret-looking values come back for the redactor; plain ones do not.
	got := strings.Join(cfg.SecretValues, ",")
	if !strings.Contains(got, "hunter2hunter2") || strings.Contains(got, "postgres://") {
		t.Errorf("secrets = %v", cfg.SecretValues)
	}
	if strings.Contains(got, "abc") || strings.Contains(got, ",postgres,") || strings.HasPrefix(got, "postgres,") {
		t.Errorf("short or plain values must not be secrets: %v", cfg.SecretValues)
	}
	// A service struct never carries env values.
	for _, s := range cfg.Services {
		if len(s.Env) != 0 {
			t.Errorf("%s keeps env values", s.Name)
		}
	}
}

func TestComposeServiceLines(t *testing.T) {
	src := "name: x\n# c\nservices:\n  postgres:\n    image: postgres:16\n    environment:\n      api: no\n  api:\n    build: .\nvolumes:\n  data:\n"
	got := ComposeServiceLines(src)
	if got["postgres"] != 4 || got["api"] != 8 || got["data"] != 0 {
		t.Errorf("lines = %v", got)
	}
}

func TestProblemFromStderr(t *testing.T) {
	cases := []struct {
		in   string
		want ProblemKind
	}{
		{"Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", ProblemDaemonDown},
		{"Cannot connect to Podman. Please verify your connection", ProblemDaemonDown},
		{"permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: Get ...", ProblemNoPermission},
		{"docker: 'compose' is not a docker command.\nSee 'docker --help'", ProblemNoCompose},
		{"docker: 'context' is not a docker command.", ProblemOther},
		{"podman: permission denied while connecting to /run/user/1000/podman/podman.sock", ProblemNoPermission},
		{"unknown shorthand flag: 'f' in -f", ProblemNoCompose},
		{"validating /w/compose.yaml: services.api Additional property foo is not allowed", ProblemBadConfig},
		{"yaml: line 5: mapping values are not allowed in this context", ProblemBadConfig},
		{"service \"db\" refers to undefined volume x: invalid compose project", ProblemBadConfig},
		{"something else broke\nmore", ProblemOther},
		{"", ProblemNone},
	}
	for _, c := range cases {
		p := ProblemFromStderr(c.in)
		if p.Kind != c.want {
			t.Errorf("ProblemFromStderr(%q) = %v, want %v", c.in, p.Kind, c.want)
		}
	}
	if p := ProblemFromStderr("yaml: line 5: mapping values\nsecond"); p.Detail != "yaml: line 5: mapping values" {
		t.Errorf("detail = %q (first line only)", p.Detail)
	}
}

func TestPortString(t *testing.T) {
	cases := map[string]Port{
		"5432":              {Published: 5432, Target: 5432, Proto: "tcp"},
		"0.0.0.0:5432→5432": {Host: "0.0.0.0", Published: 5432, Target: 5432},
		"8081→8080":         {Published: 8081, Target: 8080},
		"53/udp":            {Published: 53, Target: 53, Proto: "udp"},
	}
	for want, p := range cases {
		if got := p.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", p, got, want)
		}
	}
}

func TestProblemMessageNamesTheEngine(t *testing.T) {
	cases := []struct {
		p    Problem
		want string
	}{
		{Problem{Kind: ProblemDaemonDown}, "Docker is not running"},
		{Problem{Kind: ProblemDaemonDown, Engine: "podman"}, "Podman is not running"},
		{Problem{Kind: ProblemNoPermission}, "No access to the Docker socket"},
		{Problem{Kind: ProblemNoPermission, Engine: "podman"}, "No access to the Podman socket"},
		{ProblemFromStderr("Cannot connect to Podman. Please verify your connection"), "Podman is not running"},
		{ProblemFromStderr("podman: permission denied while connecting to /run/user/1000/podman/podman.sock"), "No access to the Podman socket"},
		{ProblemFromStderr("Cannot connect to the Docker daemon at unix:///var/run/docker.sock."), "Docker is not running"},
	}
	for _, c := range cases {
		if got := c.p.Message(); got != c.want {
			t.Errorf("%+v.Message() = %q, want %q", c.p, got, c.want)
		}
	}
}

func TestParsePSReplicasShowTheWorseState(t *testing.T) {
	up := `{"Service":"api","State":"running","Health":"","Status":"Up 2 minutes","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":8080,"Protocol":"tcp"}]}`
	healthy := `{"Service":"api","State":"running","Health":"healthy","Status":"Up 2 minutes (healthy)"}`
	sick := `{"Service":"api","State":"running","Health":"unhealthy","Status":"Up 2 minutes (unhealthy)"}`
	down := `{"Service":"api","State":"exited","Status":"Exited (1) 1 minute ago","ExitCode":1}`
	cases := []struct {
		lines []string
		want  State
	}{
		{[]string{up, down}, Exited},
		{[]string{down, up}, Exited},
		{[]string{healthy, sick}, Unhealthy},
		{[]string{sick, healthy}, Unhealthy},
		{[]string{healthy, up}, Running},
		{[]string{up, healthy}, Running},
	}
	for _, c := range cases {
		st, err := ParsePS([]byte(strings.Join(c.lines, "\n")), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got := st["api"].State; got != c.want {
			t.Errorf("replicas %v: state = %v, want %v", c.lines, got, c.want)
		}
		if strings.Contains(strings.Join(c.lines, ""), "8080") && len(st["api"].Ports) != 1 {
			t.Errorf("replicas %v: ports = %v, want the up replica's port", c.lines, st["api"].Ports)
		}
	}
}

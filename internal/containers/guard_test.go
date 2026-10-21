package containers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CP-9: inspect output for the AI keeps env NAMES and masks every value;
// labels outside the compose / OCI keys that look secret, auth fields
// and secret-looking arguments are masked too.
func TestMaskInspectEnvLabelsAndAuth(t *testing.T) {
	raw := `[{"Id":"9f3c","Config":{"Env":["POSTGRES_PASSWORD=hunter2hunter2","PATH=/usr/local/bin","EMPTY=","NOVALUE"],
"Labels":{"com.docker.compose.project":"shop","org.opencontainers.image.version":"1.4","maintainer":"ops team","traefik.auth.token":"tok-abcdef123456"},
"Cmd":["postgres","--password=s3cr3tvalue1"]},"Args":["--db-pass=s3cr3tvalue2"],
"HostConfig":{"RegistryAuth":"eyJ1c2VybmFtZSI6ImEifQ=="},"ContainerConfig":{"Env":["API_KEY=abcdefgh12345678"]}}]`
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	out, n := MaskInspect(v)
	b, _ := json.Marshal(out)
	s := string(b)
	for _, bad := range []string{"hunter2hunter2", "/usr/local/bin", "tok-abcdef123456", "s3cr3tvalue1", "s3cr3tvalue2", "eyJ1c2VybmFtZSI6ImEifQ==", "abcdefgh12345678"} {
		if strings.Contains(s, bad) {
			t.Errorf("value %q reached the masked output:\n%s", bad, s)
		}
	}
	for _, want := range []string{`"POSTGRES_PASSWORD=[REDACTED]"`, `"PATH=[REDACTED]"`, `"EMPTY="`, `"NOVALUE"`, `"API_KEY=[REDACTED]"`,
		`"com.docker.compose.project":"shop"`, `"org.opencontainers.image.version":"1.4"`, `"maintainer":"ops team"`,
		`"traefik.auth.token":"[REDACTED]"`, `"RegistryAuth":"[REDACTED]"`, `"postgres"`} {
		if !strings.Contains(s, want) {
			t.Errorf("masked output has no %s:\n%s", want, s)
		}
	}
	if n < 7 {
		t.Errorf("masked %d values, want at least 7", n)
	}
}

// List labels: only the compose and OCI keys reach the model.
func TestSafeLabels(t *testing.T) {
	got := SafeLabels(map[string]string{"com.docker.compose.service": "api", "org.opencontainers.image.source": "https://x",
		"traefik.http.routers.api.rule": "Host(`a`)", "secret.token": "abc"})
	if len(got) != 2 || got["com.docker.compose.service"] != "api" || got["org.opencontainers.image.source"] != "https://x" {
		t.Errorf("SafeLabels = %v", got)
	}
}

// Ids, names and image references the model gives: argv-safe and strict.
func TestValidRefAndName(t *testing.T) {
	for _, ok := range []string{"nginx", "nginx:1.27", "ghcr.io/acme/api:1.4", "localhost:5000/team/app:dev",
		"postgres@sha256:" + strings.Repeat("a", 64), "library/redis:7-alpine"} {
		if err := ValidImageRef(ok); err != nil {
			t.Errorf("ValidImageRef(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-q", "--help", "nginx;rm -rf /", "Nginx", "a b", "nginx:", "$(id)", "nginx\n", strings.Repeat("a", 300)} {
		if err := ValidImageRef(bad); err == nil {
			t.Errorf("ValidImageRef(%q) accepted", bad)
		}
	}
	for _, ok := range []string{"9f3c2e1a1234", "termocode-api-1", "my_db.1", "shop"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("ValidName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-f", "--all", "a/b", "a b", "x;y", "`id`", ".hidden", strings.Repeat("n", 200)} {
		if err := ValidName(bad); err == nil {
			t.Errorf("ValidName(%q) accepted", bad)
		}
	}
}

// Logs and typed inspect build plain argv lists.
func TestLogsAndInspectTypeArgv(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "logs.txt", "line one\nline two\n")
	f.Set(t, "inspect.json", `[{"Id":"abc"}]`)
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, Context: "prod"}
	out, err := e.Logs(context.Background(), "abc", 50, "10m")
	if err != nil || !strings.Contains(out, "line two") {
		t.Fatalf("logs = %q, %v", out, err)
	}
	if _, err := e.InspectType(context.Background(), "volume", "data"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	want := []string{"logs --tail 50 --timestamps --since 10m abc", "inspect --type volume data"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("argv = %q, want %q", calls, want)
	}
	for _, c := range f.CtxCalls() {
		if !strings.HasPrefix(c, "prod|") {
			t.Errorf("call without the session context: %q", c)
		}
	}
	_ = os.Remove(filepath.Join(f.Data, "argv.log"))
}

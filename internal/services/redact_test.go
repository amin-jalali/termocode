package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	in := strings.Join([]string{
		`DATABASE_URL=postgres://app:hunter2hunter2@localhost:5432/app`,
		`connecting to redis://:s3cr3tpass@redis:6379/0`,
		`export API_TOKEN="abcdef123456"`,
		`  POSTGRES_PASSWORD: hunter2hunter2`,
		`PORT=8080`,
		`FATAL:  password authentication failed for user "app"`,
		`http://localhost:8080/healthz`,
	}, "\n")
	out, n := Redact(in)
	for _, leak := range []string{"hunter2hunter2", "s3cr3tpass", "abcdef123456"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in\n%s", leak, out)
		}
	}
	for _, keep := range []string{
		"postgres://app:[REDACTED]@localhost:5432/app",
		"redis://:[REDACTED]@redis:6379/0",
		"PORT=8080",
		`password authentication failed for user "app"`,
		"http://localhost:8080/healthz",
		"POSTGRES_PASSWORD: [REDACTED]",
	} {
		if !strings.Contains(out, keep) {
			t.Errorf("want %q in\n%s", keep, out)
		}
	}
	if n != 4 {
		t.Errorf("count = %d, want 4\n%s", n, out)
	}
}

func TestEnvFileSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	body := "# c\nPOSTGRES_PASSWORD=hunter2hunter2\nPOSTGRES_USER=postgres\nexport API_KEY='k-1234567890'\nSHORT_SECRET=abc\nDATABASE_URL=\"postgres://app:hunter2hunter2@db/app\"\nNAME=postgresql-server\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(EnvFileSecrets(p), "|")
	for _, want := range []string{"hunter2hunter2", "k-1234567890", "postgres://app:hunter2hunter2@db/app"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// Plain or short values would shred normal text: never registered.
	for _, v := range EnvFileSecrets(p) {
		if v == "postgres" || v == "abc" || v == "postgresql-server" {
			t.Errorf("must not register %q", v)
		}
	}
	keys := EnvFileKeys(p)
	if strings.Join(keys, ",") != "API_KEY,DATABASE_URL,NAME,POSTGRES_PASSWORD,POSTGRES_USER,SHORT_SECRET" {
		t.Errorf("keys = %v", keys)
	}
}

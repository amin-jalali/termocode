package services

import (
	"os"
	"path/filepath"
	"testing"
)

// Only the user and database names of the database images are readable;
// a password never is.
func TestComposePublicEnvHoldsNoPassword(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.Services {
		for k, v := range s.PublicEnv {
			if !PublicEnvKey(k) || v == "hunter2hunter2" {
				t.Errorf("%s exposes %s=%q", s.Name, k, v)
			}
		}
		if s.Name == "postgres" {
			if s.PublicEnv["POSTGRES_USER"] != "postgres" || s.PublicEnv["POSTGRES_DB"] != "app" || len(s.PublicEnv) != 2 {
				t.Errorf("postgres PublicEnv = %v", s.PublicEnv)
			}
		}
		if s.Name == "api" && len(s.PublicEnv) != 0 {
			t.Errorf("api PublicEnv = %v", s.PublicEnv)
		}
	}
	for _, k := range []string{"POSTGRES_PASSWORD", "MYSQL_ROOT_PASSWORD", "REDIS_PASSWORD", "DATABASE_URL"} {
		if PublicEnvKey(k) {
			t.Errorf("%s must not be public", k)
		}
	}
}

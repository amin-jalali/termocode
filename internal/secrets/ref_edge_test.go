package secrets

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (U19 SS-5): a reference kind the resolver does not know is a bad
// reference.
func TestResolveUnknownKind(t *testing.T) {
	if _, err := (&Resolver{}).Resolve(Ref{Kind: "vault", Arg: "x"}); !errors.Is(err, ErrBadRef) {
		t.Fatalf("err = %v", err)
	}
}

// Spec (U19 SS-5): with no Getenv, ${env:} reads the process environment.
func TestResolveEnvDefaultsToProcess(t *testing.T) {
	t.Setenv("TERMOCODE_TEST_REF", canary)
	v, err := (&Resolver{}).Resolve(Ref{RefEnv, "TERMOCODE_TEST_REF"})
	if err != nil || v != canary {
		t.Fatalf("Resolve = %q %v", v, err)
	}
}

// Spec (U19 SS-5): a workspace that is gone fails ${dotenv:} with the
// reference named.
func TestResolveDotenvMissingWorkspace(t *testing.T) {
	r := &Resolver{Workspace: filepath.Join(t.TempDir(), "gone")}
	_, err := r.Resolve(Ref{RefDotenv, "DB_PASS"})
	if err == nil || !strings.Contains(err.Error(), "${dotenv:DB_PASS}: workspace") {
		t.Fatalf("err = %v", err)
	}
}

// Spec (U19 SS-5): a failing ${cmd:} shows at most 200 characters of
// stderr, and works with no stderr at all.
func TestResolveCmdFailureMessages(t *testing.T) {
	r := &Resolver{}
	_, err := r.Resolve(Ref{RefCmd, "printf '%0300d' 0 >&2; exit 3"})
	if err == nil || !strings.HasSuffix(err.Error(), "…") {
		t.Fatalf("long stderr: err = %v", err)
	}
	if i := strings.Index(err.Error(), "000"); i < 0 || len(err.Error())-i > 200+len("…") {
		t.Fatalf("stderr not cut to 200: %d chars", len(err.Error()))
	}
	_, err = r.Resolve(Ref{RefCmd, "exit 4"})
	if err == nil || !strings.HasSuffix(err.Error(), "failed: exit status 4") {
		t.Fatalf("no stderr: err = %v", err)
	}
}

package app

import (
	"slices"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

func TestSvcComposeRunLine(t *testing.T) {
	svc, args, err := svcComposeRunLine(`web rails "db:migrate now" 'a b'`)
	if err != nil || svc != "web" || !slices.Equal(args, []string{"rails", "db:migrate now", "a b"}) {
		t.Fatalf("got %q %q %v", svc, args, err)
	}
	for _, bad := range []string{"", "   ", `web "open`, "-v web"} {
		if _, _, err := svcComposeRunLine(bad); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}

func TestServicesComposeBackendUsesTheChosenEnvFile(t *testing.T) {
	st := newServicesState()
	st.root = "/p"
	st.det = services.Detection{Compose: true, ComposeFiles: []string{"compose.yaml"}}
	if c := st.composeBackend(); len(c.EnvFiles) != 0 {
		t.Fatalf("no choice: EnvFiles %q", c.EnvFiles)
	}
	st.envFile = ".env.dev"
	if c := st.composeBackend(); !slices.Equal(c.EnvFiles, []string{".env.dev"}) {
		t.Fatalf("EnvFiles %q", c.EnvFiles)
	}
}

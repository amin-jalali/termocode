package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
)

func TestServicesToolClass(t *testing.T) {
	if !contains(ai.ToolClasses, ai.ClassServices) {
		t.Errorf("ToolClasses %v has no %q (AI: Agent Auto-Approve… lists it)", ai.ToolClasses, ai.ClassServices)
	}
}

// Gate: not allowed in this project → ask with class services; allowed
// → run at once.
func TestServicesToolGate(t *testing.T) {
	st := newServicesState()
	m := Model{services: st}
	tools := m.aiServicesTools(nil)
	mut := map[string]bool{"services_start": true, "services_stop": true, "services_restart": true}
	for _, tl := range tools {
		if !mut[tl.Name] {
			if tl.Mutating || tl.Gate != nil {
				t.Errorf("%s must be read-only", tl.Name)
			}
			continue
		}
		if tl.Class != ai.ClassServices || tl.Gate == nil {
			t.Fatalf("%s: class %q gate %v", tl.Name, tl.Class, tl.Gate != nil)
		}
		if ask, class := tl.Gate(nil); !ask || class != ai.ClassServices {
			t.Errorf("%s not allowed: gate = %v %q", tl.Name, ask, class)
		}
		st.aiAllowed.Store(true)
		if ask, _ := tl.Gate(nil); ask {
			t.Errorf("%s allowed in this project: still asks", tl.Name)
		}
		st.aiAllowed.Store(false)
		delete(mut, tl.Name)
	}
	if len(mut) != 0 {
		t.Errorf("missing tools %v", mut)
	}
}

func TestServicesToolDescribe(t *testing.T) {
	cases := map[string]string{
		aiServicesDescribe("start", map[string]any{"name": "postgres"}): "Start service postgres (docker compose up -d postgres)",
		aiServicesDescribe("restart", map[string]any{"name": "api"}):    "Restart service api (docker compose restart api)",
		aiServicesDescribe("stop", map[string]any{"all": true}):         "Stop all services",
		aiServicesDescribe("stop", map[string]any{"name": "redis"}):     "Stop service redis (docker compose stop redis)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if len(aiServicesConfirmButton(ai.ClassServices)) != 1 || len(aiServicesConfirmButton(ai.ClassRun)) != 0 {
		t.Error("only class services gets \"Always in this project\"")
	}
}

// services_list shows env names, never values; secrets in status / logs
// are masked and counted.
func TestServicesToolRedaction(t *testing.T) {
	st := svcTestState()
	st.compose[0].EnvKeys = []string{"POSTGRES_PASSWORD", "POSTGRES_USER"}
	m := Model{services: st}
	ai.RegisterSecret("hunter2hunter2")
	list := m.aiServicesListReport()
	for _, want := range []string{"env keys POSTGRES_PASSWORD, POSTGRES_USER", "healthcheck yes", "depends_on postgres", "last state: exited (1)", "image postgres:16"} {
		if !strings.Contains(list, want) {
			t.Errorf("services_list has no %q:\n%s", want, list)
		}
	}
	text, n := aiServicesRedact("DATABASE_URL=postgres://app:hunter2hunter2@localhost:5432/app\nFATAL: password authentication failed for user \"app\"\nPOSTGRES_PASSWORD=hunter2hunter2")
	if strings.Contains(text, "hunter2hunter2") || n < 2 {
		t.Errorf("redact = %q (%d)", text, n)
	}
	if !strings.Contains(text, `password authentication failed for user "app"`) || !strings.Contains(text, "@localhost:5432/app") {
		t.Errorf("the diagnosis must stay: %q", text)
	}
	status := m.aiServicesStatusReport("postgres")
	if !strings.Contains(status, "healthy") || !strings.Contains(status, "localhost:5432→5432") {
		t.Errorf("status = %q", status)
	}
	if got := m.aiServicesStatusReport("api"); !strings.Contains(got, "exited") || !strings.Contains(got, "exit code 1") {
		t.Errorf("api status = %q", got)
	}
}

func TestServicesAISettled(t *testing.T) {
	st := svcTestState()
	if !st.aiSettled(svcSecCompose, "postgres") || !st.aiSettled(svcSecCompose, "redis") || !st.aiSettled(svcSecCompose, "api") {
		t.Error("healthy / running without healthcheck / exited are settled")
	}
	st.status["postgres"] = services.Status{State: services.Running}
	if st.aiSettled(svcSecCompose, "postgres") {
		t.Error("running with a healthcheck is not healthy yet")
	}
	if st.aiSettled(svcSecCompose, "mailhog") {
		t.Error("not created is not settled")
	}
}

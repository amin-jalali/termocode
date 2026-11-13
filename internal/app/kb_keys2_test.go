package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// KB-2: no key means two things in Containers, and the keys that Databases
// and Containers share keep one meaning: a add, s start, x stop, $ shell
// in a terminal, M mode, U usage.

func TestKB2DollarOpensAShellInAContainer(t *testing.T) {
	m, _ := covSvcHost(t)
	covSvcContainer(t, m, "c1")
	covSvcKey(m, "$")
	if !strings.Contains(regToast(m), "Cannot open shell") {
		t.Fatalf("$ toast = %q", regToast(m))
	}
	m2, _ := covSvcHost(t)
	covSvcContainer(t, m2, "c1")
	covSvcKey(m2, "e")
	if strings.Contains(regToast(m2), "Cannot open shell") {
		t.Fatal("e still opens a shell: e means edit")
	}
}

func TestKB2StartAndStopAllLiveOnTheServicesHeader(t *testing.T) {
	for _, k := range []string{"s", "x", "a", "A"} {
		m, _ := covSvcProject(t)
		m.services.problem.Kind = services.ProblemDaemonDown
		selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowSection && r.section == svcSecCompose })
		press(m, k)
		off := strings.Contains(regToast(m), "Docker is off")
		if (k == "s" || k == "x") != off {
			t.Errorf("%s on the SERVICES header: Docker-is-off toast = %v (s and x start and stop all; a means add)", k, off)
		}
	}
}

func TestKB2VolumesDiskUsageIsU(t *testing.T) {
	m, f := covSvcHost(t)
	f.Set(t, "all.err", "no space left")
	covSvcSection(t, m, "volumes")
	pumpHost(t, m, covSvcKey(m, "U"))
	if !strings.Contains(regToast(m), "Cannot read disk usage") {
		t.Errorf("U toast = %q", regToast(m))
	}
}

func TestKB2RegistrySearchIsF(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	selectRegistry(t, m.services, "ghcr.io")
	press(m, "f")
	if !m.promptOpen || m.promptKind != promptKindContainerSearch {
		t.Fatal("f on REGISTRIES did not ask what to search")
	}
	m.promptOpen = false
	selectRegistry(t, m.services, "ghcr.io")
	press(m, "s")
	if m.promptOpen {
		t.Fatal("s on REGISTRIES still searches: s means start")
	}
}

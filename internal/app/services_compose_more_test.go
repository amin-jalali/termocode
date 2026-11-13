//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// CTX-8: a compose row's menu has pull, build, up with options, scale and
// down with volumes.
func TestComposeMenuHasPullBuildScaleDownVolumes(t *testing.T) {
	m, _ := covSvcProject(t)
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	view := ansi.Strip(m.menu.View())
	for _, want := range []string{"Pull image", "Build", "Up with options…", "Scale…", "Down and remove volumes…"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu lacks %q:\n%s", want, view)
		}
	}
	// A local row has none of them.
	local := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "web" })
	m.openServicesMenu(local, 5, 5)
	if view := ansi.Strip(m.menu.View()); strings.Contains(view, "Scale…") || strings.Contains(view, "Pull image") {
		t.Errorf("local menu has compose items:\n%s", view)
	}
}

// Pull and build run as visible jobs in a terminal tab (no nvim here, so
// the tab cannot start and the toast names the job).
func TestComposeMenuPullBuildRunJobs(t *testing.T) {
	m, _ := covSvcProject(t)
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	m.handleServicesMenuSelect("pull")
	if !strings.Contains(regToast(m), "Cannot pull") {
		t.Errorf("pull toast = %q", regToast(m))
	}
	m.handleServicesMenuSelect("build")
	if !strings.Contains(regToast(m), "Cannot build") {
		t.Errorf("build toast = %q", regToast(m))
	}
}

// Up with options opens a second menu; each choice is an up job.
func TestComposeUpOptionsMenu(t *testing.T) {
	m, _ := covSvcProject(t)
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	m.handleServicesMenuSelect("up-options")
	if !m.menuOpen || m.menuKind != menuKindServices {
		t.Fatal("no options menu")
	}
	view := ansi.Strip(m.menu.View())
	for _, want := range []string{"up --build", "up --force-recreate", "up --no-deps", "up --pull always"} {
		if !strings.Contains(view, want) {
			t.Errorf("options menu lacks %q:\n%s", want, view)
		}
	}
	m.handleServicesMenuSelect("up-build")
	if !strings.Contains(regToast(m), "Cannot start") {
		t.Errorf("up --build toast = %q", regToast(m))
	}
}

// Scale asks for a number; a bad number says so and runs nothing.
func TestComposeScalePrompt(t *testing.T) {
	m, f := covSvcProject(t)
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	m.handleServicesMenuSelect("scale")
	if !m.promptOpen || m.promptKind != promptKindComposeScale {
		t.Fatal("scale: no prompt")
	}
	before := len(f.Calls())
	m.handlePromptSubmit("lots")
	if !strings.Contains(regToast(m), "whole number") {
		t.Errorf("bad number toast = %q", regToast(m))
	}
	m.promptKind = promptKindComposeScale
	m.handlePromptSubmit("101")
	if !strings.Contains(regToast(m), "whole number") {
		t.Errorf("too big toast = %q", regToast(m))
	}
	if len(f.Calls()) != before {
		t.Errorf("a bad number ran docker: %q", f.Calls())
	}
	m.promptKind = promptKindComposeScale
	m.handlePromptSubmit("3")
	if !strings.Contains(regToast(m), "Cannot scale") {
		t.Errorf("scale toast = %q", regToast(m))
	}
}

// Down with volumes: a Cancel-first card that names the volumes; only the
// red button runs `down --volumes`.
func TestComposeDownVolumesCard(t *testing.T) {
	m, f := covSvcProject(t)
	m.services.volumes = []string{"cache", "pgdata"}
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	m.handleServicesMenuSelect("down-volumes")
	if m.confirmKind != confirmKindServicesDownVolumes {
		t.Fatalf("kind = %v", m.confirmKind)
	}
	checkCardRule(t, "down volumes", *m, true)
	text := cardText(m)
	for _, want := range []string{"cache", "pgdata", "cannot be undone"} {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q: %s", want, text)
		}
	}
	if cmd, ok := m.handleServicesConfirm("cancel"); !ok || cmd != nil {
		t.Error("cancel did something")
	}
	if strings.Contains(covSvcCalls(f), "down") {
		t.Errorf("cancel ran down: %q", f.Calls())
	}
	m.handleServicesMenuSelect("down-volumes")
	cmd, ok := m.handleServicesConfirm("down-volumes")
	if !ok || cmd == nil {
		t.Fatal("down-volumes did nothing")
	}
	pumpHost(t, m, cmd)
	if !strings.Contains(covSvcCalls(f), "down --volumes") {
		t.Errorf("calls = %q", f.Calls())
	}
}

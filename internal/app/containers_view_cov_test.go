package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The look of a container row: glyph, uptime column and the short
// durations for every state the engine reports.
func TestCovContainerViewGlyphsAndAges(t *testing.T) {
	for _, tc := range []struct {
		state, status string
		glyph         theme.Icon
		up            string
	}{
		{"running", "Up 2 hours (unhealthy)", theme.GlyphSvcBad, "2h"},
		{"running", "Up 5 seconds (health: starting)", theme.GlyphSvcStarting, "5s"},
		{"running", "Up Less than a second", theme.GlyphSvcUp, "<1s"},
		{"running", "Up About a minute", theme.GlyphSvcUp, "1m"},
		{"running", "Up", theme.GlyphSvcUp, ""},
		{"running", "Up 3 fortnights", theme.GlyphSvcUp, ""},
		{"running", "Up many hours", theme.GlyphSvcUp, ""},
		{"restarting", "Restarting (1) 2 seconds ago", theme.GlyphSvcRestarting, "—"},
		{"created", "Created", theme.GlyphSvcStarting, "new"},
		{"removing", "Removing", theme.GlyphSvcStarting, ""},
		{"exited", "Exited (0) 1 hour ago", theme.GlyphSvcStopped, "done"},
		{"exited", "Exited (2) 1 hour ago", theme.GlyphSvcExited, "exit 2"},
		{"dead", "Dead", theme.GlyphSvcExited, "done"},
		{"weird", "", theme.GlyphSvcStopped, ""},
	} {
		c := containers.Container{State: tc.state, Status: tc.status}
		if g, _ := containerGlyph(c); g != tc.glyph {
			t.Errorf("%s %q: glyph %v", tc.state, tc.status, g)
		}
		if up, _ := containerUptime(c); up != tc.up {
			t.Errorf("%s %q: uptime %q, want %q", tc.state, tc.status, up, tc.up)
		}
	}
	if containerExitCode("Exited (3") != 0 {
		t.Fatal("unclosed exit code")
	}
	for d, want := range map[time.Duration]string{
		10 * time.Second: "1m", 30 * time.Minute: "30m", 5 * time.Hour: "5h", 3 * 24 * time.Hour: "3d",
		21 * 24 * time.Hour: "3w", 90 * 24 * time.Hour: "3mo", 800 * 24 * time.Hour: "2y",
	} {
		if got := shortAge(d); got != want {
			t.Errorf("shortAge(%v) = %q, want %q", d, got, want)
		}
	}
	if imageCreated(containers.Image{CreatedAt: "2026-01-02 03:04:05 +0000 UTC"}).IsZero() ||
		imageCreated(containers.Image{CreatedAt: "2026-01-02T03:04:05Z"}).IsZero() || !imageCreated(containers.Image{CreatedAt: "x"}).IsZero() {
		t.Fatal("imageCreated")
	}
	if r, tag := splitImageTag("localhost:5000/app"); r != "localhost:5000/app" || tag != "latest" {
		t.Fatal(r, tag)
	}
}

// The section bodies for an engine that is off, a session context that
// does not answer, a section error and the counts in the headers.
func TestCovContainerViewSectionStates(t *testing.T) {
	st := svcHostState()
	if st.engineTitle() != "Docker" || st.engineLine() != "docker 27 · default" {
		t.Fatalf("title %q line %q", st.engineTitle(), st.engineLine())
	}
	st.containerEngine.Kind = ""
	if st.engineTitle() != "The engine" {
		t.Fatal(st.engineTitle())
	}
	st.containerEngine.Kind = "docker"
	// A section error.
	st.containerErrors = map[string]string{"networks": "network ls failed\nmore"}
	rows := st.engineSectionBody("networks", hostSection("networks"))
	if len(rows) != 2 || rows[0].text != "network ls failed" {
		t.Fatalf("rows %+v", rows)
	}
	// The same while a session context is set.
	st.sessionContext = "prod"
	if rows := st.engineSectionBody("networks", hostSection("networks")); len(rows) != 1 || !strings.Contains(rows[0].text, "context prod is not reachable") {
		t.Fatalf("rows %+v", rows)
	}
	if st.engineLine() != "docker 27 · prod" {
		t.Fatal(st.engineLine())
	}
	for detail, want := range map[string]string{
		"context deadline exceeded": "timed out", "": "timed out", "dial tcp: connection refused": "connection refused",
	} {
		rows := st.remoteContextRows("containers", "k", detail)
		if len(rows) != 2 || !strings.Contains(rows[0].text, want) {
			t.Errorf("%q: %+v", detail, rows)
		}
	}
	st.sessionContext = ""
	st.containerErrors = nil
	// Section counts.
	if st.hostSectionCount("contexts") != "2" || st.hostSectionCount("networks") != "2" || st.hostSectionCount("volumes") != "1" || st.hostSectionCount("dance") != "…" {
		t.Fatalf("counts %q %q %q %q", st.hostSectionCount("contexts"), st.hostSectionCount("networks"), st.hostSectionCount("volumes"), st.hostSectionCount("dance"))
	}
	st.containerDetected, st.containerDetecting = false, true
	st.sections["containers"] = true
	if st.hostSectionCount("containers") != "…" {
		t.Fatalf("detecting %q", st.hostSectionCount("containers"))
	}
	st.containerDetected, st.containerDetecting = true, false
	st.containerEngine.Reachable = false
	if st.hostSectionCount("images") != "?" {
		t.Fatal("unreachable")
	}
	st.containerEngine.Binary = ""
	if st.hostSectionCount("images") != "" {
		t.Fatal("no binary")
	}
	// The contexts body.
	st = svcHostState()
	if rows := st.contextSectionBody("k"); len(rows) < 3 || rows[0].host != hostContext || !rows[0].current {
		t.Fatalf("contexts %+v", rows)
	}
	st.contextRows = nil
	if rows := st.contextSectionBody("k"); !strings.Contains(rows[0].text, "No contexts") {
		t.Fatalf("no contexts %+v", rows)
	}
	st.hostLoaded["contexts"] = false
	if rows := st.contextSectionBody("k"); rows[0].text != "Loading…" {
		t.Fatalf("loading %+v", rows)
	}
	st.containerErrors = map[string]string{"contexts": "boom"}
	if rows := st.contextSectionBody("k"); rows[0].text != "boom" {
		t.Fatalf("error %+v", rows)
	}
	if rows := st.hostSectionBody("contexts"); len(rows) == 0 {
		t.Fatal("hostSectionBody contexts")
	}
	// contextsHidden.
	st = svcHostState()
	if st.contextsHidden() {
		t.Fatal("two contexts hidden")
	}
	st.sessionContext = "prod"
	st.contextRows = st.contextRows[:1]
	if st.contextsHidden() {
		t.Fatal("a session context hides CONTEXTS")
	}
	st.containerEngine.Binary = ""
	if !st.contextsHidden() {
		t.Fatal("no binary shows CONTEXTS")
	}
	// Podman contexts.
	st = svcHostState()
	st.containerEngine.Kind = "podman"
	if rows := st.contextSectionBody("k"); len(rows) == 0 {
		t.Fatal("podman contexts")
	}
	if l1, _ := st.hostFooter(svcRow{kind: svcRowSection, section: hostSection("contexts")}); l1 != "podman connections" {
		t.Fatal(l1)
	}
}

// Footers of the section, group and item rows.
func TestCovContainerViewFooters(t *testing.T) {
	st := svcHostState()
	if l1, _ := st.hostFooter(svcRow{kind: svcRowSection, section: hostSection("contexts")}); l1 != "engine contexts" {
		t.Fatal(l1)
	}
	if l1, _ := st.hostFooter(svcRow{kind: svcRowGroup, section: "grp/" + allContextsSection + "/prod", name: "prod"}); l1 != "context prod" {
		t.Fatal(l1)
	}
	if l1, _ := st.hostFooter(svcRow{kind: svcRowGroup, section: "grp/servers/shop", name: "shop", hostID: "prod"}); l1 != "compose project shop on prod" {
		t.Fatal(l1)
	}
	st.busy[containerBusyPrefix+"c5"] = services.Stopping
	if _, l2 := st.hostFooter(svcRow{kind: svcRowHostItem, host: hostContainer, idx: 4}); l2 != services.Stopping.String() {
		t.Fatal(l2)
	}
	st.imageRows[0].CreatedAt = time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	if _, l2 := st.hostFooter(svcRow{kind: svcRowHostItem, host: hostImage, idx: 0}); !strings.Contains(l2, "3h old") {
		t.Fatal(l2)
	}
	st.contextRows[1].Error = "unreachable\nmore"
	if _, l2 := st.hostFooter(svcRow{kind: svcRowHostItem, host: hostContext, idx: 1}); l2 != "unreachable" {
		t.Fatal(l2)
	}
	// Rows out of range render a plain line.
	for _, h := range []hostKind{hostContainer, hostImage, hostNetwork, hostVolume} {
		if out := st.renderHostItem(svcRow{kind: svcRowHostItem, host: h, idx: 99, name: "x"}, false, false, 30); out == "" {
			t.Errorf("%v: empty", h)
		}
	}
	st.imageRows[0].Size = "9MB"
	if out := st.renderHostItem(svcRow{kind: svcRowHostItem, host: hostRegistryImage, idx: 0, name: "ghcr.io/a:1"}, false, false, 40); !strings.Contains(out, "9MB") {
		t.Fatal(out)
	}
	// A busy container row draws the busy glyph.
	_ = st.renderHostItem(svcRow{kind: svcRowHostItem, host: hostContainer, idx: 4, name: "nginx"}, false, false, 40)
	if storageRowDetail(st, svcRow{host: hostImage}) != "" {
		t.Fatal("storage detail for an image")
	}
	st.volumeRows[0].Size = "1GB"
	if got := storageRowDetail(st, svcRow{host: hostVolume, idx: 0}); got != "local · 1GB" {
		t.Fatal(got)
	}
}

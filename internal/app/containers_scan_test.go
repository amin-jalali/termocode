package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// cursorOnHost puts the cursor on the first row of one host kind.
func cursorOnHost(t *testing.T, m *Model, kind hostKind) {
	t.Helper()
	for i, r := range m.services.rows() {
		if r.kind == svcRowHostItem && r.host == kind {
			m.services.cursor = i
			return
		}
	}
	t.Fatalf("no row of kind %v", kind)
}

func withScanners(t *testing.T, names ...string) {
	t.Helper()
	old := imageScanLookPath
	t.Cleanup(func() { imageScanLookPath = old })
	imageScanLookPath = func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestImageScanKeyWithoutScannerShowsHint(t *testing.T) {
	withScanners(t)
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	cursorOnHost(t, m, hostImage)
	next, _ := m.handleServicesSidebarKey(keyMsg("c"))
	*m = next
	if got := pruneToast(m); !strings.Contains(got, "No image scanner") {
		t.Fatalf("toast: %q", got)
	}
	next, _ = m.handleServicesSidebarKey(keyMsg("C"))
	*m = next
	if got := pruneToast(m); !strings.Contains(got, "syft") {
		t.Fatalf("sbom toast: %q", got)
	}
}

func TestImageScanKeyStartsOneScan(t *testing.T) {
	withScanners(t, "trivy")
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	cursorOnHost(t, m, hostImage)
	_, ref, _ := m.selectedImage()
	next, cmd := m.handleServicesSidebarKey(keyMsg("c"))
	*m = next
	if cmd == nil || !m.services.imageScanBusy["scan:"+ref] {
		t.Fatalf("no scan started for %s", ref)
	}
	if got := pruneToast(m); !strings.Contains(got, "trivy") {
		t.Fatalf("toast: %q", got)
	}
	// A second press while it runs starts nothing new.
	next, _ = m.handleServicesSidebarKey(keyMsg("c"))
	*m = next
	if got := pruneToast(m); !strings.Contains(got, "already") {
		t.Fatalf("busy toast: %q", got)
	}
	m.onImageScan(imageScanMsg{kind: "scan", ref: ref, scanner: "trivy", vulns: []containers.Vuln{{ID: "CVE-1", Severity: "high"}}})
	if m.services.imageScanBusy["scan:"+ref] {
		t.Fatal("busy mark stays after the result")
	}
}

func TestImageScanKeysInCardAndPalette(t *testing.T) {
	_, rows := hostKeysHelp(svcRow{kind: svcRowHostItem, host: hostImage})
	var text []string
	for _, r := range rows {
		text = append(text, r.Key+" "+r.What)
	}
	all := strings.Join(text, "\n")
	if !strings.Contains(all, "scan for vulnerabilities") || !strings.Contains(all, "SBOM") {
		t.Fatalf("image key card: %s", all)
	}
	ids := map[string]bool{}
	for _, it := range servicesPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"containers-image-scan", "containers-image-sbom"} {
		if !ids[id] {
			t.Errorf("palette lacks %s", id)
		}
	}
}

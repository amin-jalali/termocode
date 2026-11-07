package app

import (
	"context"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Images: vulnerability scan + SBOM (CTX-18) ───────────────────────────
//
// c on an image row scans it with the installed scanner (trivy, grype or
// docker scout) and opens the findings as a read-only page, most severe
// first. C writes a CycloneDX SBOM (syft, trivy or docker scout) to a
// read-only JSON page. No scanner installed: a toast says what to
// install. Both only read; nothing is changed on the engine.

// imageScanLookPath finds the scanner binaries (a test seam).
var imageScanLookPath = exec.LookPath

type imageScanMsg struct {
	kind    string // scan | sbom
	ref     string
	scanner string
	vulns   []containers.Vuln
	sbom    string
	err     error
}

func (m *Model) imageScanners() []containers.Scanner {
	home, _ := os.UserHomeDir()
	exists := func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}
	return containers.FindScanners(containers.PluginLookPath(imageScanLookPath, exists, home))
}

// imageScan starts a vulnerability scan of ref (kind "scan") or an SBOM
// (kind "sbom").
func (m *Model) imageScan(kind, ref string) tea.Cmd {
	st := m.services
	if st == nil || ref == "" {
		return nil
	}
	found := m.imageScanners()
	var scanner containers.Scanner
	if kind == "scan" {
		s, ok := containers.PickScanner(found, "")
		if !ok {
			return m.servicesToast(toast.Info, "No image scanner", containers.NoScannerHint)
		}
		scanner = s
	} else {
		argv, err := containers.SBOMArgv(found, ref)
		if err != nil {
			return m.servicesToast(toast.Info, "Cannot make an SBOM", errLine(err.Error()))
		}
		scanner = containers.Scanner{Name: argv[0]}
		for _, s := range found {
			if s.Binary == argv[0] {
				scanner = s
				break
			}
		}
	}
	key := kind + ":" + ref
	if st.imageScanBusy[key] {
		return m.servicesToast(toast.Info, "Scan already running", ref)
	}
	if st.imageScanBusy == nil {
		st.imageScanBusy = map[string]bool{}
	}
	st.imageScanBusy[key] = true
	st.touch()
	x := st.hostExec()
	what := "Scanning " + ref
	if kind == "sbom" {
		what = "Making the SBOM of " + ref
	}
	note := m.servicesToast(toast.Info, what+"…", "with "+scanner.Name+"; the first run can take minutes")
	run := func() tea.Msg {
		ctx := context.Background()
		if kind == "sbom" {
			out, err := containers.GenerateSBOM(ctx, x, found, ref)
			return imageScanMsg{kind: kind, ref: ref, scanner: scanner.Name, sbom: out, err: err}
		}
		v, err := containers.ScanImage(ctx, x, scanner, ref)
		return imageScanMsg{kind: kind, ref: ref, scanner: scanner.Name, vulns: v, err: err}
	}
	return tea.Batch(note, run)
}

func (m *Model) onImageScan(msg imageScanMsg) tea.Cmd {
	delete(m.services.imageScanBusy, msg.kind+":"+msg.ref)
	m.services.touch()
	if msg.err != nil {
		title := "Scan failed"
		if msg.kind == "sbom" {
			title = "SBOM failed"
		}
		return m.servicesToast(toast.Errr, title, errLine(msg.err.Error()))
	}
	if msg.kind == "sbom" {
		if err := m.openReadOnlyPage("sbom: "+msg.ref, msg.sbom, "", "json"); err != nil {
			return m.servicesToast(toast.Errr, "Cannot open the SBOM", err.Error())
		}
		return nil
	}
	if err := m.openReadOnlyPage("scan: "+msg.ref, containers.FormatVulnReport(msg.ref, msg.scanner, msg.vulns), "", "text"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the scan report", err.Error())
	}
	sev := toast.Info
	if len(msg.vulns) > 0 && (msg.vulns[0].Severity == "critical" || msg.vulns[0].Severity == "high") {
		sev = toast.Warn
	}
	return m.servicesToast(sev, msg.ref, containers.SummarizeVulns(msg.vulns))
}

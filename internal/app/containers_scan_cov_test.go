package app

import (
	"errors"
	"strings"
	"testing"
)

// Scan and SBOM run the found tool on a scripted exec and open their
// pages; a failure toasts with its own title.
func TestCovContainerImageScanAndSBOM(t *testing.T) {
	m, x := ctrModelScripted(t)
	withScanners(t, "trivy")
	x.on("image --format cyclonedx", `{"bomFormat":"CycloneDX"}`)
	x.on("image", `{"Results":[{"Target":"x","Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"openssl","Severity":"HIGH"}]}]}`)
	if m.imageScan("scan", "") != nil {
		t.Fatal("empty ref")
	}
	msg := ctrFind[imageScanMsg](t, ctrMsgs(m.imageScan("scan", "redis:7")))
	if msg.err != nil || len(msg.vulns) != 1 {
		t.Fatalf("scan %+v", msg)
	}
	_ = m.onImageScan(msg)
	if !strings.Contains(ctrToast(m), "redis:7") || m.services.imageScanBusy["scan:redis:7"] {
		t.Fatalf("scan toast: %s", ctrToast(m))
	}
	msg = ctrFind[imageScanMsg](t, ctrMsgs(m.imageScan("sbom", "redis:7")))
	if msg.err != nil || msg.scanner != "trivy" || !strings.Contains(msg.sbom, "CycloneDX") {
		t.Fatalf("sbom %+v", msg)
	}
	if m.onImageScan(msg) != nil {
		t.Fatalf("sbom page: %s", ctrToast(m))
	}
	_ = m.onImageScan(imageScanMsg{kind: "sbom", ref: "r", err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "SBOM failed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onImageScan(imageScanMsg{kind: "scan", ref: "r", err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Scan failed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

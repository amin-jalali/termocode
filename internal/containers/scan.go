package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// ScanTimeout bounds one image scan (the first run may download a database).
const ScanTimeout = 10 * time.Minute

// NoScannerHint is shown when no scanner is installed.
const NoScannerHint = "No image scanner found. Install trivy or grype (and syft for SBOMs), then try again."

// Scanner is one installed image scanner or SBOM tool.
type Scanner struct {
	Name   string // trivy, grype or syft
	Binary string
}

var scannerOrder = []string{"trivy", "grype", "syft"}

// FindScanners lists the installed tools in a fixed order.
func FindScanners(lookPath func(string) (string, error)) []Scanner {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var out []Scanner
	for _, n := range scannerOrder {
		if p, err := lookPath(n); err == nil {
			out = append(out, Scanner{Name: n, Binary: p})
		}
	}
	return out
}

func canScan(name string) bool { return name == "trivy" || name == "grype" }

// PickScanner picks the vulnerability scanner: the preferred one when
// installed, else the first one that can scan.
func PickScanner(found []Scanner, preferred string) (Scanner, bool) {
	for _, s := range found {
		if s.Name == preferred && canScan(s.Name) {
			return s, true
		}
	}
	for _, s := range found {
		if canScan(s.Name) {
			return s, true
		}
	}
	return Scanner{}, false
}

// checkImageRef refuses refs that could be read as a flag or hold spaces.
func checkImageRef(image string) error {
	if image == "" || strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\r\n") {
		return fmt.Errorf("not a valid image name: %q", image)
	}
	return nil
}

// ScanArgv is the argv of a JSON vulnerability scan of image.
func ScanArgv(s Scanner, image string) ([]string, error) {
	if err := checkImageRef(image); err != nil {
		return nil, err
	}
	switch s.Name {
	case "trivy":
		return []string{s.Binary, "image", "--format", "json", "--quiet", "--", image}, nil
	case "grype":
		return []string{s.Binary, "-o", "json", "-q", "--", image}, nil
	}
	return nil, fmt.Errorf("%s cannot scan for vulnerabilities", s.Name)
}

// SBOMArgv is the argv that writes a CycloneDX SBOM of image to stdout:
// syft when installed, else trivy.
func SBOMArgv(found []Scanner, image string) ([]string, error) {
	if err := checkImageRef(image); err != nil {
		return nil, err
	}
	for _, s := range found {
		if s.Name == "syft" {
			return []string{s.Binary, "-o", "cyclonedx-json", "-q", "--", image}, nil
		}
	}
	for _, s := range found {
		if s.Name == "trivy" {
			return []string{s.Binary, "image", "--format", "cyclonedx", "--quiet", "--", image}, nil
		}
	}
	return nil, errors.New("no SBOM tool found: install syft or trivy")
}

// Vuln is one finding of a scan.
type Vuln struct {
	ID, Package, Installed, FixedIn, Severity, Title, Target string
}

func normSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low", "negligible":
		return "low"
	}
	return "unknown"
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "unknown": 4}

// ParseVulnReport reads the JSON report of a scanner.
func ParseVulnReport(scanner string, data []byte) ([]Vuln, error) {
	switch scanner {
	case "trivy":
		var r struct {
			Results []struct {
				Target          string
				Vulnerabilities []struct {
					VulnerabilityID, PkgName, InstalledVersion, FixedVersion, Severity, Title string
				}
			}
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("cannot read trivy report: %w", err)
		}
		var out []Vuln
		for _, res := range r.Results {
			for _, v := range res.Vulnerabilities {
				out = append(out, Vuln{ID: v.VulnerabilityID, Package: v.PkgName, Installed: v.InstalledVersion,
					FixedIn: v.FixedVersion, Severity: normSeverity(v.Severity), Title: v.Title, Target: res.Target})
			}
		}
		return out, nil
	case "grype":
		var r struct {
			Matches []struct {
				Vulnerability struct {
					ID, Severity, Description string
					Fix                       struct{ Versions []string }
				}
				Artifact struct{ Name, Version string }
			}
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("cannot read grype report: %w", err)
		}
		var out []Vuln
		for _, m := range r.Matches {
			v := m.Vulnerability
			out = append(out, Vuln{ID: v.ID, Package: m.Artifact.Name, Installed: m.Artifact.Version,
				FixedIn: strings.Join(v.Fix.Versions, ", "), Severity: normSeverity(v.Severity), Title: v.Description})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown scanner %q", scanner)
}

// SortVulns orders findings by severity (critical first), then by ID.
func SortVulns(v []Vuln) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := severityRank[v[i].Severity], severityRank[v[j].Severity]
		if a != b {
			return a < b
		}
		return v[i].ID < v[j].ID
	})
}

// SummarizeVulns is a one-line count per severity.
func SummarizeVulns(v []Vuln) string {
	if len(v) == 0 {
		return "no known vulnerabilities"
	}
	n := map[string]int{}
	for _, x := range v {
		n[x.Severity]++
	}
	var parts []string
	for _, s := range []string{"critical", "high", "medium", "low", "unknown"} {
		if n[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[s], s))
		}
	}
	return strings.Join(parts, ", ")
}

// ScanImage runs the scanner on image and returns the sorted findings.
func ScanImage(ctx context.Context, x services.Exec, s Scanner, image string) ([]Vuln, error) {
	argv, err := ScanArgv(s, image)
	if err != nil {
		return nil, err
	}
	if x == nil {
		x = services.SpawnExec{}
	}
	ctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()
	out, _, code, err := x.Capture(ctx, "", argv...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s exited %d", s.Name, code)
	}
	v, err := ParseVulnReport(s.Name, []byte(out))
	if err != nil {
		return nil, err
	}
	SortVulns(v)
	return v, nil
}

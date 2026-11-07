package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// ScanTimeout bounds one image scan (the first run may download a database).
const ScanTimeout = 10 * time.Minute

// NoScannerHint is shown when no scanner is installed.
const NoScannerHint = "No image scanner found. Install docker scout, trivy or grype (and syft for SBOMs), then try again."

// Scanner is one installed image scanner or SBOM tool.
type Scanner struct {
	Name   string // trivy, grype, scout or syft
	Binary string // for scout: the docker CLI that runs the plugin
}

var scannerOrder = []string{"trivy", "grype", "scout", "syft"}

// FindScanners lists the installed tools in a fixed order. docker scout
// is a docker CLI plugin: it counts when "docker-scout" is found and
// the docker CLI is there to run it.
func FindScanners(lookPath func(string) (string, error)) []Scanner {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var out []Scanner
	for _, n := range scannerOrder {
		if n == "scout" {
			if _, err := lookPath("docker-scout"); err != nil {
				continue
			}
			if p, err := lookPath("docker"); err == nil {
				out = append(out, Scanner{Name: n, Binary: p})
			}
			continue
		}
		if p, err := lookPath(n); err == nil {
			out = append(out, Scanner{Name: n, Binary: p})
		}
	}
	return out
}

// PluginLookPath is lookPath that also finds "docker-scout" in the docker
// CLI plugin folders (~/.docker/cli-plugins and the system ones).
func PluginLookPath(lookPath func(string) (string, error), exists func(string) bool, home string) func(string) (string, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return func(name string) (string, error) {
		p, err := lookPath(name)
		if err == nil || name != "docker-scout" || exists == nil {
			return p, err
		}
		var dirs []string
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".docker", "cli-plugins"))
		}
		dirs = append(dirs, "/usr/local/lib/docker/cli-plugins", "/usr/local/libexec/docker/cli-plugins",
			"/usr/lib/docker/cli-plugins", "/usr/libexec/docker/cli-plugins")
		for _, d := range dirs {
			if f := filepath.Join(d, name); exists(f) {
				return f, nil
			}
		}
		return "", err
	}
}

func canScan(name string) bool { return name == "trivy" || name == "grype" || name == "scout" }

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
	case "scout":
		return []string{s.Binary, "scout", "cves", "--format", "gitlab", "--", image}, nil
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
	for _, s := range found {
		if s.Name == "scout" {
			return []string{s.Binary, "scout", "sbom", "--format", "cyclonedx", "--", image}, nil
		}
	}
	return nil, errors.New("no SBOM tool found: install syft, trivy or docker scout")
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
	case "scout":
		// docker scout cves --format gitlab: the container scanning report.
		var r struct {
			Vulnerabilities []struct {
				Name, Severity, Solution, Description string
				Location                              struct {
					Dependency struct {
						Package struct{ Name string }
						Version string
					}
				}
				Identifiers []struct{ Value string }
			}
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("cannot read docker scout report: %w", err)
		}
		var out []Vuln
		for _, v := range r.Vulnerabilities {
			id := v.Name
			if len(v.Identifiers) > 0 && v.Identifiers[0].Value != "" {
				id = v.Identifiers[0].Value
			}
			d := v.Location.Dependency
			out = append(out, Vuln{ID: id, Package: d.Package.Name, Installed: d.Version,
				FixedIn: v.Solution, Severity: normSeverity(v.Severity), Title: v.Description})
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

// GenerateSBOM runs the SBOM tool on image and returns its CycloneDX JSON.
func GenerateSBOM(ctx context.Context, x services.Exec, found []Scanner, image string) (string, error) {
	argv, err := SBOMArgv(found, image)
	if err != nil {
		return "", err
	}
	if x == nil {
		x = services.SpawnExec{}
	}
	ctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()
	out, _, code, err := x.Capture(ctx, "", argv...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s exited %d", filepath.Base(argv[0]), code)
	}
	return out, nil
}

// FormatVulnReport is the read-only page of a scan: a title, the
// summary and one table row per finding (most severe first).
func FormatVulnReport(image, scanner string, v []Vuln) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Vulnerability scan of %s (%s)\n", image, scanner)
	fmt.Fprintf(&b, "Summary: %s\n", SummarizeVulns(v))
	if len(v) == 0 {
		return b.String()
	}
	b.WriteString("\n")
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEVERITY\tID\tPACKAGE\tINSTALLED\tFIXED IN\tTITLE")
	for _, x := range v {
		title := strings.Join(strings.Fields(x.Title), " ")
		if r := []rune(title); len(r) > 80 {
			title = string(r[:79]) + "…"
		}
		fixed := x.FixedIn
		if fixed == "" {
			fixed = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", x.Severity, x.ID, x.Package, x.Installed, fixed, title)
	}
	_ = w.Flush()
	return b.String()
}

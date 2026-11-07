package containers

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func lookOnly(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestFindScanners(t *testing.T) {
	got := FindScanners(lookOnly("grype", "trivy", "syft"))
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"trivy", "grype", "syft"}) {
		t.Fatalf("scanners: %v", names)
	}
	if s, ok := PickScanner(got, ""); !ok || s.Name != "trivy" {
		t.Fatalf("default pick: %v %v", s, ok)
	}
	if s, ok := PickScanner(got, "grype"); !ok || s.Name != "grype" {
		t.Fatalf("preferred pick: %v %v", s, ok)
	}
	// syft makes SBOMs only; it is never a vulnerability scanner.
	if _, ok := PickScanner(FindScanners(lookOnly("syft")), ""); ok {
		t.Fatal("syft picked as vulnerability scanner")
	}
	if len(FindScanners(lookOnly())) != 0 {
		t.Fatal("scanners found with none installed")
	}
	if !strings.Contains(NoScannerHint, "trivy") || !strings.Contains(NoScannerHint, "grype") {
		t.Fatalf("hint: %q", NoScannerHint)
	}
}

func TestScanArgv(t *testing.T) {
	tv := Scanner{Name: "trivy", Binary: "/usr/bin/trivy"}
	gr := Scanner{Name: "grype", Binary: "/usr/bin/grype"}
	a, err := ScanArgv(tv, "nginx:1.27")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/trivy", "image", "--format", "json", "--quiet", "--", "nginx:1.27"}) {
		t.Fatalf("trivy argv %v %v", a, err)
	}
	a, err = ScanArgv(gr, "nginx:1.27")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/grype", "-o", "json", "-q", "--", "nginx:1.27"}) {
		t.Fatalf("grype argv %v %v", a, err)
	}
	for _, bad := range []string{"", "-x", "--output=/etc/passwd", "a b", "x\n"} {
		if _, err := ScanArgv(tv, bad); err == nil {
			t.Fatalf("bad image %q accepted", bad)
		}
	}
	if _, err := ScanArgv(Scanner{Name: "syft", Binary: "/s"}, "x"); err == nil {
		t.Fatal("syft scan argv accepted")
	}
}

func TestSBOMArgv(t *testing.T) {
	all := FindScanners(lookOnly("trivy", "syft"))
	a, err := SBOMArgv(all, "alpine")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/syft", "-o", "cyclonedx-json", "-q", "--", "alpine"}) {
		t.Fatalf("syft sbom %v %v", a, err)
	}
	a, err = SBOMArgv(FindScanners(lookOnly("trivy")), "alpine")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/trivy", "image", "--format", "cyclonedx", "--quiet", "--", "alpine"}) {
		t.Fatalf("trivy sbom %v %v", a, err)
	}
	if _, err := SBOMArgv(FindScanners(lookOnly("grype")), "alpine"); err == nil {
		t.Fatal("grype alone cannot make an SBOM")
	}
}

const trivyJSON = `{"Results":[{"Target":"alpine","Vulnerabilities":[
{"VulnerabilityID":"CVE-1","PkgName":"openssl","InstalledVersion":"3.0","FixedVersion":"3.1","Severity":"CRITICAL","Title":"bad"},
{"VulnerabilityID":"CVE-2","PkgName":"zlib","InstalledVersion":"1.2","Severity":"LOW"}]},
{"Target":"app"}]}`

const grypeJSON = `{"matches":[
{"vulnerability":{"id":"GHSA-1","severity":"High","fix":{"versions":["2.0","2.1"]},"description":"d"},"artifact":{"name":"lib","version":"1.0"}},
{"vulnerability":{"id":"CVE-3","severity":"Negligible","fix":{"versions":[]}},"artifact":{"name":"x","version":"0"}}]}`

func TestParseVulnReport(t *testing.T) {
	v, err := ParseVulnReport("trivy", []byte(trivyJSON))
	if err != nil || len(v) != 2 {
		t.Fatalf("trivy: %v %v", v, err)
	}
	if v[0] != (Vuln{ID: "CVE-1", Package: "openssl", Installed: "3.0", FixedIn: "3.1", Severity: "critical", Title: "bad", Target: "alpine"}) {
		t.Fatalf("trivy row: %+v", v[0])
	}
	g, err := ParseVulnReport("grype", []byte(grypeJSON))
	if err != nil || len(g) != 2 {
		t.Fatalf("grype: %v %v", g, err)
	}
	if g[0] != (Vuln{ID: "GHSA-1", Package: "lib", Installed: "1.0", FixedIn: "2.0, 2.1", Severity: "high", Title: "d"}) {
		t.Fatalf("grype row: %+v", g[0])
	}
	if g[1].Severity != "low" {
		t.Fatalf("negligible maps to low: %q", g[1].Severity)
	}
	if _, err := ParseVulnReport("trivy", []byte("{bad")); err == nil {
		t.Fatal("bad json accepted")
	}
	if _, err := ParseVulnReport("syft", []byte("{}")); err == nil {
		t.Fatal("unknown scanner accepted")
	}
}

func TestSortAndSummarizeVulns(t *testing.T) {
	v := []Vuln{{ID: "b", Severity: "low"}, {ID: "a", Severity: "critical"}, {ID: "c", Severity: "unknown"}, {ID: "d", Severity: "critical"}}
	SortVulns(v)
	var ids []string
	for _, x := range v {
		ids = append(ids, x.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "d", "b", "c"}) {
		t.Fatalf("order: %v", ids)
	}
	if s := SummarizeVulns(v); s != "2 critical, 1 low, 1 unknown" {
		t.Fatalf("summary: %q", s)
	}
	if s := SummarizeVulns(nil); s != "no known vulnerabilities" {
		t.Fatalf("empty summary: %q", s)
	}
}

type scanExec struct {
	argv []string
	out  string
	code int
}

func (s *scanExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	s.argv = argv
	return s.out, "boom", s.code, nil
}
func (s *scanExec) CaptureInput(ctx context.Context, d string, _ []byte, argv ...string) (string, string, int, error) {
	return s.Capture(ctx, d, argv...)
}
func (s *scanExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

func TestScanImage(t *testing.T) {
	x := &scanExec{out: trivyJSON}
	v, err := ScanImage(context.Background(), x, Scanner{Name: "trivy", Binary: "/t"}, "alpine")
	if err != nil || len(v) != 2 || v[0].Severity != "critical" {
		t.Fatalf("scan: %v %v", v, err)
	}
	if x.argv[0] != "/t" || x.argv[len(x.argv)-1] != "alpine" {
		t.Fatalf("argv %v", x.argv)
	}
	x = &scanExec{code: 2}
	if _, err := ScanImage(context.Background(), x, Scanner{Name: "trivy", Binary: "/t"}, "alpine"); err == nil || !strings.Contains(err.Error(), "exited 2") {
		t.Fatalf("exit code error: %v", err)
	}
}

func TestFindScannersDockerScout(t *testing.T) {
	got := FindScanners(lookOnly("docker", "docker-scout"))
	if len(got) != 1 || got[0].Name != "scout" || got[0].Binary != "/usr/bin/docker" {
		t.Fatalf("scout: %+v", got)
	}
	if s, ok := PickScanner(got, ""); !ok || s.Name != "scout" {
		t.Fatalf("scout pick: %v %v", s, ok)
	}
	// The plugin without the docker CLI cannot run.
	if len(FindScanners(lookOnly("docker-scout"))) != 0 {
		t.Fatal("scout found without docker")
	}
	if !strings.Contains(NoScannerHint, "docker scout") {
		t.Fatalf("hint: %q", NoScannerHint)
	}
}

func TestScoutArgvAndSBOM(t *testing.T) {
	sc := Scanner{Name: "scout", Binary: "/usr/bin/docker"}
	a, err := ScanArgv(sc, "nginx:1.27")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/docker", "scout", "cves", "--format", "gitlab", "--", "nginx:1.27"}) {
		t.Fatalf("scout argv %v %v", a, err)
	}
	a, err = SBOMArgv([]Scanner{sc}, "alpine")
	if err != nil || !reflect.DeepEqual(a, []string{"/usr/bin/docker", "scout", "sbom", "--format", "cyclonedx", "--", "alpine"}) {
		t.Fatalf("scout sbom %v %v", a, err)
	}
}

func TestPluginLookPath(t *testing.T) {
	look := PluginLookPath(lookOnly("docker"), func(p string) bool { return p == "/home/u/.docker/cli-plugins/docker-scout" }, "/home/u")
	if p, err := look("docker-scout"); err != nil || p != "/home/u/.docker/cli-plugins/docker-scout" {
		t.Fatalf("plugin dir: %q %v", p, err)
	}
	if p, err := look("docker"); err != nil || p != "/usr/bin/docker" {
		t.Fatalf("plain look: %q %v", p, err)
	}
	if _, err := look("trivy"); err == nil {
		t.Fatal("trivy found in plugin dirs")
	}
}

const scoutGitlabJSON = `{"vulnerabilities":[
{"id":"x1","name":"CVE-9","severity":"Medium","solution":"Upgrade to 1.1","location":{"dependency":{"package":{"name":"curl"},"version":"1.0"}},"identifiers":[{"value":"CVE-9"}]},
{"id":"x2","name":"CVE-8","severity":"Critical","location":{"dependency":{"package":{"name":"libc"},"version":"2.3"}}}]}`

func TestParseScoutReport(t *testing.T) {
	v, err := ParseVulnReport("scout", []byte(scoutGitlabJSON))
	if err != nil || len(v) != 2 {
		t.Fatalf("scout: %v %v", v, err)
	}
	if v[0] != (Vuln{ID: "CVE-9", Package: "curl", Installed: "1.0", FixedIn: "Upgrade to 1.1", Severity: "medium"}) {
		t.Fatalf("scout row: %+v", v[0])
	}
	if v[1].Severity != "critical" {
		t.Fatalf("severity: %q", v[1].Severity)
	}
}

func TestGenerateSBOM(t *testing.T) {
	x := &scanExec{out: `{"bomFormat":"CycloneDX"}`}
	out, err := GenerateSBOM(context.Background(), x, FindScanners(lookOnly("syft")), "alpine")
	if err != nil || !strings.Contains(out, "CycloneDX") || x.argv[0] != "/usr/bin/syft" {
		t.Fatalf("sbom: %q %v %v", out, err, x.argv)
	}
	if _, err := GenerateSBOM(context.Background(), &scanExec{code: 1}, FindScanners(lookOnly("syft")), "alpine"); err == nil {
		t.Fatal("exit code ignored")
	}
}

func TestFormatVulnReport(t *testing.T) {
	v := []Vuln{{ID: "CVE-1", Package: "openssl", Installed: "3.0", FixedIn: "3.1", Severity: "critical", Title: "bad"}}
	s := FormatVulnReport("alpine", "trivy", v)
	for _, want := range []string{"alpine", "trivy", "1 critical", "CVE-1", "openssl", "3.1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("report misses %q:\n%s", want, s)
		}
	}
	if s := FormatVulnReport("alpine", "grype", nil); !strings.Contains(s, "no known vulnerabilities") {
		t.Fatalf("empty report: %s", s)
	}
}

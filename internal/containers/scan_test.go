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

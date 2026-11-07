package secreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/srv/main.go b/srv/main.go
--- a/srv/main.go
+++ b/srv/main.go
@@ -10,3 +10,6 @@ func run() {
 	keep := 1
+	cmd := exec.Command("sh", "-c", r.URL.Query().Get("q"))
+	cfg := &tls.Config{InsecureSkipVerify: true}
-	old := 2
+	apiKey := "sk-live-123456789"
 	done()
diff --git a/web/a.js b/web/a.js
--- a/web/a.js
+++ b/web/a.js
@@ -0,0 +1,2 @@
+el.innerHTML = name;
+const ok = 1;
`

// CMP-57: the scan finds risky added lines with their new line numbers,
// and never copies a secret's value into a hint.
func TestScanDiff(t *testing.T) {
	hs := ScanDiff(sampleDiff, 0)
	want := map[string]string{
		"srv/main.go:11": "command exec",
		"srv/main.go:12": "TLS check off",
		"srv/main.go:13": "secret in code",
		"web/a.js:1":     "raw HTML",
	}
	got := map[string]string{}
	for _, h := range hs {
		got[h.Where()] = h.Kind
		if strings.Contains(h.Text, "sk-live") {
			t.Fatalf("secret value leaked into a hint: %+v", h)
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("hint %s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("extra hints: %v", got)
	}
	if n := len(ScanDiff(sampleDiff, 2)); n != 2 {
		t.Fatalf("max 2 gave %d hints", n)
	}
}

// The threat model file is read when it is there, and the template has
// the STRIDE sections.
func TestThreatModelFile(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := Load(root); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	tpl := Template("demo")
	for _, s := range []string{"# Threat model: demo", "## Assets", "## Entry points", "## Trust boundaries", "Spoofing", "Elevation of privilege"} {
		if !strings.Contains(tpl, s) {
			t.Fatalf("template has no %q", s)
		}
	}
	if err := os.MkdirAll(filepath.Dir(Path(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(root), []byte("# my model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(root)
	if !ok || err != nil || got != "# my model\n" {
		t.Fatalf("Load = %q %v %v", got, ok, err)
	}
}

// The request asks for a threat model when none is saved, carries the
// hints and the diff, and asks for severity, STRIDE and a fix per finding.
func TestPrompt(t *testing.T) {
	p := Prompt(Input{Branch: true, Base: "origin/main", Diff: "+x\n", Hints: ScanDiff(sampleDiff, 0)})
	for _, s := range []string{"origin/main", "No threat model is saved", "P0", "STRIDE", "fix", "srv/main.go:11 command exec", "```diff\n+x\n```"} {
		if !strings.Contains(p, s) {
			t.Fatalf("prompt has no %q:\n%s", s, p)
		}
	}
	p = Prompt(Input{Model: "# my model", Diff: "+y", Cut: true})
	if !strings.Contains(p, "# my model") || strings.Contains(p, "No threat model is saved") || !strings.Contains(p, "[cut here") {
		t.Fatalf("prompt with model:\n%s", p)
	}
	if !strings.Contains(p, "uncommitted changes") {
		t.Fatalf("changes scope missing:\n%s", p)
	}
}

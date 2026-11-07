package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CMP-93 (local part): the AI and Web audit channels can be saved to a
// private file for a review.

// The saved log has a header, then the AI lines, then the Web lines;
// a secret that slipped into a line is redacted; an empty channel
// says so.
func TestAuditLogText(t *testing.T) {
	at := time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC)
	tok := "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	got := auditLogText(at, "/src/app", []string{"15:00:01  read_file  path=a.go  gate=auto  by=auto  → ok", "15:00:02  http_send  token=" + tok}, nil)
	for _, want := range []string{
		"# termocode audit log\n",
		"# saved 2026-11-07T15:04:05Z\n",
		"# workspace /src/app\n",
		"\n## AI (2 lines)\n15:00:01  read_file  path=a.go  gate=auto  by=auto  → ok\n",
		"\n## Web (0 lines)\n(empty)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, tok) {
		t.Errorf("token not redacted:\n%s", got)
	}
	if strings.Index(got, "## AI") > strings.Index(got, "## Web") {
		t.Error("AI must come first")
	}
}

// Output: Save Audit Log writes a 0600 file in termocode's audit folder
// (0700) and never overwrites one; with both channels empty it only
// says so. The AI never runs it.
func TestAuditSave(t *testing.T) {
	withConfigDir(t)
	dir := t.TempDir()
	defer func(f func() (string, error)) { auditDir = f }(auditDir)
	auditDir = func() (string, error) { return filepath.Join(dir, "audit"), nil }
	m := &Model{output: newOutputStore()}
	m.dispatchPaletteAction("output-save-audit")
	if !strings.Contains(wtLastToast(m), "Nothing to save") {
		t.Errorf("empty toast %q", wtLastToast(m))
	}
	if _, err := os.Stat(filepath.Join(dir, "audit")); err == nil {
		t.Error("the folder was made for nothing")
	}
	m.output.Append(aiAuditChannel, "15:00:01  read_file  path=a.go")
	m.output.Append(webAuditChannel, "15:00:03  fs.write  a.go  by=web:phone  → ok")
	m.auditSave()
	m.auditSave() // the same second: a second file, not an overwrite
	files, _ := filepath.Glob(filepath.Join(dir, "audit", "audit-*.log"))
	if len(files) != 2 {
		t.Fatalf("files %v", files)
	}
	fi, err := os.Stat(files[0])
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v %v", fi, err)
	}
	if di, err := os.Stat(filepath.Join(dir, "audit")); err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v %v", di, err)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), "fs.write  a.go") || !strings.Contains(string(b), "read_file") {
		t.Errorf("content\n%s", b)
	}
	if !strings.Contains(wtLastToast(m), "Audit log saved") {
		t.Errorf("toast %q", wtLastToast(m))
	}
	if p, ok := aiActionPolicyFor("output-save-audit"); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v", p)
	}
}

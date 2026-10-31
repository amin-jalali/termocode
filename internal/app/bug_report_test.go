package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestBugReportBlanksSecrets(t *testing.T) {
	raw := []byte(`{"theme":"dark","api_key":"private-value","nested":{"headers":{"Authorization":"another-value"}},"models":[{"password":"pass-value"}]}`)
	body := buildBugReport(raw, "failure private-value another-value pass-value", "chat private-value")
	for _, secret := range []string{"private-value", "another-value", "pass-value"} {
		if strings.Contains(body, secret) {
			t.Fatalf("secret %q in report", secret)
		}
	}
	if !strings.Contains(body, `"api_key": ""`) || !strings.Contains(body, `"theme": "dark"`) {
		t.Fatal("settings missing or not blanked")
	}
	if got := buildBugReport([]byte(`{"api_key":"broken`), "", ""); strings.Contains(got, "broken") {
		t.Fatal("malformed settings leaked")
	}
}

func TestBugReportPreviewBeforeSave(t *testing.T) {
	withConfigDir(t)
	root := t.TempDir()
	t.Chdir(root)
	m := &Model{w: 100, h: 40}
	m.ai = &aiState{conv: ai.Conversation{Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "optional chat text"}}}}
	m.openBugReport(false)
	if !m.confirmOpen || focusedCardID(t, *m) != "cancel" {
		t.Fatal("not Cancel first")
	}
	if strings.Contains(m.extras.bugReport.body, "optional chat text") {
		t.Fatal("chat included by default")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("wrote before preview")
	}
	m.answerBugReport("chat")
	if !strings.Contains(m.extras.bugReport.body, "optional chat text") {
		t.Fatal("chat option ignored")
	}
	body := m.extras.bugReport.body
	m.answerBugReport("save")
	paths, _ := filepath.Glob(filepath.Join(root, "termocode-report-*.md"))
	if len(paths) != 1 {
		t.Fatalf("saved paths %v", paths)
	}
	got, err := os.ReadFile(paths[0])
	if err != nil || string(got) != body {
		t.Fatal("saved report differs from preview")
	}
	info, _ := os.Stat(paths[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("report permissions")
	}
	m.openBugReport(false)
	m.answerBugReport("cancel")
	if m.extras.bugReport != nil {
		t.Fatal("cancel retained report")
	}
}
